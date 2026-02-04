package pool

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrCircuitOpen = errors.New("circuit open")
)

type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half_open"
)

type CircuitConfig struct {
	FailureThreshold int           `json:"failure_threshold"`
	SuccessThreshold int           `json:"success_threshold"`
	OpenTimeout      time.Duration `json:"open_timeout"`
	Window           time.Duration `json:"window"`
}

func DefaultCircuitConfig() CircuitConfig {
	return CircuitConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
		Window:           60 * time.Second,
	}
}

type Circuit struct {
	state             State
	failures          []time.Time
	halfOpenSuccesses int
	openedAt          time.Time
}
type Manager struct {
	mu       sync.Mutex
	perKey   map[string]*Circuit
	defaults CircuitConfig
}

func NewManager(defaults CircuitConfig) *Manager {
	if defaults.FailureThreshold <= 0 {
		defaults.FailureThreshold = 5
	}
	if defaults.SuccessThreshold <= 0 {
		defaults.SuccessThreshold = 2
	}
	if defaults.OpenTimeout <= 0 {
		defaults.OpenTimeout = 30 * time.Second
	}
	if defaults.Window <= 0 {
		defaults.Window = 60 * time.Second
	}
	return &Manager{
		perKey:   make(map[string]*Circuit),
		defaults: defaults,
	}
}
func (m *Manager) Allow(key string) (ok bool, state string, reason string) {
	key = normKey(key)
	if key == "" {
		return true, string(StateClosed), "empty_key"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.getOrCreate(key)
	now := time.Now()
	switch c.state {
	case StateOpen:
		if now.Sub(c.openedAt) >= m.defaults.OpenTimeout {
			// transition to half-open, allow one attempt
			c.state = StateHalfOpen
			c.halfOpenSuccesses = 0
			return true, string(c.state), "open_timeout_elapsed"
		}
		return false, string(c.state), "open"

	case StateHalfOpen:
		// Allow attempts; success accounting is handled in Report.
		return true, string(c.state), "half_open"

	default:
		// closed
		return true, string(StateClosed), "closed"
	}
}
func (m *Manager) Report(key string, success bool) {
	key = normKey(key)
	if key == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.getOrCreate(key)
	now := time.Now()
	switch c.state {
	case StateHalfOpen:
		if success {
			c.halfOpenSuccesses++
			if c.halfOpenSuccesses >= m.defaults.SuccessThreshold {
				// close circuit
				c.state = StateClosed
				c.failures = nil
				c.halfOpenSuccesses = 0
				c.openedAt = time.Time{}
			}
			return
		}

		// failure in half-open => open again
		c.state = StateOpen
		c.openedAt = now
		c.halfOpenSuccesses = 0
		c.failures = append(c.failures, now)
		m.pruneLocked(c, now)
		// return

	case StateOpen:
		// While open, we can still record failures (optional)
		// to reflect continued issues.
		if !success {
			c.failures = append(c.failures, now)
			m.pruneLocked(c, now)
		}
		return

	default:
		// closed
		if success {
			// optional: prune old failures on success
			m.pruneLocked(c, now)
			return
		}
		c.failures = append(c.failures, now)
		m.pruneLocked(c, now)
		if len(c.failures) >= m.defaults.FailureThreshold {
			c.state = StateOpen
			c.openedAt = now
			c.halfOpenSuccesses = 0
		}
	}
}
func (m *Manager) Snapshot(key string) map[string]any {
	key = normKey(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.getOrCreate(key)
	now := time.Now()
	m.pruneLocked(c, now)
	out := map[string]any{
		"key":      key,
		"state":    string(c.state),
		"failures": len(c.failures),
	}
	if !c.openedAt.IsZero() {
		out["opened_at"] = c.openedAt.UTC().Format(time.RFC3339Nano)
		out["open_timeout_s"] = m.defaults.OpenTimeout.Seconds()
	}
	if c.state == StateHalfOpen {
		out["half_open_successes"] = c.halfOpenSuccesses
		out["success_threshold"] = m.defaults.SuccessThreshold
	}
	return out
}
func (m *Manager) Do(ctx context.Context, key string, fn func() error) error {
	ok, _, _ := m.Allow(key)
	if !ok {
		return ErrCircuitOpen
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	err := fn()
	m.Report(key, err == nil)
	return err
}
func (m *Manager) getOrCreate(key string) *Circuit {
	c, ok := m.perKey[key]
	if !ok {
		c = &Circuit{state: StateClosed, failures: make([]time.Time, 0, m.defaults.FailureThreshold)}
		m.perKey[key] = c
	}
	return c
}
func (m *Manager) pruneLocked(c *Circuit, now time.Time) {
	if c == nil || m.defaults.Window <= 0 {
		return
	}
	cut := now.Add(-m.defaults.Window)

	// failures are appended in time order; prune from front
	i := 0
	for i < len(c.failures) && c.failures[i].Before(cut) {
		i++
	}
	if i > 0 {
		c.failures = c.failures[i:]
	}
}
func normKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
