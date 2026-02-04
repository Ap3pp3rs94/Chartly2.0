package handlers

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLimit     = 100
	defaultMaxJoined = 200
	maxLimit         = 5000
	maxJoined        = 2000
)

type correlateReq struct {
	DatasetA datasetSpec `json:"dataset_a"`
	DatasetB datasetSpec `json:"dataset_b"`
	Limit    int         `json:"limit"`
	MaxJoin  int         `json:"max_joined"`
}

type datasetSpec struct {
	ProfileID    string `json:"profile_id"`
	JoinKey      string `json:"join_key"`
	NumericField string `json:"numeric_field"`
}

type correlateResp struct {
	JoinedCount int               `json:"joined_count"`
	Correlation *correlateStats   `json:"correlation,omitempty"`
	Preview     []correlateRecord `json:"preview"`
}

type correlateStats struct {
	Coefficient    float64 `json:"coefficient"`
	PValue         float64 `json:"p_value"`
	Interpretation string  `json:"interpretation"`
}

type correlateRecord struct {
	JoinValue string      `json:"join_value"`
	AValue    interface{} `json:"a_value"`
	BValue    interface{} `json:"b_value"`
	ALabel    string      `json:"a_label"`
	BLabel    string      `json:"b_label"`
}

type resultRow struct {
	ID        string          `json:"id"`
	DroneID   string          `json:"drone_id"`
	ProfileID string          `json:"profile_id"`
	RunID     string          `json:"run_id,omitempty"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// Correlate handles POST /api/analytics/correlate
func Correlate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req correlateReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	if err := validateReq(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	resp, err := runCorrelate(req)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "correlate_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// CorrelateExport handles GET /api/analytics/correlate/export
// Expect a URL-encoded JSON spec in "spec" query parameter.
func CorrelateExport(w http.ResponseWriter, r *http.Request) {
	spec := strings.TrimSpace(r.URL.Query().Get("spec"))
	if spec == "" {
		writeErr(w, http.StatusBadRequest, "missing_spec", "spec query parameter is required")
		return
	}
	var req correlateReq
	if err := json.Unmarshal([]byte(spec), &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_spec", "spec must be valid JSON")
		return
	}
	if err := validateReq(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	resp, err := runCorrelate(req)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "correlate_failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"correlate.csv\"")
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"join_value", "a_value", "b_value", "a_label", "b_label"})
	for _, row := range resp.Preview {
		_ = cw.Write([]string{
			row.JoinValue,
			fmt.Sprint(row.AValue),
			fmt.Sprint(row.BValue),
			row.ALabel,
			row.BLabel,
		})
	}
	cw.Flush()
}

func validateReq(req *correlateReq) error {
	req.DatasetA.ProfileID = strings.TrimSpace(req.DatasetA.ProfileID)
	req.DatasetB.ProfileID = strings.TrimSpace(req.DatasetB.ProfileID)
	req.DatasetA.JoinKey = strings.TrimSpace(req.DatasetA.JoinKey)
	req.DatasetB.JoinKey = strings.TrimSpace(req.DatasetB.JoinKey)
	req.DatasetA.NumericField = strings.TrimSpace(req.DatasetA.NumericField)
	req.DatasetB.NumericField = strings.TrimSpace(req.DatasetB.NumericField)

	if req.DatasetA.ProfileID == "" || req.DatasetB.ProfileID == "" {
		return errors.New("profile_id is required for both datasets")
	}
	if req.DatasetA.JoinKey == "" || req.DatasetB.JoinKey == "" {
		return errors.New("join_key is required for both datasets")
	}
	if req.Limit <= 0 {
		req.Limit = defaultLimit
	}
	if req.Limit > maxLimit {
		req.Limit = maxLimit
	}
	if req.MaxJoin <= 0 {
		req.MaxJoin = defaultMaxJoined
	}
	if req.MaxJoin > maxJoined {
		req.MaxJoin = maxJoined
	}
	return nil
}

func runCorrelate(req correlateReq) (correlateResp, error) {
	aRows, err := fetchResults(req.DatasetA.ProfileID, req.Limit)
	if err != nil {
		return correlateResp{}, err
	}
	bRows, err := fetchResults(req.DatasetB.ProfileID, req.Limit)
	if err != nil {
		return correlateResp{}, err
	}

	aRecords := expandRows(aRows)
	bRecords := expandRows(bRows)

	joined := joinRecords(aRecords, bRecords, req.DatasetA.JoinKey, req.DatasetB.JoinKey)
	if len(joined) > req.MaxJoin {
		joined = joined[:req.MaxJoin]
	}

	preview := make([]correlateRecord, 0, len(joined))
	for _, j := range joined {
		preview = append(preview, correlateRecord{
			JoinValue: j.Key,
			AValue:    getPath(j.A, req.DatasetA.NumericField),
			BValue:    getPath(j.B, req.DatasetB.NumericField),
			ALabel:    req.DatasetA.NumericField,
			BLabel:    req.DatasetB.NumericField,
		})
	}

	var corr *correlateStats
	if req.DatasetA.NumericField != "" && req.DatasetB.NumericField != "" {
		r, n := pearson(joined, req.DatasetA.NumericField, req.DatasetB.NumericField)
		p := pValueFromR(r, n)
		corr = &correlateStats{
			Coefficient:    r,
			PValue:         p,
			Interpretation: interpret(r),
		}
	}

	return correlateResp{
		JoinedCount: len(joined),
		Correlation: corr,
		Preview:     preview,
	}, nil
}

type joinRow struct {
	Key string
	A   map[string]any
	B   map[string]any
}

func joinRecords(a, b []map[string]any, pathA, pathB string) []joinRow {
	index := map[string][]map[string]any{}
	for _, rec := range a {
		k := getPath(rec, pathA)
		if k == nil {
			continue
		}
		ks := fmt.Sprint(k)
		index[ks] = append(index[ks], rec)
	}

	out := make([]joinRow, 0)
	for _, rec := range b {
		k := getPath(rec, pathB)
		if k == nil {
			continue
		}
		ks := fmt.Sprint(k)
		if list, ok := index[ks]; ok && len(list) > 0 {
			out = append(out, joinRow{Key: ks, A: list[0], B: rec})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Key == out[j].Key {
			return fmt.Sprint(out[i].Key) < fmt.Sprint(out[j].Key)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func fetchResults(profileID string, limit int) ([]resultRow, error) {
	base := strings.TrimRight(aggregatorURL(), "/")
	u := fmt.Sprintf("%s/results?profile_id=%s&limit=%d", base, urlQueryEscape(profileID), limit)
	req, _ := http.NewRequest(http.MethodGet, u, nil)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("aggregator_status_%d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var rows []resultRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func aggregatorURL() string {
	v := strings.TrimSpace(os.Getenv("AGGREGATOR_URL"))
	if v != "" {
		return v
	}
	return "http://aggregator:8082"
}

func expandRows(rows []resultRow) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if len(r.Data) == 0 {
			continue
		}
		var v interface{}
		if err := json.Unmarshal(r.Data, &v); err != nil {
			continue
		}
		switch t := v.(type) {
		case []interface{}:
			for _, item := range t {
				if m, ok := item.(map[string]any); ok {
					out = append(out, m)
				}
			}
		case map[string]any:
			out = append(out, t)
		default:
			// ignore
		}
	}
	return out
}

func getPath(obj map[string]any, path string) interface{} {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil
	}
	normalized := strings.ReplaceAll(p, "[", ".")
	normalized = strings.ReplaceAll(normalized, "]", "")
	parts := strings.Split(normalized, ".")
	cur := interface{}(obj)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch node := cur.(type) {
		case map[string]any:
			cur = node[part]
		case []interface{}:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil
			}
			cur = node[i]
		default:
			return nil
		}
	}
	return cur
}

func toNumber(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, false
		}
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		s := strings.TrimSpace(strings.ReplaceAll(t, ",", ""))
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func pearson(rows []joinRow, pathA, pathB string) (float64, int) {
	xs := make([]float64, 0)
	ys := make([]float64, 0)
	for _, r := range rows {
		av, ok := toNumber(getPath(r.A, pathA))
		if !ok {
			continue
		}
		bv, ok := toNumber(getPath(r.B, pathB))
		if !ok {
			continue
		}
		xs = append(xs, av)
		ys = append(ys, bv)
	}
	n := len(xs)
	if n < 2 {
		return 0, n
	}
	var sx, sy float64
	for i := 0; i < n; i++ {
		sx += xs[i]
		sy += ys[i]
	}
	mx := sx / float64(n)
	my := sy / float64(n)
	var num, dx, dy float64
	for i := 0; i < n; i++ {
		vx := xs[i] - mx
		vy := ys[i] - my
		num += vx * vy
		dx += vx * vx
		dy += vy * vy
	}
	den := math.Sqrt(dx * dy)
	if den == 0 {
		return 0, n
	}
	return num / den, n
}

func pValueFromR(r float64, n int) float64 {
	if n < 3 {
		return 1.0
	}
	t := math.Abs(r) * math.Sqrt(float64(n-2)/(1-r*r))
	// normal approximation for two-tailed p-value
	p := 2 * (1 - normalCDF(t))
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	return p
}

func normalCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}

func interpret(r float64) string {
	abs := math.Abs(r)
	switch {
	case abs >= 0.8:
		return "Strong correlation"
	case abs >= 0.6:
		return "Moderate correlation"
	case abs >= 0.4:
		return "Weak correlation"
	default:
		return "Very weak correlation"
	}
}

// -------------------- utilities (local) --------------------

type errResp struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	var e errResp
	e.Error.Code = code
	e.Error.Message = msg
	_ = json.NewEncoder(w).Encode(e)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func urlQueryEscape(s string) string {
	b := &bytes.Buffer{}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			_ = b.WriteByte(c)
		} else {
			_, _ = fmt.Fprintf(b, "%%%02X", c)
		}
	}
	return b.String()
}
