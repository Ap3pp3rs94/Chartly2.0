package streaming

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

type StreamID string

type Meta struct {
	TenantID    string `json:"tenant_id"`
	SourceID    string `json:"source_id"`
	ConnectorID string `json:"connector_id"`
	Domain      string `json:"domain"`
	CreatedAt   string `json:"created_at"`
}

type StreamSource interface {
	Open(ctx context.Context, meta Meta) (io.ReadCloser, error)
}

type StreamSink interface {
	Write(ctx context.Context, meta Meta, chunk []byte) error
	Close(ctx context.Context, meta Meta) error
}

type Limits interface {
	Acquire(ctx context.Context, domain string) (release func(), err error)
}

type Breaker interface {
	Allow(key string) (ok bool, state string, reason string)
	Report(key string, success bool)
}

type LoggerFn func(level, msg string, fields map[string]any)

type StreamManager struct {
	mu sync.Mutex

	buffers map[StreamID]*RingBuffer
	metas   map[StreamID]Meta
	cancels map[StreamID]context.CancelFunc

	source  StreamSource
	sink    StreamSink
	limits  Limits
	breaker Breaker

	chunkSize    int
	bufferChunks int
	logger       LoggerFn
}

func NewStreamManager(source StreamSource, sink StreamSink, limits Limits, breaker Breaker, logger LoggerFn) *StreamManager {
	if logger == nil {
		logger = func(string, string, map[string]any) {}
	}

	return &StreamManager{
		buffers:      make(map[StreamID]*RingBuffer),
		metas:        make(map[StreamID]Meta),
		cancels:      make(map[StreamID]context.CancelFunc),
		source:       source,
		sink:         sink,
		limits:       limits,
		breaker:      breaker,
		chunkSize:    32 * 1024,
		bufferChunks: 64,
		logger:       logger,
	}
}

func (m *StreamManager) WithChunkSize(n int) *StreamManager {
	if n > 0 {
		m.chunkSize = n
	}
	return m
}

func (m *StreamManager) WithBufferChunks(n int) *StreamManager {
	if n > 0 {
		m.bufferChunks = n
	}
	return m
}

func (m *StreamManager) StartStream(ctx context.Context, id StreamID, meta Meta) error {
	m.mu.Lock()
	if _, exists := m.buffers[id]; exists {
		m.mu.Unlock()
		return errors.New("stream already exists")
	}

	if meta.CreatedAt == "" {
		meta.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if meta.Domain == "" {
		meta.Domain = "unknown"
	}

	buf := NewRingBuffer(m.bufferChunks)
	streamCtx, cancel := context.WithCancel(ctx)

	m.buffers[id] = buf
	m.metas[id] = meta
	m.cancels[id] = cancel
	m.mu.Unlock()

	m.logger("info", "stream_start", map[string]any{
		"event":        "stream_start",
		"stream_id":    string(id),
		"tenant_id":    meta.TenantID,
		"source_id":    meta.SourceID,
		"connector_id": meta.ConnectorID,
		"domain":       meta.Domain,
	})

	go m.readerLoop(streamCtx, id, meta, buf)
	go m.writerLoop(streamCtx, id, meta, buf)

	return nil
}

func (m *StreamManager) StopStream(ctx context.Context, id StreamID) error {
	_ = ctx

	m.mu.Lock()
	buf, ok := m.buffers[id]
	cancel, cok := m.cancels[id]
	meta, mok := m.metas[id]

	if cok {
		cancel()
	}
	if ok {
		buf.Close()
	}

	delete(m.buffers, id)
	delete(m.cancels, id)
	delete(m.metas, id)
	m.mu.Unlock()

	if mok {
		m.logger("info", "stream_stop", map[string]any{
			"event":     "stream_stop",
			"stream_id": string(id),
			"tenant_id": meta.TenantID,
			"source_id": meta.SourceID,
		})
	}

	return nil
}

func (m *StreamManager) Stats(id StreamID) (Meta, Stats, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	buf, ok := m.buffers[id]
	if !ok {
		return Meta{}, Stats{}, false
	}

	meta := m.metas[id]
	return meta, buf.Stats(), true
}

func (m *StreamManager) readerLoop(ctx context.Context, id StreamID, meta Meta, buf *RingBuffer) {
	defer func() {
		// reader closes buffer to unblock writer
		buf.Close()
	}()

	if m.source == nil {
		m.logger("error", "stream_source_nil", map[string]any{
			"event":     "source_nil",
			"stream_id": string(id),
		})
		return
	}

	rc, err := m.source.Open(ctx, meta)
	if err != nil {
		m.logger("error", "stream_open_failed", map[string]any{
			"event":     "open_failed",
			"stream_id": string(id),
			"error":     err.Error(),
		})
		return
	}
	defer rc.Close()

	tmp := make([]byte, m.chunkSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, rerr := rc.Read(tmp)
		if n > 0 {
			// copy into a right-sized slice so tmp can be reused
			chunk := make([]byte, n)
			copy(chunk, tmp[:n])

			if err := buf.Push(ctx, chunk); err != nil {
				return
			}
		}

		if rerr != nil {
			// EOF or read error: end stream
			return
		}
	}
}

func (m *StreamManager) writerLoop(ctx context.Context, id StreamID, meta Meta, buf *RingBuffer) {
	defer func() {
		// Best-effort close sink
		if m.sink != nil {
			_ = m.sink.Close(context.Background(), meta)
		}

		// cleanup manager state
		_ = m.StopStream(context.Background(), id)
	}()

	if m.sink == nil {
		m.logger("error", "stream_sink_nil", map[string]any{
			"event":     "sink_nil",
			"stream_id": string(id),
		})
		return
	}

	key := meta.ConnectorID
	if key == "" {
		key = meta.Domain
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		chunk, err := buf.Pop(ctx)
		if err != nil {
			if errors.Is(err, ErrClosed) {
				return
			}
			return
		}

		// circuit breaker gate
		if m.breaker != nil {
			ok, state, reason := m.breaker.Allow(key)
			if !ok {
				m.logger("warn", "stream_breaker_open", map[string]any{
					"event":     "breaker_open",
					"stream_id": string(id),
					"key":       key,
					"state":     state,
					"reason":    reason,
				})

				m.breaker.Report(key, false)
				return
			}
		}

		// per-domain limits gate
		release := func() {}
		if m.limits != nil {
			rel, lerr := m.limits.Acquire(ctx, meta.Domain)
			if lerr != nil {
				if m.breaker != nil {
					m.breaker.Report(key, false)
				}
				return
			}
			release = rel
		}

		werr := m.sink.Write(ctx, meta, chunk)
		release()

		if m.breaker != nil {
			m.breaker.Report(key, werr == nil)
		}

		if werr != nil {
			m.logger("error", "stream_write_error", map[string]any{
				"event":     "write_error",
				"stream_id": string(id),
				"error":     werr.Error(),
			})
			return
		}
	}
}
