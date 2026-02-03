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

	"net"

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

// Populated by -ldflags in Docker build:
// -X main.version=... -X main.commit=...
var (
	version = "0.0.0"

	commit = "dev"
)
const (
	serviceName = "storage"
)
type config struct {
	Env string

	Addr string

	Port int

	ReadTimeout time.Duration

	WriteTimeout time.Duration

	IdleTimeout time.Duration

	ShutdownTimeout time.Duration

	// MaxObjectBytes limits upload bodies for PUT /v0/objects.

	MaxObjectBytes int64

	// MaxHeaderBytes limits request headers.

	MaxHeaderBytes int

	// RequireTenant forces X-Tenant-Id on /v0/* endpoints when true.

	RequireTenant bool
}

func loadConfig() config {

	c := config{

		Env: getenv("STORAGE_ENV", "local"),

		Addr: getenv("STORAGE_ADDR", "0.0.0.0"),

		Port: getenvInt("STORAGE_PORT", 8083),

		ReadTimeout: getenvDuration("STORAGE_READ_TIMEOUT", 10*time.Second),

		WriteTimeout: getenvDuration("STORAGE_WRITE_TIMEOUT", 30*time.Second),

		IdleTimeout: getenvDuration("STORAGE_IDLE_TIMEOUT", 60*time.Second),

		ShutdownTimeout: getenvDuration("STORAGE_SHUTDOWN_TIMEOUT", 10*time.Second),

		MaxObjectBytes: getenvInt64("STORAGE_MAX_OBJECT_BYTES", 50*1024*1024), // 50MiB default

		MaxHeaderBytes: getenvInt("STORAGE_MAX_HEADER_BYTES", 1<<20), // 1MiB

	}

	// Tenant rules:

	// - If ENV != local => require tenant header

	// - If ENV == local => default tenant_id="local" when missing

	c.RequireTenant = strings.ToLower(strings.TrimSpace(c.Env)) != "local"

	// Optional override:

	if v, ok := os.LookupEnv("STORAGE_REQUIRE_TENANT"); ok {

		if b, ok2 := parseBoolLoose(v); ok2 {

			c.RequireTenant = b

		}

	}
	if c.Port <= 0 || c.Port > 65535 {

		c.Port = 8083

	}
	if c.MaxObjectBytes <= 0 {

		c.MaxObjectBytes = 50 * 1024 * 1024

	}
	if c.MaxHeaderBytes <= 0 {

		c.MaxHeaderBytes = 1 << 20

	}
	return c
}

// type ctxKey string

const (
	ctxRequestID ctxKey = "request_id"

	ctxTenantID ctxKey = "tenant_id"
)
// var reqCounter uint64

func main() {

	cfg := loadConfig()
logger := newJSONLogger(os.Stdout)
logger.Info("service_start", map[string]any{

		"service": serviceName,

		"version": version,

		"commit": commit,

		"env": cfg.Env,

		"addr": cfg.Addr,

		"port": cfg.Port,

		"tenant_required": cfg.RequireTenant,

		"max_object_bytes": cfg.MaxObjectBytes,
	})
store := newObjectStore()
mux := http.NewServeMux()
api := newAPI(cfg, logger, store)

	// Core probes

	mux.HandleFunc("/health", api.handleHealth)
mux.HandleFunc("/ready", api.handleReady)

	// v0 storage endpoints (in-memory)
mux.HandleFunc("/v0/objects", api.handleObjects)
mux.HandleFunc("/v0/objects/meta", api.handleObjectsMeta)
mux.HandleFunc("/v0/stats", api.handleStats)
handler := chain(

		mux,

		recoverMW(logger),

		requestIDMW(),

		tenantMW(cfg),

		loggingMW(logger),
	)
addr := net.JoinHostPort(cfg.Addr, strconv.Itoa(cfg.Port))
srv := &http.Server{

		Addr: addr,

		Handler: handler,

		ReadTimeout: cfg.ReadTimeout,

		WriteTimeout: cfg.WriteTimeout,

		IdleTimeout: cfg.IdleTimeout,

		MaxHeaderBytes: cfg.MaxHeaderBytes,

		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
go func() {

		// ListenAndServe returns http.ErrServerClosed on shutdown

		if err := srv.ListenAndServe(); err != nil {

			errCh <- err

		}

	}()

	// Graceful shutdown

	sigCh := make(chan os.Signal, 2)
signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
select {

	case sig := <-sigCh:

		logger.Info("shutdown_signal", map[string]any{"signal": sig.String()})
case err := <-errCh:

		if err != nil && !errors.Is(err, http.ErrServerClosed) {

			logger.Error("server_error", map[string]any{"error": err.Error()})

		}

	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
defer cancel()
if err := srv.Shutdown(ctx); err != nil {

		logger.Error("shutdown_error", map[string]any{"error": err.Error()})

	} else {

		logger.Info("shutdown_complete", map[string]any{"service": serviceName})

	}

	// Best-effort drain errCh (non-blocking)
select {

	case err := <-errCh:

		if err != nil && !errors.Is(err, http.ErrServerClosed) {

			logger.Error("server_error_post_shutdown", map[string]any{"error": err.Error()})

		}
	default:

	}
}

////////////////////////////////////////////////////////////////////////////////
// API
////////////////////////////////////////////////////////////////////////////////

type api struct {
	cfg config

	log *jsonLogger

	store *objectStore
}

func newAPI(cfg config, log *jsonLogger, store *objectStore) *api {

	return &api{cfg: cfg, log: log, store: store}
}
func (a *api) handleHealth(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {

		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
// return

	}
	resp := map[string]any{

		"status": "ok",

		"service": serviceName,

		"version": version,

		"commit": commit,
	}
	writeJSON(w, r, http.StatusOK, resp)
}
func (a *api) handleReady(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {

		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
// return

	}

	// No external deps wired here; readiness == process ready.

	resp := map[string]any{

		"status": "ready",

		"service": serviceName,

		"version": version,

		"commit": commit,
	}
	writeJSON(w, r, http.StatusOK, resp)
}

// In-memory object API.
// IMPORTANT: This is an in-memory store only (no persistence). It is safe for local/dev and placeholder use.
// Replace/extend with durable storage in future files.
func (a *api) handleObjects(w http.ResponseWriter, r *http.Request) {

	// Tenant enforcement occurs in middleware for /v0/*.

	switch r.Method {

	case http.MethodPut:

		a.handlePutObject(w, r)
// case http.MethodGet:

		a.handleGetObject(w, r, false)
// case http.MethodHead:

		a.handleGetObject(w, r, true)
// case http.MethodDelete:

		a.handleDeleteObject(w, r)
default:

		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")

	}
}
func (a *api) handleObjectsMeta(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {

		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
// return

	}
	tenant := tenantIDFromCtx(r.Context())
key := strings.TrimSpace(r.URL.Query().Get("key"))
if key == "" {

		writeError(w, r, http.StatusBadRequest, "invalid_request", "missing key query param")
// return

	}
	meta, ok := a.store.getMeta(tenant, key)
if !ok {

		writeError(w, r, http.StatusNotFound, "not_found", "object not found")
// return

	}
	writeJSON(w, r, http.StatusOK, meta)
}
func (a *api) handleStats(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {

		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
// return

	}
	resp := a.store.stats()
writeJSON(w, r, http.StatusOK, resp)
}
func (a *api) handlePutObject(w http.ResponseWriter, r *http.Request) {

	tenant := tenantIDFromCtx(r.Context())
key := strings.TrimSpace(r.URL.Query().Get("key"))
if key == "" {

		writeError(w, r, http.StatusBadRequest, "invalid_request", "missing key query param")
// return

	}
	if strings.Contains(key, "\x00") {

		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid key")
// return

	}

	// Enforce size limit deterministically.

	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxObjectBytes)
defer r.Body.Close()
ct := strings.TrimSpace(r.Header.Get("Content-Type"))
if ct == "" {

		ct = "application/octet-stream"

	}

	// Read body

	b, err := io.ReadAll(r.Body)
if err != nil {

		// MaxBytesReader returns a specific error string; keep it simple.

		writeError(w, r, http.StatusRequestEntityTooLarge, "too_large", "request body too large or unreadable")
// return

	}
	sum := sha256.Sum256(b)
etag := `"` + hex.EncodeToString(sum[:]) + `"`

	meta := objectMeta{

		TenantID: tenant,

		Key: key,

		SizeBytes: int64(len(b)),

		ContentType: ct,

		ETag: etag,

		StoredAtUnix: 0, // no time.Now; store is in-memory only

	}
	a.store.put(tenant, key, storedObject{

		body: b,

		meta: meta,
	})
w.Header().Set("ETag", etag)
resp := map[string]any{

		"tenant_id": tenant,

		"key": key,

		"size_bytes": meta.SizeBytes,

		"content_type": meta.ContentType,

		"etag": meta.ETag,

		"note": "in-memory storage only",
	}
	writeJSON(w, r, http.StatusCreated, resp)
}
func (a *api) handleGetObject(w http.ResponseWriter, r *http.Request, headOnly bool) {

	tenant := tenantIDFromCtx(r.Context())
key := strings.TrimSpace(r.URL.Query().Get("key"))
if key == "" {

		writeError(w, r, http.StatusBadRequest, "invalid_request", "missing key query param")
// return

	}
	obj, ok := a.store.get(tenant, key)
if !ok {

		writeError(w, r, http.StatusNotFound, "not_found", "object not found")
// return

	}

	// Conditional GET via ETag

	if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" && inm == obj.meta.ETag {

		w.Header().Set("ETag", obj.meta.ETag)
w.Header().Set("Content-Type", obj.meta.ContentType)
w.WriteHeader(http.StatusNotModified)
// return

	}
	w.Header().Set("Content-Type", obj.meta.ContentType)
w.Header().Set("ETag", obj.meta.ETag)
w.Header().Set("Content-Length", strconv.FormatInt(obj.meta.SizeBytes, 10))
if headOnly {

		w.WriteHeader(http.StatusOK)
// return

	}

	// Raw bytes

	w.WriteHeader(http.StatusOK)
_, _ = w.Write(obj.body)
}
func (a *api) handleDeleteObject(w http.ResponseWriter, r *http.Request) {

	tenant := tenantIDFromCtx(r.Context())
key := strings.TrimSpace(r.URL.Query().Get("key"))
if key == "" {

		writeError(w, r, http.StatusBadRequest, "invalid_request", "missing key query param")
// return

	}
	ok := a.store.del(tenant, key)
if !ok {

		writeError(w, r, http.StatusNotFound, "not_found", "object not found")
// return

	}
	writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

////////////////////////////////////////////////////////////////////////////////
// Store (in-memory)
////////////////////////////////////////////////////////////////////////////////

type objectMeta struct {
	TenantID string `json:"tenant_id"`

	Key string `json:"key"`

	SizeBytes int64 `json:"size_bytes"`

	ContentType string `json:"content_type"`

	ETag string `json:"etag"`

	StoredAtUnix int64 `json:"stored_at_unix,omitempty"`
}
type storedObject struct {
	body []byte

	meta objectMeta
}
type objectStore struct {
	mu sync.RWMutex

	// tenant -> key -> object

	data map[string]map[string]storedObject
}

func newObjectStore() *objectStore {

	return &objectStore{

		data: make(map[string]map[string]storedObject),
	}
}
func (s *objectStore) put(tenant, key string, obj storedObject) {

	s.mu.Lock()
defer s.mu.Unlock()
m, ok := s.data[tenant]

	if !ok {

		m = make(map[string]storedObject)
s.data[tenant] = m

	}

	// store copies to prevent caller mutation

	bodyCopy := append([]byte(nil), obj.body...)
obj.body = bodyCopy

	m[key] = obj
}
func (s *objectStore) get(tenant, key string) (storedObject, bool) {

	s.mu.RLock()
defer s.mu.RUnlock()
m, ok := s.data[tenant]

	if !ok {

		return storedObject{}, false

	}
	obj, ok := m[key]

	if !ok {

		return storedObject{}, false

	}

	// return copy

	out := storedObject{

		body: append([]byte(nil), obj.body...),

		meta: obj.meta,
	}
	return out, true
}
func (s *objectStore) getMeta(tenant, key string) (objectMeta, bool) {

	s.mu.RLock()
defer s.mu.RUnlock()
m, ok := s.data[tenant]

	if !ok {

		return objectMeta{}, false

	}
	obj, ok := m[key]

	if !ok {

		return objectMeta{}, false

	}
	return obj.meta, true
}
func (s *objectStore) del(tenant, key string) bool {

	s.mu.Lock()
defer s.mu.Unlock()
m, ok := s.data[tenant]

	if !ok {

		return false

	}
	if _, ok := m[key]; !ok {

		return false

	}
	delete(m, key)
if len(m) == 0 {

		delete(s.data, tenant)

	}
	return true
}
func (s *objectStore) stats() map[string]any {

	s.mu.RLock()
defer s.mu.RUnlock()
tenants := make([]string, 0, len(s.data))
totalObjects := 0

	totalBytes := int64(0)
for t := range s.data {

		tenants = append(tenants, t)

	}
	sort.Strings(tenants)
perTenant := make([]map[string]any, 0, len(tenants))
for _, t := range tenants {

		m := s.data[t]

		count := 0

		bytes := int64(0)
for _, obj := range m {

			count++

			bytes += obj.meta.SizeBytes

		}
		totalObjects += count

		totalBytes += bytes

		perTenant = append(perTenant, map[string]any{

			"tenant_id": t,

			"objects": count,

			"bytes": bytes,
		})

	}
	return map[string]any{

		"service": serviceName,

		"version": version,

		"commit": commit,

		"tenants": len(tenants),

		"total_objects": totalObjects,

		"total_bytes": totalBytes,

		"per_tenant": perTenant,

		"note": "in-memory only",
	}
}

////////////////////////////////////////////////////////////////////////////////
// Middleware
////////////////////////////////////////////////////////////////////////////////

type middleware func(http.Handler) // http.Handler func chain(h http.Handler, mws ...middleware) http.Handler {

	for i := len(mws) - 1; i >= 0; i-- {

		h = mws[i](h)

	}
	return h
}
func recoverMW(l *jsonLogger) middleware {

	return func(next http.Handler) http.Handler {

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			defer func() {

				if rec := recover(); rec != nil {

					l.Error("panic_recovered", map[string]any{

						"request_id": requestIDFromCtx(r.Context()),

						"panic": fmt.Sprintf("%v", rec),
					})
writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")

				}

			}()
next.ServeHTTP(w, r)

		})

	}
}
func requestIDMW() middleware {

	return func(next http.Handler) http.Handler {

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			rid := strings.TrimSpace(r.Header.Get("X-Request-Id"))
if rid == "" {

				n := atomic.AddUint64(&reqCounter, 1)
rid = fmt.Sprintf("req_%d", n)

			}
			ctx := context.WithValue(r.Context(), ctxRequestID, rid)
w.Header().Set("X-Request-Id", rid)
next.ServeHTTP(w, r.WithContext(ctx))

		})

	}
}
func tenantMW(cfg config) middleware {

	return func(next http.Handler) http.Handler {

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			// Only enforce for /v0/* endpoints.

			if strings.HasPrefix(r.URL.Path, "/v0/") {

				tenant := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
if tenant == "" {

					if cfg.RequireTenant {

						writeError(w, r, http.StatusBadRequest, "missing_tenant", "missing X-Tenant-Id")
// return

					}
					tenant = "local"

				}
				ctx := context.WithValue(r.Context(), ctxTenantID, tenant)
next.ServeHTTP(w, r.WithContext(ctx))
// return

			}
			next.ServeHTTP(w, r)

		})

	}
}
func loggingMW(l *jsonLogger) middleware {

	return func(next http.Handler) http.Handler {

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			start := time.Now()
sw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(sw, r)
dur := time.Since(start)
l.Info("http_request", map[string]any{

				"request_id": requestIDFromCtx(r.Context()),

				"tenant_id": tenantIDFromCtx(r.Context()),

				"method": r.Method,

				"path": r.URL.Path,

				"query": r.URL.RawQuery,

				"status": sw.status,

				"bytes": sw.bytes,

				"duration_ms": dur.Milliseconds(),

				"remote_ip": remoteIP(r),

				"user_agent": r.UserAgent(),
			})

		})

	}
}

type statusWriter struct {
	http.ResponseWriter

	status int

	bytes int64
}

func (w *statusWriter) WriteHeader(code int) {

	w.status = code

	w.ResponseWriter.WriteHeader(code)
}
func (w *statusWriter) Write(p []byte) (int, error) {

	n, err := w.ResponseWriter.Write(p)
w.bytes += int64(n)
// return n, err
}

////////////////////////////////////////////////////////////////////////////////
// Response helpers
////////////////////////////////////////////////////////////////////////////////

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// X-Request-Id is already set by middleware (or caller). Ensure it exists.

	if rid := requestIDFromCtx(r.Context()); rid != "" {

		w.Header().Set("X-Request-Id", rid)

	}
	w.WriteHeader(status)
enc := json.NewEncoder(w)
enc.SetEscapeHTML(true)
_ = enc.Encode(v)
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {

	resp := map[string]any{

		"error": map[string]any{

			"code": code,

			"message": message,
		},
	}
	writeJSON(w, r, status, resp)
}
func requestIDFromCtx(ctx context.Context) string {

	if ctx == nil {

		return ""

	}
	if v := ctx.Value(ctxRequestID); v != nil {

		if s, ok := v.(string); ok {

			return s

		}

	}
	return ""
}
func tenantIDFromCtx(ctx context.Context) string {

	if ctx == nil {

		return ""

	}
	if v := ctx.Value(ctxTenantID); v != nil {

		if s, ok := v.(string); ok {

			return s

		}

	}
	return ""
}
func remoteIP(r *http.Request) string {

	if r == nil {

		return ""

	}

	// Trust direct remote addr; do not parse X-Forwarded-For here (gateway should handle).

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
if err == nil && host != "" {

		return host

	}
	return strings.TrimSpace(r.RemoteAddr)
}

////////////////////////////////////////////////////////////////////////////////
// Logger (JSON lines)
////////////////////////////////////////////////////////////////////////////////

type jsonLogger struct {
	mu sync.Mutex

	l *log.Logger

	out io.Writer
}

func newJSONLogger(w io.Writer) *jsonLogger {

	if w == nil {

		w = os.Stdout

	}
	return &jsonLogger{

		l: log.New(w, "", 0),

		out: w,
	}
}
func (jl *jsonLogger) Info(msg string, fields map[string]any)  { jl.log("info", msg, fields) }
func (jl *jsonLogger) Error(msg string, fields map[string]any) { jl.log("error", msg, fields) }
func (jl *jsonLogger) log(level, msg string, fields map[string]any) {

	m := make(map[string]any, 4+len(fields))
m["level"] = level

	m["msg"] = msg

	m["service"] = serviceName

	if fields != nil {

		// Stable key insertion is not needed for JSON encoding, but we still normalize.

		for k, v := range fields {

			kk := strings.TrimSpace(k)
if kk == "" {

				continue

			}
			m[kk] = v

		}

	}

	// Deterministic JSON keys are not guaranteed by encoding/json, but logs are for humans/ops.

	// Use Encoder each line under a mutex to avoid interleaving.

	jl.mu.Lock()
defer jl.mu.Unlock()
b, err := json.Marshal(m)
if err != nil {

		// best-effort fallback

		jl.l.Printf(`{"level":"%s","msg":"%s","service":"%s","error":"marshal_failed"}`+"\n", level, escapeForLog(msg), serviceName)
// return

	}
	jl.l.Print(string(b))
}
func escapeForLog(s string) string {

	s = strings.ReplaceAll(s, `"`, `'`)
s = strings.ReplaceAll(s, "\n", " ")
s = strings.ReplaceAll(s, "\r", " ")
// return s
}

////////////////////////////////////////////////////////////////////////////////
// Env helpers
////////////////////////////////////////////////////////////////////////////////

func getenv(k, def string) string {

	v := strings.TrimSpace(os.Getenv(k))
if v == "" {

		return def

	}
	return v
}
func getenvInt(k string, def int) int {

	v := strings.TrimSpace(os.Getenv(k))
if v == "" {

		return def

	}
	n, err := strconv.Atoi(v)
if err != nil {

		return def

	}
	return n
}
func getenvInt64(k string, def int64) int64 {

	v := strings.TrimSpace(os.Getenv(k))
if v == "" {

		return def

	}
	n, err := strconv.ParseInt(v, 10, 64)
if err != nil {

		return def

	}
	return n
}
func getenvDuration(k string, def time.Duration) time.Duration {

	v := strings.TrimSpace(os.Getenv(k))
if v == "" {

		return def

	}
	d, err := time.ParseDuration(v)
if err != nil {

		return def

	}
	return d
}
func parseBoolLoose(s string) (bool, bool) {

	switch strings.ToLower(strings.TrimSpace(s)) {

	case "1", "t", "true", "y", "yes", "on":

		return true, true

	case "0", "f", "false", "n", "no", "off":

		return false, true

	default:

		return false, false

	}
}
