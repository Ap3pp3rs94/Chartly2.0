package connectors

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/connector-hub/internal/registry"
)

type SFTPConnector struct {
	BaseConnector
}

func NewSFTPConnector(id string, caps []string) SFTPConnector {
	if len(caps) == 0 {
		caps = []string{"ingest", "sync"}
	}
	return SFTPConnector{
		BaseConnector: NewBaseConnector(id, "file", caps),
	}
}
func (c SFTPConnector) ValidateConfig(cfg map[string]string) error {
	if err := c.RequireKeys(cfg, "host", "username", "auth"); err != nil {
		return registry.ErrInvalidConfig
	}
	host := strings.TrimSpace(cfg["host"])
	if host == "" {
		return registry.ErrInvalidConfig
	}
	allowPrivate := strings.EqualFold(strings.TrimSpace(cfg["allow_private_networks"]), "true")
	if !allowPrivate && isPrivateHost(host) {
		return errors.New("private networks denied")
	}
	auth := strings.ToLower(strings.TrimSpace(cfg["auth"]))
	switch auth {
	case "password":
		if strings.TrimSpace(cfg["password"]) == "" {
			return registry.ErrInvalidConfig
		}
	case "key":
		if strings.TrimSpace(cfg["key_path"]) == "" {
			return registry.ErrInvalidConfig
		}
	default:
		return registry.ErrInvalidConfig
	}

	// port is optional; validate if present
	if p := strings.TrimSpace(cfg["port"]); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return registry.ErrInvalidConfig
		}
	}
	return nil
}
func (c SFTPConnector) Ingest(ctx context.Context, cfg map[string]string, req registry.IngestRequest) (registry.IngestResult, error) {
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
		Notes:       "sftp client not available in Go standard library; feature unavailable",
	}, registry.ErrNotImplemented
}
