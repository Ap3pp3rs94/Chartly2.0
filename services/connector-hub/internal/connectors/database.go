package connectors

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/connector-hub/internal/registry"
)

type DatabaseConnector struct {
	BaseConnector
}

func NewDatabaseConnector(id string, caps []string) DatabaseConnector {
	if len(caps) == 0 {
		caps = []string{"ingest", "discover"}
	}
	return DatabaseConnector{
		BaseConnector: NewBaseConnector(id, "db", caps),
	}
}
func (c DatabaseConnector) ValidateConfig(cfg map[string]string) error {
	if err := c.RequireKeys(cfg, "dsn", "engine"); err != nil {
		return registry.ErrInvalidConfig
	}
	engine := strings.ToLower(strings.TrimSpace(cfg["engine"]))
	dsn := strings.TrimSpace(cfg["dsn"])
	if engine == "" || dsn == "" {
		return registry.ErrInvalidConfig
	}
	switch engine {
	case "postgres", "mysql", "sqlite", "mssql", "other":
		// ok
		// default:
		// return registry.ErrInvalidConfig
	}
	if engine == "sqlite" {
		// file path; no network guard needed
		// return nil
	}
	allowPrivate := strings.EqualFold(strings.TrimSpace(cfg["allow_private_networks"]), "true")
	if !allowPrivate {
		if host := extractHostFromDSN(dsn); host != "" {
			if isPrivateHost(host) {
				return errors.New("private networks denied")
			}
		}
	}
	return nil
}
func (c DatabaseConnector) Ingest(ctx context.Context, cfg map[string]string, req registry.IngestRequest) (registry.IngestResult, error) {
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
		Notes:       "sql driver not available in Go standard library; feature unavailable",
	}, registry.ErrNotImplemented
}

// extractHostFromDSN is a deterministic heuristic:
// - URL-like DSNs: scheme://user:pass@host:port/db -> host
// - key/value DSNs: host=... or server=... -> host
// - mysql style: user:pass@tcp(host:port)/db -> host
func extractHostFromDSN(dsn string) string {
	s := strings.TrimSpace(dsn)
	ls := strings.ToLower(s)

	// key/value patterns
	for _, key := range []string{"host=", "server=", "hostname="} {
		if i := strings.Index(ls, key); i >= 0 {
			rest := s[i+len(key):]
			rest = strings.TrimLeft(rest, " ")

			// value ends at space or ; or &
			end := len(rest)
			for j, ch := range rest {
				if ch == ' ' || ch == ';' || ch == '&' {
					end = j
					break
				}
			}
			val := strings.Trim(rest[:end], `"'`)

			// strip :port
			if h, _, ok := strings.Cut(val, ":"); ok {
				return h
			}
			return val
		}
	}

	// mysql tcp(host:port)
	if i := strings.Index(ls, "@tcp("); i >= 0 {
		rest := s[i+len("@tcp("):]
		if j := strings.Index(rest, ")"); j >= 0 {
			val := rest[:j]
			val = strings.TrimSpace(val)
			if h, _, ok := strings.Cut(val, ":"); ok {
				return h
			}
			return val
		}
	}

	// URL-like DSN: find "://", then parse host between @ and /
	if i := strings.Index(ls, "://"); i >= 0 {
		after := s[i+3:]
		// strip creds
		if at := strings.LastIndex(after, "@"); at >= 0 {
			after = after[at+1:]
		}

		// host up to / or ?
		end := len(after)
		for j, ch := range after {
			if ch == '/' || ch == '?' {
				end = j
				break
			}
		}
		hostport := strings.TrimSpace(after[:end])
		if host, _, ok := strings.Cut(hostport, ":"); ok {
			return host
		}
		return hostport
	}
	return ""
}
