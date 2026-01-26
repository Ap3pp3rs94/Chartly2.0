package pool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrAcquireTimeout = errors.New("acquire timeout")
	ErrPoolClosed     = errors.New("pool closed")
	ErrFactoryNil     = errors.New("factory is nil")
	ErrCloseNil       = errors.New("close func is nil")
)

type Factory func(ctx context.Context) (any, error)
type CloseFunc func(any) error
type HealthFunc func(any) error
type LoggerFn func(level, msg string, fields map[string]any)

type Config struct {
	MaxSize        int           `json:"max_size"`
	MinSize        int           `json:"min_size"`
	AcquireTimeout time.Duration `json:"acquire_timeout"`
	IdleTimeout    time.Duration `json:"idle_timeout"`
	MaxLifetime    time.Duration `json:"max_lifetime"`
	ReapInterval   time.Duration `json:"reap_interval"`
}

func DefaultConfig() Config {
	return Config{
		MaxSize:        10,
		MinSize:        0,
		AcquireTimeout: 2 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxLifetime:    10 * time.Minute,
		ReapInterval:   30 * time.Second,
	}
}

type Stats struct {
	Total           uint64 `json:"total"`
	Idle            uint64 `json:"idle"`
	InUse           uint64 `json:"in_use"`
	Created         uint64 `json:"created"`
	Closed          uint64 `json:"closed"`
	AcquireTimeouts uint64 `json:"acquire_timeouts"`
}

type pooled struct {
	res      any
	created  time.Time
	lastUsed time.Time
}

type ReleaseFunc func(healthy bool)

type Pool struct {
	cfg     Config
	factory Factory
	closeFn CloseFunc
	health  HealthFunc
	logger  LoggerFn

	ch chan *pooled

	startOnce sync.Once
	stopOnce  sync.Once

	mu     sync.Mutex
	closed bool

	wg sync.WaitGroup

	// metrics
	total           atomic.Uint64
	idle            atomic.Uint64
	inUse           atomic.Uint64
	created         atomic.Uint64
	closedCount     atomic.Uint64
	acquireTimeouts atomic.Uint64
}

func New(cfg Config, factory Factory, closeFn CloseFunc, health HealthFunc, logger LoggerFn) (*Pool, error) {
	if factory == nil {
		return nil, ErrFactoryNil
	}
	if closeFn == nil {
		return nil, ErrCloseNil
	}

	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 10
	}
	if cfg.MinSize < 0 {
		cfg.MinSize = 0
	}
	if cfg.MinSize > cfg.MaxSize {
		cfg.MinSize = cfg.MaxSize
	}
	if cfg.AcquireTimeout <= 0 {
		cfg.AcquireTimeout = 2 * time.Second
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 60 * time.Second
	}
	if cfg.MaxLifetime <= 0 {
		cfg.MaxLifetime = 10 * time.Minute
	}
	if cfg.ReapInterval <= 0 {
		cfg.ReapInterval = 30 * time.Second
	}

	if logger == nil {
		logger = func(string, string, map[string]any) {}
	}

	p := &Pool{
		cfg:     cfg,
		factory: factory,
		closeFn: closeFn,
		health:  health,
		logger:  logger,
		ch:      make(chan *pooled, cfg.MaxSize),
	}

	return p, nil
}

func (p *Pool) Start(ctx context.Context) error {
	var err error
	p.startOnce.Do(func() {
		// warm min size
		for i := 0; i < p.cfg.MinSize; i++ {
			r, e := p.newResource(ctx)
			if e != nil {
				err = e
				break
			}
			p.putIdle(r)
		}

		// start reaper
		p.wg.Add(1)
		go p.reaper(ctx)

		p.logger("info", "pool_start", map[string]any{
			"event":    "pool_start",
			"max_size": p.cfg.MaxSize,
			"min_size": p.cfg.MinSize,
		})
	})

	return err
}

func (p *Pool) Stop(ctx context.Context) error {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()

		// drain and close idle resources
		for {
			select {
			case it := <-p.ch:
				if it != nil {
					_ = p.closeResource(it.res)
					p.total.Add(^uint64(0))
					p.idle.Add(^uint64(0))
				}
			default:
				goto drained
			}
		}
	drained:
		// wait for reaper
		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-ctx.Done():
		}

		p.logger("info", "pool_stop", map[string]any{"event": "pool_stop"})
	})

	return ctx.Err()
}

func (p *Pool) Acquire(ctx context.Context) (any, ReleaseFunc, error) {
	if p.isClosed() {
		return nil, nil, ErrPoolClosed
	}

	// enforce acquire timeout
	timeout := p.cfg.AcquireTimeout
	if deadline, ok := ctx.Deadline(); ok {
		// if caller deadline sooner, honor it
		if d := time.Until(deadline); d > 0 && d < timeout {
			timeout = d
		}
	}

	acqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Try grab idle first
	select {
	case it := <-p.ch:
		if it != nil {
			p.idle.Add(^uint64(0))
			p.inUse.Add(1)

			// validate lifetime/idle
			now := time.Now()
			if p.isExpired(now, it) {
				_ = p.closeResource(it.res)
				p.total.Add(^uint64(0))
				p.inUse.Add(^uint64(0))
				// fall through to create new
			} else if p.health != nil {
				if err := p.health(it.res); err != nil {
					_ = p.closeResource(it.res)
					p.total.Add(^uint64(0))
					p.inUse.Add(^uint64(0))
				} else {
					return it.res, p.makeRelease(it), nil
				}
			} else {
				return it.res, p.makeRelease(it), nil
			}
		}
	default:
		// none idle
	}

	// Create new if we can
	for {
		if p.isClosed() {
			return nil, nil, ErrPoolClosed
		}

		curTotal := p.total.Load()
		if int(curTotal) < p.cfg.MaxSize {
			// optimistic reserve
			if p.total.CompareAndSwap(curTotal, curTotal+1) {
				res, err := p.factory(acqCtx)
				if err != nil {
					// rollback slot
					p.total.Add(^uint64(0))
					return nil, nil, err
				}
				p.created.Add(1)
				p.inUse.Add(1)
				it := &pooled{res: res, created: time.Now(), lastUsed: time.Now()}
				return res, p.makeRelease(it), nil
			}
			continue
		}

		// pool maxed: wait for idle resource
		select {
		case <-acqCtx.Done():
			p.acquireTimeouts.Add(1)
			return nil, nil, ErrAcquireTimeout
		case it := <-p.ch:
			if it == nil {
				continue
			}

			p.idle.Add(^uint64(0))
			p.inUse.Add(1)

			now := time.Now()
			if p.isExpired(now, it) {
				_ = p.closeResource(it.res)
				p.total.Add(^uint64(0))
				p.inUse.Add(^uint64(0))
				continue
			}

			if p.health != nil {
				if err := p.health(it.res); err != nil {
					_ = p.closeResource(it.res)
					p.total.Add(^uint64(0))
					p.inUse.Add(^uint64(0))
					continue
				}
			}

			return it.res, p.makeRelease(it), nil
		}
	}
}

func (p *Pool) Stats() Stats {
	return Stats{
		Total:           p.total.Load(),
		Idle:            p.idle.Load(),
		InUse:           p.inUse.Load(),
		Created:         p.created.Load(),
		Closed:          p.closedCount.Load(),
		AcquireTimeouts: p.acquireTimeouts.Load(),
	}
}

func (p *Pool) makeRelease(it *pooled) ReleaseFunc {
	var once sync.Once
	return func(healthy bool) {
		once.Do(func() {
			p.inUse.Add(^uint64(0))
			it.lastUsed = time.Now()

			if p.isClosed() {
				_ = p.closeResource(it.res)
				p.total.Add(^uint64(0))
				return
			}

			if !healthy {
				_ = p.closeResource(it.res)
				p.total.Add(^uint64(0))
				return
			}

			// return to idle if possible; otherwise close
			select {
			case p.ch <- it:
				p.idle.Add(1)
			default:
				_ = p.closeResource(it.res)
				p.total.Add(^uint64(0))
			}
		})
	}
}

func (p *Pool) newResource(ctx context.Context) (*pooled, error) {
	res, err := p.factory(ctx)
	if err != nil {
		return nil, err
	}

	p.total.Add(1)
	p.created.Add(1)
	return &pooled{res: res, created: time.Now(), lastUsed: time.Now()}, nil
}

func (p *Pool) putIdle(it *pooled) {
	if it == nil {
		return
	}

	select {
	case p.ch <- it:
		p.idle.Add(1)
	default:
		_ = p.closeResource(it.res)
		p.total.Add(^uint64(0))
	}
}

func (p *Pool) closeResource(res any) error {
	p.closedCount.Add(1)
	return p.closeFn(res)
}

func (p *Pool) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *Pool) isExpired(now time.Time, it *pooled) bool {
	if it == nil {
		return true
	}

	if p.cfg.MaxLifetime > 0 && now.Sub(it.created) > p.cfg.MaxLifetime {
		return true
	}

	if p.cfg.IdleTimeout > 0 && now.Sub(it.lastUsed) > p.cfg.IdleTimeout {
		return true
	}

	return false
}

func (p *Pool) reaper(ctx context.Context) {
	defer p.wg.Done()

	t := time.NewTicker(p.cfg.ReapInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.reapOnce()
		}
	}
}

func (p *Pool) reapOnce() {
	if p.isClosed() {
		return
	}

	now := time.Now()

	// best-effort: scan up to current idle length
	n := len(p.ch)
	for i := 0; i < n; i++ {
		select {
		case it := <-p.ch:
			if it == nil {
				continue
			}

			p.idle.Add(^uint64(0))

			if p.isExpired(now, it) {
				_ = p.closeResource(it.res)
				p.total.Add(^uint64(0))
				continue
			}

			// keep it
			select {
			case p.ch <- it:
				p.idle.Add(1)
			default:
				_ = p.closeResource(it.res)
				p.total.Add(^uint64(0))
			}
		default:
			return
		}
	}
}
