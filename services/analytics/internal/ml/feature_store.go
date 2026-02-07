package ml

import (
	"crypto/sha256"

	"encoding/base64"

	"encoding/hex"

	"encoding/json"

	"errors"

	"fmt"

	"sort"

	"strconv"

	"strings"

	"sync"

	"time"
)

var (
	ErrFeatureExists  = errors.New("feature exists")
	ErrFeatureMissing = errors.New("feature missing")
	ErrInvalidFeature = errors.New("invalid feature")
	ErrInvalidWrite   = errors.New("invalid write")
	ErrInvalidQuery   = errors.New("invalid query")
	ErrStoreClosed    = errors.New("feature store closed")
)

type FeatureType string

const (
	FeatureNumber FeatureType = "number"

	FeatureString FeatureType = "string"

	FeatureBool FeatureType = "bool"

	FeatureJSON FeatureType = "json"
)

type Granularity string

const (
	GranularityEvent Granularity = "event"

	GranularityMinute Granularity = "minute"

	GranularityHour Granularity = "hour"

	GranularityDay Granularity = "day"
)

type FeatureKey struct {
	TenantID string `json:"tenant_id"`

	Namespace string `json:"namespace"`

	Name string `json:"name"`

	EntityType string `json:"entity_type"`

	EntityID string `json:"entity_id,omitempty"`

	Granularity Granularity `json:"granularity"`
}
type FeatureDef struct {
	Key FeatureKey `json:"key"`

	Type FeatureType `json:"type"`

	Description string `json:"description,omitempty"`

	Tags []string `json:"tags,omitempty"`

	TTLSeconds int64 `json:"ttl_seconds,omitempty"` // 0 = no TTL

	Meta map[string]string `json:"meta,omitempty"`
}
type FeaturePoint struct {
	Key FeatureKey `json:"key"`

	TS string `json:"ts"`

	Value any `json:"value"`

	Meta map[string]string `json:"meta,omitempty"`
}
type Query struct {
	TenantID string `json:"tenant_id"`

	Namespace string `json:"namespace"`

	Name string `json:"name"`

	EntityType string `json:"entity_type"`

	EntityID string `json:"entity_id,omitempty"`

	Granularity Granularity `json:"granularity"`

	Start string `json:"start,omitempty"` // inclusive

	End string `json:"end,omitempty"` // exclusive

	Limit int `json:"limit,omitempty"`

	Asc bool `json:"asc,omitempty"`

	IncludeMeta bool `json:"include_meta,omitempty"`

	NextCursor string `json:"next_cursor,omitempty"` // input cursor
}
type Result struct {
	Points []FeaturePoint `json:"points"`

	Truncated bool `json:"truncated"`

	NextCursor string `json:"next_cursor,omitempty"`
}
type FeatureStore struct {
	mu sync.RWMutex

	closed bool

	defs map[string]FeatureDef // key: defKeyString

	points map[string][]storedPoint // key: seriesKeyString (tenant/ns/name/entityType/entityID/granularity)
}
type storedPoint struct {
	ts time.Time

	tsS string

	val any

	meta map[string]string
}

func NewFeatureStore() *FeatureStore {

	return &FeatureStore{

		defs: make(map[string]FeatureDef),

		points: make(map[string][]storedPoint),
	}
}
func (fs *FeatureStore) Close() {

	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.closed = true
}
func (fs *FeatureStore) Define(def FeatureDef) error {

	def = normalizeDef(def)
	if err := validateDef(def); err != nil {

		return err

	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.closed {

		return ErrStoreClosed

	}
	k := defKeyString(def.Key)
	if _, ok := fs.defs[k]; ok {

		return ErrFeatureExists

	}
	fs.defs[k] = def

	return nil
}
func (fs *FeatureStore) GetDefinition(key FeatureKey) (FeatureDef, bool) {

	key = normalizeKey(key)
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	def, ok := fs.defs[defKeyString(key)]

	return def, ok
}
func (fs *FeatureStore) ListDefinitions(tenantID string) []FeatureDef {

	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {

		return nil

	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]FeatureDef, 0, len(fs.defs))
	for _, d := range fs.defs {

		if d.Key.TenantID == tenantID {

			out = append(out, d)

		}

	}
	sort.Slice(out, func(i, j int) bool {

		return defKeyString(out[i].Key) < defKeyString(out[j].Key)

	})
	return out
}
func (fs *FeatureStore) Put(point FeaturePoint) error {

	point = normalizePoint(point)
	if err := validatePointBasics(point); err != nil {

		return err

	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.closed {

		return ErrStoreClosed

	}

	// Definition must exist

	def, ok := fs.defs[defKeyString(point.Key)]

	if !ok {

		return ErrFeatureMissing

	}

	// Validate value type

	if err := validateValueAgainstType(def.Type, point.Value); err != nil {

		return err

	}

	// Parse ts

	ts, err := parseTS(point.TS)
	if err != nil {

		return err

	}
	seriesK := seriesKeyString(point.Key)
	fs.evictExpiredLocked(seriesK, def, nowFromMeta(point.Meta))
	sp := storedPoint{ts: ts, tsS: point.TS, val: point.Value, meta: normalizeMeta(point.Meta)}
	fs.points[seriesK] = insertPointSorted(fs.points[seriesK], sp)
	return nil
}
func (fs *FeatureStore) PutBatch(points []FeaturePoint) (ok int, failed int, err error) {

	if points == nil {

		return 0, 0, nil

	}
	for _, p := range points {

		if e := fs.Put(p); e != nil {

			failed++

			err = e // keep last error (deterministic; no aggregation)
			// continue

		}
		ok++

	}
	return ok, failed, err
}
func (fs *FeatureStore) Query(q Query) (Result, error) {

	q = normalizeQuery(q)
	if err := validateQuery(q); err != nil {

		return Result{}, err

	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.closed {

		return Result{}, ErrStoreClosed

	}

	// Find def (needed for TTL/type awareness)
	key := FeatureKey{

		TenantID: q.TenantID,

		Namespace: q.Namespace,

		Name: q.Name,

		EntityType: q.EntityType,

		EntityID: q.EntityID,

		Granularity: q.Granularity,
	}
	def, ok := fs.defs[defKeyString(key)]

	if !ok {

		return Result{}, ErrFeatureMissing

	}
	seriesK := seriesKeyString(key)

	// TTL eviction uses "now" (optional)
	now := nowFromCursorOrMeta(q.NextCursor, q)
	fs.evictExpiredLocked(seriesK, def, now)
	pts := fs.points[seriesK]

	if len(pts) == 0 {

		return Result{Points: nil}, nil

	}
	startTS, _ := parseTSIfProvided(q.Start)
	endTS, _ := parseTSIfProvided(q.End)

	// Determine start index using cursor or start time

	startIdx := 0

	if strings.TrimSpace(q.NextCursor) != "" {

		cur, ok := decodeCursor(q.NextCursor)
		if ok && cur.Hash == queryHash(q) {

			// forward-only for Asc=true

			startIdx = clampInt(cur.LastIdx+1, 0, len(pts))
			if !cur.LastTS.IsZero() {

				// ensure we don't move backwards if underlying series shifted

				startIdx = maxInt(startIdx, upperBoundTS(pts, cur.LastTS))

			}

		}

	}

	// Apply start time

	if !startTS.IsZero() {

		startIdx = maxInt(startIdx, lowerBoundTS(pts, startTS))

	}

	// Apply end time by calculating end index (exclusive)
	endIdx := len(pts)
	if !endTS.IsZero() {

		endIdx = minInt(endIdx, lowerBoundTS(pts, endTS))

	}
	if startIdx > endIdx {

		startIdx = endIdx

	}
	limit := q.Limit

	if limit <= 0 {

		limit = 1000

	}
	if limit > 50000 {

		limit = 50000

	}
	res := Result{Points: make([]FeaturePoint, 0, minInt(limit, endIdx-startIdx))}
	if q.Asc {

		n := 0

		for i := startIdx; i < endIdx && n < limit; i++ {

			res.Points = append(res.Points, materializePoint(key, pts[i], q.IncludeMeta))
			n++

		}
		if startIdx+n < endIdx {

			res.Truncated = true

			last := startIdx + n - 1

			if last >= 0 && last < len(pts) {

				res.NextCursor = encodeCursor(cursor{

					LastTS: pts[last].ts,

					LastIdx: last,

					Hash: queryHash(q),
				})

			}

		}

	} else {

		// Desc: deterministic reverse slicing; cursor forward-only not supported => require empty cursor

		if strings.TrimSpace(q.NextCursor) != "" {

			return Result{}, fmt.Errorf("%w: cursor not supported for desc queries", ErrInvalidQuery)

		}
		n := 0

		for i := endIdx - 1; i >= startIdx && n < limit; i-- {

			res.Points = append(res.Points, materializePoint(key, pts[i], q.IncludeMeta))
			n++

		}
		if endIdx-n > startIdx {

			res.Truncated = true

			// No cursor for desc to keep semantics simple/deterministic

		}

	}
	return res, nil
}

// ExportTenant exports a deterministic JSON payload containing definitions and all points for tenant.
// TTL is enforced only if a "now" is included in the per-point Meta["now"] during Put; otherwise TTL is not enforced here.
func (fs *FeatureStore) ExportTenant(tenantID string) ([]byte, error) {

	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {

		return nil, fmt.Errorf("%w: tenant_id required", ErrInvalidQuery)

	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.closed {

		return nil, ErrStoreClosed

	}
	defs := make([]FeatureDef, 0)
	for _, d := range fs.defs {

		if d.Key.TenantID == tenantID {

			defs = append(defs, d)

		}

	}
	sort.Slice(defs, func(i, j int) bool { return defKeyString(defs[i].Key) < defKeyString(defs[j].Key) })
	type exportedSeries struct {
		Key FeatureKey `json:"key"`

		Points []FeaturePoint `json:"points"`
	}
	seriesKeys := make([]string, 0)
	for sk := range fs.points {

		if strings.HasPrefix(sk, tenantID+"|") {

			seriesKeys = append(seriesKeys, sk)

		}

	}
	sort.Strings(seriesKeys)
	series := make([]exportedSeries, 0, len(seriesKeys))
	for _, sk := range seriesKeys {

		key := parseSeriesKey(sk)
		def, ok := fs.defs[defKeyString(key)]

		if ok {

			// Optional eviction based on any stored "now" (best-effort)
			fs.evictExpiredLocked(sk, def, time.Time{})

		}
		stored := fs.points[sk]

		points := make([]FeaturePoint, 0, len(stored))
		for _, sp := range stored {

			points = append(points, materializePoint(key, sp, true))

		}
		series = append(series, exportedSeries{Key: key, Points: points})

	}
	payload := struct {
		TenantID string `json:"tenant_id"`

		Defs []FeatureDef `json:"defs"`

		Series []exportedSeries `json:"series"`
	}{

		TenantID: tenantID,

		Defs: defs,

		Series: series,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {

		return nil, fmt.Errorf("%w: %v", ErrInvalidQuery, err)

	}
	return b, nil
}

// ImportTenant validates and merges tenant payload. Conflicts are handled deterministically:
// - Definitions: if key exists and differs => keep existing, ignore incoming.
// - Points: inserted by ts (dedupe exact timestamp by keeping existing, ignoring incoming).
func (fs *FeatureStore) ImportTenant(tenantID string, payload []byte) error {

	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {

		return fmt.Errorf("%w: tenant_id required", ErrInvalidQuery)

	}
	if len(payload) == 0 {

		return fmt.Errorf("%w: empty payload", ErrInvalidQuery)

	}
	var doc struct {
		TenantID string `json:"tenant_id"`

		Defs []FeatureDef `json:"defs"`

		Series []struct {
			Key FeatureKey `json:"key"`

			Points []FeaturePoint `json:"points"`
		} `json:"series"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {

		return fmt.Errorf("%w: invalid json", ErrInvalidQuery)

	}
	if strings.TrimSpace(doc.TenantID) != "" && strings.TrimSpace(doc.TenantID) != tenantID {

		return fmt.Errorf("%w: tenant mismatch", ErrInvalidQuery)

	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.closed {

		return ErrStoreClosed

	}

	// defs

	for _, d := range doc.Defs {

		d = normalizeDef(d)
		if d.Key.TenantID == "" {

			d.Key.TenantID = tenantID

		}
		if d.Key.TenantID != tenantID {

			continue

		}
		if err := validateDef(d); err != nil {

			continue

		}
		k := defKeyString(d.Key)
		if existing, ok := fs.defs[k]; ok {

			// deterministic conflict: keep existing if differs

			if !defsEqual(existing, d) {

				continue

			}

		} else {

			fs.defs[k] = d

		}

	}

	// points

	for _, s := range doc.Series {

		k := normalizeKey(s.Key)
		if k.TenantID == "" {

			k.TenantID = tenantID

		}
		if k.TenantID != tenantID {

			continue

		}
		def, ok := fs.defs[defKeyString(k)]

		if !ok {

			continue

		}
		seriesK := seriesKeyString(k)
		for _, p := range s.Points {

			p = normalizePoint(p)
			p.Key = k

			if p.TS == "" {

				continue

			}
			ts, err := parseTS(p.TS)
			if err != nil {

				continue

			}
			if err := validateValueAgainstType(def.Type, p.Value); err != nil {

				continue

			}
			sp := storedPoint{ts: ts, tsS: p.TS, val: p.Value, meta: normalizeMeta(p.Meta)}
			fs.points[seriesK] = insertPointSortedDedupe(fs.points[seriesK], sp)

		}

	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// TTL eviction (best-effort, deterministic)
////////////////////////////////////////////////////////////////////////////////

// TTL rules are enforced only when "now" is provided (Query via cursor/meta; Put via point.Meta["now"]).
// This avoids calling time.Now and keeps behavior deterministic relative to caller-provided time.
func (fs *FeatureStore) evictExpiredLocked(seriesK string, def FeatureDef, now time.Time) {

	if def.TTLSeconds <= 0 {

		return

	}
	if now.IsZero() {

		return

	}
	pts := fs.points[seriesK]

	if len(pts) == 0 {

		return

	}
	cutoff := now.Add(-time.Duration(def.TTLSeconds) * time.Second)

	// Points are sorted asc by ts.

	idx := lowerBoundTS(pts, cutoff)
	if idx <= 0 {

		return

	}

	// Evict older than cutoff

	fs.points[seriesK] = append([]storedPoint(nil), pts[idx:]...)
}
func nowFromMeta(meta map[string]string) time.Time {

	if meta == nil {

		return time.Time{}

	}
	s := strings.TrimSpace(meta["now"])
	if s == "" {

		return time.Time{}

	}
	t, err := parseTS(s)
	if err != nil {

		return time.Time{}

	}
	return t
}
func nowFromCursorOrMeta(cursorStr string, q Query) time.Time {

	// Prefer explicit now in query via cursor decode payload field if present; else allow q.NextCursor to carry it.
	// Also allow q.NextCursor empty and caller to pass "now" via q.NextCursor? not possible; so only cursor.

	if strings.TrimSpace(cursorStr) == "" {

		return time.Time{}

	}
	c, ok := decodeCursor(cursorStr)
	if !ok {

		return time.Time{}

	}
	return c.Now
}

////////////////////////////////////////////////////////////////////////////////
// Cursor
////////////////////////////////////////////////////////////////////////////////

type cursor struct {
	LastTS time.Time `json:"last_ts"`

	LastIdx int `json:"last_idx"`

	Hash string `json:"hash"`

	Now time.Time `json:"now,omitempty"` // optional "now" for TTL enforcement
}

func encodeCursor(c cursor) string {

	// deterministic JSON (fields fixed)
	b, err := json.Marshal(c)
	if err != nil {

		return ""

	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeCursor(s string) (cursor, bool) {

	s = strings.TrimSpace(s)
	if s == "" {

		return cursor{}, false

	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {

		return cursor{}, false

	}
	var c cursor

	if err := json.Unmarshal(b, &c); err != nil {

		return cursor{}, false

	}
	if c.LastIdx < 0 {

		c.LastIdx = 0

	}
	return c, true
}
func queryHash(q Query) string {

	// Hash query parameters excluding NextCursor for safety

	type payload struct {
		TenantID string `json:"tenant_id"`

		Namespace string `json:"namespace"`

		Name string `json:"name"`

		EntityType string `json:"entity_type"`

		EntityID string `json:"entity_id"`

		Granularity string `json:"granularity"`

		Start string `json:"start"`

		End string `json:"end"`

		Limit int `json:"limit"`

		Asc bool `json:"asc"`

		IncludeMeta bool `json:"include_meta"`
	}
	p := payload{

		TenantID: q.TenantID,

		Namespace: q.Namespace,

		Name: q.Name,

		EntityType: q.EntityType,

		EntityID: q.EntityID,

		Granularity: string(q.Granularity),

		Start: q.Start,

		End: q.End,

		Limit: q.Limit,

		Asc: q.Asc,

		IncludeMeta: q.IncludeMeta,
	}
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex24(sum[:])
}
func hex24(b []byte) string {

	return hex.EncodeToString(b)[:24]
}

////////////////////////////////////////////////////////////////////////////////
// Validation + normalization
////////////////////////////////////////////////////////////////////////////////

func normalizeKey(k FeatureKey) FeatureKey {

	k.TenantID = strings.TrimSpace(k.TenantID)
	k.Namespace = strings.TrimSpace(k.Namespace)
	k.Name = strings.TrimSpace(k.Name)
	k.EntityType = strings.TrimSpace(k.EntityType)
	k.EntityID = strings.TrimSpace(k.EntityID)
	k.Granularity = Granularity(strings.TrimSpace(string(k.Granularity)))
	return k
}
func normalizeDef(d FeatureDef) FeatureDef {

	d.Key = normalizeKey(d.Key)
	d.Type = FeatureType(strings.TrimSpace(string(d.Type)))
	d.Description = strings.TrimSpace(d.Description)
	d.Tags = normalizeTags(d.Tags)
	d.Meta = normalizeMeta(d.Meta)
	if d.TTLSeconds < 0 {

		d.TTLSeconds = 0

	}
	return d
}
func normalizePoint(p FeaturePoint) FeaturePoint {

	p.Key = normalizeKey(p.Key)
	p.TS = strings.TrimSpace(p.TS)
	p.Meta = normalizeMeta(p.Meta)
	return p
}
func normalizeQuery(q Query) Query {

	q.TenantID = strings.TrimSpace(q.TenantID)
	q.Namespace = strings.TrimSpace(q.Namespace)
	q.Name = strings.TrimSpace(q.Name)
	q.EntityType = strings.TrimSpace(q.EntityType)
	q.EntityID = strings.TrimSpace(q.EntityID)
	q.Granularity = Granularity(strings.TrimSpace(string(q.Granularity)))
	q.Start = strings.TrimSpace(q.Start)
	q.End = strings.TrimSpace(q.End)
	if q.Limit == 0 {

		q.Limit = 1000

	}

	// default Asc = true

	if q.Asc == false {

		// keep false

	} else {

		q.Asc = true

	}
	return q
}
func validateDef(d FeatureDef) error {

	if d.Key.TenantID == "" {

		return fmt.Errorf("%w: tenant_id required", ErrInvalidFeature)

	}
	if d.Key.Namespace == "" || d.Key.Name == "" || d.Key.EntityType == "" {

		return fmt.Errorf("%w: namespace/name/entity_type required", ErrInvalidFeature)

	}
	if d.Key.Granularity == "" {

		return fmt.Errorf("%w: granularity required", ErrInvalidFeature)

	}
	switch d.Type {

	case FeatureNumber, FeatureString, FeatureBool, FeatureJSON:

	default:

		return fmt.Errorf("%w: unknown type %q", ErrInvalidFeature, string(d.Type))

	}
	return nil
}
func validatePointBasics(p FeaturePoint) error {

	if p.Key.TenantID == "" {

		return fmt.Errorf("%w: tenant_id required", ErrInvalidWrite)

	}
	if p.Key.Namespace == "" || p.Key.Name == "" || p.Key.EntityType == "" {

		return fmt.Errorf("%w: namespace/name/entity_type required", ErrInvalidWrite)

	}
	if p.Key.Granularity == "" {

		return fmt.Errorf("%w: granularity required", ErrInvalidWrite)

	}
	if p.TS == "" {

		return fmt.Errorf("%w: ts required", ErrInvalidWrite)

	}
	if _, err := parseTS(p.TS); err != nil {

		return err

	}
	return nil
}
func validateQuery(q Query) error {

	if q.TenantID == "" {

		return fmt.Errorf("%w: tenant_id required", ErrInvalidQuery)

	}
	if q.Namespace == "" || q.Name == "" || q.EntityType == "" {

		return fmt.Errorf("%w: namespace/name/entity_type required", ErrInvalidQuery)

	}
	if q.Granularity == "" {

		return fmt.Errorf("%w: granularity required", ErrInvalidQuery)

	}
	if q.Start != "" {

		if _, err := parseTS(q.Start); err != nil {

			return err

		}

	}
	if q.End != "" {

		if _, err := parseTS(q.End); err != nil {

			return err

		}

	}
	if q.Start != "" && q.End != "" {

		s, _ := parseTS(q.Start)
		e, _ := parseTS(q.End)
		if !e.After(s) {

			return fmt.Errorf("%w: end must be after start", ErrInvalidQuery)

		}

	}
	if q.Limit < 0 {

		return fmt.Errorf("%w: limit must be >= 0", ErrInvalidQuery)

	}
	if q.Limit > 50000 {

		return fmt.Errorf("%w: limit max 50000", ErrInvalidQuery)

	}
	return nil
}
func validateValueAgainstType(t FeatureType, v any) error {

	switch t {

	case FeatureNumber:

		switch v.(type) {

		case float64, float32, int, int32, int64, uint, uint32, uint64:

			return nil

		default:

			// json numbers decode as float64

			return fmt.Errorf("%w: value must be number", ErrInvalidWrite)

		}
	case FeatureString:

		if _, ok := v.(string); !ok {

			return fmt.Errorf("%w: value must be string", ErrInvalidWrite)

		}
		return nil

	case FeatureBool:

		if _, ok := v.(bool); !ok {

			return fmt.Errorf("%w: value must be bool", ErrInvalidWrite)

		}
		return nil

	case FeatureJSON:

		// Accept any JSON-marshalable value; do a marshal test for determinism

		_, err := json.Marshal(v)
		if err != nil {

			return fmt.Errorf("%w: value must be json-marshalable", ErrInvalidWrite)

		}
		return nil

	default:

		return fmt.Errorf("%w: unknown feature type", ErrInvalidWrite)

	}
}
func normalizeMeta(m map[string]string) map[string]string {

	if m == nil {

		return nil

	}
	keys := make([]string, 0, len(m))
	for k := range m {

		k2 := strings.TrimSpace(k)
		if k2 == "" {

			continue

		}
		keys = append(keys, k2)

	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, k := range keys {

		out[k] = strings.TrimSpace(m[k])

	}
	if len(out) == 0 {

		return nil

	}
	return out
}

////////////////////////////////////////////////////////////////////////////////
// Keys + series ops
////////////////////////////////////////////////////////////////////////////////

func defKeyString(k FeatureKey) string {

	k = normalizeKey(k)

	// include entity_id; empty is allowed

	return strings.Join([]string{

		k.TenantID,

		k.Namespace,

		k.Name,

		k.EntityType,

		k.EntityID,

		string(k.Granularity),
	}, "|")
}
func seriesKeyString(k FeatureKey) string {

	return defKeyString(k)
}
func parseSeriesKey(s string) FeatureKey {

	parts := strings.Split(s, "|")
	k := FeatureKey{}
	if len(parts) >= 6 {

		k.TenantID = parts[0]

		k.Namespace = parts[1]

		k.Name = parts[2]

		k.EntityType = parts[3]

		k.EntityID = parts[4]

		k.Granularity = Granularity(parts[5])

	}
	return normalizeKey(k)
}
func insertPointSorted(points []storedPoint, p storedPoint) []storedPoint {

	// Dedupe by exact timestamp: replace existing value deterministically (keep newer? we choose replace)
	i := sort.Search(len(points), func(i int) bool {

		return !points[i].ts.Before(p.ts)

	})
	if i < len(points) && points[i].ts.Equal(p.ts) {

		points[i] = p

		return points

	}
	points = append(points, storedPoint{})
	copy(points[i+1:], points[i:])
	points[i] = p

	return points
}
func insertPointSortedDedupe(points []storedPoint, p storedPoint) []storedPoint {

	// Dedupe by exact timestamp: keep existing deterministically (ignore incoming)
	i := sort.Search(len(points), func(i int) bool {

		return !points[i].ts.Before(p.ts)

	})
	if i < len(points) && points[i].ts.Equal(p.ts) {

		return points

	}
	points = append(points, storedPoint{})
	copy(points[i+1:], points[i:])
	points[i] = p

	return points
}
func lowerBoundTS(points []storedPoint, ts time.Time) int {

	return sort.Search(len(points), func(i int) bool {

		return !points[i].ts.Before(ts)

	})
}
func upperBoundTS(points []storedPoint, ts time.Time) int {

	return sort.Search(len(points), func(i int) bool {

		return points[i].ts.After(ts)

	})
}
func materializePoint(k FeatureKey, sp storedPoint, includeMeta bool) FeaturePoint {

	out := FeaturePoint{

		Key: k,

		TS: sp.tsS,

		Value: sp.val,
	}
	if includeMeta && sp.meta != nil {

		out.Meta = cloneMeta(sp.meta)

	}
	return out
}
func cloneMeta(m map[string]string) map[string]string {

	if m == nil {

		return nil

	}
	out := make(map[string]string, len(m))
	for k, v := range m {

		out[k] = v

	}
	return out
}

////////////////////////////////////////////////////////////////////////////////
// Time parsing
////////////////////////////////////////////////////////////////////////////////

func parseTS(s string) (time.Time, error) {

	s = strings.TrimSpace(s)
	if s == "" {

		return time.Time{}, fmt.Errorf("%w: ts required", ErrInvalidWrite)

	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {

		return t.UTC(), nil

	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {

		return time.Time{}, fmt.Errorf("%w: invalid ts", ErrInvalidWrite)

	}
	return t.UTC(), nil
}
func parseTSIfProvided(s string) (time.Time, bool) {

	s = strings.TrimSpace(s)
	if s == "" {

		return time.Time{}, false

	}
	t, err := parseTS(s)
	if err != nil {

		return time.Time{}, false

	}
	return t, true
}

////////////////////////////////////////////////////////////////////////////////
// misc
////////////////////////////////////////////////////////////////////////////////

func defsEqual(a, b FeatureDef) bool {

	na := normalizeDef(a)
	nb := normalizeDef(b)
	if defKeyString(na.Key) != defKeyString(nb.Key) {

		return false

	}
	if na.Type != nb.Type || na.Description != nb.Description || na.TTLSeconds != nb.TTLSeconds {

		return false

	}
	if !stringSliceEqual(na.Tags, nb.Tags) {

		return false

	}
	return stringMapEqual(na.Meta, nb.Meta)
}
func stringSliceEqual(a, b []string) bool {

	if len(a) != len(b) {

		return false

	}
	for i := range a {

		if a[i] != b[i] {

			return false

		}

	}
	return true
}
func stringMapEqual(a, b map[string]string) bool {

	if len(a) != len(b) {

		return false

	}
	for k, v := range a {

		if b[k] != v {

			return false

		}

	}
	return true
}
func clampInt(x, lo, hi int) int {

	if x < lo {

		return lo

	}
	if x > hi {

		return hi

	}
	return x
}
func minInt(a, b int) int {

	if a < b {

		return a

	}
	return b
}
func maxInt(a, b int) int {

	if a > b {

		return a

	}
	return b
}

// best-effort stringify helper (unused currently but kept for future use)
func anyToString(v any) string {

	switch t := v.(type) {

	case string:

		return t

	case bool:

		if t {

			return "true"

		}
		return "false"

	case float64:

		return strconv.FormatFloat(t, 'g', -1, 64)
		// case float32:

		return strconv.FormatFloat(float64(t), 'g', -1, 64)
		// case int:

		return strconv.Itoa(t)
		// case int64:

		return strconv.FormatInt(t, 10)
		// case uint64:

		return strconv.FormatUint(t, 10)
	default:

		b, err := json.Marshal(t)
		if err != nil {

			return ""

		}
		return string(b)

	}
}
