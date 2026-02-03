package registry

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// type Status string

const (
	StatusUnknown   Status = "unknown"
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

var (
	ErrCheckNotFound = errors.New("check not found")
)

type CheckResult struct {
	ConnectorID string            `json:"connector_id"`
	Status      Status            `json:"status"`
	CheckedAt   string            `json:"checked_at"` // RFC3339Nano
	LatencyMs   int64             `json:"latency_ms"`
	Message     string            `json:"message,omitempty"`
	Details     map[string]string `json:"details,omitempty"`
}
type Checker interface {
	Check(ctx context.Context, id string) (CheckResult, error)
}
type StaticChecker struct {
	mu sync.RWMutex
	m  map[string]staticEntry
}
type staticEntry struct {
	status  Status
	message string
}

func NewStaticChecker() *StaticChecker {
	return &StaticChecker{m: make(map[string]staticEntry)}
}
func (c *StaticChecker) Set(id string, status Status, message string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[id] = staticEntry{status: status, message: strings.TrimSpace(message)}
}
func (c *StaticChecker) Check(ctx context.Context, id string) (CheckResult, error) {
	_ = ctx

	id = strings.TrimSpace(id)
	start := time.Now()
	c.mu.RLock()
	ent, ok := c.m[id]
	c.mu.RUnlock()
	lat := time.Since(start).Milliseconds()
	if !ok {
		// Prefer unknown over error to keep health endpoints stable.
		return CheckResult{
			ConnectorID: id,
			Status:      StatusUnknown,
			CheckedAt:   time.Now().UTC().Format(time.RFC3339Nano),
			LatencyMs:   lat,
			Message:     "no health check configured",
		}, nil
	}
	return CheckResult{
		ConnectorID: id,
		Status:      ent.status,
		CheckedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		LatencyMs:   lat,
		Message:     ent.message,
	}, nil
}
