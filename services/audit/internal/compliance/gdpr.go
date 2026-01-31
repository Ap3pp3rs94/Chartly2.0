package compliance

// GDPR utilities (deterministic, library-only).
//
// This file provides data-only helpers for GDPR-aligned handling of audit events.
// It does NOT implement enforcement by itself (no DB deletes, no HTTP handlers).
//
// Core features:
//   - Subject ID normalization (stable, defensive).
//   - Pseudonymization (deterministic HMAC-SHA256 with caller-provided secret).
//   - Redaction for event-like documents represented as map[string]any.
//   - Retention decisions based on caller-provided "now" and event timestamps.
//
// Determinism guarantees:
//   - No randomness.
//   - No time.Now usage (caller supplies time.Time).
//   - Canonical JSON helper sorts keys at all depths by converting maps to ordered KV slices.
//   - Redaction applies deterministic rules and emits deterministic summaries.
//
// Notes:
//   - Treat these helpers as a toolkit. Enforcement (delete/export/retention application) is a higher-level concern.
//   - This code intentionally stays stdlib-only.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrGDPR        = errors.New("gdpr failed")
	ErrGDPRInvalid = errors.New("gdpr invalid")
	ErrGDPRPolicy  = errors.New("gdpr policy")
)

// SubjectID is an identifier for a data subject (user/customer/device/etc.).
// It is treated as a string in normalized form.
type SubjectID string

// Pseudonym describes a deterministic pseudonymous token derived from a SubjectID.
type Pseudonym struct {
	Algorithm string `json:"algorithm"` // "hmac-sha256"
	Value     string `json:"value"`     // hex (64)
}

// RedactionRule defines what to redact and how.
// Paths are dot-separated keys, e.g. "detail.email" or "meta.ip".
// Wildcards are NOT supported in v0 to keep determinism and simplicity.
type RedactionRule struct {
	Path        string `json:"path"`
	Replacement string `json:"replacement"` // e.g. "[REDACTED]"
	Enabled     bool   `json:"enabled"`
	Notes       string `json:"notes,omitempty"`
}

// RetentionPolicy is a deterministic rule-set that decides whether an event should be kept.
// This is data-only: it returns decisions; it does not delete anything.
type RetentionPolicy struct {
	Version string `json:"version"`
	// MaxAge is the default retention window. Events older than (now - MaxAge) are considered expired.
	MaxAge time.Duration `json:"max_age"`
	// PerAction overrides (optional): action -> max_age.
	// WARNING: map order is not deterministic; callers should not serialize this map directly if they require stable bytes.
	PerAction map[string]time.Duration `json:"per_action,omitempty"`
	// If true, events missing/invalid timestamps are treated as expired.
	ExpireOnInvalidTS bool `json:"expire_on_invalid_ts"`
}

// Decision is a deterministic result of applying policy.
type Decision struct {
	Keep     bool   `json:"keep"`
	Reason   string `json:"reason"` // stable string
	EventTS  string `json:"event_ts,omitempty"`
	CutoffTS string `json:"cutoff_ts,omitempty"`
}

// DeletionRequest is a data-only representation of a GDPR deletion request.
type DeletionRequest struct {
	TenantID    string    `json:"tenant_id"`
	SubjectID   SubjectID `json:"subject_id"`
	RequestedAt string    `json:"requested_at"` // RFC3339Nano (caller-provided)
	Reason      string    `json:"reason,omitempty"`
}

// NormalizeSubjectID produces a stable, defensive representation:
// - trims space
// - removes NUL
// - collapses whitespace
// - lowercases (optional via parameter)
func NormalizeSubjectID(s string, lowercase bool) SubjectID {
	s = normCollapse(s)
	if lowercase {
		s = strings.ToLower(s)
	}
	return SubjectID(s)
}

// Pseudonymize deterministically derives a pseudonym from a subject id using HMAC-SHA256.
// - secret must be provided by caller (e.g., per-tenant secret).
// - output is stable for same (secret, subjectID).
func Pseudonymize(secret []byte, subject SubjectID) (Pseudonym, error) {
	if len(secret) == 0 {
		return Pseudonym{}, fmt.Errorf("%w: %w: secret required", ErrGDPR, ErrGDPRInvalid)
	}
	s := normCollapse(string(subject))
	if s == "" {
		return Pseudonym{}, fmt.Errorf("%w: %w: subject_id required", ErrGDPR, ErrGDPRInvalid)
	}
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write([]byte(s))
	sum := m.Sum(nil)
	return Pseudonym{Algorithm: "hmac-sha256", Value: hex.EncodeToString(sum)}, nil
}

// Redact applies deterministic redaction rules to an event-like document.
// The document is treated as a JSON-like object: map[string]any nested with map[string]any and []any.
// Returns a deep-copied redacted document and a deterministic summary.
func Redact(doc map[string]any, rules []RedactionRule) (map[string]any, map[string]string, error) {
	if doc == nil {
		return map[string]any{}, map[string]string{}, nil
	}

	rd := deepCopyAnyMap(doc)

	// Normalize and sort rules deterministically by Path.
	nrules := make([]RedactionRule, 0, len(rules))
	for _, r := range rules {
		rr := r
		rr.Path = normCollapse(rr.Path)
		rr.Replacement = normCollapse(rr.Replacement)
		rr.Notes = normCollapse(rr.Notes)
		if rr.Replacement == "" {
			rr.Replacement = "[REDACTED]"
		}
		if rr.Path == "" {
			continue
		}
		nrules = append(nrules, rr)
	}
	
	sort.Slice(nrules, func(i, j int) bool {
		if nrules[i].Path != nrules[j].Path {
			return nrules[i].Path < nrules[j].Path
		}
		return nrules[i].Replacement < nrules[j].Replacement
	})

	// Apply enabled rules only.
	applied := make([]string, 0, len(nrules))
	for _, r := range nrules {
		if !r.Enabled {
			continue
		}
		if applyPathRedaction(rd, r.Path, r.Replacement) {
			applied = append(applied, r.Path)
		}
	}

	// Deterministic summary map.
	summary := make(map[string]string)
	summary["redaction.applied_count"] = fmt.Sprintf("%d", len(applied))
	for i, p := range applied {
		summary[fmt.Sprintf("redaction.path.%03d", i+1)] = p
	}

	return rd, summary, nil
}

// DefaultRedactionRules provides a conservative baseline list of common PII keys.
// This list is stable and ordered; callers may extend/disable entries.
func DefaultRedactionRules() []RedactionRule {
	// NOTE: these are heuristic, data-only rules. Do not claim enforcement outside calling code.
	return []RedactionRule{
		{Path: "detail.email", Replacement: "[REDACTED]", Enabled: true, Notes: "email"},
		{Path: "detail.phone", Replacement: "[REDACTED]", Enabled: true, Notes: "phone"},
		{Path: "detail.ip", Replacement: "[REDACTED]", Enabled: true, Notes: "ip"},
		{Path: "detail.name", Replacement: "[REDACTED]", Enabled: true, Notes: "name"},
		{Path: "meta.email", Replacement: "[REDACTED]", Enabled: true, Notes: "email"},
		{Path: "meta.phone", Replacement: "[REDACTED]", Enabled: true, Notes: "phone"},
		{Path: "meta.ip", Replacement: "[REDACTED]", Enabled: true, Notes: "ip"},
		{Path: "actor_id", Replacement: "[REDACTED]", Enabled: false, Notes: "enable if actor_id is PII in your system"},
	}
}

// DecideRetention returns a deterministic keep/drop decision.
// Inputs:
// - now: caller-provided current time
// - eventTS: RFC3339 or RFC3339Nano (string)
// - action: optional action name for PerAction overrides
func DecideRetention(policy RetentionPolicy, now time.Time, eventTS string, action string) (Decision, error) {
	p := normalizePolicy(policy)
	if now.IsZero() {
		return Decision{}, fmt.Errorf("%w: %w: now required", ErrGDPR, ErrGDPRInvalid)
	}

	eventTS = normCollapse(eventTS)
	action = normCollapse(action)

	if eventTS == "" {
		if p.ExpireOnInvalidTS {
			return Decision{Keep: false, Reason: "expired:missing_ts"}, nil
		}
		return Decision{Keep: true, Reason: "keep:missing_ts"}, nil
	}

	t, err := parseRFC3339(eventTS)
	if err != nil {
		if p.ExpireOnInvalidTS {
			return Decision{Keep: false, Reason: "expired:invalid_ts", EventTS: eventTS}, nil
		}
		return Decision{Keep: true, Reason: "keep:invalid_ts", EventTS: eventTS}, nil
	}

	maxAge := p.MaxAge
	if action != "" && p.PerAction != nil {
		if v, ok := p.PerAction[action]; ok && v > 0 {
			maxAge = v
		}
	}

	if maxAge <= 0 {
		return Decision{}, fmt.Errorf("%w: %w: max_age must be >0", ErrGDPR, ErrGDPRPolicy)
	}

	cutoff := now.Add(-maxAge)
	keep := t.After(cutoff) || t.Equal(cutoff)

	return Decision{
		Keep:     keep,
		Reason:   ternary(keep, "keep:within_window", "expired:outside_window"),
		EventTS:  t.UTC().Format(time.RFC3339Nano),
		CutoffTS: cutoff.UTC().Format(time.RFC3339Nano),
	}, nil
}

// CanonicalJSONBytes returns deterministic JSON bytes for a JSON-like value.
// It converts maps into ordered kv arrays at all depths, producing stable output.
func CanonicalJSONBytes(v any) ([]byte, error) {
	c := canonicalizeAny(v)
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("%w: canonical json marshal: %v", ErrGDPR, err)
	}
	return b, nil
}

////////////////////////////////////////////////////////////////////////////////
// Internal helpers (deterministic)
////////////////////////////////////////////////////////////////////////////////

func normalizePolicy(p RetentionPolicy) RetentionPolicy {
	pp := p
	pp.Version = normCollapse(pp.Version)
	if pp.Version == "" {
		pp.Version = "v1"
	}
	if pp.MaxAge <= 0 {
		pp.MaxAge = 90 * 24 * time.Hour // default 90 days
	}

	// Normalize PerAction keys deterministically (copy into a new map).
	if pp.PerAction != nil {
		tmp := make(map[string]time.Duration, len(pp.PerAction))
		keys := make([]string, 0, len(pp.PerAction))
		for k := range pp.PerAction {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			nk := normCollapse(k)
			if nk == "" {
				continue
			}
			v := pp.PerAction[k]
			if v <= 0 {
				continue
			}
			tmp[nk] = v
		}
		pp.PerAction = tmp
	}

	return pp
}

// applyPathRedaction redacts the value at a dot-path if present.
// Returns true if something was redacted.
func applyPathRedaction(root map[string]any, path string, replacement string) bool {
	path = normCollapse(path)
	if path == "" {
		return false
	}
	parts := strings.Split(path, ".")
	cur := any(root)
	for i := 0; i < len(parts); i++ {
		k := normCollapse(parts[i])
		if k == "" {
			return false
		}
		isLast := (i == len(parts)-1)
		switch node := cur.(type) {
		case map[string]any:
			if isLast {
				if _, ok := node[k]; ok {
					node[k] = replacement
					return true
				}
				return false
			}
			nxt, ok := node[k]
			if !ok {
				return false
			}
			cur = nxt
		default:
			return false
		}
	}
	return false
}

// canonicalKV is an ordered key/value pair for deterministic JSON.
type canonicalKV struct {
	K string `json:"k"`
	V any    `json:"v"`
}

// canonicalizeAny converts JSON-like values into a deterministic representation:
// - map[string]any becomes []canonicalKV sorted by key
// - map[string]string becomes []canonicalKV sorted by key
// - []any preserved order (caller must ensure input order is deterministic if needed)
func canonicalizeAny(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return normCollapse(t)
	case bool:
		return t
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case map[string]string:
		keys := make([]string, 0, len(t))
		tmp := make(map[string]string, len(t))
		for k, v := range t {
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
		out := make([]canonicalKV, 0, len(keys))
		for _, k := range keys {
			out = append(out, canonicalKV{K: k, V: tmp[k]})
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(t))
		tmp := make(map[string]any, len(t))
		for k, v := range t {
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

func deepCopyAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	// Copy deterministically by sorted keys.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(m))
	for _, k := range keys {
		out[k] = deepCopyAny(m[k])
	}
	return out
}

func deepCopyAny(v any) any {
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
	case map[string]any:
		return deepCopyAnyMap(t)
	case map[string]string:
		out := make(map[string]string, len(t))
		// Copy deterministically by sorted keys.
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out[k] = t[k]
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = deepCopyAny(t[i])
		}
		return out
	default:
		return t
	}
}

func parseRFC3339(s string) (time.Time, error) {
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
	return strings.Join(strings.Fields(s), " ")
}

func ternary(ok bool, a, b string) string {
	if ok {
		return a
	}
	return b
}
