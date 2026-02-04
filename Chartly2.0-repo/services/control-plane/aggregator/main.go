package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
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
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	defaultPort = "8082"
	dbPath      = "/app/data/results.db"
)

type resultIn struct {
	DroneID   string            `json:"drone_id"`
	ProfileID string            `json:"profile_id"`
	RunID     string            `json:"run_id"`
	Data      []json.RawMessage `json:"data"`
}

type runIn struct {
	RunID      string `json:"run_id"`
	DroneID    string `json:"drone_id"`
	ProfileID  string `json:"profile_id"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Status     string `json:"status"`
	RowsOut    int64  `json:"rows_out"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error"`
}

type runRow struct {
	RunID      string `json:"run_id"`
	DroneID    string `json:"drone_id"`
	ProfileID  string `json:"profile_id"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Status     string `json:"status"`
	RowsOut    int64  `json:"rows_out"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error"`
}

type serviceDetail struct {
	Status string `json:"status"`
}

type server struct {
	db *sql.DB
}

func main() {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		logLine("ERROR", "mkdir_failed", "err=%s", err.Error())
		os.Exit(1)
	}

	dsn := fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=ON", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		logLine("ERROR", "db_open_failed", "err=%s", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	s := &server{db: db}
	if err := s.initSchema(); err != nil {
		logLine("ERROR", "schema_init_failed", "err=%s", err.Error())
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/results", s.handleResults)
	mux.HandleFunc("/results/summary", s.handleSummary)
	mux.HandleFunc("/records", s.handleRecords)
	mux.HandleFunc("/runs", s.handleRuns)
	mux.HandleFunc("/runs/", s.handleRunGet)

	h := withCORS(withRequestLogging(mux))

	addr := ":" + defaultPort
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	logLine("INFO", "starting", "addr=%s db=%s", addr, dbPath)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	run_id TEXT,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	data TEXT NOT NULL
	);`,
		`CREATE INDEX IF NOT EXISTS idx_results_drone ON results(drone_id);`,
		`CREATE INDEX IF NOT EXISTS idx_results_profile ON results(profile_id);`,
		`CREATE INDEX IF NOT EXISTS idx_results_run ON results(run_id);`,

		`CREATE TABLE IF NOT EXISTS records (
	record_id TEXT NOT NULL,
	profile_id TEXT NOT NULL,
	run_id TEXT NOT NULL,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	data TEXT NOT NULL,
	PRIMARY KEY(record_id, profile_id)
	);`,
		`CREATE INDEX IF NOT EXISTS idx_records_profile ON records(profile_id);`,
		`CREATE INDEX IF NOT EXISTS idx_records_run ON records(run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_records_ts ON records(timestamp);`,

		`CREATE TABLE IF NOT EXISTS runs (
	run_id TEXT PRIMARY KEY,
	drone_id TEXT NOT NULL,
	profile_id TEXT NOT NULL,
	started_at DATETIME NOT NULL,
	finished_at DATETIME,
	status TEXT NOT NULL,
	rows_out INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL,
	error TEXT
	);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_profile ON runs(profile_id);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_drone ON runs(drone_id);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_started ON runs(started_at);`,
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

	totalResults, err := s.count("results")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}
	totalRecords, err := s.count("records")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}
	totalRuns, err := s.count("runs")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "healthy",
		"total_results": totalResults,
		"total_records": totalRecords,
		"total_runs":    totalRuns,
	})
}

func (s *server) handleResults(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handleResultsPost(w, r)
	case http.MethodGet:
		s.handleResultsGet(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
	}
}

func (s *server) handleResultsPost(w http.ResponseWriter, r *http.Request) {
	var in resultIn
	if err := decodeJSONStrict(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}

	in.DroneID = strings.TrimSpace(in.DroneID)
	in.ProfileID = strings.TrimSpace(in.ProfileID)
	in.RunID = strings.TrimSpace(in.RunID)
	if in.DroneID == "" || in.ProfileID == "" || in.RunID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_fields"})
		return
	}

	insertedResults := 0
	insertedRecords := 0
	dedupedRecords := 0

	for _, raw := range in.Data {
		canon, err := canonicalJSON(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_record_json"})
			return
		}

		recordID := recordIDFromJSON(canon)
		// insert into records (dedupe)
		res, err := s.db.Exec(`INSERT OR IGNORE INTO records(record_id, profile_id, run_id, data) VALUES(?,?,?,?)`,
			recordID, in.ProfileID, in.RunID, string(canon))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
			return
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			dedupedRecords++
		} else {
			insertedRecords++
		}

		// append to results
		id, err := newUUIDv4()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "uuid_failed"})
			return
		}
		if _, err := s.db.Exec(`INSERT INTO results(id, drone_id, profile_id, run_id, data) VALUES(?,?,?,?,?)`,
			id, in.DroneID, in.ProfileID, in.RunID, string(canon)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
			return
		}
		insertedResults++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"inserted_results": insertedResults,
		"inserted_records": insertedRecords,
		"deduped_records":  dedupedRecords,
		"run_id":           in.RunID,
	})
}

func (s *server) handleResultsGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := parseLimit(q.Get("limit"))

	sqlq := `SELECT id, drone_id, profile_id, run_id, timestamp, data FROM results ORDER BY timestamp DESC, id ASC LIMIT ?`
	rows, err := s.db.Query(sqlq, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}
	defer rows.Close()

	type row struct {
		ID        string          `json:"id"`
		DroneID   string          `json:"drone_id"`
		ProfileID string          `json:"profile_id"`
		RunID     string          `json:"run_id"`
		Timestamp string          `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
	}

	out := make([]row, 0, limit)
	for rows.Next() {
		var rrow row
		var dataStr string
		if err := rows.Scan(&rrow.ID, &rrow.DroneID, &rrow.ProfileID, &rrow.RunID, &rrow.Timestamp, &dataStr); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
			return
		}
		rrow.Data = json.RawMessage([]byte(dataStr))
		out = append(out, rrow)
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}

	q := r.URL.Query()
	profileID := strings.TrimSpace(q.Get("profile_id"))
	runID := strings.TrimSpace(q.Get("run_id"))
	limit := parseLimit(q.Get("limit"))

	sqlq := `SELECT data, timestamp, record_id FROM records`
	conds := make([]string, 0, 2)
	args := make([]any, 0, 3)
	if profileID != "" {
		conds = append(conds, "profile_id = ?")
		args = append(args, profileID)
	}
	if runID != "" {
		conds = append(conds, "run_id = ?")
		args = append(args, runID)
	}
	if len(conds) > 0 {
		sqlq += " WHERE " + strings.Join(conds, " AND ")
	}
	sqlq += " ORDER BY timestamp DESC, record_id ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(sqlq, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}
	defer rows.Close()

	out := make([]json.RawMessage, 0, limit)
	for rows.Next() {
		var dataStr string
		var ts string
		var rid string
		if err := rows.Scan(&dataStr, &ts, &rid); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
			return
		}
		out = append(out, json.RawMessage([]byte(dataStr)))
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handleRunsPost(w, r)
	case http.MethodGet:
		s.handleRunsGet(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
	}
}

func (s *server) handleRunsPost(w http.ResponseWriter, r *http.Request) {
	var in runIn
	if err := decodeJSONStrict(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}

	in.RunID = strings.TrimSpace(in.RunID)
	in.DroneID = strings.TrimSpace(in.DroneID)
	in.ProfileID = strings.TrimSpace(in.ProfileID)
	in.Status = strings.TrimSpace(in.Status)
	if in.RunID == "" || in.DroneID == "" || in.ProfileID == "" || in.Status == "" || in.StartedAt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_fields"})
		return
	}

	if _, err := time.Parse(time.RFC3339, in.StartedAt); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_started_at"})
		return
	}
	if strings.TrimSpace(in.FinishedAt) != "" {
		if _, err := time.Parse(time.RFC3339, in.FinishedAt); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_finished_at"})
			return
		}
	}

	in.Error = sanitizeError(in.Error)

	_, err := s.db.Exec(`INSERT OR REPLACE INTO runs(run_id, drone_id, profile_id, started_at, finished_at, status, rows_out, duration_ms, error)
	VALUES(?,?,?,?,?,?,?,?,?)`,
		in.RunID, in.DroneID, in.ProfileID, in.StartedAt, emptyToNull(in.FinishedAt), in.Status, in.RowsOut, in.DurationMs, emptyToNull(in.Error))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}

	row := runRow{
		RunID:      in.RunID,
		DroneID:    in.DroneID,
		ProfileID:  in.ProfileID,
		StartedAt:  in.StartedAt,
		FinishedAt: in.FinishedAt,
		Status:     in.Status,
		RowsOut:    in.RowsOut,
		DurationMs: in.DurationMs,
		Error:      in.Error,
	}

	writeJSON(w, http.StatusOK, row)
}

func (s *server) handleRunsGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	droneID := strings.TrimSpace(q.Get("drone_id"))
	profileID := strings.TrimSpace(q.Get("profile_id"))
	limit := parseLimit(q.Get("limit"))

	sqlq := `SELECT run_id, drone_id, profile_id, started_at, finished_at, status, rows_out, duration_ms, error FROM runs`
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
	sqlq += " ORDER BY started_at DESC, run_id ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(sqlq, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}
	defer rows.Close()

	out := make([]runRow, 0, limit)
	for rows.Next() {
		var rr runRow
		var finished sql.NullString
		var errStr sql.NullString
		if err := rows.Scan(&rr.RunID, &rr.DroneID, &rr.ProfileID, &rr.StartedAt, &finished, &rr.Status, &rr.RowsOut, &rr.DurationMs, &errStr); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
			return
		}
		if finished.Valid {
			rr.FinishedAt = finished.String
		}
		if errStr.Valid {
			rr.Error = errStr.String
		}
		out = append(out, rr)
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleRunGet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/runs/"), "/")
	runID := strings.TrimSpace(parts[0])
	if runID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_run_id"})
		return
	}

	var rr runRow
	var finished sql.NullString
	var errStr sql.NullString
	row := s.db.QueryRow(`SELECT run_id, drone_id, profile_id, started_at, finished_at, status, rows_out, duration_ms, error FROM runs WHERE run_id = ?`, runID)
	if err := row.Scan(&rr.RunID, &rr.DroneID, &rr.ProfileID, &rr.StartedAt, &finished, &rr.Status, &rr.RowsOut, &rr.DurationMs, &errStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "db_error"})
		return
	}
	if finished.Valid {
		rr.FinishedAt = finished.String
	}
	if errStr.Valid {
		rr.Error = errStr.String
	}

	writeJSON(w, http.StatusOK, rr)
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

	total, err := s.count("results")
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

func (s *server) count(table string) (int, error) {
	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func decodeJSONStrict(r *http.Request, v any) error {
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return json.Marshal(obj)
}

func recordIDFromJSON(canon []byte) string {
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := randRead(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32]), nil
}

func randRead(b []byte) (int, error) {
	f, err := os.Open("/dev/urandom")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Read(b)
}

var authRe = regexp.MustCompile(`(?i)(authorization\s*:\s*[^\s]+|bearer\s+[a-z0-9\-\._]+|token\s*[:=]\s*[^\s]+)`) // best-effort

func sanitizeError(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = authRe.ReplaceAllString(s, "[redacted]")
	if len(s) > 2048 {
		s = s[:2048]
	}
	return s
}

func emptyToNull(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func parseLimit(v string) int {
	limit := 100
	if strings.TrimSpace(v) != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}
	return limit
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
