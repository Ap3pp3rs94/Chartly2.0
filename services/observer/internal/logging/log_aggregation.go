package logging

// In-memory log aggregation helper (deterministic, stdlib-only).
//
// This file provides a small aggregation layer that can ingest structured log entries and
// produce deterministic summaries (counts by service/level/event) over a time window.
//
// Determinism guarantees:
//   - Duplicate detection uses a deterministic composite key.
//   - Eviction is deterministic: oldest by TS, then by key.
//   - Summary ordering is deterministic: count desc, then key asc.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrAgg         = errors.New("aggregation failed")
	ErrAggInvalid  = errors.New("aggregation invalid")
	ErrAggTooLarge = errors.New("aggregation too large")
)

type Entry struct {
	TenantID string
	TS       string // RFC3339Nano
	Service  string
	Level    string
	Event    string
	Fields   map[string]string
}

type Aggregator struct {
	mu      sync.Mutex
	max     int
	entries []Entry
	idx     map[string]struct{}
}

func NewAggregator(maxEntries int) *Aggregator {
	m := maxEntries
	if m <= 0 {
		m = 200000
	}
	return &Aggregator{
		max:     m,
		entries: make([]Entry, 0, min(1024, m)),
		idx:     make(map[string]struct{}),
	}
}

func (a *Aggregator) Add(e Entry) error {
	en, _, key, err := normalizeEntry(e)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := a.idx[key]; ok {
		return nil
	}
	a.idx[key] = struct{}{}
	a.entries = append(a.entries, en)

	if a.max > 0 && len(a.entries) > a.max {
		a.evictDeterministic()
	}

	return nil
}

func (a *Aggregator) Summary(tenantID string, since string, limit int) ([]map[string]any, error) {
	tid := norm(tenantID)
	if tid == "" {
		return nil, fmt.Errorf("%w: %w: tenantID required", ErrAgg, ErrAggInvalid)
	}

	var sinceT time.Time
	var hasSince bool
	if norm(since) != "" {
		t, err := parseRFC3339(norm(since))
		if err != nil {
			return nil, err
		}
		sinceT = t
		hasSince = true
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 5000 {
		limit = 5000
	}

	a.mu.Lock()
	items := make([]Entry, 0, len(a.entries))
	for _, e := range a.entries {
		if e.TenantID != tid {
			continue
		}
		if hasSince {
			t, err := parseRFC3339(e.TS)
			if err != nil || !t.After(sinceT) {
				continue
			}
		}
		items = append(items, e)
	}
	a.mu.Unlock()

	counts := make(map[string]int, 64)
	for _, e := range items {
		k := e.Service + "|" + e.Level + "|" + e.Event
		counts[k]++
	}

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type row struct {
		key   string
		count int
	}
	rows := make([]row, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, row{key: k, count: counts[k]})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].key < rows[j].key
	})

	if len(rows) > limit {
		rows = rows[:limit]
	}

	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		parts := strings.Split(r.key, "|")
		svc := ""
		lvl := ""
		ev := ""
		if len(parts) > 0 {
			svc = parts[0]
		}
		if len(parts) > 1 {
			lvl = parts[1]
		}
		if len(parts) > 2 {
			ev = parts[2]
		}
		out = append(out, map[string]any{
			"service": svc,
			"level":   lvl,
			"event":   ev,
			"count":   r.count,
		})
	}

	return out, nil
}

func (a *Aggregator) Stats() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()

	tenants := make(map[string]struct{})
	for _, e := range a.entries {
		tenants[e.TenantID] = struct{}{}
	}

	keys := []string{"entries", "max_entries", "tenants"}
	sort.Strings(keys)

	_ = keys
	return map[string]any{
		"entries":     len(a.entries),
		"max_entries": a.max,
		"tenants":     len(tenants),
	}
}

////////////////////////////////////////////////////////////////////////////////
// Internal helpers (deterministic)
////////////////////////////////////////////////////////////////////////////////

func (a *Aggregator) evictDeterministic() {
	if len(a.entries) <= a.max || a.max <= 0 {
		return
	}
	type item struct {
		pos int
		ts  time.Time
		key string
	}
	items := make([]item, 0, len(a.entries))
	for i, e := range a.entries {
		t, err := parseRFC3339(e.TS)
		if err != nil {
			t = time.Unix(0, 0).UTC()
		}
		items = append(items, item{
			pos: i,
			ts:  t,
			key: entryKey(e),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ts.Before(items[j].ts) {
			return true
		}
		if items[i].ts.After(items[j].ts) {
			return false
		}
		return items[i].key < items[j].key
	})

	dropN := len(a.entries) - a.max
	toDrop := make(map[int]struct{}, dropN)
	for i := 0; i < dropN; i++ {
		toDrop[items[i].pos] = struct{}{}
	}

	newEntries := make([]Entry, 0, a.max)
	newIdx := make(map[string]struct{}, a.max)
	for i, e := range a.entries {
		if _, drop := toDrop[i]; drop {
			continue
		}
		newEntries = append(newEntries, e)
		newIdx[entryKey(e)] = struct{}{}
	}
	a.entries = newEntries
	a.idx = newIdx
}

func normalizeEntry(e Entry) (Entry, time.Time, string, error) {
	en := Entry{
		TenantID: norm(e.TenantID),
		TS:       norm(e.TS),
		Service:  norm(e.Service),
		Level:    norm(e.Level),
		Event:    norm(e.Event),
		Fields:   normalizeStringMap(e.Fields),
	}
	if en.TenantID == "" || en.TS == "" || en.Service == "" || en.Level == "" || en.Event == "" {
		return Entry{}, time.Time{}, "", fmt.Errorf("%w: %w: missing required fields", ErrAgg, ErrAggInvalid)
	}
	ts, err := parseRFC3339(en.TS)
	if err != nil {
		return Entry{}, time.Time{}, "", fmt.Errorf("%w: %w: invalid ts", ErrAgg, ErrAggInvalid)
	}
	key := entryKey(en)
	return en, ts, key, nil
}

func entryKey(e Entry) string {
	return e.TenantID + "|" + e.TS + "|" + e.Service + "|" + e.Level + "|" + e.Event
}

func normalizeStringMap(m map[string]string) map[string]string {
	if m == nil || len(m) == 0 {
		return map[string]string{}
	}
	// Reuse normalization from structured_log.go behavior.
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
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	return s
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
