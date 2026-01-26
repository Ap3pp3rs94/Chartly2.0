package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type queryEcho struct {
	SourceID   string `json:"source_id"`
	MetricName string `json:"metric_name,omitempty"`
	Start      string `json:"start,omitempty"`
	End        string `json:"end,omitempty"`
	Limit      int    `json:"limit"`
}

type queryStubResp struct {
	Error   errResp   `json:"error"`
	Request queryEcho `json:"request"`
}

func envIsLocalQuery() bool {
	env := strings.TrimSpace(os.Getenv("CHARTLY_ENV"))
	if env == "" {
		env = "local"
	}
	return strings.EqualFold(env, "local")
}

func tenantFromHeaderQuery(r *http.Request) (string, bool) {
	t := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if t == "" {
		if envIsLocalQuery() {
			return "local", true
		}
		return "", false
	}
	return t, true
}

func parseRFC3339Optional(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", true
	}
	if _, err := time.Parse(time.RFC3339, v); err == nil {
		return v, true
	}
	if _, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return v, true
	}
	return "", false
}

func Query(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromHeaderQuery(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing_tenant", "X-Tenant-Id header is required")
		return
	}

	q := r.URL.Query()
	sourceID := strings.TrimSpace(q.Get("source_id"))
	if sourceID == "" {
		writeErr(w, http.StatusBadRequest, "missing_source_id", "source_id is required")
		return
	}

	metric := strings.TrimSpace(q.Get("metric_name"))

	start, ok := parseRFC3339Optional(q.Get("start"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_start", "start must be RFC3339")
		return
	}

	end, ok := parseRFC3339Optional(q.Get("end"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_end", "end must be RFC3339")
		return
	}

	limit := 1000
	if ls := strings.TrimSpace(q.Get("limit")); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n < 1 || n > 50000 {
			writeErr(w, http.StatusBadRequest, "invalid_limit", "limit must be 1..50000")
			return
		}
		limit = n
	}

	// Validate source exists and enabled (tenant-scoped)
	list := sources.list(tenantID)
	var src *Source
	for i := range list {
		if list[i].ID == sourceID {
			src = &list[i]
			break
		}
	}
	if src == nil {
		writeErr(w, http.StatusNotFound, "source_not_found", "source not found")
		return
	}
	if !src.Enabled {
		writeErr(w, http.StatusConflict, "source_disabled", "source is disabled")
		return
	}

	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotImplemented)

	var resp queryStubResp
	resp.Error.Error.Code = "not_implemented"
	resp.Error.Error.Message = "query not implemented"
	resp.Request = queryEcho{
		SourceID:   sourceID,
		MetricName: metric,
		Start:      start,
		End:        end,
		Limit:      limit,
	}

	_ = json.NewEncoder(w).Encode(resp)
}
