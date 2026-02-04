package handlers

import (
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const correlateTimeout = 20 * time.Second

// Correlate proxies POST /api/analytics/correlate to the analytics service.
func Correlate(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimRight(analyticsURL(), "/") + "/api/analytics/correlate"
	proxyJSON(w, r, target)
}

// CorrelateExport proxies GET /api/analytics/correlate/export to the analytics service.
func CorrelateExport(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimRight(analyticsURL(), "/") + "/api/analytics/correlate/export"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	proxyJSON(w, r, target)
}

func analyticsURL() string {
	v := strings.TrimSpace(os.Getenv("ANALYTICS_URL"))
	if v != "" {
		return v
	}
	return "http://analytics:8084"
}

func proxyJSON(w http.ResponseWriter, r *http.Request, target string) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "proxy_error", "failed to build request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: correlateTimeout}
	resp, err := client.Do(req)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "analytics_unavailable", "analytics service unavailable")
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
