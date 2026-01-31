package relational

// Postgres-backed object store (library-only).
//
// This package provides a production-grade, defensive PostgreSQL persistence layer intended
// for Chartly Storage Service evolution beyond the current in-memory v0 store.
//
// IMPORTANT:
// - Standard library ONLY: uses database/sql (no driver import here). A postgres driver must be
//   registered elsewhere at runtime (e.g., via a blank import in an app module).
// - This file does NOT implement HTTP handlers, filesystem writes, or network calls.
// - Multi-tenant safety: all operations are scoped by explicit tenant_id + object_key.
// - Determinism:
//     * No time.Now usage; clock is caller-provided or fixed to Unix(0,0) by default.
//     * Metadata JSON is serialized deterministically (sorted keys).
//     * Table name is validated to avoid injection risks.
//
// Schema (created by EnsureSchema):
//   chartly_objects:
//     tenant_id      TEXT       NOT NULL
//     object_key     TEXT       NOT NULL
//     content_type   TEXT       NOT NULL
//     sha256         TEXT       NOT NULL  -- hex
//     bytes          BIGINT     NOT NULL
//     headers_json   TEXT       NOT NULL  -- canonical JSON (stable ordering)
//     body           BYTEA      NOT NULL
//     created_at     TIMESTAMPTZ NOT NULL
//     updated_at     TIMESTAMPTZ NOT NULL
//     PRIMARY KEY (tenant_id, object_key)
//
// Notes:
// - headers_json is stored as TEXT to avoid depending on jsonb features; it contains canonical JSON.
// - created_at/updated_at timestamps are supplied by Clock (default fixed epoch for determinism).
// - This store is suitable for durable persistence behind the same semantics as the v0 HTTP API.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrInvalidInput indicates caller passed invalid tenant/object/meta/data.
	ErrInvalidInput = errors.New("invalid input")
	// ErrNotFound indicates missing object.
	ErrNotFound = errors.New("not found")
	// ErrConflict indicates a write conflict (optional when If-Match semantics exist upstream).
	ErrConflict = errors.New("conflict")
	// ErrTooLarge indicates a size limit breach.
	ErrTooLarge = errors.New("too large")
	// ErrDB indicates database operation failure.
	ErrDB = errors.New("db error")
)

type Clock func() time.Time

type Options struct {
	// MaxObjectBytes limits stored object size. If <=0, no explicit limit (caller may enforce).
	MaxObjectBytes int64
	// Clock supplies created_at/updated_at timestamps. If nil, uses time.Unix(0,0).UTC().
	Clock Clock
	// TableName allows overriding the table name (default "chartly_objects").
	TableName string
}

type Object struct {
	TenantID    string
	ObjectKey   string
	ContentType string
	Bytes       int64
	SHA256      string // hex
	Headers     map[string]string
	Body        []byte
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PutResult provides deterministic write metadata.
type PutResult struct {
	TenantID    string
	ObjectKey   string
	ContentType string
	Bytes       int64
	SHA256      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Inserted    bool // true if new row, false if replaced (best-effort; see notes)
}

type Stats struct {
	Objects int64
	Bytes   int64
}

// PostgresStore is a durable object store backed by PostgreSQL.
type PostgresStore struct {
	db    *sql.DB
	opts  Options
	table string
}

func NewPostgresStore(db *sql.DB, opts Options) (*PostgresStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: db is nil", ErrInvalidInput)
	}

	table := strings.TrimSpace(opts.TableName)
	if table == "" {
		table = "chartly_objects"
	}
	if err := validateTableName(table); err != nil {
		return nil, fmt.Errorf("%w: invalid table name", ErrInvalidInput)
	}

	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Unix(0, 0).UTC() }
	}

	return &PostgresStore{db: db, opts: opts, table: table}, nil
}

// EnsureSchema creates the backing table if it does not exist.
// This is intentionally minimal and idempotent.
func (s *PostgresStore) EnsureSchema(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	q := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  tenant_id    TEXT NOT NULL,
  object_key   TEXT NOT NULL,
  content_type TEXT NOT NULL,
  sha256       TEXT NOT NULL,
  bytes        BIGINT NOT NULL,
  headers_json TEXT NOT NULL,
  body         BYTEA NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL,
  updated_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, object_key)
);`, s.table)

	if _, err := s.db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("%w: ensure schema: %v", ErrDB, err)
	}

	return nil
}

// Put stores or replaces an object.
//
// Behavior:
// - If the object exists, it is replaced (body + headers + content_type + sha256 + bytes + updated_at).
// - If not, it is inserted.
// - SHA256 is computed over the body bytes deterministically.
// - headers are stored as canonical JSON (sorted keys) for stable representation.
func (s *PostgresStore) Put(ctx context.Context, tenantID, objectKey, contentType string, body []byte, headers map[string]string) (PutResult, error) {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)
	contentType = strings.TrimSpace(contentType)

	if tenantID == "" || objectKey == "" || contentType == "" {
		return PutResult{}, fmt.Errorf("%w: tenantID/objectKey/contentType required", ErrInvalidInput)
	}

	if body == nil {
		body = []byte{}
	}

	if s.opts.MaxObjectBytes > 0 && int64(len(body)) > s.opts.MaxObjectBytes {
		return PutResult{}, fmt.Errorf("%w: object exceeds max bytes", ErrTooLarge)
	}

	now := s.opts.Clock()

	sum := sha256.Sum256(body)
	shaHex := hex.EncodeToString(sum[:])

	hJSON, err := canonicalHeaderJSON(headers)
	if err != nil {
		return PutResult{}, err
	}

	q := fmt.Sprintf(`
INSERT INTO %s
  (tenant_id, object_key, content_type, sha256, bytes, headers_json, body, created_at, updated_at)
VALUES
  ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (tenant_id, object_key) DO UPDATE SET
  content_type = EXCLUDED.content_type,
  sha256       = EXCLUDED.sha256,
  bytes        = EXCLUDED.bytes,
  headers_json = EXCLUDED.headers_json,
  body         = EXCLUDED.body,
  updated_at   = EXCLUDED.updated_at
RETURNING created_at, updated_at;`, s.table)

	var createdAt, updatedAt time.Time
	if err := s.db.QueryRowContext(ctx, q, tenantID, objectKey, contentType, shaHex, int64(len(body)), hJSON, body, now, now).Scan(&createdAt, &updatedAt); err != nil {
		return PutResult{}, fmt.Errorf("%w: put: %v", ErrDB, err)
	}

	res := PutResult{
		TenantID:    tenantID,
		ObjectKey:   objectKey,
		ContentType: contentType,
		Bytes:       int64(len(body)),
		SHA256:      shaHex,
		CreatedAt:   createdAt.UTC(),
		UpdatedAt:   updatedAt.UTC(),
		Inserted:    false, // deterministic default; accurate detection requires extra query/tx
	}

	return res, nil
}

// Get returns the stored object and metadata.
func (s *PostgresStore) Get(ctx context.Context, tenantID, objectKey string) (Object, error) {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)

	if tenantID == "" || objectKey == "" {
		return Object{}, fmt.Errorf("%w: tenantID/objectKey required", ErrInvalidInput)
	}

	q := fmt.Sprintf(`
SELECT content_type, sha256, bytes, headers_json, body, created_at, updated_at
FROM %s
WHERE tenant_id = $1 AND object_key = $2;`, s.table)

	var (
		ct        string
		shaHex    string
		nBytes    int64
		hJSON     string
		body      []byte
		createdAt time.Time
		updatedAt time.Time
	)

	err := s.db.QueryRowContext(ctx, q, tenantID, objectKey).Scan(&ct, &shaHex, &nBytes, &hJSON, &body, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Object{}, fmt.Errorf("%w: %s", ErrNotFound, objectKey)
		}
		return Object{}, fmt.Errorf("%w: get: %v", ErrDB, err)
	}

	hdrs, err := decodeHeaderJSON(hJSON)
	if err != nil {
		return Object{}, err
	}

	return Object{
		TenantID:    tenantID,
		ObjectKey:   objectKey,
		ContentType: ct,
		Bytes:       nBytes,
		SHA256:      shaHex,
		Headers:     hdrs,
		Body:        body,
		CreatedAt:   createdAt.UTC(),
		UpdatedAt:   updatedAt.UTC(),
	}, nil
}

// Head returns object metadata without body.
func (s *PostgresStore) Head(ctx context.Context, tenantID, objectKey string) (Object, error) {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)

	if tenantID == "" || objectKey == "" {
		return Object{}, fmt.Errorf("%w: tenantID/objectKey required", ErrInvalidInput)
	}

	q := fmt.Sprintf(`
SELECT content_type, sha256, bytes, headers_json, created_at, updated_at
FROM %s
WHERE tenant_id = $1 AND object_key = $2;`, s.table)

	var (
		ct        string
		shaHex    string
		nBytes    int64
		hJSON     string
		createdAt time.Time
		updatedAt time.Time
	)

	err := s.db.QueryRowContext(ctx, q, tenantID, objectKey).Scan(&ct, &shaHex, &nBytes, &hJSON, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Object{}, fmt.Errorf("%w: %s", ErrNotFound, objectKey)
		}
		return Object{}, fmt.Errorf("%w: head: %v", ErrDB, err)
	}

	hdrs, err := decodeHeaderJSON(hJSON)
	if err != nil {
		return Object{}, err
	}

	return Object{
		TenantID:    tenantID,
		ObjectKey:   objectKey,
		ContentType: ct,
		Bytes:       nBytes,
		SHA256:      shaHex,
		Headers:     hdrs,
		Body:        nil,
		CreatedAt:   createdAt.UTC(),
		UpdatedAt:   updatedAt.UTC(),
	}, nil
}

// Delete removes an object. If not found, returns ErrNotFound.
func (s *PostgresStore) Delete(ctx context.Context, tenantID, objectKey string) error {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)

	if tenantID == "" || objectKey == "" {
		return fmt.Errorf("%w: tenantID/objectKey required", ErrInvalidInput)
	}

	q := fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1 AND object_key = $2;`, s.table)
	res, err := s.db.ExecContext(ctx, q, tenantID, objectKey)
	if err != nil {
		return fmt.Errorf("%w: delete: %v", ErrDB, err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, objectKey)
	}

	return nil
}

// Stat returns aggregate counts for a tenant (deterministic).
func (s *PostgresStore) Stat(ctx context.Context, tenantID string) (Stats, error) {
	tenantID = norm(tenantID)
	if tenantID == "" {
		return Stats{}, fmt.Errorf("%w: tenantID required", ErrInvalidInput)
	}

	q := fmt.Sprintf(`
SELECT COALESCE(COUNT(*),0) AS objects, COALESCE(SUM(bytes),0) AS bytes
FROM %s
WHERE tenant_id = $1;`, s.table)

	var st Stats
	if err := s.db.QueryRowContext(ctx, q, tenantID).Scan(&st.Objects, &st.Bytes); err != nil {
		return Stats{}, fmt.Errorf("%w: stat: %v", ErrDB, err)
	}

	return st, nil
}

// VerifyBodyHash recomputes SHA256 over body and ensures it matches stored SHA.
// Useful for periodic integrity checks.
func (s *PostgresStore) VerifyBodyHash(ctx context.Context, tenantID, objectKey string) error {
	obj, err := s.Get(ctx, tenantID, objectKey)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(obj.Body)
	shaHex := hex.EncodeToString(sum[:])
	if shaHex != obj.SHA256 {
		return fmt.Errorf("%w: stored sha256 mismatch", ErrConflict)
	}

	return nil
}

// -----------------------------------------------------------------------------
// Canonical metadata encoding (deterministic)
// -----------------------------------------------------------------------------

func canonicalHeaderJSON(h map[string]string) (string, error) {
	m := make(map[string]string)
	for k, v := range h {
		kk := normKey(k)
		if kk == "" {
			continue
		}
		m[kk] = normVal(v)
	}

	// Deterministic ordering: encode as an array of kv pairs.
	type kv struct {
		K string `json:"k"`
		V string `json:"v"`
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	arr := make([]kv, 0, len(keys))
	for _, k := range keys {
		arr = append(arr, kv{K: k, V: m[k]})
	}

	b, err := json.Marshal(arr)
	if err != nil {
		return "", fmt.Errorf("%w: headers json: %v", ErrDB, err)
	}

	return string(b), nil
}

func decodeHeaderJSON(s string) (map[string]string, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if s == "" {
		return map[string]string{}, nil
	}

	type kv struct {
		K string `json:"k"`
		V string `json:"v"`
	}

	var arr []kv
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return nil, fmt.Errorf("%w: headers decode: %v", ErrDB, err)
	}

	out := make(map[string]string, len(arr))
	for _, it := range arr {
		k := normKey(it.K)
		if k == "" {
			continue
		}
		out[k] = normVal(it.V)
	}

	return out, nil
}

// -----------------------------------------------------------------------------
// String normalization (defensive; deterministic)
// -----------------------------------------------------------------------------

func norm(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

func normKey(s string) string {
	s = norm(s)
	s = strings.ToLower(s)
	return s
}

func normVal(s string) string {
	return norm(s)
}

// validateTableName is a conservative check to prevent SQL injection when using fmt.Sprintf.
// Allows only letters, digits, underscore, and dot. Must start with a letter or underscore.
func validateTableName(name string) error {
	if name == "" {
		return ErrInvalidInput
	}
	for i, r := range name {
		if i == 0 {
			if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
				return ErrInvalidInput
			}
			continue
		}
		if r == '.' || r == '_' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}
