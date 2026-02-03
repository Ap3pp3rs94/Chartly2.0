package connectors

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/connector-hub/internal/registry"
)

type WebSocketConnector struct {
	BaseConnector
}

func NewWebSocketConnector(id string, caps []string) WebSocketConnector {
	if len(caps) == 0 {
		caps = []string{"ingest"}
	}
	return WebSocketConnector{
		BaseConnector: NewBaseConnector(id, "webhook", caps),
	}
}
func (c WebSocketConnector) ValidateConfig(cfg map[string]string) error {
	if err := c.RequireKeys(cfg, "url"); err != nil {
		return registry.ErrInvalidConfig
	}
	raw := strings.TrimSpace(cfg["url"])
	if !(strings.HasPrefix(raw, "ws://") || strings.HasPrefix(raw, "wss://")) {
		return registry.ErrInvalidConfig
	}
	allowPrivate := strings.EqualFold(strings.TrimSpace(cfg["allow_private_networks"]), "true")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return registry.ErrInvalidConfig
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return registry.ErrInvalidConfig
	}
	if !allowPrivate {
		if isPrivateHost(u.Hostname()) {
			return errors.New("private networks denied")
		}
	}
	return nil
}
func (c WebSocketConnector) Ingest(ctx context.Context, cfg map[string]string, req registry.IngestRequest) (registry.IngestResult, error) {
	_ = ctx
	_ = req

	if err := c.ValidateConfig(cfg); err != nil {
		return registry.IngestResult{Accepted: false, ConnectorID: c.ID(), Notes: "invalid config"}, err
	}

	// timeout override (no-op for now, but accepted for config parity)
	if v := strings.TrimSpace(cfg["timeout_ms"]); v != "" {
		if ms, err := time.ParseDuration(v + "ms"); err == nil && ms > 0 {
			_ = ms
		}
	}
	return registry.IngestResult{
		Accepted:    false,
		ConnectorID: c.ID(),
		Notes:       "websocket client not available in Go standard library; not implemented",
	}, registry.ErrNotImplemented
}
