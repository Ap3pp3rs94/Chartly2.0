package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

type config struct {
	Env             string
	Addr            string
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	MaxBodyBytes    int64
	MaxHeaderBytes  int
	TenantHeader    string
	LocalTenant     string
}
type auditEvent struct {
	TenantID   string            `json:"tenant_id"`
	EventID    string            `json:"event_id"`
	EventTS    string            `json:"event_ts"` // RFC3339/RFC3339Nano
	Action     string            `json:"action"`
	ObjectKey  string            `json:"object_key,omitempty"`
	RequestID  string            `json:"request_id,omitempty"`
	ActorID    string            `json:"actor_id,omitempty"`
	Source     string            `json:"source,omitempty"`
	Outcome    string            `json:"outcome"`
	Detail     map[string]any    `json:"detail,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
	ReceivedAt string            `json:"received_at,omitempty"` // server adds (RFC3339Nano)
}
type store struct {
	mu     sync.Mutex
	events []auditEvent
	// For deterministic lookup, keep an index of tenant->eventID->pos.
	idx map[string]map[string]int
}

func newStore() *store {
	return &store{
		events: make([]auditEvent, 0, 1024),
		idx:    make(map[string]map[string]int),
	}
}
func (s *store) add(ev auditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.TenantID == "" || ev.EventID == "" {
		return errors.New("tenant_id and event_id required")
	}
	if _, ok := s.idx[ev.TenantID]; !ok {
		s.idx[ev.TenantID] = make(map[string]int)
	}
	if _, exists := s.idx[ev.TenantID][ev.EventID]; exists {
		// Idempotent insert: ignore duplicates deterministically.
		// return nil
	}
	pos := len(s.events)
	s.events = append(s.events, ev)
	s.idx[ev.TenantID][ev.EventID] = pos
	return nil
}
func (s *store) list(tenantID string, since string, limit int) ([]auditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id required")
	}
	var sinceT time.Time
	var hasSince bool
	if strings.TrimSpace(since) != "" {
		t, err := parseRFC3339(since)
		if err != nil {
			return nil, err
		}
		sinceT = t
		hasSince = true
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 5000 {
		limit = 5000
	}

	// Deterministic ordering:
	// - preserve append order (which is deterministic relative to ingestion).
	out := make([]auditEvent, 0, min(limit, len(s.events)))
	for _, ev := range s.events {
		if ev.TenantID != tenantID {
			continue
		}
		if hasSince {
			t, err := parseRFC3339(ev.EventTS)
			if err != nil {
				continue
			}
			if !t.After(sinceT) {
				continue
			}
		}
		out = append(out, ev)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type server struct {
	cfg   config
	store *store
	reqN  uint64
}

func main() {
	cfg := loadConfig()
	s := &server{
		cfg:   cfg,
		store: newStore(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/v0/events", s.withMiddleware(s.handleEvents))
	h := &http.Server{
		Addr:              netAddr(cfg.Addr, cfg.Port),
		Handler:           mux,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
		ReadHeaderTimeout: minDuration(cfg.ReadTimeout, 5*time.Second),
	}

	// Start server
	errCh := make(chan error, 1)
	go func() {
		logJSON("info", "audit_server_start", map[string]any{
			"addr":         h.Addr,
			"buildVersion": buildVersion,
			"buildCommit":  buildCommit,
			"buildDate":    buildDate,
			"env":          cfg.Env,
		})
		errCh <- h.ListenAndServe()
	}()

	// Shutdown handling
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		logJSON("info", "shutdown_signal", map[string]any{"signal": sig.String()})
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logJSON("error", "server_error", map[string]any{"error": err.Error()})
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = h.Shutdown(ctx)
	logJSON("info", "audit_server_stopped", map[string]any{"addr": h.Addr})
}
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
func (s *server) handleReady(w http.ResponseWriter, r *http.Request) {
	// In-memory v0 is always "ready".
	writeJSON(w, http.StatusOK, map[string]any{"ready": true})
}
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request, tenantID string, reqID string) {
	switch r.Method {
	case http.MethodPost:
		s.handlePostEvent(w, r, tenantID, reqID)
		// return
		// case http.MethodGet:
		s.handleGetEvents(w, r, tenantID)
		// return
		// default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		// return
	}
}
func (s *server) handlePostEvent(w http.ResponseWriter, r *http.Request, tenantID string, reqID string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read body failed"})
		// return
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "empty body"})
		// return
	}
	var ev auditEvent
	dec := json.NewDecoder(bytesReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ev); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		// return
	}

	// Ensure no trailing junk.
	// var extra any
	if err := dec.Decode(&extra); err == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "trailing json"})
		// return
	} else if err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "trailing json"})
		// return
	}

	// Enforce tenant scope: tenant header overrides payload if missing; if payload mismatches header, reject.
	ev.TenantID = strings.TrimSpace(ev.TenantID)
	if ev.TenantID == "" {
		ev.TenantID = tenantID
	} else if ev.TenantID != tenantID {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tenant_id mismatch"})
		// return
	}
	ev.EventID = strings.TrimSpace(ev.EventID)
	ev.Action = strings.TrimSpace(ev.Action)
	ev.Outcome = strings.TrimSpace(ev.Outcome)
	if ev.EventID == "" || ev.Action == "" || ev.Outcome == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "event_id/action/outcome required"})
		// return
	}

	// Validate event_ts format.
	ev.EventTS = strings.TrimSpace(ev.EventTS)
	if ev.EventTS == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "event_ts required"})
		// return
	}
	if _, err := parseRFC3339(ev.EventTS); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "event_ts must be rfc3339"})
		// return
	}

	// Attach request context
	ev.RequestID = firstNonEmpty(strings.TrimSpace(ev.RequestID), reqID)
	ev.ReceivedAt = time.Now().UTC().Format(time.RFC3339Nano)

	// Normalize meta map keys deterministically if present.
	ev.Meta = normalizeStringMap(ev.Meta)
	if err := s.store.add(ev); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		// return
	}
	logJSON("info", "event_ingested", map[string]any{
		"tenant_id":  ev.TenantID,
		"event_id":   ev.EventID,
		"action":     ev.Action,
		"outcome":    ev.Outcome,
		"request_id": ev.RequestID,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}
func (s *server) handleGetEvents(w http.ResponseWriter, r *http.Request, tenantID string) {
	q := r.URL.Query()
	limit := atoiDefault(q.Get("limit"), 200)
	since := strings.TrimSpace(q.Get("since"))
	evs, err := s.store.list(tenantID, since, limit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		// return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenantID,
		"count":     len(evs),
		"events":    evs,
	})
}
func (s *server) withMiddleware(next func(http.ResponseWriter, *http.Request, string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Size limit
		if s.cfg.MaxBodyBytes > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		}
		reqID := s.requestID(r)
		w.Header().Set("X-Request-Id", reqID)
		tenantID, terr := s.tenantID(r)
		if terr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": terr.Error()})
			// return
		}

		// Tenant required for /v0/* unless local env
		if strings.HasPrefix(r.URL.Path, "/v0/") && strings.ToLower(s.cfg.Env) != "local" {
			if tenantID == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing tenant header"})
				// return
			}
		}

		// Local env default tenant.
		if strings.ToLower(s.cfg.Env) == "local" && tenantID == "" {
			tenantID = s.cfg.LocalTenant
		}

		// Panic recovery
		defer func() {
			if rec := recover(); rec != nil {
				logJSON("error", "panic_recovered", map[string]any{
					"panic": fmt.Sprintf("%v", rec),
					"path":  r.URL.Path,
				})
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal"})
			}
		}()
		logJSON("info", "request", map[string]any{
			"method":     r.Method,
			"path":       r.URL.Path,
			"tenant_id":  tenantID,
			"request_id": reqID,
			"remote":     r.RemoteAddr,
		})
		next(w, r, tenantID, reqID)
	}
}
func (s *server) requestID(r *http.Request) string {
	// Prefer caller-provided deterministic ID if present.
	if v := strings.TrimSpace(r.Header.Get("X-Request-Id")); v != "" {
		return v
	}

	// Deterministic per-process monotonic counter.
	n := atomic.AddUint64(&s.reqN, 1)

	// Hash method+path+content-length+counter (no time.Now).
	cl := r.Header.Get("Content-Length")
	if cl == "" {
		cl = "0"
	}
	seed := strings.Join([]string{r.Method, r.URL.Path, cl, strconv.FormatUint(n, 10)}, "|")
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:16]) // 32 hex chars
}
func (s *server) tenantID(r *http.Request) (string, error) {
	h := s.cfg.TenantHeader
	if h == "" {
		h = "X-Tenant-Id"
	}
	v := strings.TrimSpace(r.Header.Get(h))
	// local env may default later
	// return v, nil
}
func loadConfig() config {
	env := strings.TrimSpace(getenv("AUDIT_ENV", "local"))
	addr := strings.TrimSpace(getenv("AUDIT_ADDR", "0.0.0.0"))
	port := atoiDefault(getenv("AUDIT_PORT", "8084"), 8084)
	readTO := parseDuration(getenv("AUDIT_READ_TIMEOUT", "10s"), 10*time.Second)
	writeTO := parseDuration(getenv("AUDIT_WRITE_TIMEOUT", "10s"), 10*time.Second)
	idleTO := parseDuration(getenv("AUDIT_IDLE_TIMEOUT", "60s"), 60*time.Second)
	shutTO := parseDuration(getenv("AUDIT_SHUTDOWN_TIMEOUT", "10s"), 10*time.Second)
	maxBody := atoi64Default(getenv("AUDIT_MAX_BODY_BYTES", "1048576"), 1048576)
	maxHdr := atoiDefault(getenv("AUDIT_MAX_HEADER_BYTES", "32768"), 32768)
	return config{
		Env:             env,
		Addr:            addr,
		Port:            port,
		ReadTimeout:     readTO,
		WriteTimeout:    writeTO,
		IdleTimeout:     idleTO,
		ShutdownTimeout: shutTO,
		MaxBodyBytes:    maxBody,
		MaxHeaderBytes:  maxHdr,
		TenantHeader:    "X-Tenant-Id",
		LocalTenant:     "local",
	}
}
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}
func logJSON(level string, event string, fields map[string]any) {
	// Deterministic field ordering is not guaranteed by Go maps.
	// This logger uses a stable encoding by converting to a sorted list internally.
	type kv struct {
		K string `json:"k"`
		V any    `json:"v"`
	}
	keys := make([]string, 0, len(fields)+2)
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	arr := make([]kv, 0, len(keys)+2)
	arr = append(arr, kv{K: "level", V: level})
	arr = append(arr, kv{K: "event", V: event})
	for _, k := range keys {
		arr = append(arr, kv{K: k, V: fields[k]})
	}
	b, _ := json.Marshal(arr)
	log.Print(string(b))
}
func parseRFC3339(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
func parseDuration(s string, def time.Duration) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}
func getenv(k string, def string) string {
	v := os.Getenv(k)
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
func atoiDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
func atoi64Default(s string, def int64) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}
func netAddr(addr string, port int) string {
	if addr == "" {
		addr = "0.0.0.0"
	}
	if port <= 0 {
		port = 8084
	}
	return fmt.Sprintf("%s:%d", addr, port)
}
func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func bytesReader(b []byte) *bytes.Reader {
	return bytes.NewReader(b)
}
func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
func normalizeStringMap(m map[string]string) map[string]string {
	if m == nil || len(m) == 0 {
		return map[string]string{}
	}
	keys := make([]string, 0, len(m))
	tmp := make(map[string]string, len(m))
	for k, v := range m {
		kk := strings.TrimSpace(strings.ReplaceAll(k, "\x00", ""))
		if kk == "" {
			continue
		}
		tmp[kk] = strings.TrimSpace(strings.ReplaceAll(v, "\x00", ""))
	}
	for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = tmp[k]
	}
	return out
}
