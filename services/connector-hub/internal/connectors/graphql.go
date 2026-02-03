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

type GraphQLConnector struct {
	BaseConnector
}

func NewGraphQLConnector(id string, caps []string) GraphQLConnector {
	if len(caps) == 0 {
		caps = []string{"ingest"}
	}
	return GraphQLConnector{
		BaseConnector: NewBaseConnector(id, "api", caps),
	}
}
func (c GraphQLConnector) ValidateConfig(cfg map[string]string) error {
	if err := c.RequireKeys(cfg, "endpoint", "query"); err != nil {
		return registry.ErrInvalidConfig
	}
	endpoint := strings.TrimSpace(cfg["endpoint"])
	if !(strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://")) {
		return registry.ErrInvalidConfig
	}
	allowPrivate := strings.EqualFold(strings.TrimSpace(cfg["allow_private_networks"]), "true")
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return registry.ErrInvalidConfig
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return registry.ErrInvalidConfig
	}
	if !allowPrivate {
		if isPrivateHost(u.Hostname()) {
			return errors.New("private networks denied")
		}
	}
	return nil
}
func (c GraphQLConnector) Ingest(ctx context.Context, cfg map[string]string, req registry.IngestRequest) (registry.IngestResult, error) {
	if err := c.ValidateConfig(cfg); err != nil {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "invalid config"}, err
	}
	endpoint := strings.TrimSpace(cfg["endpoint"])
	query := cfg["query"]
	opName := strings.TrimSpace(cfg["operation_name"])

	// timeout override
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
	bodyObj := map[string]any{
		"query":     query,
		"variables": req.Payload,
	}
	if opName != "" {
		bodyObj["operationName"] = opName
	}
	b, _ := json.Marshal(bodyObj)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
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
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(req.TenantID) != "" {
		httpReq.Header.Set("X-Tenant-Id", req.TenantID)
	}
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
	client := &http.Client{Transport: transport}
	res, err := client.Do(httpReq)
	if err != nil {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "request failed"}, err
	}
	defer res.Body.Close()
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
