package ledger

// Append-only in-memory ledger for audit events (deterministic, multi-tenant safe).
//
// This package provides a small, production-grade in-memory ledger that supports:
//   - Idempotent append (tenant_id + event_id unique)
//   - Deterministic listing (TS asc, EventID asc)
//   - Deterministic eviction when max events is exceeded (drop oldest globally by TS, then tenant, then event_id)
//
// The ledger is designed as a v0 in-memory implementation that can later be replaced with a durable
// Postgres-backed ledger without changing the consumer API.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrLedger         = errors.New("ledger failed")
	ErrLedgerInvalid  = errors.New("ledger invalid")
	ErrLedgerTooLarge = errors.New("ledger too large")
)

type Event struct {
	TenantID  string            `json:"tenant_id"`
	EventID   string            `json:"event_id"`
	TS        string            `json:"ts"` // RFC3339/RFC3339Nano
	Action    string            `json:"action"`
	ObjectKey string            `json:"object_key,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	ActorID   string            `json:"actor_id,omitempty"`
	Source    string            `json:"source,omitempty"`
	Outcome   string            `json:"outcome"`
	Detail    map[string]any    `json:"detail,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}
type AppendOnly struct {
	mu     sync.Mutex
	max    int
	events []Event
	// index: tenant -> event_id -> position in events slice
	idx map[string]map[string]int
}

func NewAppendOnly(maxEvents int) *AppendOnly {
	m := maxEvents
	if m <= 0 {
		m = 100000
	}
	return &AppendOnly{
		max:    m,
		events: make([]Event, 0, min(1024, m)),
		idx:    make(map[string]map[string]int),
	}
}

// Append inserts an event idempotently.
// Returns inserted=false if tenant_id+event_id already exists.
func (l *AppendOnly) Append(e Event) (bool, error) {
	ev, _, err := normalizeAndValidate(e)
	if err != nil {
		return false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.idx[ev.TenantID]; !ok {
		l.idx[ev.TenantID] = make(map[string]int)
	}
	if _, exists := l.idx[ev.TenantID][ev.EventID]; exists {
		return false, nil
	}
	pos := len(l.events)
	l.events = append(l.events, ev)
	l.idx[ev.TenantID][ev.EventID] = pos

	// Evict if needed (deterministic: drop oldest globally by TS, then tenant_id, then event_id).
	if l.max > 0 && len(l.events) > l.max {
		l.evictDeterministic()
	}
	return true, nil
}

// Get returns an event by tenant+id.
func (l *AppendOnly) Get(tenantID string, eventID string) (Event, bool) {
	t := norm(tenantID)
	id := norm(eventID)
	if t == "" || id == "" {
		return Event{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	m := l.idx[t]
	if m == nil {
		return Event{}, false
	}
	pos, ok := m[id]
	if !ok || pos < 0 || pos >= len(l.events) {
		return Event{}, false
	}

	// Return a deep copy so callers cannot mutate internal state.
	return deepCopyEvent(l.events[pos]), true
}

// List returns events for a tenant in deterministic order.
// since: optional RFC3339/RFC3339Nano; returns events strictly after since.
// limit: default 200; max 5000.
func (l *AppendOnly) List(tenantID string, since string, limit int) ([]Event, error) {
	t := norm(tenantID)
	if t == "" {
		return nil, fmt.Errorf("%w: %w: tenantID required", ErrLedger, ErrLedgerInvalid)
	}
	var sinceT time.Time
	var hasSince bool
	if norm(since) != "" {
		st, err := parseRFC3339(norm(since))
		if err != nil {
			return nil, err
		}
		sinceT = st
		hasSince = true
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 5000 {
		limit = 5000
	}
	l.mu.Lock()
	// Snapshot and filter under lock.
	tmp := make([]Event, 0, min(limit, len(l.events)))
	for _, ev := range l.events {
		if ev.TenantID != t {
			continue
		}
		if hasSince {
			et, err := parseRFC3339(ev.TS)
			if err != nil {
				continue
			}
			if !et.After(sinceT) {
				continue
			}
		}
		tmp = append(tmp, deepCopyEvent(ev))
	}
	l.mu.Unlock()

	// Deterministic order: TS asc, EventID asc
	sort.Slice(tmp, func(i, j int) bool {
		ti, _ := parseRFC3339(tmp[i].TS)
		tj, _ := parseRFC3339(tmp[j].TS)
		if ti.Before(tj) {
			return true
		}
		if ti.After(tj) {
			return false
		}
		return tmp[i].EventID < tmp[j].EventID
	})
	if len(tmp) > limit {
		tmp = tmp[:limit]
	}
	return tmp, nil
}

// Stats returns deterministic ledger stats.
// Note: map iteration order is not deterministic; callers should treat this as a bag of values.
// Keys are stable and documented.
func (l *AppendOnly) Stats() map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	tenants := len(l.idx)
	total := len(l.events)
	return map[string]any{
		"total_events": total,
		"tenants":      tenants,
		"max_events":   l.max,
	}
}

////////////////////////////////////////////////////////////////////////////////
// Eviction + normalization (deterministic)
////////////////////////////////////////////////////////////////////////////////

func (l *AppendOnly) evictDeterministic() {
	if len(l.events) <= l.max || l.max <= 0 {
		return
	}

	// Build sortable view with parsed timestamps.
	type item struct {
		pos int
		ts  time.Time
		tid string
		eid string
	}
	items := make([]item, 0, len(l.events))
	for i, ev := range l.events {
		t, err := parseRFC3339(ev.TS)
		if err != nil {
			// If invalid TS slipped in, treat as zero time to evict first deterministically.
			t = time.Unix(0, 0).UTC()
		}
		items = append(items, item{pos: i, ts: t, tid: ev.TenantID, eid: ev.EventID})
	}
	sort.Slice(items, func(i, j int) bool {
		a := items[i]
		b := items[j]
		if a.ts.Before(b.ts) {
			return true
		}
		if a.ts.After(b.ts) {
			return false
		}
		if a.tid != b.tid {
			return a.tid < b.tid
		}
		return a.eid < b.eid
	})

	// Determine how many to drop (oldest first).
	dropN := len(l.events) - l.max
	toDrop := make(map[int]struct{}, dropN)
	for i := 0; i < dropN; i++ {
		toDrop[items[i].pos] = struct{}{}
	}

	// Rebuild events slice without dropped positions; rebuild index deterministically.
	newEvents := make([]Event, 0, l.max)
	newIdx := make(map[string]map[string]int)
	for i, ev := range l.events {
		if _, drop := toDrop[i]; drop {
			continue
		}
		pos := len(newEvents)
		newEvents = append(newEvents, ev)
		if _, ok := newIdx[ev.TenantID]; !ok {
			newIdx[ev.TenantID] = make(map[string]int)
		}
		newIdx[ev.TenantID][ev.EventID] = pos
	}
	l.events = newEvents
	l.idx = newIdx
}
func normalizeAndValidate(e Event) (Event, time.Time, error) {
	ev := deepCopyEvent(e)
	ev.TenantID = norm(ev.TenantID)
	ev.EventID = norm(ev.EventID)
	ev.TS = norm(ev.TS)
	ev.Action = norm(ev.Action)
	ev.Outcome = norm(ev.Outcome)
	ev.ObjectKey = norm(ev.ObjectKey)
	ev.RequestID = norm(ev.RequestID)
	ev.ActorID = norm(ev.ActorID)
	ev.Source = norm(ev.Source)
	if ev.TenantID == "" || ev.EventID == "" || ev.Action == "" || ev.Outcome == "" {
		return Event{}, time.Time{}, fmt.Errorf("%w: %w: tenant_id/event_id/action/outcome required", ErrLedger, ErrLedgerInvalid)
	}
	if ev.TS == "" {
		return Event{}, time.Time{}, fmt.Errorf("%w: %w: ts required", ErrLedger, ErrLedgerInvalid)
	}
	ts, err := parseRFC3339(ev.TS)
	if err != nil {
		return Event{}, time.Time{}, fmt.Errorf("%w: %w: invalid ts", ErrLedger, ErrLedgerInvalid)
	}

	// Normalize maps deterministically (copy + stable key normalization).
	ev.Meta = normalizeStringMap(ev.Meta)
	ev.Detail = normalizeAnyMap(ev.Detail)
	// return ev, ts, nil
}
func deepCopyEvent(e Event) Event {
	out := e
	if e.Meta != nil {
		out.Meta = make(map[string]string, len(e.Meta))
		for k, v := range e.Meta {
			out.Meta[k] = v
		}
	}
	if e.Detail != nil {
		out.Detail = deepCopyAnyMap(e.Detail)
	}
	return out
}
func normalizeStringMap(m map[string]string) map[string]string {
	if m == nil || len(m) == 0 {
		return map[string]string{}
	}
	tmp := make(map[string]string, len(m))
	for k, v := range m {
		kk := normCollapse(k)
		if kk == "" {
			continue
		}
		tmp[kk] = normCollapse(v)
	}
	keys := make([]string, 0, len(tmp))
	for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = tmp[k]
	}
	return out
}
func normalizeAnyMap(m map[string]any) map[string]any {
	if m == nil || len(m) == 0 {
		return map[string]any{}
	}

	// Normalize keys; values are kept but deep-copied.
	tmp := make(map[string]any, len(m))
	for k, v := range m {
		kk := normCollapse(k)
		if kk == "" {
			continue
		}
		tmp[kk] = deepCopyAny(v)
	}
	keys := make([]string, 0, len(tmp))
	for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		out[k] = tmp[k]
	}
	return out
}
func deepCopyAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyAny(v)
	}
	return out
}
func deepCopyAny(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return normCollapse(t)
		// case bool:
		// return t
		// case float64:
		// return t
		// case float32:
		return float64(t)
		// case int:
		return float64(t)
		// case int64:
		return float64(t)
		// case uint64:
		return float64(t)
	case map[string]any:
		return deepCopyAnyMap(t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = deepCopyAny(t[i])
		}
		return out
	default:
		// Fallback to fmt string to remain deterministic for unknown types.
		return normCollapse(fmt.Sprintf("%v", t))
	}
}
func parseRFC3339(s string) (time.Time, error) {
	s = norm(s)
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
func norm(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	// return s
}
func normCollapse(s string) string {
	s = norm(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
