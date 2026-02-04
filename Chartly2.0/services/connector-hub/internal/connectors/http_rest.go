package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/connector-hub/internal/registry"
)

type HTTPRestConnector struct {
	BaseConnector
}

func NewHTTPRestConnector(id string, caps []string) HTTPRestConnector {
	if len(caps) == 0 {
		caps = []string{"ingest"}
	}
	return HTTPRestConnector{
		BaseConnector: NewBaseConnector(id, "api", caps),
	}
}
func (c HTTPRestConnector) ValidateConfig(cfg map[string]string) error {
	if err := c.RequireKeys(cfg, "base_url"); err != nil {
		return registry.ErrInvalidConfig
	}
	base := strings.TrimSpace(cfg["base_url"])
	if !(strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://")) {
		return registry.ErrInvalidConfig
	}
	m := strings.ToUpper(strings.TrimSpace(cfg["method"]))
	if m == "" {
		m = "GET"
	}
	switch m {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		// ok
	default:
		return registry.ErrInvalidConfig
	}
	return nil
}
func (c HTTPRestConnector) Ingest(ctx context.Context, cfg map[string]string, req registry.IngestRequest) (registry.IngestResult, error) {
	if err := c.ValidateConfig(cfg); err != nil {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "invalid config"}, err
	}
	base := strings.TrimSpace(cfg["base_url"])
	path := strings.TrimSpace(cfg["path"])
	if path == "" {
		path = "/"
	}
	m := strings.ToUpper(strings.TrimSpace(cfg["method"]))
	if m == "" {
		m = "GET"
	}
	allowPrivate := strings.EqualFold(strings.TrimSpace(cfg["allow_private_networks"]), "true")
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "invalid base_url"}, registry.ErrInvalidConfig
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "non-http scheme denied"}, registry.ErrInvalidConfig
	}

	// SSRF guard: deny private/loopback unless explicitly allowed.
	if !allowPrivate {
		host := u.Hostname()
		if isPrivateHost(host) {
			return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "private networks denied"}, errors.New("private networks denied")
		}
	}
	fullURL := strings.TrimRight(base, "/") + path
	var body io.Reader
	hasBody := (m == "POST" || m == "PUT" || m == "PATCH")
	if hasBody {
		b, _ := json.Marshal(req.Payload)
		body = bytes.NewReader(b)
	}

	// timeout config override
	timeout := c.timeout
	if v := strings.TrimSpace(cfg["timeout_ms"]); v != "" {
		if ms, err := time.ParseDuration(v + "ms"); err == nil && ms > 0 {
			timeout = ms
		}
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	httpReq, err := http.NewRequestWithContext(ctx, m, fullURL, body)
	if err != nil {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "request build failed"}, err
	}

	// headers.* passthrough
	for k, v := range cfg {
		if strings.HasPrefix(strings.ToLower(k), "headers.") {
			h := strings.TrimSpace(k[len("headers."):])
			if h != "" {
				httpReq.Header.Set(h, v)
			}
		}
	}
	if hasBody {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// propagate tenant
	if strings.TrimSpace(req.TenantID) != "" {
		httpReq.Header.Set("X-Tenant-Id", req.TenantID)
	}

	// best-effort request id from payload if present
	if rid := strings.TrimSpace(req.Payload["request_id"]); rid != "" {
		httpReq.Header.Set("X-Request-Id", rid)
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
	}
	res, err := client.Do(httpReq)
	if err != nil {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "request failed"}, err
	}
	defer res.Body.Close()

	// capture up to 1KB for notes
	buf, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	accepted := res.StatusCode >= 200 && res.StatusCode < 300
	notes := "status=" + res.Status
	if len(buf) > 0 {
		notes += " body=" + sanitizeNote(string(buf))
	}
	return registry.IngestResult{
		Accepted:    accepted,
		ConnectorID: c.ID(),
		Notes:       notes,
	}, nil
}

// isPrivateHost attempts to detect loopback/private/link-local hosts.
// NOTE: For hostnames, we only block obvious localhost forms unless resolved IP is provided.
func isPrivateHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || h == "localhost.localdomain" {
		return true
	}

	// If host is IP literal, check ranges.
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return isPrivateIP(ip)
}
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// IPv4 RFC1918
	ip4 := ip.To4()
	if ip4 != nil {
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		case ip4[0] == 127:
			return true
		case ip4[0] == 169 && ip4[1] == 254:
			return true
		default:
			return false
		}
	}

	// IPv6 unique local fc00::/7 and loopback ::1
	if len(ip) == net.IPv6len {
		if ip[0]&0xfe == 0xfc {
			return true
		}
		if ip.IsLoopback() {
			return true
		}
	}
	return false
}
func sanitizeNote(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return s[:512] + ""
	}
	return s
}
