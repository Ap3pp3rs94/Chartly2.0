package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	maxBodyBytes = 1 << 20 // 1MB
)

type Source struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Config    map[string]any `json:"config"`
	Enabled   bool           `json:"enabled"`
	TenantID  string         `json:"tenant_id"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}
type sourceCreateReq struct {
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	Enabled *bool          `json:"enabled"`
	Config  map[string]any `json:"config"`
}
type sourcesCreateResp struct {
	Source Source `json:"source"`
}
type sourcesListResp struct {
	Sources []Source `json:"sources"`
}
type errResp struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	var e errResp
	e.Error.Code = code
	e.Error.Message = msg
	_ = json.NewEncoder(w).Encode(e)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func nowRFC3339Nano() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
func generateSourceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "src_" + hex.EncodeToString(b[:]), nil
}
func envIsLocal() bool {
	env := strings.TrimSpace(os.Getenv("CHARTLY_ENV"))
	if env == "" {
		env = "local"
	}
	return strings.EqualFold(env, "local")
}
func tenantFromHeader(r *http.Request) (string, bool) {
	t := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if t == "" {
		if envIsLocal() {
			return "local", true
		}
		return "", false
	}
	return t, true
}
func validateKind(kind string) bool {
	switch kind {
	case "api", "domain", "db", "file", "webhook", "other":
		return true
	default:
		return false
	}
}

var forbiddenConfigKeys = map[string]struct{}{
	"password":    {},
	"secret":      {},
	"token":       {},
	"api_key":     {},
	"apikey":      {},
	"private_key": {},
	"access_key":  {},
	"secret_key":  {},
}

func hasForbiddenTopLevelConfigKeys(cfg map[string]any) (string, bool) {
	for k := range cfg {
		kl := strings.ToLower(strings.TrimSpace(k))
		if _, bad := forbiddenConfigKeys[kl]; bad {
			return k, true
		}
	}
	return "", false
}

// In-memory store (v0 wiring). This will later be replaced by storage service calls,
// but provides correct semantics and allows the gateway to operate end-to-end early.
type sourceStore struct {
	mu   sync.RWMutex
	data map[string]map[string]Source // tenant_id -> source_id -> Source
}

func newSourceStore() *sourceStore {
	return &sourceStore{data: make(map[string]map[string]Source)}
}
func (s *sourceStore) upsert(src Source) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.data[src.TenantID]
	if !ok {
		m = make(map[string]Source)
		s.data[src.TenantID] = m
	}
	m[src.ID] = src
}
func (s *sourceStore) list(tenantID string) []Source {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.data[tenantID]
	if m == nil {
		return []Source{}
	}
	out := make([]Source, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

var sources = newSourceStore()

// SourcesCreate handles POST /sources
func SourcesCreate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromHeader(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing_tenant", "X-Tenant-Id header is required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer r.Body.Close()
	var req sourceCreateReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Kind = strings.TrimSpace(req.Kind)
	if len(req.Name) < 1 || len(req.Name) > 256 {
		writeErr(w, http.StatusBadRequest, "invalid_name", "name must be 1..256 characters")
		return
	}
	if !validateKind(req.Kind) {
		writeErr(w, http.StatusBadRequest, "invalid_kind", "kind must be one of: api, domain, db, file, webhook, other")
		return
	}
	if req.Config == nil {
		req.Config = map[string]any{}
	}
	if badKey, bad := hasForbiddenTopLevelConfigKeys(req.Config); bad {
		writeErr(w, http.StatusBadRequest, "forbidden_config_key", "config contains forbidden key: "+badKey)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	id, err := generateSourceID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "id_generation_failed", "failed to generate source id")
		return
	}
	ts := nowRFC3339Nano()
	src := Source{
		ID:        id,
		Kind:      req.Kind,
		Name:      req.Name,
		Config:    req.Config,
		Enabled:   enabled,
		TenantID:  tenantID,
		CreatedAt: ts,
		UpdatedAt: ts,
	}
	sources.upsert(src)
	writeJSON(w, http.StatusCreated, sourcesCreateResp{Source: src})
}

// SourcesList handles GET /sources
func SourcesList(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromHeader(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing_tenant", "X-Tenant-Id header is required")
		return
	}
	list := sources.list(tenantID)
	writeJSON(w, http.StatusOK, sourcesListResp{Sources: list})
}
