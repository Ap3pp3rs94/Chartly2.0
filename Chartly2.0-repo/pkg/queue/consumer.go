package queue

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Handler func(ctx context.Context, msg DequeueResult) // error type Logger interface {
	Printf(format string, args ...any)
}
type Metrics interface {
	IncDequeueEmpty(queue QueueName)
	IncDequeueError(queue QueueName)
	IncAck(queue QueueName)
	IncAckError(queue QueueName)
	IncNack(queue QueueName)
	IncNackError(queue QueueName)
	IncRetry(queue QueueName)
	IncDLQ(queue QueueName)
	ObserveHandleDuration(queue QueueName, d time.Duration)
}
type Clock interface {
	Now()
	// time.Time
}
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type RetryDecision struct {
	Delay  time.Duration
	ToDLQ  bool
	Reason string
}
type RetryPolicy interface {
	Decide(env Envelope, handlerErr error)
	RetryDecision
}
type DefaultRetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	JitterPct   int
}

func (p DefaultRetryPolicy) Decide(env Envelope, handlerErr error) RetryDecision {
	maxAtt := p.MaxAttempts
	if maxAtt <= 0 {
		maxAtt = MaxRecommendedAttempts

	}
	base := p.BaseDelay
	if base <= 0 {
		base = 250 * time.Millisecond

	}
	maxD := p.MaxDelay
	if maxD <= 0 {
		maxD = 30 * time.Second

	}
	jp := p.JitterPct
	if jp <= 0 || jp > 50 {
		jp = 20

	}
	att := env.Attempt
	if att < 0 {
		att = 0

	}
	if att >= maxAtt {
		return RetryDecision{
			ToDLQ:  true,
			Delay:  0,
			Reason: fmt.Sprintf("max_attempts_exceeded:%d", maxAtt),
		}

	}
	shift := att
	if shift > 20 {
		shift = 20

	}
	factor := time.Duration(1 << uint(shift))
	delay := base * factor
	if delay > maxD {
		delay = maxD

	}
	delay = deterministicJitter(delay, jp, "retry", string(env.ID), env.Type, env.Tenant, att)
	if delay < 0 {
		delay = 0

	}
	return RetryDecision{Delay: delay, ToDLQ: false}
}

type RunnerOptions struct {
	Queue QueueName

	Concurrency int

	PollTimeout       time.Duration
	VisibilityTimeout time.Duration

	EmptyBackoffMin time.Duration
	EmptyBackoffMax time.Duration

	HandlerTimeout time.Duration

	MaxConsecutiveErrors int

	Logger  Logger
	Metrics Metrics
	Clock   Clock

	Retry RetryPolicy
}
type Runner struct {
	consumer Consumer
	handler  Handler
	opts     RunnerOptions
	clock    Clock
}

func NewRunner(consumer Consumer, handler Handler, opts RunnerOptions) (*Runner, error) {
	if consumer == nil {
		return nil, fmt.Errorf("%w: consumer is nil", ErrInvalid)

	}
	if handler == nil {
		return nil, fmt.Errorf("%w: handler is nil", ErrInvalid)

	}
	if strings.TrimSpace(string(opts.Queue)) == "" {
		return nil, fmt.Errorf("%w: queue name required", ErrInvalid)

	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4

	}
	if opts.Concurrency > 256 {
		opts.Concurrency = 256

	}
	if opts.PollTimeout <= 0 {
		opts.PollTimeout = 2 * time.Second

	}
	if opts.VisibilityTimeout <= 0 {
		opts.VisibilityTimeout = 30 * time.Second

	}
	if opts.EmptyBackoffMin <= 0 {
		opts.EmptyBackoffMin = 200 * time.Millisecond

	}
	if opts.EmptyBackoffMax <= 0 {
		opts.EmptyBackoffMax = 5 * time.Second

	}
	if opts.EmptyBackoffMax < opts.EmptyBackoffMin {
		opts.EmptyBackoffMax = opts.EmptyBackoffMin

	}
	if opts.Retry == nil {
		opts.Retry = DefaultRetryPolicy{}

	}
	clk := opts.Clock
	if clk == nil {
		clk = systemClock{}

	}
	return &Runner{
		consumer: consumer,
		handler:  handler,
		opts:     opts,
		clock:    clk,
	}, nil
}
func (r *Runner) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()

	}
	var wg sync.WaitGroup
	errCh := make(chan error, r.opts.Concurrency)
	for i := 0; i < r.opts.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			if err := r.workerLoop(ctx, workerID); err != nil &&
				!errors.Is(err, context.Canceled) &&
				!errors.Is(err, context.DeadlineExceeded) {
				select {
				case errCh <- err:
				default:

				}
			}
		}(i + 1)

	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		<-done
		return ctx.Err()
	case err := <-errCh:
		<-done
		return err
	case <-done:
		if ctx.Err() != nil {
			return ctx.Err()

		}
		return nil

	}
}
func (r *Runner) workerLoop(ctx context.Context, workerID int) error {
	logf := func(format string, args ...any) {
		if r.opts.Logger != nil {
			r.opts.Logger.Printf(format, args...)

		}
	}
	metrics := r.opts.Metrics

	backoff := r.opts.EmptyBackoffMin
	consecErr := 0

	for {
		if err := ctx.Err(); err != nil {
			return err

		}
		res, err := r.consumer.Dequeue(ctx, r.opts.Queue, r.opts.PollTimeout, r.opts.VisibilityTimeout)
		if err != nil {
			if errors.Is(err, ErrEmpty) {
				if metrics != nil {
					metrics.IncDequeueEmpty(r.opts.Queue)

				}
				sleep := deterministicJitter(backoff, 20, "empty", string(r.opts.Queue), workerID)
				if sleep < 0 {
					sleep = 0

				}
				timer := time.NewTimer(sleep)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:

				}
				backoff = backoff * 2
				if backoff > r.opts.EmptyBackoffMax {
					backoff = r.opts.EmptyBackoffMax

				}
				continue

			}
			consecErr++
			if metrics != nil {
				metrics.IncDequeueError(r.opts.Queue)

			}
			logf("queue runner: worker=%d dequeue error: %v", workerID, err)
			if r.opts.MaxConsecutiveErrors > 0 && consecErr >= r.opts.MaxConsecutiveErrors {
				return fmt.Errorf("queue runner: max consecutive errors reached (%d): %w", consecErr, err)

			}
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:

			}
			continue

		}
		backoff = r.opts.EmptyBackoffMin
		consecErr = 0

		hctx := ctx
		var cancel context.CancelFunc
		if r.opts.HandlerTimeout > 0 {
			hctx, cancel = context.WithTimeout(ctx, r.opts.HandlerTimeout)

		}
		hctx = WithMessageContext(hctx, r.opts.Queue, res)
		start := r.clock.Now()
		herr := r.handler(hctx, res)
		dur := r.clock.Now().Sub(start)
		if cancel != nil {
			cancel()

		}
		if metrics != nil {
			metrics.ObserveHandleDuration(r.opts.Queue, dur)

		}
		if herr == nil {
			if err := r.consumer.Ack(ctx, r.opts.Queue, res.Receipt); err != nil {
				if metrics != nil {
					metrics.IncAckError(r.opts.Queue)

				}
				logf("queue runner: worker=%d ack error: %v", workerID, err)
				_ = r.consumer.Nack(ctx, r.opts.Queue, res.Receipt, 1*time.Second)
				if metrics != nil {
					metrics.IncNack(r.opts.Queue)

				}
			} else if metrics != nil {
				metrics.IncAck(r.opts.Queue)

			}
			continue

		}
		decision := r.opts.Retry.Decide(res.Env, herr)
		if decision.ToDLQ {
			if metrics != nil {
				metrics.IncDLQ(r.opts.Queue)

			}
			if err := r.consumer.NackWithDeadLetter(ctx, r.opts.Queue, res.Receipt, decision.Delay, decision.Reason); err == nil {
				logf("queue runner: worker=%d dlq via nack-with-dlq reason=%q", workerID, decision.Reason)
				continue

			}
			if dlq, ok := r.consumer.(DeadLetter); ok {
				_ = dlq.MoveToDLQ(ctx, r.opts.Queue, res.Receipt, decision.Reason)
				continue

			}
			_ = r.consumer.Nack(ctx, r.opts.Queue, res.Receipt, decision.Delay)
			if metrics != nil {
				metrics.IncNack(r.opts.Queue)

			}
			continue

		}
		if metrics != nil {
			metrics.IncRetry(r.opts.Queue)

		}
		if err := r.consumer.Nack(ctx, r.opts.Queue, res.Receipt, decision.Delay); err != nil {
			if metrics != nil {
				metrics.IncNackError(r.opts.Queue)

			}
			logf("queue runner: worker=%d nack error: %v", workerID, err)
			_ = r.consumer.Ack(ctx, r.opts.Queue, res.Receipt)
			if metrics != nil {
				metrics.IncAck(r.opts.Queue)

			}
		} else if metrics != nil {
			metrics.IncNack(r.opts.Queue)

		}
	}
}

// type msgCtxKey string

const (
	ctxQueueName msgCtxKey = "queue.queue_name"
	ctxReceipt   msgCtxKey = "queue.receipt"
	ctxMsgID     msgCtxKey = "queue.message_id"
	ctxAttempt   msgCtxKey = "queue.attempt"
	ctxType      msgCtxKey = "queue.type"
	ctxTenant    msgCtxKey = "queue.tenant"
)

func WithMessageContext(ctx context.Context, q QueueName, msg DequeueResult) context.Context {
	ctx = context.WithValue(ctx, ctxQueueName, string(q))
	ctx = context.WithValue(ctx, ctxReceipt, msg.Receipt)
	ctx = context.WithValue(ctx, ctxMsgID, string(msg.Env.ID))
	ctx = context.WithValue(ctx, ctxAttempt, msg.Env.Attempt)
	ctx = context.WithValue(ctx, ctxType, msg.Env.Type)
	ctx = context.WithValue(ctx, ctxTenant, msg.Env.Tenant)
	return ctx
}
func deterministicJitter(base time.Duration, pct int, parts ...any) time.Duration {
	if pct <= 0 {
		return base

	}
	if pct > 50 {
		pct = 50

	}
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(fmt.Sprint(p)))
		_, _ = h.Write([]byte{0})

	}
	sum := h.Sum(nil)
	u := binary.LittleEndian.Uint64(sum[:8])
	span := uint64(pct*2 + 1)
	deltaPct := int(u%span) - pct

	delta := (base * time.Duration(deltaPct)) / 100
	return base + delta
}
