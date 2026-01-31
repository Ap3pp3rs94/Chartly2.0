package ledger

// Hash chain utilities for audit events (deterministic, tamper-evident).
//
// This file provides canonical hashing and hash-chain construction/verification over Event values.
// The goal is to make it easy for higher layers to detect missing/reordered/tampered events.
//
// Core idea (deterministic):
//   - CanonicalEventBytes(e) => canonical JSON bytes with sorted keys at all depths.
//   - chain hash progression:
//       prev := "GENESIS"
//       hash := sha256(prev + "\n" + canonicalEventJSON) as hex
//       prev = hash
//
// Determinism guarantees:
//   - No randomness, no time.Now usage.
//   - Stable ordering: TS asc, then EventID asc.
//   - No map iteration without sorting (canonical JSON builder sorts keys).
//
// This is library-only: no network, no filesystem, no HTTP.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrChain         = errors.New("chain failed")
	ErrChainInvalid  = errors.New("chain invalid")
	ErrChainMismatch = errors.New("chain mismatch")
)

const genesisPrevHash = "GENESIS"

type Link struct {
	TenantID string `json:"tenant_id"`
	EventID  string `json:"event_id"`
	TS       string `json:"ts"`
	PrevHash string `json:"prev_hash"`
	Hash     string `json:"hash"`
}

type Chain struct {
	TenantID string `json:"tenant_id"`
	Head     string `json:"head"`
	Links    []Link `json:"links"`
}

// BuildChain constructs a deterministic hash-chain for the given events.
// If tenantID is empty, it is inferred from the first event and then enforced across all events.
func BuildChain(tenantID string, events []Event) (Chain, error) {
	if len(events) == 0 {
		return Chain{}, fmt.Errorf("%w: %w: no events", ErrChain, ErrChainInvalid)
	}

	tid := normCollapse(tenantID)
	if tid == "" {
		tid = normCollapse(events[0].TenantID)
	}
	if tid == "" {
		return Chain{}, fmt.Errorf("%w: %w: tenant_id required", ErrChain, ErrChainInvalid)
	}

	// Copy events (do not mutate caller) and normalize tenant enforcement.
	evs := make([]Event, len(events))
	for i := range events {
		evs[i] = deepCopyEventForChain(events[i])
		if normCollapse(evs[i].TenantID) == "" {
			return Chain{}, fmt.Errorf("%w: %w: event tenant_id missing", ErrChain, ErrChainInvalid)
		}
		if normCollapse(evs[i].TenantID) != tid {
			return Chain{}, fmt.Errorf("%w: %w: mixed tenant_id", ErrChain, ErrChainInvalid)
		}
		if normCollapse(evs[i].EventID) == "" || normCollapse(evs[i].TS) == "" {
			return Chain{}, fmt.Errorf("%w: %w: event_id/ts required", ErrChain, ErrChainInvalid)
		}
		// Validate TS parseability (deterministic parsing only).
		if _, err := parseRFC3339Strict(evs[i].TS); err != nil {
			return Chain{}, fmt.Errorf("%w: %w: invalid ts", ErrChain, ErrChainInvalid)
		}
	}

	// Deterministic sort: TS asc, EventID asc.
	sort.Slice(evs, func(i, j int) bool {
		ti, _ := parseRFC3339Strict(evs[i].TS)
		tj, _ := parseRFC3339Strict(evs[j].TS)
		if ti.Before(tj) {
			return true
		}
		if ti.After(tj) {
			return false
		}
		return normCollapse(evs[i].EventID) < normCollapse(evs[j].EventID)
	})

	links := make([]Link, 0, len(evs))
	prev := genesisPrevHash

	for _, e := range evs {
		b, err := CanonicalEventBytes(e)
		if err != nil {
			return Chain{}, fmt.Errorf("%w: %v", ErrChain, err)
		}
		h := hashStep(prev, b)
		links = append(links, Link{
			TenantID: tid,
			EventID:  normCollapse(e.EventID),
			TS:       normCollapse(e.TS),
			PrevHash: prev,
			Hash:     h,
		})
		prev = h
	}

	return Chain{TenantID: tid, Head: prev, Links: links}, nil
}

// VerifyChain recomputes the chain from events and ensures it matches the provided chain.
// It verifies:
//   - tenant consistency
//   - link-by-link PrevHash/Hash correctness
//   - Head correctness
func VerifyChain(chain Chain, events []Event) error {
	tid := normCollapse(chain.TenantID)
	if tid == "" {
		return fmt.Errorf("%w: %w: chain tenant_id required", ErrChain, ErrChainInvalid)
	}

	built, err := BuildChain(tid, events)
	if err != nil {
		return err
	}

	if normCollapse(chain.Head) != normCollapse(built.Head) {
		return fmt.Errorf("%w: head mismatch", ErrChainMismatch)
	}

	// Compare links deterministically by position (BuildChain output order is deterministic).
	if len(chain.Links) != len(built.Links) {
		return fmt.Errorf("%w: link count mismatch", ErrChainMismatch)
	}

	for i := range built.Links {
		a := chain.Links[i]
		b := built.Links[i]
		if normCollapse(a.TenantID) != normCollapse(b.TenantID) ||
			normCollapse(a.EventID) != normCollapse(b.EventID) ||
			normCollapse(a.TS) != normCollapse(b.TS) ||
			normCollapse(a.PrevHash) != normCollapse(b.PrevHash) ||
			normCollapse(a.Hash) != normCollapse(b.Hash) {
			return fmt.Errorf("%w: link mismatch at index %d", ErrChainMismatch, i)
		}
	}

	return nil
}

// CanonicalEventBytes returns canonical JSON bytes for hashing.
// - Keys are sorted lexicographically at all depths.
// - Maps are normalized without mutating caller state.
// - Strings are normalized (trim, remove NUL, collapse whitespace).
func CanonicalEventBytes(e Event) ([]byte, error) {
	// Build a canonical representation as a recursively sorted structure.
	// We avoid relying on encoding/json map iteration order by converting maps to ordered arrays.
	ce := canonicalEvent{
		TenantID:  normCollapse(e.TenantID),
		EventID:   normCollapse(e.EventID),
		TS:        normCollapse(e.TS),
		Action:    normCollapse(e.Action),
		ObjectKey: normCollapse(e.ObjectKey),
		RequestID: normCollapse(e.RequestID),
		ActorID:   normCollapse(e.ActorID),
		Source:    normCollapse(e.Source),
		Outcome:   normCollapse(e.Outcome),
		Meta:      canonicalStringMap(e.Meta),
		Detail:    canonicalAnyMap(e.Detail),
	}

	// Validate minimal fields for hashing.
	if ce.TenantID == "" || ce.EventID == "" || ce.TS == "" || ce.Action == "" || ce.Outcome == "" {
		return nil, fmt.Errorf("%w: %w: missing required fields", ErrChain, ErrChainInvalid)
	}

	// Marshal a struct which contains only ordered slices and scalar fields => deterministic output.
	b, err := json.Marshal(ce)
	if err != nil {
		return nil, fmt.Errorf("%w: json marshal: %v", ErrChain, err)
	}

	return b, nil
}

////////////////////////////////////////////////////////////////////////////////
// Internal canonical structures (ordered)
////////////////////////////////////////////////////////////////////////////////

type canonicalKV struct {
	K string      `json:"k"`
	V interface{} `json:"v"`
}

type canonicalSKV struct {
	K string `json:"k"`
	V string `json:"v"`
}

type canonicalEvent struct {
	TenantID  string         `json:"tenant_id"`
	EventID   string         `json:"event_id"`
	TS        string         `json:"ts"`
	Action    string         `json:"action"`
	ObjectKey string         `json:"object_key,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	ActorID   string         `json:"actor_id,omitempty"`
	Source    string         `json:"source,omitempty"`
	Outcome   string         `json:"outcome"`
	Meta      []canonicalSKV `json:"meta,omitempty"`
	Detail    []canonicalKV  `json:"detail,omitempty"`
}

func canonicalStringMap(m map[string]string) []canonicalSKV {
	if m == nil || len(m) == 0 {
		return nil
	}

	keys := make([]string, 0, len(m))
	tmp := make(map[string]string, len(m))

	for k, v := range m {
		kk := normCollapse(k)
		if kk == "" {
			continue
		}
		tmp[kk] = normCollapse(v)
	}

	for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]canonicalSKV, 0, len(keys))
	for _, k := range keys {
		out = append(out, canonicalSKV{K: k, V: tmp[k]})
	}
	return out
}

func canonicalAnyMap(m map[string]any) []canonicalKV {
	if m == nil || len(m) == 0 {
		return nil
	}

	keys := make([]string, 0, len(m))
	tmp := make(map[string]any, len(m))

	for k, v := range m {
		kk := normCollapse(k)
		if kk == "" {
			continue
		}
		tmp[kk] = canonicalizeAny(v)
	}

	for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]canonicalKV, 0, len(keys))
	for _, k := range keys {
		out = append(out, canonicalKV{K: k, V: tmp[k]})
	}
	return out
}

func canonicalizeAny(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return normCollapse(t)
	case bool:
		return t
	case float64:
		// json.Unmarshal uses float64 for numbers; keep as-is.
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case json.Number:
		// Convert to float64 deterministically if possible, otherwise string.
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t.String()
	case map[string]any:
		return canonicalAnyMap(t) // ordered kv slice
	case map[string]string:
		return canonicalStringMap(t) // ordered skv slice
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = canonicalizeAny(t[i])
		}
		return out
	default:
		// Deterministic fallback: stringify.
		return normCollapse(fmt.Sprintf("%v", t))
	}
}

////////////////////////////////////////////////////////////////////////////////
// Hash step + helpers
////////////////////////////////////////////////////////////////////////////////

func hashStep(prev string, canonicalEventJSON []byte) string {
	prev = strings.TrimSpace(prev)
	if prev == "" {
		prev = genesisPrevHash
	}

	// sha256(prev + "\n" + bytes)
	h := sha256.New()
	_, _ = h.Write([]byte(prev))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write(canonicalEventJSON)
	return hex.EncodeToString(h.Sum(nil))
}

func deepCopyEventForChain(e Event) Event {
	// Minimal deep copy to avoid caller mutation during canonicalization.
	out := e
	// Copy meta
	if e.Meta != nil {
		out.Meta = make(map[string]string, len(e.Meta))
		for k, v := range e.Meta {
			out.Meta[k] = v
		}
	}
	// Copy detail
	if e.Detail != nil {
		out.Detail = deepCopyAnyMapForChain(e.Detail)
	}
	return out
}

func deepCopyAnyMapForChain(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	// Copy in deterministic key order to avoid accidental map sharing; map order still irrelevant internally.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = deepCopyAnyForChain(m[k])
	}
	return out
}

func deepCopyAnyForChain(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return t
	case bool:
		return t
	case float64:
		return t
	case float32:
		return t
	case int:
		return t
	case int64:
		return t
	case uint64:
		return t
	case json.Number:
		return json.Number(t)
	case map[string]any:
		return deepCopyAnyMapForChain(t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = deepCopyAnyForChain(t[i])
		}
		return out
	default:
		return t
	}
}

func parseRFC3339Strict(s string) (time.Time, error) {
	s = normCollapse(s)
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

func normCollapse(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if s == "" {
		return ""
	}
	// Collapse whitespace deterministically.
	return strings.Join(strings.Fields(s), " ")
}

// Optional helper: stable float formatting if a caller wants to embed numbers as strings.
// Not used in canonicalization, but useful for debugging; kept deterministic.
func formatFloatStable(f float64) string {
	// Use strconv with 'g' but with a fixed precision to reduce platform variance.
	// NOTE: json.Marshal already outputs deterministically for float64 in Go.
	return strconv.FormatFloat(f, 'g', -1, 64)
}
