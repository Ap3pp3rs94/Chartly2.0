package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"
)

const (
	defaultPort        = "8081"
	defaultProfilesDir = "/app/profiles/government"
	defaultAggURL      = "http://aggregator:8082"
)

type Profile struct {
	ID      string `json:"id" yaml:"id"`
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version" yaml:"version"`
	Digest  string `json:"digest" yaml:"-"`
	Content string `json:"content" yaml:"-"`

	Enabled  *bool   `json:"enabled,omitempty" yaml:"-"`
	Interval string  `json:"interval,omitempty" yaml:"-"`
	Jitter   string  `json:"jitter,omitempty" yaml:"-"`
	Limits   *Limits `json:"limits,omitempty" yaml:"-"`
}

type profileYAML struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

type Overrides struct {
	Enabled    *bool  `json:"enabled,omitempty"`
	Interval   string `json:"interval,omitempty"`
	Jitter     string `json:"jitter,omitempty"`
	MaxRecords *int   `json:"max_records,omitempty"`
	MaxPages   *int   `json:"max_pages,omitempty"`
	MaxBytes   *int   `json:"max_bytes,omitempty"`
}

type Limits struct {
	MaxRecords *int `json:"max_records,omitempty"`
	MaxPages   *int `json:"max_pages,omitempty"`
	MaxBytes   *int `json:"max_bytes,omitempty"`
}

type store struct {
	mu          sync.RWMutex
	profiles    map[string]Profile
	profilesDir string
	aggURL      string
	client      *http.Client
}

func main() {
	profilesDir := strings.TrimSpace(os.Getenv("PROFILES_DIR"))
	if profilesDir == "" {
		profilesDir = defaultProfilesDir
	}

	aggURL := strings.TrimSpace(os.Getenv("AGGREGATOR_URL"))
	if aggURL == "" {
		aggURL = defaultAggURL
	}

	s := &store{
		profiles:    make(map[string]Profile),
		profilesDir: profilesDir,
		aggURL:      aggURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
	_ = s.loadAll()

	r := mux.NewRouter()

	r.HandleFunc("/health", s.handleHealth).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/metrics", s.handleMetrics).Methods(http.MethodGet, http.MethodOptions)

	r.HandleFunc("/profiles", s.handleProfilesList).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/profiles", s.handleProfilesCreate).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/profiles/{id}", s.handleProfileGet).Methods(http.MethodGet, http.MethodOptions)

	r.HandleFunc("/profiles/{id}/status", s.handleProfileStatus).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/profiles/{id}:pause", s.handleProfilePause).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/profiles/{id}:resume", s.handleProfileResume).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/profiles/{id}:setSchedule", s.handleProfileSetSchedule).Methods(http.MethodPost, http.MethodOptions)

	handler := requestLoggingMiddleware(withCORS(withAuth(r)))

	addr := ":" + defaultPort
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logLine("INFO", "starting", "addr=%s profiles_dir=%s aggregator_url=%s", addr, profilesDir, aggURL)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logLine("ERROR", "listen_failed", "err=%s", err.Error())
		os.Exit(1)
	}
}

func (s *store) loadAll() error {
	entries, err := os.ReadDir(s.profilesDir)
	if err != nil {
		logLine("WARN", "profiles_dir_unavailable", "dir=%s err=%s", s.profilesDir, err.Error())
		return nil
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(strings.ToLower(n), ".yaml") {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	next := make(map[string]Profile)
	for _, name := range names {
		full := filepath.Join(s.profilesDir, name)
		b, rerr := os.ReadFile(full)
		if rerr != nil {
			logLine("WARN", "profile_read_failed", "file=%s err=%s", name, rerr.Error())
			continue
		}
		content := normalizeYAMLBytes(b)
		meta, perr := parseProfileYAML(string(content))
		if perr != nil || strings.TrimSpace(meta.ID) == "" {
			logLine("WARN", "profile_parse_failed", "file=%s err=%s", name, errString(perr))
			continue
		}
		p := Profile{
			ID:      strings.TrimSpace(meta.ID),
			Name:    strings.TrimSpace(meta.Name),
			Version: strings.TrimSpace(meta.Version),
			Digest:  digestBytes(content),
			Content: string(content),
		}
		p = s.applyOverrides(p)
		next[p.ID] = p
	}

	s.mu.Lock()
	s.profiles = next
	s.mu.Unlock()

	return nil
}

func parseProfileYAML(content string) (profileYAML, error) {
	var meta profileYAML
	dec := yaml.NewDecoder(strings.NewReader(content))
	dec.KnownFields(false)
	if err := dec.Decode(&meta); err != nil {
		return profileYAML{}, err
	}
	return meta, nil
}

func normalizeYAMLBytes(b []byte) []byte {
	out := bytes.TrimRight(b, "\r\n")
	out = append(out, '\n')
	return out
}

func digestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *store) overridesPath(id string) string {
	return filepath.Join(s.profilesDir, ".overrides", id+".json")
}

func (s *store) readOverrides(id string) (Overrides, error) {
	p := s.overridesPath(id)
	b, err := os.ReadFile(p)
	if err != nil {
		return Overrides{}, err
	}
	var o Overrides
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return Overrides{}, err
	}
	return o, nil
}

func (s *store) writeOverrides(id string, o Overrides) error {
	dir := filepath.Join(s.profilesDir, ".overrides")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(dir, id+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		return errors.New("write_failed")
	}
	dst := filepath.Join(dir, id+".json")
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func (s *store) applyOverrides(p Profile) Profile {
	o, err := s.readOverrides(p.ID)
	if err != nil {
		return p
	}
	if o.Enabled != nil {
		p.Enabled = o.Enabled
	}
	if strings.TrimSpace(o.Interval) != "" {
		p.Interval = strings.TrimSpace(o.Interval)
	}
	if strings.TrimSpace(o.Jitter) != "" {
		p.Jitter = strings.TrimSpace(o.Jitter)
	}
	if o.MaxRecords != nil || o.MaxPages != nil || o.MaxBytes != nil {
		p.Limits = &Limits{
			MaxRecords: o.MaxRecords,
			MaxPages:   o.MaxPages,
			MaxBytes:   o.MaxBytes,
		}
	}
	return p
}

func (s *store) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.mu.RLock()
	n := len(s.profiles)
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "healthy",
		"profiles_count": n,
	})
}

func (s *store) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	writeJSON(w, http.StatusOK, metricsSnapshot())
}

func (s *store) handleProfilesList(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.mu.RLock()
	out := make([]Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, p)
	}
	s.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, out)
}

func (s *store) handleProfileGet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	s.mu.RLock()
	p, ok := s.profiles[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return
	}

	writeJSON(w, http.StatusOK, p)
}

type statusBridge struct {
	ProfileID string         `json:"profile_id"`
	Digest    string         `json:"digest"`
	LastRun   map[string]any `json:"last_run"`
}

func (s *store) handleProfileStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	s.mu.RLock()
	p, ok := s.profiles[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return
	}

	last, err := s.fetchLastRun(id)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "aggregator_unavailable"})
		return
	}

	out := statusBridge{
		ProfileID: id,
		Digest:    p.Digest,
		LastRun:   last,
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *store) fetchLastRun(profileID string) (map[string]any, error) {
	url := strings.TrimRight(s.aggURL, "/") + "/runs?profile_id=" + urlQueryEscape(profileID) + "&limit=1"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("aggregator_status_%d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil {
		return nil, err
	}
	if len(arr) == 0 {
		return nil, nil
	}
	return arr[0], nil
}

func urlQueryEscape(s string) string {
	replacer := strings.NewReplacer(" ", "%20", "\n", "%0A", "\r", "%0D")
	return replacer.Replace(s)
}

type createProfileRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Content string `json:"content"`
}

type setScheduleRequest struct {
	Enabled  *bool   `json:"enabled,omitempty"`
	Interval string  `json:"interval,omitempty"`
	Jitter   string  `json:"jitter,omitempty"`
	Limits   *Limits `json:"limits,omitempty"`
}

var safeIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func (s *store) requireAPIKey(w http.ResponseWriter, r *http.Request) bool {
	envKey := strings.TrimSpace(os.Getenv("REGISTRY_API_KEY"))
	if envKey == "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "api_key_not_configured"})
		return false
	}
	hKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if hKey == "" || hKey != envKey {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
		return false
	}
	return true
}

func (s *store) handleProfilesCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !s.requireAPIKey(w, r) {
		return
	}

	body, berr := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if berr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_body"})
		return
	}
	defer r.Body.Close()

	var req createProfileRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.Version = strings.TrimSpace(req.Version)

	if req.ID == "" || !safeIDRe.MatchString(req.ID) || strings.Contains(req.ID, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_id"})
		return
	}
	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_content"})
		return
	}

	meta, perr := parseProfileYAML(req.Content)
	if perr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_yaml"})
		return
	}
	yid := strings.TrimSpace(meta.ID)
	if yid != "" && yid != req.ID {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id_mismatch"})
		return
	}

	if err := os.MkdirAll(s.profilesDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}

	content := normalizeYAMLBytes([]byte(req.Content))
	dst := filepath.Join(s.profilesDir, req.ID+".yaml")

	tmp, err := os.CreateTemp(s.profilesDir, req.ID+".tmp-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(content)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}

	meta2, perr2 := parseProfileYAML(string(content))
	if perr2 != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}

	p := Profile{
		ID:      req.ID,
		Name:    firstNonEmpty(strings.TrimSpace(meta2.Name), req.Name),
		Version: firstNonEmpty(strings.TrimSpace(meta2.Version), req.Version),
		Digest:  digestBytes(content),
		Content: string(content),
	}
	p = s.applyOverrides(p)

	s.mu.Lock()
	s.profiles[p.ID] = p
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, p)
}

func (s *store) handleProfilePause(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.requireAPIKey(w, r) {
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}

	o := Overrides{Enabled: boolPtr(false)}
	if err := s.writeOverrides(id, o); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}
	s.reloadProfile(id)
	writeJSON(w, http.StatusOK, map[string]any{"status": "paused", "id": id})
}

func (s *store) handleProfileResume(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.requireAPIKey(w, r) {
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}

	o := Overrides{Enabled: boolPtr(true)}
	if err := s.writeOverrides(id, o); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}
	s.reloadProfile(id)
	writeJSON(w, http.StatusOK, map[string]any{"status": "resumed", "id": id})
}

func (s *store) handleProfileSetSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.requireAPIKey(w, r) {
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}

	body, berr := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if berr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_body"})
		return
	}
	defer r.Body.Close()

	var req setScheduleRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}

	o := Overrides{}
	if req.Enabled != nil {
		o.Enabled = req.Enabled
	}
	if strings.TrimSpace(req.Interval) != "" {
		o.Interval = strings.TrimSpace(req.Interval)
	}
	if strings.TrimSpace(req.Jitter) != "" {
		o.Jitter = strings.TrimSpace(req.Jitter)
	}
	if req.Limits != nil {
		o.MaxRecords = req.Limits.MaxRecords
		o.MaxPages = req.Limits.MaxPages
		o.MaxBytes = req.Limits.MaxBytes
	}

	if err := s.writeOverrides(id, o); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}
	s.reloadProfile(id)

	writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "id": id})
}

func (s *store) reloadProfile(id string) {
	full := filepath.Join(s.profilesDir, id+".yaml")
	b, err := os.ReadFile(full)
	if err != nil {
		return
	}
	content := normalizeYAMLBytes(b)
	meta, perr := parseProfileYAML(string(content))
	if perr != nil || strings.TrimSpace(meta.ID) == "" {
		return
	}
	p := Profile{
		ID:      strings.TrimSpace(meta.ID),
		Name:    strings.TrimSpace(meta.Name),
		Version: strings.TrimSpace(meta.Version),
		Digest:  digestBytes(content),
		Content: string(content),
	}
	p = s.applyOverrides(p)

	s.mu.Lock()
	s.profiles[p.ID] = p
	s.mu.Unlock()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func boolPtr(v bool) *bool { return &v }

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func withAuth(next http.Handler) http.Handler {
	required := envBool("AUTH_REQUIRED", false)
	tenantRequired := envBool("AUTH_TENANT_REQUIRED", false)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		if !required {
			next.ServeHTTP(w, r)
			return
		}
		principal := strings.TrimSpace(r.Header.Get("X-Principal"))
		if principal == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		if tenantRequired {
			tenant := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
			if tenant == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "tenant_required"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// --- Middleware ---

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		dur := time.Since(start).Milliseconds()
		metricsRecord(rec.status, dur)
		level := "INFO"
		if rec.status >= 500 {
			level = "ERROR"
		} else if rec.status >= 400 {
			level = "WARN"
		}
		ts := time.Now().UTC().Format(time.RFC3339)
		fmt.Fprintf(os.Stdout, "%s %s method=%s path=%s status=%d duration_ms=%d\n",
			ts, level, r.Method, r.URL.Path, rec.status, dur)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Key, X-Principal, X-Tenant-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func logLine(level, msg, format string, args ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stdout, "%s %s %s %s\n", ts, level, msg, line)
}

// --- minimal metrics ---

var metricsMu sync.Mutex
var metricsReq int64
var metricsErr int64
var metricsDurMs int64

func metricsRecord(status int, durMs int64) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	metricsReq++
	if status >= 400 {
		metricsErr++
	}
	metricsDurMs += durMs
}

func metricsSnapshot() map[string]any {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	avg := int64(0)
	if metricsReq > 0 {
		avg = metricsDurMs / metricsReq
	}
	return map[string]any{
		"requests_total":  metricsReq,
		"errors_total":    metricsErr,
		"avg_duration_ms": avg,
	}
}
