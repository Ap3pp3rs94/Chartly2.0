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

	slots    chan struct{}
	items    chan struct{}
	closed   atomic.Bool
	dropped  atomic.Uint64
	closedCh chan struct{}
	mu       sync.Mutex
}

func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	r := &RingBuffer{
		capacity: capacity,
		buf:      make([][]byte, capacity),
		slots:    make(chan struct{}, capacity),
		items:    make(chan struct{}, capacity),
		closedCh: make(chan struct{}),
	}
	for i := 0; i < capacity; i++ {
		r.slots <- struct{}{}
	}
	return r
}
func (r *RingBuffer) Close() {
	if r.closed.CompareAndSwap(false, true) {
		close(r.closedCh)
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
	select {
	case <-r.slots:
		// proceed
	case <-r.closedCh:
		r.dropped.Add(1)
		return ErrClosed
	default:
		return ErrWouldBlock
	}
	if r.closed.Load() {
		// release slot
		r.slots <- struct{}{}
		r.dropped.Add(1)
		return ErrClosed
	}
	r.mu.Lock()
	r.buf[r.tail] = chunk
	r.tail = (r.tail + 1) % r.capacity
	r.size++
	r.mu.Unlock()
	r.items <- struct{}{}
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
	select {
	case <-r.slots:
		// acquired slot
	case <-ctx.Done():
		r.dropped.Add(1)
		return ctx.Err()
	case <-r.closedCh:
		r.dropped.Add(1)
		return ErrClosed
	}
	if r.closed.Load() {
		// release slot
		r.slots <- struct{}{}
		r.dropped.Add(1)
		return ErrClosed
	}
	r.mu.Lock()
	r.buf[r.tail] = chunk
	r.tail = (r.tail + 1) % r.capacity
	r.size++
	r.mu.Unlock()
	r.items <- struct{}{}
	return nil
}
func (r *RingBuffer) TryPop() ([]byte, error) {
	select {
	case <-r.items:
		r.mu.Lock()
		chunk := r.popLocked()
		r.mu.Unlock()
		// release slot
		r.slots <- struct{}{}
		return chunk, nil
	case <-r.closedCh:
		// if closed and empty, return closed
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.size == 0 {
			return nil, ErrClosed
		}
		chunk := r.popLocked()
		// release slot
		r.slots <- struct{}{}
		return chunk, nil
	default:
		if r.closed.Load() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.size == 0 {
				return nil, ErrClosed
			}
		}
		return nil, ErrWouldBlock
	}
}
func (r *RingBuffer) Pop(ctx context.Context) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	for {
		select {
		case <-r.items:
			r.mu.Lock()
			chunk := r.popLocked()
			r.mu.Unlock()
			r.slots <- struct{}{}
			return chunk, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.closedCh:
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.size == 0 {
				return nil, ErrClosed
			}
			chunk := r.popLocked()
			r.slots <- struct{}{}
			return chunk, nil
		}
	}
}
func (r *RingBuffer) popLocked() []byte {
	chunk := r.buf[r.head]
	r.buf[r.head] = nil
	r.head = (r.head + 1) % r.capacity
	r.size--
	return chunk
}
