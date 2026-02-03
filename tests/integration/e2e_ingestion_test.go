package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func getenv(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def

	}
	return v
}
func mustLeadingSlash(p string) string {
	if p == "" {
		return p

	}
	if strings.HasPrefix(p, "/") {
		return p

	}
	return "/" + p
}
func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	path = mustLeadingSlash(path)
	return base + path
}
func readBodySnippet(r io.Reader, max int64) string {
	b, _ := io.ReadAll(io.LimitReader(r, max))
	s := string(b)
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
func traceparentConst() string {
	// Valid W3C traceparent format; constant is fine for deterministic tests.
	return "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
}
func doReq(t *testing.T, client *http.Client, req *http.Request) (int, string) {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http request failed: %v", err)

	}
	defer resp.Body.Close()
	snippet := readBodySnippet(resp.Body, 2048)
	return resp.StatusCode, snippet
}
func TestE2E_Ingestion(t *testing.T) {
	// SAFE BY DEFAULT: require explicit opt-in.
	if strings.TrimSpace(os.Getenv("CHARTLY_E2E")) != "1" {
		t.Skip("skipping e2e: set CHARTLY_E2E=1 to enable")

	}
	baseURL := strings.TrimSpace(os.Getenv("CHARTLY_BASE_URL"))
	if baseURL == "" {
		t.Skip("skipping e2e: CHARTLY_BASE_URL not set")

	}
	healthPath := getenv("CHARTLY_HEALTH_PATH", "/health")
	readyPath := getenv("CHARTLY_READY_PATH", "/ready")
	ingestPath := getenv("CHARTLY_INGEST_PATH", "/v1/events")
	tenantID := strings.TrimSpace(os.Getenv("CHARTLY_TENANT_ID"))
	requestID := strings.TrimSpace(os.Getenv("CHARTLY_REQUEST_ID"))
	if requestID == "" {
		requestID = fmt.Sprintf("e2e-%d", os.Getpid())

	}
	timeoutMS := getenv("CHARTLY_TIMEOUT_MS", "10000")
	perReqTimeout, err := time.ParseDuration(timeoutMS + "ms")
	if err != nil {
		t.Fatalf("invalid CHARTLY_TIMEOUT_MS=%q: %v", timeoutMS, err)

	} // Per-request timeout enforced by client.Timeout (not a shared suite-budget context).
	client := &http.Client{Timeout: perReqTimeout}
	commonHeaders := func(req *http.Request) {
		req.Header.Set("accept", "application/json")
		req.Header.Set("x-request-id", requestID)
		req.Header.Set("traceparent", traceparentConst())
		if tenantID != "" {
			req.Header.Set("x-chartly-tenant", tenantID)

		}

	} // 1) /health

	{
		url := joinURL(baseURL, healthPath)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		commonHeaders(req)
		code, body := doReq(t, client, req)
		if code < 200 || code > 299 {
			t.Fatalf("GET %s expected 2xx, got %d. body:\n%s", url, code, body)

		}

	} // 2) /ready

	{
		url := joinURL(baseURL, readyPath)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		commonHeaders(req)
		code, body := doReq(t, client, req)
		if code < 200 || code > 299 {
			t.Fatalf("GET %s expected 2xx, got %d. body:\n%s", url, code, body)

		}

	} // 3)
	// ingestion POST
	payload := map[string]any{
		"kind":    "e2e_ingestion_test",
		"ts_unix": time.Now().Unix(),
		"msg":     "hello from Chartly e2e ingestion test",
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal payload failed: %v", err)

	}
	{
		url := joinURL(baseURL, ingestPath)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(bodyBytes))
		commonHeaders(req)
		req.Header.Set("content-type", "application/json")
		code, body := doReq(t, client, req)
		if code < 200 || code > 299 {
			t.Fatalf(
				"POST %s expected 2xx, got %d. body:\n%s\n\nHint: set CHARTLY_INGEST_PATH to the correct endpoint if /v1/events is not implemented.",
				url, code, body,
			)
		}
	}
}
