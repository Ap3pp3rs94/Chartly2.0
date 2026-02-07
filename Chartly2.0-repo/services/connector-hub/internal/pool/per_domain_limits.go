package pool

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrLimitExceeded = errors.New("limit exceeded")
)

type Limits struct {
	MaxConcurrent int     `json:"max_concurrent"`
	RPS           float64 `json:"rps"`
	Burst         int     `json:"burst"`
}
type Decision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}
type domainState struct {
	tokens     float64
	lastRefill time.Time
	inFlight   int
}
type DomainLimiter struct {
	mu       sync.Mutex
	defaults Limits
	per      map[string]Limits
	state    map[string]*domainState
}

func NewDomainLimiter(defaults Limits) *DomainLimiter {
	if defaults.MaxConcurrent <= 0 {
		defaults.MaxConcurrent = 8
	}
	if defaults.Burst <= 0 {
		// if burst unspecified, default to MaxConcurrent
		defaults.Burst = defaults.MaxConcurrent
	}
	return &DomainLimiter{
		defaults: defaults,
		per:      make(map[string]Limits),
		state:    make(map[string]*domainState),
	}
}
func (l *DomainLimiter) Set(domain string, lim Limits) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return
	}
	if lim.MaxConcurrent <= 0 {
		lim.MaxConcurrent = l.defaults.MaxConcurrent
	}
	if lim.Burst <= 0 {
		lim.Burst = l.defaults.Burst
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.per[domain] = lim
	// reset state to apply new limits cleanly
	delete(l.state, domain)
}
func (l *DomainLimiter) Acquire(ctx context.Context, domain string) (func(), error) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return func() {}, nil
	}
	t := time.NewTicker(25 * time.Millisecond)
	defer t.Stop()
	for {
		ok, rel := l.tryAcquire(domain)
		if ok {
			return rel, nil
		}
		select {
		case <-ctx.Done():
			return nil, ErrLimitExceeded
		case <-t.C:
			// retry
		}
	}
}
func (l *DomainLimiter) tryAcquire(domain string) (bool, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim := l.defaults
	if v, ok := l.per[domain]; ok {
		lim = mergeLimits(l.defaults, v)
	}
	st, ok := l.state[domain]
	if !ok {
		st = &domainState{
			tokens:     float64(lim.Burst),
			lastRefill: time.Now(),
			inFlight:   0,
		}
		l.state[domain] = st
	}
	now := time.Now()
	refill(st, lim, now)

	// concurrency gate
	if lim.MaxConcurrent > 0 && st.inFlight >= lim.MaxConcurrent {
		return false, nil
	}

	// rate gate (if RPS==0 => unlimited)
	if lim.RPS > 0 {
		if st.tokens < 1.0 {
			return false, nil
		}
		st.tokens -= 1.0
	}
	st.inFlight++

	released := false
	release := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if released {
			return
		}
		released = true

		st2, ok2 := l.state[domain]
		if !ok2 {
			return
		}
		if st2.inFlight > 0 {
			st2.inFlight--
		}

		// tokens are not refunded; token bucket controls rate.
	}
	return true, release
}
func (l *DomainLimiter) Snapshot(domain string) map[string]any {
	domain = normalizeDomain(domain)
	l.mu.Lock()
	defer l.mu.Unlock()
	lim := l.defaults
	if v, ok := l.per[domain]; ok {
		lim = mergeLimits(l.defaults, v)
	}
	st, ok := l.state[domain]
	if !ok {
		return map[string]any{
			"domain": domain,
			"limits": lim,
			"state": map[string]any{
				"tokens":    float64(lim.Burst),
				"in_flight": 0,
			},
		}
	}
	return map[string]any{
		"domain": domain,
		"limits": lim,
		"state": map[string]any{
			"tokens":      st.tokens,
			"in_flight":   st.inFlight,
			"last_refill": st.lastRefill.UTC().Format(time.RFC3339Nano),
		},
	}
}
func mergeLimits(def, in Limits) Limits {
	out := in
	if out.MaxConcurrent <= 0 {
		out.MaxConcurrent = def.MaxConcurrent
	}
	if out.Burst <= 0 {
		out.Burst = def.Burst
	}
	return out
}
func refill(st *domainState, lim Limits, now time.Time) {
	if st == nil {
		return
	}
	if lim.RPS <= 0 {
		// no rate limiting
		st.lastRefill = now
		return
	}
	elapsed := now.Sub(st.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	st.lastRefill = now
	st.tokens += elapsed * lim.RPS

	capacity := float64(lim.Burst)
	if capacity <= 0 {
		capacity = 1
	}
	if st.tokens > capacity {
		st.tokens = capacity
	}
}
func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	if i := strings.IndexByte(d, '/'); i >= 0 {
		d = d[:i]
	}
	return d
}
