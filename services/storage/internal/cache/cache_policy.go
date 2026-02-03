package cache

// Cache policy engine (deterministic, library-only).
//
// This file defines a small policy layer that controls what the storage service caches,
// for how long (TTL), and how keys are constructed.
//
// Determinism guarantees:
//   - No randomness.
//   - No time.Now usage for decisions; callers provide sizes/timestamps if needed.
//   - Rules are matched deterministically and returned in stable order.
//   - Key computation is stable across platforms.
//
// Key format (stable):
//   chartly:<tenant>:<kind>:<part1>:<part2>...
//
// Notes:
//   - This package does not perform cache I/O. It only defines the policy decisions and keying.
//   - Multi-tenant safety is enforced by requiring tenantID and embedding it into all keys.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrPolicy        = errors.New("policy failed")
	ErrPolicyInvalid = errors.New("policy invalid")
)

// type Kind string

const (
	KindObjectBody Kind = "object_body"
	KindObjectMeta Kind = "object_meta"
	KindStats      Kind = "stats"
	KindList       Kind = "list"
)

type Rule struct {
	Kind     Kind
	TTL      time.Duration
	Enabled  bool
	MaxBytes int64
	Notes    string
}
type Policy struct {
	Version    string
	DefaultTTL time.Duration
	Rules      []Rule
}

// DefaultPolicy returns a production-sensible default cache policy.
// These defaults are conservative and can be overridden by configuration at higher layers.
func DefaultPolicy() Policy {
	return normalizePolicy(Policy{
		Version:    "v1",
		DefaultTTL: 30 * time.Second,
		Rules: []Rule{
			// Keep stable order by Kind lexicographically.
			{Kind: KindList, TTL: 5 * time.Second, Enabled: true, MaxBytes: 0, Notes: "list results are volatile; keep short"},
			{Kind: KindObjectBody, TTL: 30 * time.Second, Enabled: true, MaxBytes: 4 * 1024 * 1024, Notes: "cache small bodies only"},
			{Kind: KindObjectMeta, TTL: 60 * time.Second, Enabled: true, MaxBytes: 0, Notes: "meta is cheap and useful"},
			{Kind: KindStats, TTL: 10 * time.Second, Enabled: true, MaxBytes: 0, Notes: "stats are frequent; keep short"},
		},
	})
}

// RuleFor returns the rule for a kind deterministically.
// If multiple rules match (should not), it selects:
//   - longest TTL, then
//   - enabled=true, then
//   - lexicographically smallest Notes, then
//   - larger MaxBytes
func (p Policy) RuleFor(kind Kind) Rule {
	pn := normalizePolicy(p)
	matches := make([]Rule, 0, 2)
	for _, r := range pn.Rules {
		if r.Kind == kind {
			matches = append(matches, normalizeRule(r, pn.DefaultTTL))
		}
	}
	if len(matches) == 0 {
		return Rule{
			Kind:     kind,
			TTL:      pn.DefaultTTL,
			Enabled:  false,
			MaxBytes: 0,
			Notes:    "no rule",
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	sort.Slice(matches, func(i, j int) bool {
		a := matches[i]
		b := matches[j]

		if a.TTL != b.TTL {
			return a.TTL > b.TTL
		}
		if a.Enabled != b.Enabled {
			return a.Enabled && !b.Enabled
		}
		if a.Notes != b.Notes {
			return a.Notes < b.Notes
		}
		if a.MaxBytes != b.MaxBytes {
			return a.MaxBytes > b.MaxBytes
		}
		return false
	})
	return matches[0]
}

// Admit decides whether an item of given kind and size should be cached.
// Returns (ttl, ok). ttl=0 and ok=false means do not cache.
func (p Policy) Admit(kind Kind, objBytes int64) (time.Duration, bool) {
	r := p.RuleFor(kind)
	if !r.Enabled {
		return 0, false
	}
	if r.MaxBytes > 0 && objBytes > r.MaxBytes {
		return 0, false
	}
	if r.TTL <= 0 {
		return 0, false
	}
	return r.TTL, true
}

// Key builds a stable, tenant-scoped cache key.
//
// Rules:
//   - tenantID is required.
//   - kind is required.
//   - parts are normalized (trim; remove NUL; collapse whitespace).
//   - parts may not contain ":" (to avoid ambiguous splitting). If present, an error is returned.
//     (If you need to support arbitrary strings, implement a deterministic escaping scheme at a higher layer.)
func Key(tenantID string, kind Kind, parts ...string) (string, error) {
	t := norm(tenantID)
	if t == "" {
		return "", fmt.Errorf("%w: %w: tenantID required", ErrPolicy, ErrPolicyInvalid)
	}
	k := norm(string(kind))
	if k == "" {
		return "", fmt.Errorf("%w: %w: kind required", ErrPolicy, ErrPolicyInvalid)
	}
	normalized := make([]string, 0, len(parts))
	for _, p := range parts {
		pn := normCollapse(p)
		if pn == "" {
			continue
		}
		if strings.Contains(pn, ":") {
			return "", fmt.Errorf("%w: %w: parts must not contain ':'", ErrPolicy, ErrPolicyInvalid)
		}
		normalized = append(normalized, pn)
	}
	var b strings.Builder
	b.WriteString("chartly:")
	b.WriteString(t)
	b.WriteString(":")
	b.WriteString(k)
	for _, p := range normalized {
		b.WriteString(":")
		b.WriteString(p)
	}
	return b.String(), nil
}

// AsMap exports the policy in a deterministic shape (stable slices and sorted keys within rules).
// Note: Go map iteration order is randomized; callers should marshal using a stable encoder
// or transform this map into an ordered representation if strict deterministic bytes are required.
func (p Policy) AsMap() map[string]any {
	pn := normalizePolicy(p)

	// Export rules in stable order by Kind then Notes.
	rules := make([]Rule, len(pn.Rules))
	copy(rules, pn.Rules)
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Kind != rules[j].Kind {
			return string(rules[i].Kind) < string(rules[j].Kind)
		}
		return rules[i].Notes < rules[j].Notes
	})
	outRules := make([]map[string]any, 0, len(rules))
	for _, r := range rules {
		rr := normalizeRule(r, pn.DefaultTTL)
		outRules = append(outRules, map[string]any{
			"kind":      string(rr.Kind),
			"ttl_ms":    rr.TTL.Milliseconds(),
			"enabled":   rr.Enabled,
			"max_bytes": rr.MaxBytes,
			"notes":     rr.Notes,
		})
	}
	return map[string]any{
		"version":        pn.Version,
		"default_ttl_ms": pn.DefaultTTL.Milliseconds(),
		"rules":          outRules,
	}
}

////////////////////////////////////////////////////////////////////////////////
// Normalization helpers
////////////////////////////////////////////////////////////////////////////////

func normalizePolicy(p Policy) Policy {
	pp := p
	pp.Version = norm(pp.Version)
	if pp.Version == "" {
		pp.Version = "v1"
	}
	if pp.DefaultTTL <= 0 {
		pp.DefaultTTL = 30 * time.Second
	}
	if pp.Rules == nil {
		pp.Rules = []Rule{}
	}
	nr := make([]Rule, 0, len(pp.Rules))
	for _, r := range pp.Rules {
		nr = append(nr, normalizeRule(r, pp.DefaultTTL))
	}
	pp.Rules = nr
	return pp
}
func normalizeRule(r Rule, defaultTTL time.Duration) Rule {
	rr := r
	rr.Kind = Kind(norm(string(rr.Kind)))
	rr.Notes = normCollapse(rr.Notes)
	if rr.TTL <= 0 {
		rr.TTL = defaultTTL
	}
	if rr.MaxBytes < 0 {
		rr.MaxBytes = 0
	}
	return rr
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
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}
