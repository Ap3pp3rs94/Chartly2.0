package connectors

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/connector-hub/internal/registry"
)

type LoggerFn func(level, msg string, fields map[string]any) type BaseConnector struct {
	id      string
	kind    string
	caps    []string
	timeout time.Duration
	logger  LoggerFn
}

func NewBaseConnector(id, kind string, caps []string) BaseConnector {
	b := BaseConnector{
		id:      strings.TrimSpace(id),
		kind:    strings.ToLower(strings.TrimSpace(kind)),
		timeout: 15 * time.Second,
		logger:  func(string, string, map[string]any) {},
	}
	b.caps = normalizeCaps(caps)
	return b
}
func (b BaseConnector) ID() string   { return b.id }
func (b BaseConnector) Kind() string { return b.kind }
func (b BaseConnector) Capabilities() []string {
	out := make([]string, len(b.caps))
	copy(out, b.caps)
	return out
}
func (b BaseConnector) WithTimeout(d time.Duration) BaseConnector {
	if d > 0 {
		b.timeout = d
	}
	return b
}
func (b BaseConnector) WithLogger(fn LoggerFn) BaseConnector {
	if fn != nil {
		b.logger = fn
	}
	return b
}

// ValidateConfig default allows any config. Override in concrete connector.
func (b BaseConnector) ValidateConfig(cfg map[string]string) error {
	_ = cfg
	return nil
}
func (b BaseConnector) RequireKeys(cfg map[string]string, keys ...string) error {
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if cfg == nil {
			return registry.ErrInvalidConfig
		}
		if _, ok := cfg[k]; !ok {
			return registry.ErrInvalidConfig
		}
	}
	return nil
}
func (b BaseConnector) AllowOnlyKeys(cfg map[string]string, keys ...string) error {
	if cfg == nil {
		return nil
	}
	allowed := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		allowed[k] = struct{}{}
	}
	for k := range cfg {
		if _, ok := allowed[k]; !ok {
			return registry.ErrInvalidConfig
		}
	}
	return nil
}
func (b BaseConnector) Ingest(ctx context.Context, cfg map[string]string, req registry.IngestRequest) (registry.IngestResult, error) {
	_ = cfg
	_ = req

	// honor timeout
	if b.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}
	select {
	case <-ctx.Done():
		return registry.IngestResult{Accepted: false, ConnectorID: b.id, Notes: "timeout"}, ctx.Err()
	default:
	}
	return registry.IngestResult{Accepted: false, ConnectorID: b.id, Notes: "not implemented"}, registry.ErrNotImplemented
}
func normalizeCaps(in []string) []string {
	set := make(map[string]struct{}, len(in))
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		set[v] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Compile-time check: BaseConnector satisfies registry.Connector (behavior may be overridden).
var _ registry.Connector = BaseConnector{}
