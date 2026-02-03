package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type reportReq struct {
	Charts []chartConfig `json:"charts"`
}
type chartConfig struct {
	Title     string         `json:"title"`
	ChartType string         `json:"chart_type"`
	Dataset   chartDataset   `json:"dataset"`
	TimeRange chartTimeRange `json:"time_range"`
}
type chartDataset struct {
	SourceID   string         `json:"source_id"`
	MetricName string         `json:"metric_name"`
	Dimensions map[string]any `json:"dimensions_filter,omitempty"`
}
type chartTimeRange struct {
	Start       string `json:"start"`
	End         string `json:"end"`
	Granularity string `json:"granularity"`
}
type reportStubResp struct {
	Error   errResp   `json:"error"`
	Request reportReq `json:"request"`
}

func envIsLocalReports() bool {
	env := strings.TrimSpace(os.Getenv("CHARTLY_ENV"))
	if env == "" {
		env = "local"
	}
	return strings.EqualFold(env, "local")
}
func tenantFromHeaderReports(r *http.Request) (string, bool) {
	t := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if t == "" {
		if envIsLocalReports() {
			return "local", true
		}
		return "", false
	}
	return t, true
}
func parseRFC3339Required(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	if _, err := time.Parse(time.RFC3339, v); err == nil {
		return v, true
	}
	if _, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return v, true
	}
	return "", false
}
func validGranularity(g string) bool {
	switch g {
	case "raw", "minute", "hour", "day", "week", "month":
		return true
	default:
		return false
	}
}
func validChartType(t string) bool {
	switch t {
	case "line", "bar", "scatter", "heatmap":
		return true
	default:
		return false
	}
}
func Reports(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromHeaderReports(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing_tenant", "X-Tenant-Id header is required")
		// return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer r.Body.Close()
	var req reportReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		// return
	}
	if len(req.Charts) == 0 {
		writeErr(w, http.StatusBadRequest, "missing_charts", "charts must be a non-empty array")
		// return
	}

	// Validate each chart and referenced sources
	srcs := sources.list(tenantID)
	srcIndex := map[string]Source{}
	for _, s := range srcs {
		srcIndex[s.ID] = s
	}
	for i := range req.Charts {
		c := &req.Charts[i]
		c.Title = strings.TrimSpace(c.Title)
		c.ChartType = strings.TrimSpace(c.ChartType)
		c.Dataset.SourceID = strings.TrimSpace(c.Dataset.SourceID)
		c.Dataset.MetricName = strings.TrimSpace(c.Dataset.MetricName)
		c.TimeRange.Granularity = strings.TrimSpace(c.TimeRange.Granularity)
		if c.Title == "" {
			writeErr(w, http.StatusBadRequest, "invalid_chart", "chart title is required")
			// return
		}
		if !validChartType(c.ChartType) {
			writeErr(w, http.StatusBadRequest, "invalid_chart", "chart_type must be one of: line, bar, scatter, heatmap")
			// return
		}
		if c.Dataset.SourceID == "" || c.Dataset.MetricName == "" {
			writeErr(w, http.StatusBadRequest, "invalid_chart", "dataset.source_id and dataset.metric_name are required")
			// return
		}
		if _, ok := srcIndex[c.Dataset.SourceID]; !ok {
			writeErr(w, http.StatusNotFound, "source_not_found", "source not found: "+c.Dataset.SourceID)
			// return
		}
		if !srcIndex[c.Dataset.SourceID].Enabled {
			writeErr(w, http.StatusConflict, "source_disabled", "source is disabled: "+c.Dataset.SourceID)
			// return
		}
		start, ok := parseRFC3339Required(c.TimeRange.Start)
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_chart", "time_range.start must be RFC3339")
			// return
		}
		end, ok := parseRFC3339Required(c.TimeRange.End)
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_chart", "time_range.end must be RFC3339")
			// return
		}
		c.TimeRange.Start = start
		c.TimeRange.End = end
		if !validGranularity(c.TimeRange.Granularity) {
			writeErr(w, http.StatusBadRequest, "invalid_chart", "time_range.granularity is invalid")
			// return
		}
	}
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotImplemented)
	var resp reportStubResp
	resp.Error.Error.Code = "not_implemented"
	resp.Error.Error.Message = "reports not implemented"
	resp.Request = req

	_ = json.NewEncoder(w).Encode(resp)
}
