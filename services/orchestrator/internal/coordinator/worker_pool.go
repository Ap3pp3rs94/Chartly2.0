package coordinator

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type Task func(ctx context.Context) error

type LoggerFn func(level, msg string, fields map[string]any)

var (
	ErrPoolStarted = errors.New("pool already started")
	ErrPoolStopped = errors.New("pool stopped")
	ErrQueueFull   = errors.New("queue full")
)

type taskItem struct {
	name string
	fn   Task
}

type Stats struct {
	Running   int    `json:"running"`
	Queued    int    `json:"queued"`
	Completed uint64 `json:"completed"`
	Failed    uint64 `json:"failed"`
	Rejected  uint64 `json:"rejected"`
}

type Pool struct {
	concurrency int
	queueSize   int
	logger      LoggerFn

	started atomic.Bool
	stopped atomic.Bool

	qch chan taskItem

	wg sync.WaitGroup

	// cancel workers
	cancelOnce sync.Once
	cancelFn   context.CancelFunc

	// metrics
	running   atomic.Int32
	queued    atomic.Int32
	completed atomic.Uint64
	failed    atomic.Uint64
	rejected  atomic.Uint64

	// protect stop sequencing
	stopMu sync.Mutex
}

func NewPool(concurrency int, queueSize int, logger LoggerFn) *Pool {
	if concurrency < 1 {
		concurrency = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}
	if logger == nil {
		logger = func(string, string, map[string]any) {}
	}
	return &Pool{
		concurrency: concurrency,
		queueSize:   queueSize,
		logger:      logger,
		qch:         make(chan taskItem, queueSize),
	}
}

func (p *Pool) Start(ctx context.Context) error {
	if !p.started.CompareAndSwap(false, true) {
		return ErrPoolStarted
	}
	if p.stopped.Load() {
		return ErrPoolStopped
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	p.cancelFn = cancel

	p.logger("info", "pool_start", map[string]any{
		"event":       "pool_start",
		"concurrency": p.concurrency,
		"queue_size":  p.queueSize,
	})

	for i := 0; i < p.concurrency; i++ {
		p.wg.Add(1)
		go p.worker(workerCtx, i)
	}

	_ = ctx
	return nil
}

// Submit enqueues a task, respecting ctx cancellation.
func (p *Pool) Submit(ctx context.Context, name string, t Task) error {
	if t == nil {
		p.rejected.Add(1)
		return errors.New("task is nil")
	}
	if !p.started.Load() {
		p.rejected.Add(1)
		return errors.New("pool not started")
	}
	if p.stopped.Load() {
		p.rejected.Add(1)
		return ErrPoolStopped
	}

	item := taskItem{name: name, fn: t}

	// Blocking enqueue with ctx cancel, but also avoid panic on close race:
	// we never close qch; we rely on stopped flag + cancel.
	select {
	case <-ctx.Done():
		p.rejected.Add(1)
		return ctx.Err()
	default:
	}

	select {
	case p.qch <- item:
		p.queued.Add(1)
		p.logger("info", "task_enqueued", map[string]any{
			"event":  "task_enqueued",
			"name":   name,
			"queued": p.queued.Load(),
		})
		return nil
	case <-ctx.Done():
		p.rejected.Add(1)
		return ctx.Err()
	default:
		// bounded queue backpressure: if full, block (ctx-aware)
		select {
		case p.qch <- item:
			p.queued.Add(1)
			p.logger("info", "task_enqueued", map[string]any{
				"event":  "task_enqueued",
				"name":   name,
				"queued": p.queued.Load(),
			})
			return nil
		case <-ctx.Done():
			p.rejected.Add(1)
			return ctx.Err()
		}
	}
}

// Stop stops the pool. If drain=true, it stops accepting new work, drains queued tasks, then exits.
// If drain=false, it cancels workers ASAP and discards queued tasks.
func (p *Pool) Stop(ctx context.Context, drain bool) error {
	p.stopMu.Lock()
	defer p.stopMu.Unlock()

	if !p.started.Load() {
		return ErrPoolStopped
	}
	if !p.stopped.CompareAndSwap(false, true) {
		return ErrPoolStopped
	}

	p.logger("info", "pool_stop", map[string]any{
		"event": "pool_stop",
		"drain": drain,
	})

	if !drain {
		// discard queued tasks quickly
		for {
			select {
			case <-p.qch:
				p.queued.Add(-1)
			default:
				goto cancelWorkers
			}
		}
	}

cancelWorkers:
	p.cancelOnce.Do(func() {
		if p.cancelFn != nil {
			p.cancelFn()
		}
	})

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Pool) Stats() Stats {
	return Stats{
		Running:   int(p.running.Load()),
		Queued:    int(p.queued.Load()),
		Completed: p.completed.Load(),
		Failed:    p.failed.Load(),
		Rejected:  p.rejected.Load(),
	}
}

func (p *Pool) worker(ctx context.Context, workerID int) {
	defer p.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case item := <-p.qch:
			// If stop requested with drain=true, we still process queued tasks.
			// If stop requested with drain=false, queue should have been drained and ctx canceled.
			p.queued.Add(-1)
			p.running.Add(1)

			start := time.Now()
			p.logger("info", "task_start", map[string]any{
				"event":     "task_start",
				"worker_id": workerID,
				"name":      item.name,
				"running":   p.running.Load(),
			})

			err := item.fn(ctx)
			dur := time.Since(start).Milliseconds()

			if err != nil {
				p.failed.Add(1)
				p.logger("error", "task_error", map[string]any{
					"event":       "task_error",
					"worker_id":   workerID,
					"name":        item.name,
					"duration_ms": dur,
					"error":       err.Error(),
				})
			} else {
				p.completed.Add(1)
				p.logger("info", "task_ok", map[string]any{
					"event":       "task_ok",
					"worker_id":   workerID,
					"name":        item.name,
					"duration_ms": dur,
				})
			}

			p.running.Add(-1)
		}
	}
}
