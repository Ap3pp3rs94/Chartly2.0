package queue

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"
)

var (
	ErrSourceClosed = errors.New("source closed")
	ErrEmpty        = errors.New("empty")
)

type Source interface {
	Next(ctx context.Context) (Envelope, error)
}
type Handler interface {
	Handle(ctx context.Context, env Envelope)
	// error
}
type RetryPolicy interface {
	Next(jobID string, attempt int) (delay time.Duration, ok bool, reason string)
}
type LoggerFn func(level, msg string, fields map[string]any)

// ChannelSource adapts an in-memory envelope channel to Source.
type ChannelSource struct {
	ch <-chan Envelope
}

func NewChannelSource(ch <-chan Envelope) *ChannelSource {
	return &ChannelSource{ch: ch}
}
func (s *ChannelSource) Next(ctx context.Context) (Envelope, error) {
	select {
	case env, ok := <-s.ch:
		if !ok {
			return Envelope{}, ErrSourceClosed
		}
		return env, nil
	case <-ctx.Done():
		return Envelope{}, ctx.Err()
	default:
		return Envelope{}, ErrEmpty
	}
}

type Consumer struct {
	src         Source
	handler     Handler
	retry       RetryPolicy
	concurrency int
	logger      LoggerFn

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup

	rndMu sync.Mutex
	rnd   *rand.Rand
}

func NewConsumer(src Source, handler Handler, retry RetryPolicy, concurrency int, logger LoggerFn) *Consumer {
	if concurrency <= 0 {
		concurrency = 1
	}
	if logger == nil {
		logger = func(string, string, map[string]any) {}
	}
	srcRand := rand.NewSource(time.Now().UnixNano())
	return &Consumer{
		src:         src,
		handler:     handler,
		retry:       retry,
		concurrency: concurrency,
		logger:      logger,
		stopCh:      make(chan struct{}),
		rnd:         rand.New(srcRand),
	}
}
func (c *Consumer) Start(ctx context.Context) error {
	for i := 0; i < c.concurrency; i++ {
		c.wg.Add(1)
		go c.worker(ctx, i)
	}
	return nil
}
func (c *Consumer) Stop(ctx context.Context) error {
	c.stopOnce.Do(func() { close(c.stopCh) })
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *Consumer) worker(ctx context.Context, workerID int) {
	defer c.wg.Done()
	emptyBackoff := 100 * time.Millisecond
	const emptyBackoffMax = 1 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
		}
		env, err := c.src.Next(ctx)
		if err != nil {
			if errors.Is(err, ErrSourceClosed) {
				c.logger("info", "source_closed", map[string]any{
					"event":     "source_closed",
					"worker_id": workerID,
				})
				return
			}
			if errors.Is(err, ErrEmpty) {
				sleep := jitterDuration(c, emptyBackoff, 0.25)
				select {
				case <-time.After(sleep):
				case <-ctx.Done():
					return
				case <-c.stopCh:
					return
				}
				emptyBackoff *= 2
				if emptyBackoff > emptyBackoffMax {
					emptyBackoff = emptyBackoffMax
				}
				continue
			}

			// Unknown error
			c.logger("warn", "source_error", map[string]any{
				"event":     "source_error",
				"worker_id": workerID,
				"error":     err.Error(),
			})
			select {
			case <-time.After(jitterDuration(c, 250*time.Millisecond, 0.30)):
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			}
			continue
		}
		emptyBackoff = 100 * time.Millisecond

		// Basic envelope validation
		if env.Type != "job" || env.Version == "" {
			c.logger("warn", "dropped_envelope", map[string]any{
				"event":     "dropped",
				"worker_id": workerID,
				"reason":    "invalid_envelope",
				"type":      env.Type,
				"version":   env.Version,
			})
			continue
		}
		c.logger("info", "job_dequeued", map[string]any{
			"event":     "dequeue",
			"worker_id": workerID,
			"tenant_id": env.Job.TenantID,
			"job_id":    env.Job.JobID,
			"source_id": env.Job.SourceID,
			"job_type":  env.Job.JobType,
			"attempt":   env.Job.Attempt,
		})
		c.handleWithRetry(ctx, workerID, env)
	}
}
func (c *Consumer) handleWithRetry(ctx context.Context, workerID int, env Envelope) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
		}
		start := time.Now()
		err := c.handler.Handle(ctx, env)
		dur := time.Since(start).Milliseconds()
		if err == nil {
			c.logger("info", "job_handled", map[string]any{
				"event":       "handled",
				"worker_id":   workerID,
				"tenant_id":   env.Job.TenantID,
				"job_id":      env.Job.JobID,
				"source_id":   env.Job.SourceID,
				"job_type":    env.Job.JobType,
				"attempt":     env.Job.Attempt,
				"duration_ms": dur,
			})
			return
		}
		nextAttempt := env.Job.Attempt + 1
		delay, ok, reason := time.Duration(0), false, "no_retry_policy"
		if c.retry != nil {
			delay, ok, reason = c.retry.Next(env.Job.JobID, nextAttempt)
		}
		if !ok || delay <= 0 {
			c.logger("error", "job_terminal_failure", map[string]any{
				"event":       "terminal_failure",
				"worker_id":   workerID,
				"tenant_id":   env.Job.TenantID,
				"job_id":      env.Job.JobID,
				"source_id":   env.Job.SourceID,
				"job_type":    env.Job.JobType,
				"attempt":     env.Job.Attempt,
				"duration_ms": dur,
				"error":       err.Error(),
				"reason":      reason,
			})
			return
		}
		c.logger("warn", "job_retry_scheduled", map[string]any{
			"event":       "retry_scheduled",
			"worker_id":   workerID,
			"tenant_id":   env.Job.TenantID,
			"job_id":      env.Job.JobID,
			"source_id":   env.Job.SourceID,
			"job_type":    env.Job.JobType,
			"attempt":     nextAttempt,
			"delay_ms":    delay.Milliseconds(),
			"duration_ms": dur,
			"error":       err.Error(),
			"reason":      reason,
		})
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		}

		// Local re-dispatch (in-memory): increment attempt and continue.
		env.Job.Attempt = nextAttempt
	}
}
func jitterDuration(c *Consumer, base time.Duration, pct float64) time.Duration {
	if pct <= 0 {
		return base
	}
	min := float64(base)
	min = min * (1.0 - pct)
	max := float64(base)
	max = max * (1.0 + pct)
	if min < 0 {
		min = 0
	}
	delta := max - min
	if delta <= 0 {
		return base
	}
	c.rndMu.Lock()
	u := c.rnd.Float64()
	c.rndMu.Unlock()
	return time.Duration(min + u*delta)
}
