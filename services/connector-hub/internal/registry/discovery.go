package registry

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrMetaExists  = errors.New("connector meta exists")
	ErrMetaMissing = errors.New("connector meta missing")
)

type ConnectorMeta struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Capabilities []string `json:"capabilities,omitempty"`
	Enabled      bool     `json:"enabled"`
	Notes        string   `json:"notes,omitempty"`
}

type Catalog interface {
	List(ctx context.Context) ([]ConnectorMeta, error)
	Get(ctx context.Context, id string) (ConnectorMeta, bool, error)
}

type InMemoryCatalog struct {
	mu sync.RWMutex
	m  map[string]ConnectorMeta
}

func NewInMemoryCatalog() *InMemoryCatalog {
	return &InMemoryCatalog{m: make(map[string]ConnectorMeta)}
}

func (c *InMemoryCatalog) Add(meta ConnectorMeta) error {
	id := strings.TrimSpace(meta.ID)
	if id == "" {
		return errors.New("id is empty")
	}

	meta.ID = id
	meta.Kind = strings.ToLower(strings.TrimSpace(meta.Kind))
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Notes = strings.TrimSpace(meta.Notes)
	meta.Capabilities = normalizeCaps(meta.Capabilities)

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.m[id]; ok {
		return ErrMetaExists
	}

	c.m[id] = meta
	return nil
}

func (c *InMemoryCatalog) Remove(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("id is empty")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.m[id]; !ok {
		return ErrMetaMissing
	}

	delete(c.m, id)
	return nil
}

func (c *InMemoryCatalog) List(ctx context.Context) ([]ConnectorMeta, error) {
	_ = ctx

	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]ConnectorMeta, 0, len(c.m))
	for _, v := range c.m {
		out = append(out, v)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *InMemoryCatalog) Get(ctx context.Context, id string) (ConnectorMeta, bool, error) {
	_ = ctx

	id = strings.TrimSpace(id)

	c.mu.RLock()
	defer c.mu.RUnlock()

	v, ok := c.m[id]
	return v, ok, nil
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
