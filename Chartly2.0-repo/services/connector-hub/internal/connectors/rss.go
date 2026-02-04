package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/connector-hub/internal/registry"
)

type RSSConnector struct {
	BaseConnector
}

func NewRSSConnector(id string, caps []string) RSSConnector {
	if len(caps) == 0 {
		caps = []string{"ingest"}
	}
	return RSSConnector{
		BaseConnector: NewBaseConnector(id, "api", caps),
	}
}
func (c RSSConnector) ValidateConfig(cfg map[string]string) error {
	if err := c.RequireKeys(cfg, "feed_url"); err != nil {
		return registry.ErrInvalidConfig
	}
	raw := strings.TrimSpace(cfg["feed_url"])
	if !(strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")) {
		return registry.ErrInvalidConfig
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return registry.ErrInvalidConfig
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return registry.ErrInvalidConfig
	}
	allowPrivate := strings.EqualFold(strings.TrimSpace(cfg["allow_private_networks"]), "true")
	if !allowPrivate && isPrivateHost(u.Hostname()) {
		return errors.New("private networks denied")
	}
	if mb := strings.TrimSpace(cfg["max_bytes"]); mb != "" {
		n, err := strconv.Atoi(mb)
		if err != nil || n < 1024 || n > 10*1024*1024 {
			return registry.ErrInvalidConfig
		}
	}
	return nil
}
func (c RSSConnector) Ingest(ctx context.Context, cfg map[string]string, req registry.IngestRequest) (registry.IngestResult, error) {
	_ = req

	if err := c.ValidateConfig(cfg); err != nil {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "invalid config"}, err
	}
	raw := strings.TrimSpace(cfg["feed_url"])
	maxBytes := 1024 * 1024
	if mb := strings.TrimSpace(cfg["max_bytes"]); mb != "" {
		if n, err := strconv.Atoi(mb); err == nil && n >= 1024 && n <= 10*1024*1024 {
			maxBytes = n
		}
	}
	ua := strings.TrimSpace(cfg["user_agent"])
	if ua == "" {
		ua = "Chartly-ConnectorHub/0"
	}

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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "request build failed"}, err
	}
	httpReq.Header.Set("User-Agent", ua)
	if strings.TrimSpace(req.TenantID) != "" {
		httpReq.Header.Set("X-Tenant-Id", req.TenantID)
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
	body, err := io.ReadAll(io.LimitReader(res.Body, int64(maxBytes)))
	if err != nil {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "read failed"}, err
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	accepted := res.StatusCode >= 200 && res.StatusCode < 300
	notes := "status=" + res.Status + " bytes=" + strconv.Itoa(len(body)) + " sha256=" + hash

	return registry.IngestResult{
		Accepted:    accepted,
		ConnectorID: c.ID(),
		Notes:       notes,
	}, nil
}
