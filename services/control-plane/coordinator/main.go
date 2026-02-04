package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

const (
	defaultPort        = "8083"
	defaultRegistryURL = "http://localhost:8081"
	heartbeatWindow    = 5 * time.Minute
)

type Drone struct {
	ID               string    `json:"id"`
	Status           string    `json:"status"`
	LastHeartbeat    time.Time `json:"last_heartbeat"`
	RegisteredAt     time.Time `json:"registered_at"`
	AssignedProfiles []string  `json:"assigned_profiles"`
}

type profileListItem struct {
	ID      string `json:"id"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type server struct {
	mu     sync.RWMutex
	drones map[string]*Drone
	force  map[string]map[string]struct{}

	registryURL string
	client      *http.Client
}

func main() {
	regURL := strings.TrimSpace(os.Getenv("REGISTRY_URL"))
	if regURL == "" {
		regURL = defaultRegistryURL
	}

	s := &server{
		drones:      make(map[string]*Drone),
		force:       make(map[string]map[string]struct{}),
		registryURL: regURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	go s.sweeper()

	r := mux.NewRouter()

	r.HandleFunc("/health", s.handleHealth).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/drones/register", s.handleRegister).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/drones/heartbeat", s.handleHeartbeat).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/drones", s.handleList).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/drones/stats", s.handleStats).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/drones/{id}/work", s.handleWork).Methods(http.MethodGet, http.MethodOptions)

	r.HandleFunc("/profiles/{id}:runNow", s.handleRunNow).Methods(http.MethodPost, http.MethodOptions)

	handler := withRequestLogging(withCORS(withAuth(r)))

	addr := ":" + defaultPort
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logLine("INFO", "starting", "addr=%s registry_url=%s", addr, regURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logLine("ERROR", "listen_failed", "err=%s", err.Error())
		os.Exit(1)
	}
}

func (s *server) sweeper() {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()

	for range t.C {
		now := time.Now().UTC()
		s.mu.Lock()
		for _, d := range s.drones {
			if !d.LastHeartbeat.IsZero() && now.Sub(d.LastHeartbeat) > heartbeatWindow {
				d.Status = "offline"
			}
		}
		s.mu.Unlock()
	}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	active := s.countActive()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "healthy",
		"active_drones": active,
	})
}

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	type req struct {
		ID string `json:"id"`
	}
	var in req
	if err := decodeJSONStrict(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	in.ID = strings.TrimSpace(in.ID)
	if in.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}

	profiles, err := s.fetchEnabledProfileIDs()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "registry_unavailable"})
		return
	}
	sort.Strings(profiles)

	now := time.Now().UTC()

	s.mu.Lock()
	d, ok := s.drones[in.ID]
	if !ok {
		d = &Drone{ID: in.ID, RegisteredAt: now}
		s.drones[in.ID] = d
	}
	d.AssignedProfiles = profiles
	d.LastHeartbeat = now
	d.Status = "idle"
	if _, ok := s.force[in.ID]; !ok {
		s.force[in.ID] = make(map[string]struct{})
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, d)
}

func (s *server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	type req struct {
		ID string `json:"id"`
	}
	var in req
	if err := decodeJSONStrict(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	in.ID = strings.TrimSpace(in.ID)
	if in.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}

	now := time.Now().UTC()

	s.mu.Lock()
	d, ok := s.drones[in.ID]
	if !ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return
	}
	d.LastHeartbeat = now
	d.Status = "active"
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, d)
}

func (s *server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	now := time.Now().UTC()
	out := make([]*Drone, 0, 32)

	s.mu.RLock()
	for _, d := range s.drones {
		if !d.LastHeartbeat.IsZero() && now.Sub(d.LastHeartbeat) <= heartbeatWindow {
			out = append(out, d)
		}
	}
	s.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	now := time.Now().UTC()

	total := 0
	active := 0
	offline := 0

	s.mu.RLock()
	for _, d := range s.drones {
		total++
		if d.LastHeartbeat.IsZero() {
			offline++
			continue
		}
		if now.Sub(d.LastHeartbeat) > heartbeatWindow {
			offline++
			continue
		}
		if d.Status == "active" {
			active++
		}
	}
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"total":   total,
		"active":  active,
		"offline": offline,
	})
}

func (s *server) handleRunNow(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}

	s.mu.Lock()
	queued := 0
	for droneID := range s.drones {
		if _, ok := s.force[droneID]; !ok {
			s.force[droneID] = make(map[string]struct{})
		}
		if _, exists := s.force[droneID][id]; !exists {
			s.force[droneID][id] = struct{}{}
			queued++
		}
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"queued_for": queued,
		"profile_id": id,
	})
}

func (s *server) handleWork(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}

	s.mu.Lock()
	if _, ok := s.drones[id]; !ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return
	}
	q := s.force[id]
	out := make([]string, 0, len(q))
	for pid := range q {
		out = append(out, pid)
	}
	sort.Strings(out)
	s.force[id] = make(map[string]struct{})
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"drone_id": id,
		"profiles": out,
	})
}

func (s *server) countActive() int {
	now := time.Now().UTC()
	n := 0
	s.mu.RLock()
	for _, d := range s.drones {
		if !d.LastHeartbeat.IsZero() && now.Sub(d.LastHeartbeat) <= heartbeatWindow {
			n++
		}
	}
	s.mu.RUnlock()
	return n
}

func (s *server) fetchEnabledProfileIDs() ([]string, error) {
	url := strings.TrimRight(s.registryURL, "/") + "/profiles"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("registry_status_%d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	var arr []profileListItem
	if err := json.Unmarshal(b, &arr); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(arr))
	for _, p := range arr {
		enabled := true
		if p.Enabled != nil {
			enabled = *p.Enabled
		}
		if enabled {
			id := strings.TrimSpace(p.ID)
			if id != "" {
				out = append(out, id)
			}
		}
	}
	return out, nil
}

func decodeJSONStrict(r *http.Request, v any) error {
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

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
		if r.Method == http.MethodOptions || r.URL.Path == "/health" {
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
