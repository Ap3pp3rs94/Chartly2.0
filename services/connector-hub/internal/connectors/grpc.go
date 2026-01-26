package connectors

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/connector-hub/internal/registry"
)

type GRPCConnector struct {
	BaseConnector
}

func NewGRPCConnector(id string, caps []string) GRPCConnector {
	if len(caps) == 0 {
		caps = []string{"ingest"}
	}

	return GRPCConnector{
		BaseConnector: NewBaseConnector(id, "api", caps),
	}
}

func (c GRPCConnector) ValidateConfig(cfg map[string]string) error {
	if err := c.RequireKeys(cfg, "target"); err != nil {
		return registry.ErrInvalidConfig
	}

	raw := strings.TrimSpace(cfg["target"])
	if raw == "" {
		return registry.ErrInvalidConfig
	}

	allowPrivate := strings.EqualFold(strings.TrimSpace(cfg["allow_private_networks"]), "true")

	// Accept host:port OR grpc(s)://host:port
	u, err := parseTarget(raw)
	if err != nil || u.Hostname() == "" {
		return registry.ErrInvalidConfig
	}

	if u.Scheme != "grpc" && u.Scheme != "grpcs" {
		return registry.ErrInvalidConfig
	}

	if !allowPrivate {
		if isPrivateHost(u.Hostname()) {
			return errors.New("private networks denied")
		}
	}

	return nil
}

func (c GRPCConnector) Ingest(ctx context.Context, cfg map[string]string, req registry.IngestRequest) (registry.IngestResult, error) {
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
		Notes:       "grpc client not available in Go standard library; not implemented",
	}, registry.ErrNotImplemented
}

func parseTarget(raw string) (*url.URL, error) {
	if strings.Contains(raw, "://") {
		return url.Parse(raw)
	}

	// interpret as host:port
	return url.Parse("grpc://" + raw)
}
