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
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort      = "8084"
	defaultAggURL    = "http://aggregator:8082"
	maxBodyBytes     = 8 << 20
	maxLimitPerInput = 5000
)

type reportSpec struct {
	Join   []string    `json:"join"`
	Inputs []inputSpec `json:"inputs"`
	Window windowSpec  `json:"window"`
	Output outputSpec  `json:"output"`
}

type inputSpec struct {
	ProfileID string `json:"profile_id"`
	Measure   string `json:"measure"`
}

type windowSpec struct {
	Limit int `json:"limit"`
}

type outputSpec struct {
	Type   string `json:"type"`
	Method string `json:"method"`
}

type server struct {
	aggURL string
	client *http.Client
}

type kv struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type tableRow struct {
	Join   []string `json:"join"`
	Values []kv     `json:"values"`
}

func main() {
	aggURL := strings.TrimSpace(os.Getenv("AGGREGATOR_URL"))
	if aggURL == "" {
		aggURL = defaultAggURL
	}

	s := &server{
		aggURL: aggURL,
		client: &http.Client{Timeout: 10 * time.Second},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/reports", s.handleReports)
	mux.HandleFunc("/reports/", s.handleReportGet)

	handler := withRequestLogging(withCORS(withAuth(mux)))

	addr := ":" + defaultPort
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	logLine("INFO", "starting", "addr=%s aggregator_url=%s", addr, aggURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logLine("ERROR", "listen_failed", "err=%s", err.Error())
		os.Exit(1)
	}
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
	writeJSON(w, http.StatusOK, map[string]any{"status": "healthy"})
}

func (s *server) handleReports(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_body"})
		return
	}
	defer r.Body.Close()

	var spec reportSpec
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}

	spec = normalizeSpec(spec)
	if len(spec.Join) == 0 || len(spec.Inputs) == 0 || spec.Output.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_spec"})
		return
	}

	limit := spec.Window.Limit
	if limit <= 0 || limit > maxLimitPerInput {
		limit = maxLimitPerInput
	}

	inputData := make([][]map[string]any, 0, len(spec.Inputs))
	for _, in := range spec.Inputs {
		rows, ferr := s.fetchRecords(in.ProfileID, limit)
		if ferr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "aggregator_unavailable"})
			return
		}
		inputData = append(inputData, rows)
	}

	joined := joinRecords(spec.Join, spec.Inputs, inputData)

	result := map[string]any{}
	if strings.EqualFold(spec.Output.Type, "table") {
		result["rows"] = joined
	} else if strings.EqualFold(spec.Output.Type, "correlation") {
		corr := computeCorrelation(joined, spec.Inputs)
		result["correlation"] = corr
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_output_type"})
		return
	}

	specBytes := canonicalJSONBytes(spec)
	sum := sha256.Sum256(specBytes)
	reportID := "sha256:" + hex.EncodeToString(sum[:])

	out := map[string]any{
		"report_id":  reportID,
		"created_at": nil,
		"spec_hash":  reportID,
		"result":     result,
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleReportGet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}

	writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_implemented_persistence"})
}

func normalizeSpec(spec reportSpec) reportSpec {
	joins := make([]string, 0, len(spec.Join))
	for _, j := range spec.Join {
		t := strings.TrimSpace(j)
		if t != "" {
			joins = append(joins, t)
		}
	}
	sort.Strings(joins)
	spec.Join = joins

	inputs := make([]inputSpec, 0, len(spec.Inputs))
	for _, in := range spec.Inputs {
		in.ProfileID = strings.TrimSpace(in.ProfileID)
		in.Measure = strings.TrimSpace(in.Measure)
		if in.ProfileID != "" && in.Measure != "" {
			inputs = append(inputs, in)
		}
	}
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].ProfileID == inputs[j].ProfileID {
			return inputs[i].Measure < inputs[j].Measure
		}
		return inputs[i].ProfileID < inputs[j].ProfileID
	})
	spec.Inputs = inputs

	spec.Output.Type = strings.TrimSpace(spec.Output.Type)
	spec.Output.Method = strings.TrimSpace(spec.Output.Method)

	return spec
}

func (s *server) fetchRecords(profileID string, limit int) ([]map[string]any, error) {
	q := url.Values{}
	q.Set("profile_id", profileID)
	q.Set("limit", strconv.Itoa(limit))
	urlStr := strings.TrimRight(s.aggURL, "/") + "/records?" + q.Encode()

	req, _ := http.NewRequest(http.MethodGet, urlStr, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("aggregator_status_%d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}

	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0, len(arr))
	for _, r := range arr {
		data := extractData(r)
		if data != nil {
			rows = append(rows, data)
		}
	}
	return rows, nil
}

func extractData(rec map[string]any) map[string]any {
	if rec == nil {
		return nil
	}
	if v, ok := rec["data"]; ok {
		switch t := v.(type) {
		case map[string]any:
			return t
		case string:
			var m map[string]any
			if err := json.Unmarshal([]byte(t), &m); err == nil {
				return m
			}
		}
	}
	return rec
}

func joinRecords(joinKeys []string, inputs []inputSpec, data [][]map[string]any) []tableRow {
	rows := map[string]map[string]any{}

	for idx, in := range inputs {
		for _, rec := range data[idx] {
			keyTuple := buildJoinTuple(joinKeys, rec)
			if keyTuple == "" {
				continue
			}
			row, ok := rows[keyTuple]
			if !ok {
				row = map[string]any{"join": parseJoinTuple(keyTuple)}
				rows[keyTuple] = row
			}
			val := getValueByPath(rec, in.Measure)
			row[in.ProfileID+":"+in.Measure] = val
		}
	}

	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]tableRow, 0, len(keys))
	for _, k := range keys {
		row := rows[k]
		join := []string{}
		if jv, ok := row["join"].([]string); ok {
			join = jv
		}

		vals := make([]kv, 0, len(row))
		keys2 := make([]string, 0, len(row))
		for kk := range row {
			if kk == "join" {
				continue
			}
			keys2 = append(keys2, kk)
		}
		sort.Strings(keys2)
		for _, kk := range keys2 {
			vals = append(vals, kv{Key: kk, Value: row[kk]})
		}
		out = append(out, tableRow{Join: join, Values: vals})
	}

	return out
}

func buildJoinTuple(joinKeys []string, rec map[string]any) string {
	vals := make([]string, 0, len(joinKeys))
	for _, k := range joinKeys {
		v := getValueByPath(rec, k)
		if v == nil {
			return ""
		}
		vals = append(vals, fmt.Sprintf("%v", v))
	}
	return strings.Join(vals, "|")
}

func parseJoinTuple(tuple string) []string {
	if tuple == "" {
		return []string{}
	}
	return strings.Split(tuple, "|")
}

func computeCorrelation(rows []tableRow, inputs []inputSpec) map[string]any {
	if len(inputs) < 2 {
		return map[string]any{"row_count": 0, "pearson": nil}
	}

	keyA := inputs[0].ProfileID + ":" + inputs[0].Measure
	keyB := inputs[1].ProfileID + ":" + inputs[1].Measure

	var xs, ys []float64
	for _, row := range rows {
		var aVal any
		var bVal any
		for _, v := range row.Values {
			if v.Key == keyA {
				aVal = v.Value
			} else if v.Key == keyB {
				bVal = v.Value
			}
		}
		xa, okA := parseNumber(aVal)
		ya, okB := parseNumber(bVal)
		if okA && okB {
			xs = append(xs, xa)
			ys = append(ys, ya)
		}
	}

	corr := pearson(xs, ys)
	return map[string]any{
		"row_count": len(xs),
		"pearson":   corr,
	}
}

func pearson(xs, ys []float64) any {
	n := len(xs)
	if n == 0 || n != len(ys) {
		return nil
	}

	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i := 0; i < n; i++ {
		x := xs[i]
		y := ys[i]
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
		sumY2 += y * y
	}

	num := float64(n)*sumXY - sumX*sumY
	den := mathSqrt((float64(n)*sumX2 - sumX*sumX) * (float64(n)*sumY2 - sumY*sumY))
	if den == 0 {
		return nil
	}
	return num / den
}

func parseNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return f, true
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

func getValueByPath(obj map[string]any, path string) any {
	parts := strings.Split(path, ".")
	var cur any = obj
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[p]
		if !ok {
			return nil
		}
	}
	return cur
}

func canonicalJSONBytes(v any) []byte {
	b, _ := json.Marshal(v)
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err == nil {
		return append(buf.Bytes(), '\n')
	}
	return append(b, '\n')
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Key, X-Principal, X-Tenant-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func mathSqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	x := v
	for i := 0; i < 8; i++ {
		x = 0.5 * (x + v/x)
	}
	return x
}

func logLine(level, msg, format string, args ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stdout, "%s %s %s %s\n", ts, level, msg, line)
}
