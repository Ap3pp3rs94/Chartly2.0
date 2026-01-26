package registry

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrConnectorExists  = errors.New("connector exists")
	ErrConnectorMissing = errors.New("connector missing")
	ErrInvalidConfig    = errors.New("invalid config")
	ErrNotImplemented   = errors.New("not implemented")
)

type IngestRequest struct {
	JobID       string            `json:"job_id"`
	TenantID    string            `json:"tenant_id"`
	SourceID    string            `json:"source_id"`
	RequestedAt string            `json:"requested_at"` // RFC3339Nano
	Payload     map[string]string `json:"payload,omitempty"`
}

type IngestResult struct {
	Accepted    bool   `json:"accepted"`
	ConnectorID string `json:"connector_id"`
	Notes       string `json:"notes,omitempty"`
}

type Connector interface {
	ID() string
	Kind() string
	Capabilities() []string
	ValidateConfig(cfg map[string]string) error
	Ingest(ctx context.Context, cfg map[string]string, req IngestRequest) (IngestResult, error)
}

type Registry struct {
	mu sync.RWMutex
	m  map[string]Connector
}

func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Connector)}
}

func (r *Registry) Register(c Connector) error {
	if c == nil {
		return errors.New("connector is nil")
	}

	id := strings.TrimSpace(c.ID())
	if id == "" {
		return errors.New("connector id is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.m[id]; ok {
		return ErrConnectorExists
	}

	r.m[id] = c
	return nil
}

func (r *Registry) Get(id string) (Connector, bool) {
	id = strings.TrimSpace(id)

	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.m[id]
	return c, ok
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]string, 0, len(r.m))
	for k := range r.m {
		out = append(out, k)
	}

	sort.Strings(out)
	return out
}

func (r *Registry) Capabilities(id string) ([]string, error) {
	c, ok := r.Get(id)
	if !ok {
		return nil, ErrConnectorMissing
	}

	caps := c.Capabilities()

	// normalize and de-dup deterministically
	set := make(map[string]struct{}, len(caps))
	for _, v := range caps {
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
	return out, nil
}

////////////////////////////////////////////////////////////////////////////////
// Built-in noop connector (safe default)
////////////////////////////////////////////////////////////////////////////////

type NoopConnector struct{}

func (NoopConnector) ID() string { return "noop" }
func (NoopConnector) Kind() string { return "other" }
func (NoopConnector) Capabilities() []string { return []string{"ingest"} }

func (NoopConnector) ValidateConfig(cfg map[string]string) error {
	_ = cfg
	return nil
}

func (NoopConnector) Ingest(ctx context.Context, cfg map[string]string, req IngestRequest) (IngestResult, error) {
	_ = ctx
	_ = cfg
	_ = req

	return IngestResult{
		Accepted:    true,
		ConnectorID: "noop",
		Notes:       "noop accepted",
	}, nil
}
