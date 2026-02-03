package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	defaultPort = "8082"
	dbPath      = "/app/data/results.db"
)

type createResultRequest struct {
	DroneID   string          `json:"drone_id"`
	ProfileID string          `json:"profile_id"`
	Data      json.RawMessage `json:"data"`
}

type ResultRow struct {
	ID        string          `json:"id"`
	DroneID   string          `json:"drone_id"`
	ProfileID string          `json:"profile_id"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type server struct {
	db *sql.DB
}

func main() {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		logLine("ERROR", "mkdir_failed", "err=%s", err.Error())
		os.Exit(1)
	}

	// WAL + busy timeout, keep it simple and provider-neutral.
	dsn := fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=ON", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		logLine("ERROR", "db_open_failed", "err=%s", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(1) // sqlite best practice for simple services

	s := &server{db: db}
	if err := s.initSchema(); err != nil {
		logLine("ERROR", "schema_init_failed", "err=%s", err.Error())
		os.Exit(1)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/results/summary", s.handleSummary)
	mux.HandleFunc("/results", s.handleResults)

	handler := withRequestLogging(withCORS(mux))

	addr := ":" + defaultPort
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	logLine("INFO", "starting", "addr=%s db=%s", addr, dbPath)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logLine("ERROR", "listen_failed", "err=%s", err.Error())
		os.Exit(1)
	}
}

func (s *server) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS results (
	id TEXT PRIMARY KEY,
	drone_id TEXT NOT NULL,
	profile_id TEXT NOT NULL,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	data TEXT NOT NULL
	);`,
		`CREATE INDEX IF NOT EXISTS idx_drone ON results(drone_id);`,
		`CREATE INDEX IF NOT EXISTS idx_profile ON results(profile_id);`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}

	total, err := s.countTotal()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "healthy",
		"total_results": total,
	})
}

func (s *server) handleResults(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handleResultsCreate(w, r)
	case http.MethodGet:
		s.handleResultsQuery(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
	}
}

func (s *server) handleResultsCreate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_body"})
		return
	}
	defer r.Body.Close()

	var req createResultRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}

	req.DroneID = strings.TrimSpace(req.DroneID)
	req.ProfileID = strings.TrimSpace(req.ProfileID)
	if req.DroneID == "" || req.ProfileID == "" || len(req.Data) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_fields"})
		return
	}

	// Canonicalize data JSON deterministically (Go json sorts map keys).
	var anyData any
	if err := json.Unmarshal(req.Data, &anyData); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_data_json"})
		return
	}
	canon, _ := json.Marshal(anyData)

	id, err := newUUIDv4()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "uuid_failed"})
		return
	}

	_, err = s.db.Exec(`INSERT INTO results(id,drone_id,profile_id,data) VALUES(?,?,?,?)`,
		id, req.DroneID, req.ProfileID, string(canon))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}

	var ts string
	_ = s.db.QueryRow(`SELECT timestamp FROM results WHERE id = ?`, id).Scan(&ts)

	row := ResultRow{
		ID:        id,
		DroneID:   req.DroneID,
		ProfileID: req.ProfileID,
		Timestamp: ts,
		Data:      canon,
	}

	writeJSON(w, http.StatusCreated, row)
}

func (s *server) handleResultsQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	droneID := strings.TrimSpace(q.Get("drone_id"))
	profileID := strings.TrimSpace(q.Get("profile_id"))

	limit := 100
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_limit"})
			return
		}
		limit = n
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}

	sqlq := `SELECT id, drone_id, profile_id, timestamp, data FROM results`
	conds := make([]string, 0, 2)
	args := make([]any, 0, 3)

	if droneID != "" {
		conds = append(conds, "drone_id = ?")
		args = append(args, droneID)
	}
	if profileID != "" {
		conds = append(conds, "profile_id = ?")
		args = append(args, profileID)
	}
	if len(conds) > 0 {
		sqlq += " WHERE " + strings.Join(conds, " AND ")
	}
	ssqlq := sqlq + " ORDER BY timestamp DESC, id ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(ssqlq, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}
	defer rows.Close()

	out := make([]ResultRow, 0, limit)
	for rows.Next() {
		var rr ResultRow
		var dataStr string
		if err := rows.Scan(&rr.ID, &rr.DroneID, &rr.ProfileID, &rr.Timestamp, &dataStr); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
			return
		}
		rr.Data = json.RawMessage([]byte(dataStr))
		out = append(out, rr)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}

	total, err := s.countTotal()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}

	var unique int
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT drone_id) FROM results`).Scan(&unique); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}

	type profCount struct {
		ProfileID string `json:"profile_id"`
		Count     int    `json:"count"`
	}
	profiles := make([]profCount, 0, 16)

	rows, err := s.db.Query(`SELECT profile_id, COUNT(*) FROM results GROUP BY profile_id`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p profCount
		if err := rows.Scan(&p.ProfileID, &p.Count); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
			return
		}
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ProfileID < profiles[j].ProfileID })

	writeJSON(w, http.StatusOK, map[string]any{
		"total_results": total,
		"unique_drones": unique,
		"profiles":      profiles,
	})
}

func (s *server) countTotal() (int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM results`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	hexStr := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:32]), nil
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

func withRequestLogging(next http.Handler) http.Handler {
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Key")
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
