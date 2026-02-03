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

func getenvMT(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def

	}
	return v
}
func mustSlashMT(p string) string {
	if p == "" {
		return p

	}
	if strings.HasPrefix(p, "/") {
		return p

	}
	return "/" + p
}
func joinURLMT(base, path string) string {
	base = strings.TrimRight(base, "/")
	path = mustSlashMT(path)
	return base + path
}
func traceparentConstMT() string {
	return "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
}
func readSnippetMT(r io.Reader, max int64) string {
	b, _ := io.ReadAll(io.LimitReader(r, max))
	s := string(b)
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
func doReqMT(t *testing.T, c *http.Client, req *http.Request) (int, string) {
	t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("http request failed: %v", err)

	}
	defer resp.Body.Close()
	return resp.StatusCode, readSnippetMT(resp.Body, 2048)
}
func parseEnvMSMT(t *testing.T, key, def string) time.Duration {
	t.Helper()
	ms := getenvMT(key, def)
	d, err := time.ParseDuration(ms + "ms")
	if err != nil {
		t.Fatalf("invalid %s=%q: %v", key, ms, err)

	}
	return d
}
func containsMarker(body, marker string) bool {
	// We only claim what we control: the exact JSON-ish key/value pair for tenant_marker.
	// Accept both common spacing variants.
	return strings.Contains(body, `"tenant_marker":"`+marker+`"`) ||
		strings.Contains(body, `"tenant_marker": "`+marker+`"`)
}
func TestE2E_MultiTenantIsolation(t *testing.T) {
	if strings.TrimSpace(os.Getenv("CHARTLY_E2E")) != "1" {
		t.Skip("skipping e2e: set CHARTLY_E2E=1 to enable")

	}
	baseURL := strings.TrimSpace(os.Getenv("CHARTLY_BASE_URL"))
	if baseURL == "" {
		t.Skip("skipping e2e: CHARTLY_BASE_URL not set")

	}
	tenantA := strings.TrimSpace(os.Getenv("CHARTLY_TENANT_A"))
	tenantB := strings.TrimSpace(os.Getenv("CHARTLY_TENANT_B"))
	if tenantA == "" || tenantB == "" {
		t.Skip("skipping e2e: set CHARTLY_TENANT_A and CHARTLY_TENANT_B")

	}
	healthPath := getenvMT("CHARTLY_HEALTH_PATH", "/health")
	readyPath := getenvMT("CHARTLY_READY_PATH", "/ready")
	writePath := getenvMT("CHARTLY_MT_WRITE_PATH", "/v1/events")
	readPath := getenvMT("CHARTLY_MT_READ_PATH", "/v1/events/query")
	requestID := strings.TrimSpace(os.Getenv("CHARTLY_REQUEST_ID"))
	if requestID == "" {
		requestID = fmt.Sprintf("e2e-%d-mt", os.Getpid())

	}
	perReqTimeout := parseEnvMSMT(t, "CHARTLY_TIMEOUT_MS", "15000")
	client := &http.Client{Timeout: perReqTimeout}

	// Headers:
	// - request-id + traceparent always
	// - tenant header only for tenant-scoped operations
	commonHeadersBase := func(req *http.Request) {
		req.Header.Set("accept", "application/json")
		req.Header.Set("x-request-id", requestID)
		req.Header.Set("traceparent", traceparentConstMT())

	}
	commonHeadersTenant := func(req *http.Request, tenant string) {
		commonHeadersBase(req)
		req.Header.Set("x-chartly-tenant", tenant)

	} // health

	{
		url := joinURLMT(baseURL, healthPath)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		commonHeadersBase(req)
		code, body := doReqMT(t, client, req)
		if code < 200 || code > 299 {
			t.Fatalf("health failed url=%s status=%d body:\n%s", url, code, body)

		}

	} // ready

	{
		url := joinURLMT(baseURL, readyPath)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		commonHeadersBase(req)
		code, body := doReqMT(t, client, req)
		if code < 200 || code > 299 {
			t.Fatalf("ready failed url=%s status=%d body:\n%s", url, code, body)

		}

	}
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())

	// Write marker events
	writeEvent := func(tenant, marker string) {
		payload := map[string]any{
			"kind":          "mt_test",
			"tenant_marker": marker,
			"nonce":         nonce,
			"ts_unix":       time.Now().Unix(),
		}
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal write payload failed: %v", err)

		}
		url := joinURLMT(baseURL, writePath)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(b))
		commonHeadersTenant(req, tenant)
		req.Header.Set("content-type", "application/json")
		code, body := doReqMT(t, client, req)
		if !(code >= 200 && code <= 299) && code != 202 {
			t.Fatalf("write failed tenant=%s url=%s status=%d body:\n%s", tenant, url, code, body)

		}

	}
	writeEvent(tenantA, "A")
	writeEvent(tenantB, "B")

	// Query helper: best-effort query by nonce to keep response small
	queryTenant := func(tenant string) (string, int, string) {
		q := map[string]any{
			"kind":  "mt_test",
			"nonce": nonce,
		}
		b, err := json.Marshal(q)
		if err != nil {
			t.Fatalf("json.Marshal query payload failed: %v", err)

		}
		url := joinURLMT(baseURL, readPath)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(b))
		commonHeadersTenant(req, tenant)
		req.Header.Set("content-type", "application/json")
		code, body := doReqMT(t, client, req)
		return url, code, body

	} // Tenant A must see A marker and must NOT see B marker (exact pair only).

	{
		url, code, body := queryTenant(tenantA)
		if code < 200 || code > 299 {
			t.Fatalf("query failed tenant=%s url=%s status=%d body:\n%s", tenantA, url, code, body)

		}
		if !containsMarker(body, "A") {
			t.Fatalf("isolation failed tenant=%s url=%s status=%d missing marker=A body:\n%s", tenantA, url, code, body)

		}
		if containsMarker(body, "B") {
			t.Fatalf("isolation failed tenant=%s url=%s status=%d leaked marker=B body:\n%s", tenantA, url, code, body)

		}

	} // Tenant B must see B marker and must NOT see A marker (exact pair only).

	{
		url, code, body := queryTenant(tenantB)
		if code < 200 || code > 299 {
			t.Fatalf("query failed tenant=%s url=%s status=%d body:\n%s", tenantB, url, code, body)

		}
		if !containsMarker(body, "B") {
			t.Fatalf("isolation failed tenant=%s url=%s status=%d missing marker=B body:\n%s", tenantB, url, code, body)

		}
		if containsMarker(body, "A") {
			t.Fatalf("isolation failed tenant=%s url=%s status=%d leaked marker=A body:\n%s", tenantB, url, code, body)

		}
	}
}
