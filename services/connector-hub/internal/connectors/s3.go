package connectors

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/connector-hub/internal/registry"
)

type S3Connector struct {
	BaseConnector
}

func NewS3Connector(id string, caps []string) S3Connector {
	if len(caps) == 0 {
		caps = []string{"ingest", "sync"}
	}
	return S3Connector{
		BaseConnector: NewBaseConnector(id, "file", caps),
	}
}
func (c S3Connector) ValidateConfig(cfg map[string]string) error {
	if err := c.RequireKeys(cfg, "bucket", "auth"); err != nil {
		return registry.ErrInvalidConfig
	}
	bucket := strings.TrimSpace(cfg["bucket"])
	if bucket == "" {
		return registry.ErrInvalidConfig
	}
	auth := strings.ToLower(strings.TrimSpace(cfg["auth"]))
	switch auth {
	case "access_key":
		if strings.TrimSpace(cfg["access_key_id"]) == "" || strings.TrimSpace(cfg["secret_access_key"]) == "" {
			return registry.ErrInvalidConfig
		}
	case "iam_role":
		// placeholder: assume ambient credentials
	case "anonymous":
		// ok
		// default:
		// return registry.ErrInvalidConfig
	}
	ep := strings.TrimSpace(cfg["endpoint"])
	if ep != "" {
		if !(strings.HasPrefix(ep, "http://") || strings.HasPrefix(ep, "https://")) {
			return registry.ErrInvalidConfig
		}
		u, err := url.Parse(ep)
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
	}
	return nil
}
func (c S3Connector) Ingest(ctx context.Context, cfg map[string]string, req registry.IngestRequest) (registry.IngestResult, error) {
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
		Notes:       "aws sdk not available in Go standard library; not implemented",
	}, registry.ErrNotImplemented
}
