package registry

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type Field struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Optional bool   `json:"optional"`
}
type Schema struct {
	Version     string  `json:"version"`
	GeneratedAt string  `json:"generated_at"` // RFC3339Nano
	Fields      []Field `json:"fields"`
}

// type DriftType string

const (
	DriftAdded           DriftType = "added"
	DriftRemoved         DriftType = "removed"
	DriftTypeChanged     DriftType = "type_changed"
	DriftOptionalChanged DriftType = "optional_changed"
)

type DriftEvent struct {
	Ts          string    `json:"ts"` // RFC3339Nano
	ConnectorID string    `json:"connector_id"`
	SourceID    string    `json:"source_id"`
	DriftType   DriftType `json:"drift_type"`
	FieldPath   string    `json:"field_path"`
	From        string    `json:"from,omitempty"`
	To          string    `json:"to,omitempty"`
	Notes       string    `json:"notes,omitempty"`
}

func Diff(oldS, newS Schema) []DriftEvent {
	oldM := make(map[string]Field, len(oldS.Fields))
	newM := make(map[string]Field, len(newS.Fields))
	for _, f := range oldS.Fields {
		p := strings.TrimSpace(f.Path)
		if p == "" {
			continue
		}
		f.Path = p
		f.Type = strings.ToLower(strings.TrimSpace(f.Type))
		oldM[p] = f
	}
	for _, f := range newS.Fields {
		p := strings.TrimSpace(f.Path)
		if p == "" {
			continue
		}
		f.Path = p
		f.Type = strings.ToLower(strings.TrimSpace(f.Type))
		newM[p] = f
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	out := make([]DriftEvent, 0)

	// removed + changes
	for p, of := range oldM {
		nf, ok := newM[p]
		if !ok {
			out = append(out, DriftEvent{
				Ts:        now,
				DriftType: DriftRemoved,
				FieldPath: p,
				From:      of.Type,
				To:        "",
			})
			// continue
		}
		if of.Type != nf.Type {
			out = append(out, DriftEvent{
				Ts:        now,
				DriftType: DriftTypeChanged,
				FieldPath: p,
				From:      of.Type,
				To:        nf.Type,
			})
		}
		if of.Optional != nf.Optional {
			out = append(out, DriftEvent{
				Ts:        now,
				DriftType: DriftOptionalChanged,
				FieldPath: p,
				From:      boolStr(of.Optional),
				To:        boolStr(nf.Optional),
			})
		}
	}

	// added
	for p, nf := range newM {
		if _, ok := oldM[p]; !ok {
			out = append(out, DriftEvent{
				Ts:        now,
				DriftType: DriftAdded,
				FieldPath: p,
				From:      "",
				To:        nf.Type,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FieldPath == out[j].FieldPath {
			return out[i].DriftType < out[j].DriftType
		}
		return out[i].FieldPath < out[j].FieldPath
	})
	// return out
}
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type DriftStore struct {
	mu      sync.RWMutex
	schemas map[string][]Schema
	events  map[string][]DriftEvent
}

func NewDriftStore() *DriftStore {
	return &DriftStore{
		schemas: make(map[string][]Schema),
		events:  make(map[string][]DriftEvent),
	}
}
func (s *DriftStore) Put(connectorID, sourceID string, schema Schema) {
	key := storeKey(connectorID, sourceID)
	if strings.TrimSpace(schema.GeneratedAt) == "" {
		schema.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if strings.TrimSpace(schema.Version) == "" {
		schema.Version = "v1"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// var prev Schema
	hist := s.schemas[key]
	if len(hist) > 0 {
		prev = hist[len(hist)-1]
	}

	// append schema (bounded)
	hist = append(hist, schema)
	if len(hist) > 20 {
		hist = hist[len(hist)-20:]
	}
	s.schemas[key] = hist

	// compute drift vs prev
	if len(prev.Fields) > 0 {
		d := Diff(prev, schema)
		if len(d) > 0 {
			// attach ids and bound events
			evs := s.events[key]
			now := time.Now().UTC().Format(time.RFC3339Nano)
			for i := range d {
				d[i].Ts = now
				d[i].ConnectorID = connectorID
				d[i].SourceID = sourceID
			}
			evs = append(evs, d...)
			if len(evs) > 200 {
				evs = evs[len(evs)-200:]
			}
			s.events[key] = evs
		}
	}
}
func (s *DriftStore) Latest(connectorID, sourceID string) (Schema, bool) {
	key := storeKey(connectorID, sourceID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := s.schemas[key]
	if len(h) == 0 {
		return Schema{}, false
	}
	return h[len(h)-1], true
}
func (s *DriftStore) History(connectorID, sourceID string) []Schema {
	key := storeKey(connectorID, sourceID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := s.schemas[key]
	out := make([]Schema, len(h))
	copy(out, h)
	// return out
}
func (s *DriftStore) Events(connectorID, sourceID string) []DriftEvent {
	key := storeKey(connectorID, sourceID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	ev := s.events[key]
	out := make([]DriftEvent, len(ev))
	copy(out, ev)
	// return out
}
func storeKey(connectorID, sourceID string) string {
	return strings.ToLower(strings.TrimSpace(connectorID)) + "|" + strings.ToLower(strings.TrimSpace(sourceID))
}
