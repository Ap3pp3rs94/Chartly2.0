package streaming

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

var (
	ErrClosed     = errors.New("closed")
	ErrWouldBlock = errors.New("would block")
)

type Stats struct {
	Capacity int    `json:"capacity"`
	Len      int    `json:"len"`
	Dropped  uint64 `json:"dropped"`
	Closed   bool   `json:"closed"`
}

// RingBuffer is a bounded buffer of byte chunks.
// It stores []byte references; callers must not mutate after pushing.
type RingBuffer struct {
	capacity int
	buf      [][]byte
	head     int
	tail     int
	size     int

	mu       sync.Mutex
	notEmpty *sync.Cond
	notFull  *sync.Cond

	closed  atomic.Bool
	dropped atomic.Uint64
}

func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}

	r := &RingBuffer{
		capacity: capacity,
		buf:      make([][]byte, capacity),
	}
		r.notEmpty = sync.NewCond(&r.mu)
		r.notFull = sync.NewCond(&r.mu)
	return r
}

func (r *RingBuffer) Close() {
	if r.closed.CompareAndSwap(false, true) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.notEmpty.Broadcast()
		r.notFull.Broadcast()
	}
}

func (r *RingBuffer) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()

	return Stats{
		Capacity: r.capacity,
		Len:      r.size,
		Dropped:  r.dropped.Load(),
		Closed:   r.closed.Load(),
	}
}

func (r *RingBuffer) TryPush(chunk []byte) error {
	if r.closed.Load() {
		r.dropped.Add(1)
		return ErrClosed
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed.Load() {
		r.dropped.Add(1)
		return ErrClosed
	}
	if r.size == r.capacity {
		return ErrWouldBlock
	}

	r.pushLocked(chunk)
	return nil
}

func (r *RingBuffer) Push(ctx context.Context, chunk []byte) error {
	if ctx.Err() != nil {
		r.dropped.Add(1)
		return ctx.Err()
	}
	if r.closed.Load() {
		r.dropped.Add(1)
		return ErrClosed
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for r.size == r.capacity && !r.closed.Load() {
		if ctx.Err() != nil {
			r.dropped.Add(1)
			return ctx.Err()
		}

		waitCh := make(chan struct{})
		go func() {
			r.notFull.Wait()
			close(waitCh)
		}()

		r.mu.Unlock()
		select {
		case <-waitCh:
		case <-ctx.Done():
			r.mu.Lock()
			r.dropped.Add(1)
			return ctx.Err()
		}
		r.mu.Lock()
	}

	if r.closed.Load() {
		r.dropped.Add(1)
		return ErrClosed
	}

	r.pushLocked(chunk)
	return nil
}

func (r *RingBuffer) TryPop() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == 0 {
		if r.closed.Load() {
			return nil, ErrClosed
		}
		return nil, ErrWouldBlock
	}

	return r.popLocked(), nil
}

func (r *RingBuffer) Pop(ctx context.Context) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for r.size == 0 && !r.closed.Load() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		waitCh := make(chan struct{})
		go func() {
			r.notEmpty.Wait()
			close(waitCh)
		}()

		r.mu.Unlock()
		select {
		case <-waitCh:
		case <-ctx.Done():
			r.mu.Lock()
			return nil, ctx.Err()
		}
		r.mu.Lock()
	}

	if r.size == 0 && r.closed.Load() {
		return nil, ErrClosed
	}

	return r.popLocked(), nil
}

func (r *RingBuffer) pushLocked(chunk []byte) {
	r.buf[r.tail] = chunk
	r.tail = (r.tail + 1) % r.capacity
	r.size++
	r.notEmpty.Signal()
}

func (r *RingBuffer) popLocked() []byte {
	chunk := r.buf[r.head]
	r.buf[r.head] = nil
	r.head = (r.head + 1) % r.capacity
	r.size--
	r.notFull.Signal()
	return chunk
}
