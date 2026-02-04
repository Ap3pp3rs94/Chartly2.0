package quarantine

import (
	"errors"

	"strings"

	"sync"

	"time"
)

// type Reason string

const (
	ReasonPIIDetected Reason = "pii_detected"

	ReasonSchemaViolation Reason = "schema_violation"

	ReasonOutlierDetected Reason = "outlier_detected"

	ReasonParseError Reason = "parse_error"

	ReasonPolicyDenied Reason = "policy_denied"

	ReasonUnknown Reason = "unknown"
)

type Entry struct {
	Ts string `json:"ts"`

	TenantID string `json:"tenant_id"`

	SourceID string `json:"source_id"`

	ConnectorID string `json:"connector_id,omitempty"`

	JobID string `json:"job_id,omitempty"`

	Reason Reason `json:"reason"`

	Details map[string]string `json:"details,omitempty"`

	Payload map[string]any `json:"payload,omitempty"`
}
type Store interface {
	Put(e Entry)
	// error

	List(tenantID string, limit int) []Entry
}
type InMemoryStore struct {
	mu sync.Mutex

	per map[string]*ring

	cap int
}
type ring struct {
	buf []Entry

	head int

	size int
}

func NewInMemoryStore() *InMemoryStore {

	return &InMemoryStore{

		per: make(map[string]*ring),

		cap: 1000,
	}
}
func (s *InMemoryStore) Put(e Entry) error {

	tid := strings.TrimSpace(e.TenantID)
	if tid == "" {

		return errors.New("tenant_id empty")

	}
	if strings.TrimSpace(e.Ts) == "" {

		e.Ts = time.Now().UTC().Format(time.RFC3339Nano)

	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.per[tid]

	if r == nil {

		r = &ring{buf: make([]Entry, s.cap)}
		s.per[tid] = r

	}
	if r.size < s.cap {

		idx := (r.head + r.size) % s.cap

		r.buf[idx] = e

		r.size++

		return nil

	}

	// overwrite oldest

	r.buf[r.head] = e

	r.head = (r.head + 1) % s.cap

	return nil
}
func (s *InMemoryStore) List(tenantID string, limit int) []Entry {

	tid := strings.TrimSpace(tenantID)
	if tid == "" {

		return nil

	}
	if limit <= 0 {

		limit = 50

	}
	if limit > 1000 {

		limit = 1000

	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.per[tid]

	if r == nil || r.size == 0 {

		return nil

	}
	n := r.size

	if limit < n {

		n = limit

	}

	// newest-first

	out := make([]Entry, 0, n)
	for i := 0; i < n; i++ {

		idx := (r.head + r.size - 1 - i) % s.cap

		out = append(out, r.buf[idx])

	}
	return out
}

type LoggerFn func(level, msg string, fields map[string]any) type Manager struct {
	store Store

	logger LoggerFn
}

func NewManager(store Store, logger LoggerFn) *Manager {

	if store == nil {

		store = NewInMemoryStore()

	}
	if logger == nil {

		logger = func(string, string, map[string]any) {}

	}
	return &Manager{store: store, logger: logger}
}
func (m *Manager) Quarantine(e Entry) error {

	if strings.TrimSpace(e.Ts) == "" {

		e.Ts = time.Now().UTC().Format(time.RFC3339Nano)

	}
	if strings.TrimSpace(string(e.Reason)) == "" {

		e.Reason = ReasonUnknown

	}
	err := m.store.Put(e)
	if err == nil {

		m.logger("warn", "quarantine", map[string]any{

			"event": "quarantine",

			"tenant_id": e.TenantID,

			"source_id": e.SourceID,

			"connector_id": e.ConnectorID,

			"job_id": e.JobID,

			"reason": string(e.Reason),
		})

	}
	return err
}
func (m *Manager) Recent(tenantID string, limit int) []Entry {

	return m.store.List(tenantID, limit)
}
