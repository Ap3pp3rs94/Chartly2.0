package main

import (
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
	MaxEvents       int
}

type observation struct {
	TenantID  string            `json:"tenant_id"`
	ID        string            `json:"id"`
	TS        string            `json:"ts"`
	Service   string            `json:"service"`
	Component string            `json:"component,omitempty"`
	Kind      string            `json:"kind"`
	Status    string            `json:"status"`
	LatencyMS float64           `json:"latency_ms,omitempty"`
	Message   string            `json:"message,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

type store struct {
	mu    sync.Mutex
	max   int
	items []observation
}

func newStore(max int) *store {
	if max <= 0 {
		max = 200000
	}
	return &store{
		max:   max,
		items: make([]observation, 0, min(1024, max)),
	}
}

func (s *store) append(ev observation) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = append(s.items, ev)
	if len(s.items) > s.max {
		drop := len(s.items) - s.max
		if drop > 0 {
			s.items = append([]observation(nil), s.items[drop:]...)
		}
	}
}

func (s *store) snapshot() []observation {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := make([]observation, len(s.items))
	copy(cp, s.items)
	return cp
}

func (s *store) list(tenantID, service string, since time.Time, hasSince bool, limit int) []observation {
	items := s.snapshot()
	tenantID = norm(tenantID)
	service = norm(service)

	if limit <= 0 {
		limit = 200
	}
	if limit > 5000 {
		limit = 5000
	}

	out := make([]observation, 0, min(limit, len(items)))
	for _, ev := range items {
		if ev.TenantID != tenantID {
			continue
		}
		if service != "" && ev.Service != service {
			continue
		}
		if hasSince {
			t, err := parseRFC3339(ev.TS)
			if err != nil || !t.After(since) {
				continue
			}
		}
		out = append(out, ev)
	}

	sort.Slice(out, func(i, j int) bool {
		ti, _ := parseRFC3339(out[i].TS)
		tj, _ := parseRFC3339(out[j].TS)
		if ti.Before(tj) {
			return true
		}
		if ti.After(tj) {
			return false
		}
		return out[i].ID < out[j].ID
	})

	if len(out) > limit {
		out = out[:limit]
	}

	cp := make([]observation, len(out))
	for i := range out {
		cp[i] = out[i]
		cp[i].Meta = copyMeta(out[i].Meta)
	}
	return cp
}

func (s *store) metrics(tenantID string) []map[string]any {
	items := s.snapshot()
	tenantID = norm(tenantID)

	counts := make(map[string]int)
	for _, ev := range items {
		if ev.TenantID != tenantID {
			continue
		}
		k := ev.Service + "|" + ev.Status
		counts[k]++
	}

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		parts := strings.Split(k, "|")
		svc := parts[0]
		st := ""
		if len(parts) > 1 {
			st = parts[1]
		}
		out = append(out, map[string]any{
			"service": svc,
			"status":  st,
			"count":   counts[k],
		})
	}
	return out
}

type server struct {
	cfg  config
	st   *store
	reqN uint64
}

func main() {
	log.SetFlags(0)
	cfg := loadConfig()

	s := &server{
		cfg: cfg,
		st:  newStore(cfg.MaxEvents),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/v0/observe", s.withMiddleware(s.handleObserve))
	mux.HandleFunc("/v0/metrics", s.withMiddleware(s.handleMetrics))

	h := &http.Server{
		Addr:              netAddr(cfg.Addr, cfg.Port),
		Handler:           mux,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
		ReadHeaderTimeout: minDuration(cfg.ReadTimeout, 5*time.Second),
	}

	errCh := make(chan error, 1)
	go func() {
		logJSON("info", "observer_server_start", map[string]any{
			"addr":         h.Addr,
			"env":          cfg.Env,
			"buildVersion": buildVersion,
			"buildCommit":  buildCommit,
			"buildDate":    buildDate,
		})
		errCh <- h.ListenAndServe()
	}()

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

	logJSON("info", "observer_server_stopped", map[string]any{"addr": h.Addr})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleReady(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ready": true})
}

func (s *server) handleObserve(w http.ResponseWriter, r *http.Request, tenantID, reqID string) {
	switch r.Method {
	case http.MethodPost:
		s.handlePostObserve(w, r, tenantID, reqID)
		return
	case http.MethodGet:
		s.handleGetObserve(w, r, tenantID)
		return
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
}

func (s *server) handlePostObserve(w http.ResponseWriter, r *http.Request, tenantID, reqID string) {
	var in observation
	if err := decodeJSONStrict(r.Body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	in.TenantID = norm(in.TenantID)
	if in.TenantID == "" {
		in.TenantID = tenantID
	} else if in.TenantID != tenantID {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tenant_id mismatch"})
		return
	}

	in.Service = norm(in.Service)
	in.Kind = norm(in.Kind)
	in.Status = norm(in.Status)
	in.Component = norm(in.Component)
	in.Message = norm(in.Message)

	if in.Service == "" || in.Kind == "" || in.Status == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "service/kind/status required"})
		return
	}

	if in.TS == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ts required"})
		return
	}

	ts, err := parseRFC3339(in.TS)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ts must be rfc3339"})
		return
	}
	in.TS = ts.UTC().Format(time.RFC3339Nano)

	in.Meta = normalizeStringMap(in.Meta)
	in.ID = norm(in.ID)
	if in.ID == "" {
		in.ID = deterministicID(in)
	}
	in.RequestID = reqID

	s.st.append(in)

	logJSON("info", "observation_ingested", map[string]any{
		"tenant_id":  tenantID,
		"id":         in.ID,
		"service":    in.Service,
		"status":     in.Status,
		"request_id": reqID,
	})

	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "id": in.ID})
}

func (s *server) handleGetObserve(w http.ResponseWriter, r *http.Request, tenantID string) {
	q := r.URL.Query()
	paramTenant := norm(q.Get("tenant_id"))
	if paramTenant != "" && paramTenant != tenantID {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tenant_id mismatch"})
		return
	}
	if paramTenant == "" {
		paramTenant = tenantID
	}

	service := q.Get("service")
	limit := atoiDefault(q.Get("limit"), 200)

	sinceRaw := strings.TrimSpace(q.Get("since"))
	var since time.Time
	var hasSince bool
	if sinceRaw != "" {
		t, err := parseRFC3339(sinceRaw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "since must be rfc3339"})
			return
		}
		since = t
		hasSince = true
	}

	ev := s.st.list(paramTenant, service, since, hasSince, limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": paramTenant,
		"count":     len(ev),
		"items":     ev,
	})
}

func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request, tenantID, reqID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	m := s.st.metrics(tenantID)
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenantID,
		"metrics":   m,
	})
	_ = reqID
}

func (s *server) withMiddleware(next func(http.ResponseWriter, *http.Request, string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.MaxBodyBytes > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		}

		reqID := s.requestID(r)
		w.Header().Set("X-Request-Id", reqID)

		tenantID := strings.TrimSpace(r.Header.Get(s.cfg.TenantHeader))
		isLocal := strings.EqualFold(s.cfg.Env, "local")

		if strings.HasPrefix(r.URL.Path, "/v0/") && !isLocal {
			if tenantID == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing tenant header"})
				return
			}
		}
		if isLocal && tenantID == "" {
			tenantID = s.cfg.LocalTenant
		}

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
	if v := strings.TrimSpace(r.Header.Get("X-Request-Id")); v != "" {
		return v
	}

	n := atomic.AddUint64(&s.reqN, 1)
	cl := r.Header.Get("Content-Length")
	if cl == "" {
		cl = "0"
	}
	seed := strings.Join([]string{r.Method, r.URL.Path, cl, strconv.FormatUint(n, 10)}, "|")
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func deterministicID(ev observation) string {
	parts := []string{
		norm(ev.TenantID),
		norm(ev.Service),
		norm(ev.Component),
		norm(ev.Kind),
		norm(ev.Status),
		norm(ev.TS),
		norm(ev.Message),
	}

	metaKeys := make([]string, 0, len(ev.Meta))
	for k := range ev.Meta {
		metaKeys = append(metaKeys, k)
	}
	sort.Strings(metaKeys)
	for _, k := range metaKeys {
		parts = append(parts, k+"="+ev.Meta[k])
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func loadConfig() config {
	env := strings.TrimSpace(getenv("OBSERVER_ENV", "local"))
	addr := strings.TrimSpace(getenv("OBSERVER_ADDR", "0.0.0.0"))
	port := atoiDefault(getenv("OBSERVER_PORT", "8086"), 8086)

	readTO := parseDuration(getenv("OBSERVER_READ_TIMEOUT", "10s"), 10*time.Second)
	writeTO := parseDuration(getenv("OBSERVER_WRITE_TIMEOUT", "10s"), 10*time.Second)
	idleTO := parseDuration(getenv("OBSERVER_IDLE_TIMEOUT", "60s"), 60*time.Second)
	shutTO := parseDuration(getenv("OBSERVER_SHUTDOWN_TIMEOUT", "10s"), 10*time.Second)

	maxBody := atoi64Default(getenv("OBSERVER_MAX_BODY_BYTES", "1048576"), 1048576)
	maxHdr := atoiDefault(getenv("OBSERVER_MAX_HEADER_BYTES", "32768"), 32768)

	maxEvents := atoiDefault(getenv("OBSERVER_MAX_EVENTS", "200000"), 200000)

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
		MaxEvents:       maxEvents,
	}
}

func decodeJSONStrict(r io.Reader, out any) error {
	if r == nil {
		return errors.New("missing body")
	}

	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return errors.New("invalid json")
	}

	var extra any
	if err := dec.Decode(&extra); err == nil {
		return errors.New("trailing json")
	}
	if !errors.Is(err := dec.Decode(&extra), io.EOF) {
		return errors.New("trailing json")
	}

	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func logJSON(level string, event string, fields map[string]any) {
	type kv struct {
		K string `json:"k"`
		V any    `json:"v"`
	}

	if fields == nil {
		fields = map[string]any{}
	}
	fields["level"] = level
	fields["event"] = event

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	arr := make([]kv, 0, len(keys))
	for _, k := range keys {
		arr = append(arr, kv{K: k, V: fields[k]})
	}

	b, _ := json.Marshal(arr)
	log.Print(string(b))
}

func parseRFC3339(s string) (time.Time, error) {
	s = norm(s)
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, s)
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
		port = 8086
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

func normalizeStringMap(m map[string]string) map[string]string {
	if m == nil || len(m) == 0 {
		return map[string]string{}
	}

	tmp := make(map[string]string, len(m))
	for k, v := range m {
		kk := normCollapse(k)
		if kk == "" {
			continue
		}
		tmp[kk] = normCollapse(v)
	}

	keys := make([]string, 0, len(tmp))
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

func copyMeta(m map[string]string) map[string]string {
	if m == nil || len(m) == 0 {
		return map[string]string{}
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

func norm(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

func normCollapse(s string) string {
	s = norm(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
