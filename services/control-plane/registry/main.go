package main

import (
	"bytes"
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
)

type Profile struct {
	ID      string `json:"id" yaml:"id"`
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version" yaml:"version"`
	Content string `json:"content"`
}

type profileYAML struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

type store struct {
	mu          sync.RWMutex
	profiles    map[string]Profile
	profilesDir string
}

func main() {
	profilesDir := strings.TrimSpace(os.Getenv("PROFILES_DIR"))
	if profilesDir == "" {
		profilesDir = defaultProfilesDir

	}
	st := &store{
		profiles:    make(map[string]Profile),
		profilesDir: profilesDir,
	}
	_ = st.loadAll() // Do not crash if missing

	r := mux.NewRouter()

	r.Use(requestLoggingMiddleware)

	// Health
	r.HandleFunc("/health", st.handleHealth).Methods(http.MethodGet, http.MethodOptions)

	// Profiles
	r.HandleFunc("/profiles", st.handleProfilesList).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/profiles", st.handleProfilesCreate).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/profiles/{id}", st.handleProfileGet).Methods(http.MethodGet, http.MethodOptions)

	// Wrap with CORS last (runs after logging wrapper prelude, still handles OPTIONS deterministically)
	handler := withCORS(r)

	addr := ":" + defaultPort
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	logLine("INFO", "starting", "addr=%s profiles_dir=%s", addr, profilesDir)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logLine("ERROR", "listen_failed", "err=%s", err.Error())
		os.Exit(1)

	}
}

func (s *store) loadAll() error {
	entries, err := os.ReadDir(s.profilesDir)
	if err != nil {
		// Missing directory is allowed by contract; start empty.
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
		content := string(b)
		meta, perr := parseProfileYAML(content)
		if perr != nil || strings.TrimSpace(meta.ID) == "" {
			logLine("WARN", "profile_parse_failed", "file=%s err=%s", name, errString(perr))
			continue

		}
		p := Profile{
			ID:      strings.TrimSpace(meta.ID),
			Name:    strings.TrimSpace(meta.Name),
			Version: strings.TrimSpace(meta.Version),
			Content: content,
		}
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
	dec.KnownFields(false) // allow forward-compatible fields in YAML
	if err := dec.Decode(&meta); err != nil {
		return profileYAML{}, err

	}
	return meta, nil
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
	id := mux.Vars(r)["id"]
	id = strings.TrimSpace(id)

	s.mu.RLock()
	p, ok := s.profiles[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return

	}
	writeJSON(w, http.StatusOK, p)
}

type createProfileRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Content string `json:"content"`
}

var safeIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func (s *store) handleProfilesCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return

	}
	// API key gating: header + env must be set and match.
	envKey := strings.TrimSpace(os.Getenv("REGISTRY_API_KEY"))
	if envKey == "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "api_key_not_configured"})
		return

	}
	hKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if hKey == "" || hKey != envKey {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
		return

	}
	body, berr := io.ReadAll(io.LimitReader(r.Body, 8<<20)) // 8MiB hard cap
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
	// Validate YAML, ensure id consistency if YAML includes id.
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

	// Ensure output dir exists (even if it didn't at startup)
	if err := os.MkdirAll(s.profilesDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return

	}
	content := req.Content
	if !strings.HasSuffix(content, "\n") {
		content += "\n"

	}
	dst := filepath.Join(s.profilesDir, req.ID+".yaml")

	// Atomic write: temp file in same directory then rename.
	tmp, err := os.CreateTemp(s.profilesDir, req.ID+".tmp-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return

	}
	tmpName := tmp.Name()
	_, werr := tmp.Write([]byte(content))
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

	// Reload store entry deterministically from file bytes.
	meta2, perr2 := parseProfileYAML(content)
	if perr2 != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return

	}
	p := Profile{
		ID:      req.ID,
		Name:    firstNonEmpty(strings.TrimSpace(meta2.Name), req.Name),
		Version: firstNonEmpty(strings.TrimSpace(meta2.Version), req.Version),
		Content: content,
	}

	s.mu.Lock()
	s.profiles[p.ID] = p
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, p)
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
		// Allow-all CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return

		}
		next.ServeHTTP(w, r)
	})
}

func logLine(level, msg string, format string, args ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stdout, "%s %s %s %s\n", ts, level, msg, line)
}
