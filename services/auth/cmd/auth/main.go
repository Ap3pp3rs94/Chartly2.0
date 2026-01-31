package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	HMACSecret      []byte
}

type tokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type tokenClaims struct {
	TenantID  string   `json:"tenant_id"`
	Subject   string   `json:"subject"`
	IssuedAt  string   `json:"issued_at"`  // RFC3339/RFC3339Nano (caller-provided)
	ExpiresAt string   `json:"expires_at"` // RFC3339/RFC3339Nano (caller-provided)
	Scopes    []string `json:"scopes,omitempty"`
	TokenID   string   `json:"token_id"` // deterministic id
	RequestID string   `json:"request_id,omitempty"`
}

type issueRequest struct {
	Subject   string   `json:"subject"`
	IssuedAt  string   `json:"issued_at"`
	ExpiresAt string   `json:"expires_at"`
	Scopes    []string `json:"scopes,omitempty"`
}

type verifyRequest struct {
	Token string `json:"token"`
}

type revokeRequest struct {
	Token string `json:"token"`
}

type server struct {
	cfg  config
	reqN uint64

	mu      sync.Mutex
	revoked map[string]struct{} // token_id -> revoked
}

func main() {
	cfg := loadConfig()

	// Enforce secret in non-local environments.
	if strings.ToLower(cfg.Env) != "local" && len(cfg.HMACSecret) == 0 {
		logJSON("error", "missing_secret", map[string]any{"env": cfg.Env})
		os.Exit(1)
	}

	s := &server{
		cfg:     cfg,
		revoked: make(map[string]struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/v0/token", s.withMiddleware(s.handleIssue))
	mux.HandleFunc("/v0/verify", s.withMiddleware(s.handleVerify))
	mux.HandleFunc("/v0/revoke", s.withMiddleware(s.handleRevoke))

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
		logJSON("info", "auth_server_start", map[string]any{
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

	logJSON("info", "auth_server_stopped", map[string]any{"addr": h.Addr})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleReady(w http.ResponseWriter, r *http.Request) {
	// v0 is always ready (in-memory).
	writeJSON(w, http.StatusOK, map[string]any{"ready": true})
}

func (s *server) handleIssue(w http.ResponseWriter, r *http.Request, tenantID, reqID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	var in issueRequest
	if err := decodeJSONStrict(r.Body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	sub := normCollapse(in.Subject)
	if sub == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "subject required"})
		return
	}

	iat := normCollapse(in.IssuedAt)
	exp := normCollapse(in.ExpiresAt)
	ti, err := parseRFC3339(iat)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "issued_at must be rfc3339"})
		return
	}
	te, err := parseRFC3339(exp)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "expires_at must be rfc3339"})
		return
	}
	if te.Before(ti) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "expires_at must be >= issued_at"})
		return
	}

	scopes := normalizeScopes(in.Scopes)

	claims := tokenClaims{
		TenantID:  tenantID,
		Subject:   sub,
		IssuedAt:  ti.UTC().Format(time.RFC3339Nano),
		ExpiresAt: te.UTC().Format(time.RFC3339Nano),
		Scopes:    scopes,
		RequestID: reqID,
	}
	claims.TokenID = deterministicTokenID(claims)

	tok, err := signToken(s.cfg.HMACSecret, claims)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "sign failed"})
		return
	}

	logJSON("info", "token_issued", map[string]any{
		"tenant_id":  tenantID,
		"subject":    sub,
		"token_id":   claims.TokenID,
		"request_id": reqID,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"token":  tok,
		"claims": claims,
	})
}

func (s *server) handleVerify(w http.ResponseWriter, r *http.Request, tenantID, reqID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	var in verifyRequest
	if err := decodeJSONStrict(r.Body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	tok := strings.TrimSpace(in.Token)
	if tok == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "token required"})
		return
	}

	claims, err := verifyToken(s.cfg.HMACSecret, tok)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid token"})
		return
	}

	// Enforce tenant header scope: token tenant must match request tenant.
	if claims.TenantID != tenantID {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "tenant mismatch"})
		return
	}

	// Check revocation
	if s.isRevoked(claims.TokenID) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "revoked"})
		return
	}

	// Check expiration (requires time.Now for runtime validity)
	now := time.Now().UTC()
	exp, err := parseRFC3339(claims.ExpiresAt)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid exp"})
		return
	}
	if !now.Before(exp) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "expired"})
		return
	}

	logJSON("info", "token_verified", map[string]any{
		"tenant_id":  tenantID,
		"token_id":   claims.TokenID,
		"request_id": reqID,
	})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "claims": claims})
}

func (s *server) handleRevoke(w http.ResponseWriter, r *http.Request, tenantID, reqID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	var in revokeRequest
	if err := decodeJSONStrict(r.Body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	tok := strings.TrimSpace(in.Token)
	if tok == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "token required"})
		return
	}

	claims, err := verifyToken(s.cfg.HMACSecret, tok)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid token"})
		return
	}

	if claims.TenantID != tenantID {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "tenant mismatch"})
		return
	}

	s.revoke(claims.TokenID)

	logJSON("info", "token_revoked", map[string]any{
		"tenant_id":  tenantID,
		"token_id":   claims.TokenID,
		"request_id": reqID,
	})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) revoke(tokenID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoked[tokenID] = struct{}{}
}

func (s *server) isRevoked(tokenID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.revoked[tokenID]
	return ok
}

func (s *server) withMiddleware(next func(http.ResponseWriter, *http.Request, string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Size limits
		if s.cfg.MaxBodyBytes > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		}

		reqID := s.requestID(r)
		w.Header().Set("X-Request-Id", reqID)

		tenantID, err := s.tenantID(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		// Tenant required for /v0/* unless local env; local defaults to "local".
		if strings.HasPrefix(r.URL.Path, "/v0/") && strings.ToLower(s.cfg.Env) != "local" {
			if tenantID == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing tenant header"})
				return
			}
		}

		if strings.ToLower(s.cfg.Env) == "local" && tenantID == "" {
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
	return hex.EncodeToString(sum[:16])
}

func (s *server) tenantID(r *http.Request) (string, error) {
	h := s.cfg.TenantHeader
	if h == "" {
		h = "X-Tenant-Id"
	}
	return strings.TrimSpace(r.Header.Get(h)), nil
}

////////////////////////////////////////////////////////////////////////////////
// Token signing / verification (HS256)
////////////////////////////////////////////////////////////////////////////////

func signToken(secret []byte, claims tokenClaims) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("missing secret")
	}

	h := tokenHeader{Alg: "HS256", Typ: "JWT"}
	hb, err := json.Marshal(h)
	if err != nil {
		return "", err
	}

	// Ensure scopes are sorted deterministically before signing.
	c := claims
	c.Scopes = normalizeScopes(c.Scopes)

	pb, err := json.Marshal(c)
	if err != nil {
		return "", err
	}

	h64 := b64url(hb)
	p64 := b64url(pb)
	unsigned := h64 + "." + p64

	sig := hmacSHA256(secret, []byte(unsigned))
	t64 := b64url(sig)

	return unsigned + "." + t64, nil
}

func verifyToken(secret []byte, tok string) (tokenClaims, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("bad token")
	}

	unsigned := parts[0] + "." + parts[1]
	want := hmacSHA256(secret, []byte(unsigned))
	got, err := b64urlDecode(parts[2])
	if err != nil {
		return tokenClaims{}, errors.New("bad sig")
	}

	if !hmac.Equal(want, got) {
		return tokenClaims{}, errors.New("sig mismatch")
	}

	pb, err := b64urlDecode(parts[1])
	if err != nil {
		return tokenClaims{}, errors.New("bad payload")
	}

	var c tokenClaims
	dec := json.NewDecoder(strings.NewReader(string(pb)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return tokenClaims{}, errors.New("bad claims")
	}

	// Validate time fields deterministically.
	ti, err := parseRFC3339(c.IssuedAt)
	if err != nil {
		return tokenClaims{}, errors.New("bad iat")
	}
	te, err := parseRFC3339(c.ExpiresAt)
	if err != nil {
		return tokenClaims{}, errors.New("bad exp")
	}
	if te.Before(ti) {
		return tokenClaims{}, errors.New("exp before iat")
	}

	c.Scopes = normalizeScopes(c.Scopes)
	if normCollapse(c.TokenID) == "" {
		c.TokenID = deterministicTokenID(c)
	}

	return c, nil
}

func deterministicTokenID(c tokenClaims) string {
	parts := []string{
		normCollapse(c.TenantID),
		normCollapse(c.Subject),
		normCollapse(c.IssuedAt),
		normCollapse(c.ExpiresAt),
		strings.Join(normalizeScopes(c.Scopes), ","),
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:16])
}

func normalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}

	tmp := make([]string, 0, len(scopes))
	for _, s := range scopes {
		n := normCollapse(s)
		if n == "" {
			continue
		}
		tmp = append(tmp, n)
	}

	sort.Strings(tmp)

	// Dedup deterministically
	out := make([]string, 0, len(tmp))
	var last string
	for _, s := range tmp {
		if s != last {
			out = append(out, s)
			last = s
		}
	}

	return out
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func hmacSHA256(secret []byte, data []byte) []byte {
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write(data)
	return m.Sum(nil)
}

////////////////////////////////////////////////////////////////////////////////
// Utilities
////////////////////////////////////////////////////////////////////////////////

func loadConfig() config {
	env := strings.TrimSpace(getenv("AUTH_ENV", "local"))
	addr := strings.TrimSpace(getenv("AUTH_ADDR", "0.0.0.0"))
	port := atoiDefault(getenv("AUTH_PORT", "8085"), 8085)

	readTO := parseDuration(getenv("AUTH_READ_TIMEOUT", "10s"), 10*time.Second)
	writeTO := parseDuration(getenv("AUTH_WRITE_TIMEOUT", "10s"), 10*time.Second)
	idleTO := parseDuration(getenv("AUTH_IDLE_TIMEOUT", "60s"), 60*time.Second)
	shutTO := parseDuration(getenv("AUTH_SHUTDOWN_TIMEOUT", "10s"), 10*time.Second)

	maxBody := atoi64Default(getenv("AUTH_MAX_BODY_BYTES", "1048576"), 1048576)
	maxHdr := atoiDefault(getenv("AUTH_MAX_HEADER_BYTES", "32768"), 32768)

	tenantHeader := getenv("AUTH_TENANT_HEADER", "X-Tenant-Id")
	localTenant := getenv("AUTH_LOCAL_TENANT", "local")

	secret := getenv("AUTH_HMAC_SECRET", "")
	if strings.ToLower(env) == "local" && strings.TrimSpace(secret) == "" {
		secret = "dev-secret"
	}

	secB := []byte(secret)

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
		TenantHeader:    tenantHeader,
		LocalTenant:     localTenant,
		HMACSecret:      secB,
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
	} else if err != io.EOF {
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
	// Stable encoding: convert fields into sorted kv array.
	type kv struct {
		K string `json:"k"`
		V any    `json:"v"`
	}

	keys := make([]string, 0, len(fields))
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
	s = normCollapse(s)
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
		port = 8085
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

func normCollapse(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
