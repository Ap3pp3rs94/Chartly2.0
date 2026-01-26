package scheduler

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrEngineStarted = errors.New("trigger engine already started")
	ErrEngineStopped = errors.New("trigger engine stopped")
)

// JobsProvider supplies cron jobs (in-memory or loaded from config elsewhere).
// This engine does not own persistence.
type JobsProvider interface {
	List(ctx context.Context) ([]CronJob, error)
}

// Enqueuer enqueues a job request for execution (e.g., to Redis in the future).
type Enqueuer interface {
	Enqueue(ctx context.Context, tenantID string, job JobRequest) (jobID string, err error)
}

// Clock enables deterministic testing.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// JobRequest is the minimal job envelope the engine triggers.
type JobRequest struct {
	SourceID string `json:"source_id"`
	JobType  string `json:"job_type"`
}

// LoggerFn is a structured logger signature.
type LoggerFn func(level, msg string, fields map[string]any)

type jobKey struct {
	Tenant string
	Name   string
}

type firedKey struct {
	Tenant string
	Name   string
	Minute int64 // unix minute in job timezone
}

// TriggerEngine polls JobsProvider and triggers enqueues near their due time.
type TriggerEngine struct {
	provider JobsProvider
	enqueuer Enqueuer
	clock    Clock

	pollInterval time.Duration
	jitterPct    float64
	maxLookahead time.Duration

	logger LoggerFn

	started atomic.Bool
	stopCh  chan struct{}
	wg      sync.WaitGroup

	mu       sync.Mutex
	nextRun  map[jobKey]time.Time  // cache
	lastFire map[firedKey]struct{} // dedup within minute

	rndMu sync.Mutex
	rnd   *rand.Rand

	defaultTZ string
}

// NewTriggerEngine constructs an engine with safe defaults.
// pollInterval defaults to 15s; jitterPct defaults to 0.20; maxLookahead defaults to 5m.
func NewTriggerEngine(provider JobsProvider, enqueuer Enqueuer, logger LoggerFn) *TriggerEngine {
	if logger == nil {
		logger = func(string, string, map[string]any) {}
	}
	src := rand.NewSource(time.Now().UnixNano())
	return &TriggerEngine{
		provider:     provider,
		enqueuer:     enqueuer,
		clock:        realClock{},
		pollInterval: 15 * time.Second,
		jitterPct:    0.20,
		maxLookahead: 5 * time.Minute,
		logger:       logger,
		stopCh:       make(chan struct{}),
		nextRun:      make(map[jobKey]time.Time),
		lastFire:     make(map[firedKey]struct{}),
		rnd:          rand.New(src),
		defaultTZ:    "America/Chicago",
	}
}

// WithClock sets a custom clock (optional).
func (e *TriggerEngine) WithClock(c Clock) *TriggerEngine {
	if c != nil {
		e.clock = c
	}
	return e
}

// WithPollInterval sets poll interval (optional).
func (e *TriggerEngine) WithPollInterval(d time.Duration) *TriggerEngine {
	if d > 0 {
		e.pollInterval = d
	}
	return e
}

// WithMaxLookahead sets lookahead window (optional).
func (e *TriggerEngine) WithMaxLookahead(d time.Duration) *TriggerEngine {
	if d > 0 {
		e.maxLookahead = d
	}
	return e
}

// WithJitterPct sets jitter percent [0..1) (optional).
func (e *TriggerEngine) WithJitterPct(p float64) *TriggerEngine {
	if p >= 0 && p < 1 {
		e.jitterPct = p
	}
	return e
}

// Start launches the engine loop.
func (e *TriggerEngine) Start(ctx context.Context) error {
	if !e.started.CompareAndSwap(false, true) {
		return ErrEngineStarted
	}
	e.wg.Add(1)
	go e.loop(ctx)
	return nil
}

// Stop signals the engine to stop and waits until it exits or ctx times out.
func (e *TriggerEngine) Stop(ctx context.Context) error {
	if !e.started.Load() {
		return ErrEngineStopped
	}
	select {
	case <-e.stopCh:
	default:
		close(e.stopCh)
	}

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *TriggerEngine) loop(ctx context.Context) {
	defer e.wg.Done()

	for {
		select {
		case <-ctx.Done():
			e.logger("info", "trigger_engine_ctx_done", map[string]any{"event": "engine_stop"})
			return
		case <-e.stopCh:
			e.logger("info", "trigger_engine_stop", map[string]any{"event": "engine_stop"})
			return
		default:
		}

		e.tick(ctx)
		sleep := jitterDuration(e, e.pollInterval, e.jitterPct)
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		}
	}
}

func (e *TriggerEngine) tick(ctx context.Context) {
	if e.provider == nil || e.enqueuer == nil {
		e.logger("warn", "trigger_engine_unwired", map[string]any{"event": "engine_unwired"})
		return
	}

	now := e.clock.Now()

	jobs, err := e.provider.List(ctx)
	if err != nil {
		e.logger("warn", "jobs_provider_error", map[string]any{
			"event": "jobs_provider_error",
			"error": err.Error(),
		})
		return
	}

	e.maybePrune()

	for _, j := range jobs {
		if !j.Enabled {
			continue
		}

		jobType := j.JobType
		if strings.TrimSpace(jobType) == "" {
			jobType = "ingest"
		}

		if err := j.Validate(); err != nil {
			e.logger("warn", "cron_job_invalid", map[string]any{
				"event":     "job_invalid",
				"tenant_id": j.TenantID,
				"name":      j.Name,
				"error":     err.Error(),
			})
			continue
		}

		loc := e.loadLocation(j.Timezone)
		key := jobKey{Tenant: j.TenantID, Name: j.Name}

		nr := e.getCachedNextRun(key)

		// If cache missing or stale, recompute from "now".
		if nr.IsZero() || nr.Before(now.Add(-1*time.Minute)) {
			tn, err := NextRun(now, j.Cron, loc)
			if err != nil {
				e.logger("warn", "next_run_error", map[string]any{
					"event":     "next_run_error",
					"tenant_id": j.TenantID,
					"name":      j.Name,
					"error":     err.Error(),
				})
				continue
			}
			nr = tn
			e.setCachedNextRun(key, nr)
		}

		// Only trigger jobs that are within lookahead.
		if nr.After(now.Add(e.maxLookahead)) {
			continue
		}

		minuteKey := firedKey{Tenant: j.TenantID, Name: j.Name, Minute: unixMinute(nr.In(loc))}
		if e.alreadyFired(minuteKey) {
			continue
		}

		jobID, err := e.enqueuer.Enqueue(ctx, j.TenantID, JobRequest{
			SourceID: j.SourceID,
			JobType:  jobType,
		})
		if err != nil {
			e.logger("warn", "trigger_enqueue_error", map[string]any{
				"event":        "enqueue_error",
				"tenant_id":    j.TenantID,
				"name":         j.Name,
				"source_id":    j.SourceID,
				"job_type":     jobType,
				"scheduled_at": nr.In(loc).Format(time.RFC3339),
				"error":        err.Error(),
			})
			continue
		}

		e.markFired(minuteKey)

		e.logger("info", "trigger_fired", map[string]any{
			"event":        "trigger_fired",
			"tenant_id":    j.TenantID,
			"name":         j.Name,
			"source_id":    j.SourceID,
			"job_type":     jobType,
			"job_id":       jobID,
			"scheduled_at": nr.In(loc).Format(time.RFC3339),
		})

		// Precompute next run after the fired time to avoid re-firing.
		nn, err := NextRun(nr.Add(time.Minute), j.Cron, loc)
		if err == nil {
			e.setCachedNextRun(key, nn)
		} else {
			e.setCachedNextRun(key, time.Time{})
		}
	}
}

func (e *TriggerEngine) loadLocation(tz string) *time.Location {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		tz = e.defaultTZ
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (e *TriggerEngine) getCachedNextRun(k jobKey) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.nextRun[k]
}

func (e *TriggerEngine) setCachedNextRun(k jobKey, t time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t.IsZero() {
		delete(e.nextRun, k)
		return
	}
	e.nextRun[k] = t
}

func (e *TriggerEngine) alreadyFired(k firedKey) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.lastFire[k]
	return ok
}

func (e *TriggerEngine) markFired(k firedKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastFire[k] = struct{}{}
}

func (e *TriggerEngine) maybePrune() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.lastFire) > 5000 {
		e.lastFire = make(map[firedKey]struct{})
	}
	if len(e.nextRun) > 5000 {
		e.nextRun = make(map[jobKey]time.Time)
	}
}

func unixMinute(t time.Time) int64 { return t.Unix() / 60 }

func jitterDuration(e *TriggerEngine, base time.Duration, pct float64) time.Duration {
	if pct <= 0 {
		return base
	}
	min := float64(base) * (1.0 - pct)
	max := float64(base) * (1.0 + pct)
	if min < 0 {
		min = 0
	}
	delta := max - min
	if delta <= 0 {
		return base
	}
	e.rndMu.Lock()
	u := e.rnd.Float64()
	e.rndMu.Unlock()
	return time.Duration(min + u*delta)
}
