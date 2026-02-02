package integration_test

import (
"bytes"
"encoding/json"
"fmt"
"io"
"net/http"
"os"
"strings"
"testing"
"time"
)

func getenvRG(k, def string) string {
v := strings.TrimSpace(os.Getenv(k))
if v == "" {
return def

}return v
}

func mustSlashRG(p string) string {
if p == "" {
return p

}if strings.HasPrefix(p, "/") {
return p

}return "/" + p
}

func joinURLRG(base, path string) string {
base = strings.TrimRight(base, "/")
path = mustSlashRG(path)
return base + path
}

func traceparentConstRG() string {
return "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
}

func readSnippetRG(r io.Reader, max int64) string {
b, _ := io.ReadAll(io.LimitReader(r, max))
s := string(b)
s = strings.ReplaceAll(s, "\r", "")
return s
}

func doReqRG(t *testing.T, c *http.Client, req *http.Request) (int, string) {
t.Helper()
resp, err := c.Do(req)
if err != nil {
t.Fatalf("http request failed: %v", err)

}defer resp.Body.Close()
return resp.StatusCode, readSnippetRG(resp.Body, 2048)
}

func parseIntEnvMS(t *testing.T, key, def string) time.Duration {
t.Helper()
ms := getenvRG(key, def)
d, err := time.ParseDuration(ms + "ms")
if err != nil {
t.Fatalf("invalid %s=%q: %v", key, ms, err)

}return d
}

func extractJobID(body string) string {
b := strings.TrimSpace(body)
if b == "" {
return ""

}// Try JSON first: {"job_id":"..."} or {"jobId":"..."}
var m map[string]any
if json.Unmarshal([]byte(b), &m) == nil {
if v, ok := m["job_id"]; ok {
if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
return strings.TrimSpace(s)

}
}if v, ok := m["jobId"]; ok {
if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
return strings.TrimSpace(s)

}
}
}// Fallback: treat as plain job id.
return b
}

func isReady(body string) bool {
b := strings.TrimSpace(strings.ToLower(body))
if b == "" {
return false

}// Plain signals
if b == "ready" || b == "done" || b == "complete" {
return true

}// JSON signals
var m map[string]any
if json.Unmarshal([]byte(body), &m) == nil {
if v, ok := m["status"]; ok {
if s, ok := v.(string); ok {
s = strings.ToLower(strings.TrimSpace(s))
if s == "ready" || s == "done" || s == "complete" {
return true

}
}
}if v, ok := m["ready"]; ok {
if bb, ok := v.(bool); ok && bb {
return true

}
}
}return false
}

func TestE2E_ReportGeneration(t *testing.T) {
if strings.TrimSpace(os.Getenv("CHARTLY_E2E")) != "1" {
t.Skip("skipping e2e: set CHARTLY_E2E=1 to enable")


}baseURL := strings.TrimSpace(os.Getenv("CHARTLY_BASE_URL"))
if baseURL == "" {
t.Skip("skipping e2e: CHARTLY_BASE_URL not set")


}healthPath := getenvRG("CHARTLY_HEALTH_PATH", "/health")
readyPath := getenvRG("CHARTLY_READY_PATH", "/ready")

genPath := getenvRG("CHARTLY_REPORT_GEN_PATH", "/v1/reports/generate")
statusPath := getenvRG("CHARTLY_REPORT_STATUS_PATH", "/v1/reports/status")
fetchPath := getenvRG("CHARTLY_REPORT_FETCH_PATH", "/v1/reports/fetch")

tenantID := strings.TrimSpace(os.Getenv("CHARTLY_TENANT_ID"))
requestID := strings.TrimSpace(os.Getenv("CHARTLY_REQUEST_ID"))
if requestID == "" {
requestID = fmt.Sprintf("e2e-%d", os.Getpid())


}perReqTimeout := parseIntEnvMS(t, "CHARTLY_TIMEOUT_MS", "15000")
pollInterval := parseIntEnvMS(t, "CHARTLY_POLL_INTERVAL_MS", "500")
pollBudget := parseIntEnvMS(t, "CHARTLY_POLL_TIMEOUT_MS", "20000")

client := &http.Client{Timeout: perReqTimeout}

commonHeaders := func(req *http.Request) {
req.Header.Set("accept", "application/json")
req.Header.Set("x-request-id", requestID)
req.Header.Set("traceparent", traceparentConstRG())
if tenantID != "" {
req.Header.Set("x-chartly-tenant", tenantID)

}

}// health

{url := joinURLRG(baseURL, healthPath)
req, _ := http.NewRequest(http.MethodGet, url, nil)
commonHeaders(req)
code, body := doReqRG(t, client, req)
if code < 200 || code > 299 {
t.Fatalf("GET %s expected 2xx, got %d. body:\n%s", url, code, body)

}

}// ready

{url := joinURLRG(baseURL, readyPath)
req, _ := http.NewRequest(http.MethodGet, url, nil)
commonHeaders(req)
code, body := doReqRG(t, client, req)
if code < 200 || code > 299 {
t.Fatalf("GET %s expected 2xx, got %d. body:\n%s", url, code, body)

}

}// POST report generation
reqBody := map[string]any{
"kind": "test_report",
"params": map[string]any{
"from": "2026-01-01",
"to":   "2026-01-31",
},

}bb, err := json.Marshal(reqBody)
if err != nil {
t.Fatalf("json.Marshal request failed: %v", err)


}genURL := joinURLRG(baseURL, genPath)
req, _ := http.NewRequest(http.MethodPost, genURL, bytes.NewReader(bb))
commonHeaders(req)
req.Header.Set("content-type", "application/json")

code, body := doReqRG(t, client, req)

// If synchronous success, assert non-empty body.
if code == 200 || code == 201 {
if strings.TrimSpace(body) == "" {
t.Fatalf("POST %s returned %d but body was empty", genURL, code)

}return


}// If async accepted, poll.
if code != 202 {
t.Fatalf("POST %s expected 200/201/202, got %d. body:\n%s", genURL, code, body)


}jobID := extractJobID(body)
if jobID == "" {
t.Fatalf("POST %s returned 202 but no job id found. body:\n%s", genURL, body)


}deadline := time.Now().Add(pollBudget)
statusURL := joinURLRG(baseURL, statusPath) + "/" + jobID
fetchURL := joinURLRG(baseURL, fetchPath) + "/" + jobID

// Poll status until ready
for {
if time.Now().After(deadline) {
t.Fatalf("poll timeout after %s. last status url=%s", pollBudget.String(), statusURL)


}sreq, _ := http.NewRequest(http.MethodGet, statusURL, nil)
commonHeaders(sreq)
scode, sbody := doReqRG(t, client, sreq)

if scode >= 200 && scode <= 299 && isReady(sbody) {
break


}time.Sleep(pollInterval)


}// Fetch report result
freq, _ := http.NewRequest(http.MethodGet, fetchURL, nil)
commonHeaders(freq)
fcode, fbody := doReqRG(t, client, freq)

if fcode < 200 || fcode > 299 {
t.Fatalf("GET %s expected 2xx, got %d. body:\n%s", fetchURL, fcode, fbody)

}if strings.TrimSpace(fbody) == "" {
t.Fatalf("GET %s returned %d but body was empty", fetchURL, fcode)

}}
