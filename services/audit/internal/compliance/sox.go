package compliance

// SOX utilities (deterministic, library-only).
//
// This file provides data-only helpers intended to support SOX-aligned audit controls,
// focusing on change management and financial reporting-related auditability.
//
// It does NOT implement enforcement (no DB writes/deletes, no HTTP handlers).
//
// Features:
//   - Control classification and mapping from event-like documents.
//   - Retention guidance (caller provides "now").
//   - Integrity attestation via SHA256 over canonical JSON bytes.
//   - Change record extraction helpers (who/what/when).
//
// Determinism guarantees:
//   - No randomness.
//   - No time.Now usage (caller supplies time.Time).
//   - Canonical JSON uses ordered KV slices at all depths for stable bytes.

import (
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
	ErrSOX        = errors.New("sox failed")
	ErrSOXInvalid = errors.New("sox invalid")
	ErrSOXPolicy  = errors.New("sox policy")
)

type Control string

const (
	ControlAccess             Control = "access"
	ControlChangeManagement   Control = "change_mgmt"
	ControlFinancialReporting Control = "financial_reporting"
	ControlSecurity           Control = "security"
	ControlOperations         Control = "operations"
	ControlUnknown            Control = "unknown"
)

// MappingRule maps an event to one or more controls using deterministic substring matching.
type MappingRule struct {
	ActionContains string    `json:"action_contains"`
	OutcomeEquals  string    `json:"outcome_equals,omitempty"`
	Controls       []Control `json:"controls"`
	Enabled        bool      `json:"enabled"`
	Notes          string    `json:"notes,omitempty"`
}

// RetentionPolicy provides guidance for how long to retain SOX-relevant audit artifacts.
type RetentionPolicy struct {
	Version    string                 `json:"version"`
	// Default: 7 years (commonly used for SOX-related retention guidance).
	MaxAge     time.Duration          `json:"max_age"`
	PerControl map[Control]time.Duration `json:"per_control,omitempty"`
}

type Attestation struct {
	Algorithm string `json:"algorithm"` // "sha256"
	Value     string `json:"value"`     // hex (64)
}

type ChangeRecord struct {
	ActorID   string            `json:"actor_id,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	ObjectKey string            `json:"object_key,omitempty"`
	Action    string            `json:"action,omitempty"`
	Outcome   string            `json:"outcome,omitempty"`
	TS        string            `json:"ts,omitempty"` // RFC3339Nano if available
	Fields    []string          `json:"fields,omitempty"` // deterministic sorted list
	Summary   map[string]string `json:"summary,omitempty"` // deterministic keys
}

// DefaultMappingRules returns a conservative baseline set of rules.
// These are heuristic and data-only; update them to match your domain vocabulary.
func DefaultMappingRules() []MappingRule {
	return []MappingRule{
		{ActionContains: "access", Controls: []Control{ControlAccess, ControlSecurity}, Enabled: true, Notes: "access-related actions"},
		{ActionContains: "login", Controls: []Control{ControlAccess, ControlSecurity}, Enabled: true, Notes: "login"},
		{ActionContains: "role", Controls: []Control{ControlAccess}, Enabled: true, Notes: "role changes"},
		{ActionContains: "permission", Controls: []Control{ControlAccess}, Enabled: true, Notes: "permissions"},
		{ActionContains: "deploy", Controls: []Control{ControlChangeManagement}, Enabled: true, Notes: "deployments"},
		{ActionContains: "change", Controls: []Control{ControlChangeManagement}, Enabled: true, Notes: "change management"},
		{ActionContains: "config", Controls: []Control{ControlChangeManagement}, Enabled: true, Notes: "configuration changes"},
		{ActionContains: "invoice", Controls: []Control{ControlFinancialReporting}, Enabled: true, Notes: "invoices"},
		{ActionContains: "payment", Controls: []Control{ControlFinancialReporting}, Enabled: true, Notes: "payments"},
		{ActionContains: "revenue", Controls: []Control{ControlFinancialReporting}, Enabled: true, Notes: "revenue"},
	}
}

// MapControls returns the set of controls for an event action/outcome using deterministic rules.
// Output is sorted and deduplicated deterministically.
func MapControls(action string, outcome string, rules []MappingRule) ([]Control, error) {
	// Avoid shadowing errors by using distinct var name.
	a := strings.ToLower(normCollapse(action))
	o := strings.ToLower(normCollapse(outcome))
	if a == "" {
		return []Control{ControlUnknown}, nil
	}

	nrules := normalizeRules(rules)
	found := make([]Control, 0, 4)
	for _, r := range nrules {
		if !r.Enabled {
			continue
		}
		if r.ActionContains != "" && strings.Contains(a, r.ActionContains) {
			if r.OutcomeEquals != "" && o != r.OutcomeEquals {
				continue
			}
			for _, c := range r.Controls {
				found = append(found, c)
			}
		}
	}

	if len(found) == 0 {
		return []Control{ControlUnknown}, nil
	}

	// Deduplicate + sort deterministically.
	m := make(map[Control]struct{}, len(found))
	for _, c := range found {
		m[c] = struct{}{}
	}
	out := make([]Control, 0, len(m))
	for c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })

	return out, nil
}

// DecideRetention returns keep/drop guidance for a control at time "now" relative to eventTS.
func DecideRetention(policy RetentionPolicy, now time.Time, eventTS string, control Control) (bool, string, error) {
	p := normalizeRetention(policy)
	if now.IsZero() {
		return false, "", fmt.Errorf("%w: %w: now required", ErrSOX, ErrSOXInvalid)
	}

	eventTS = normCollapse(eventTS)
	if eventTS == "" {
		// If we cannot timestamp, advise keep to avoid accidental non-compliance.
		return true, "keep:missing_ts", nil
	}

	t, err := parseRFC3339(eventTS)
	if err != nil {
		return true, "keep:invalid_ts", nil
	}

	maxAge := p.MaxAge
	if p.PerControl != nil {
		if v, ok := p.PerControl[control]; ok && v > 0 {
			maxAge = v
		}
	}

	if maxAge <= 0 {
		return false, "", fmt.Errorf("%w: %w: max_age must be >0", ErrSOX, ErrSOXPolicy)
	}

	cutoff := now.Add(-maxAge)
	keep := t.After(cutoff) || t.Equal(cutoff)
	if keep {
		return true, "keep:within_window", nil
	}
	return false, "expired:outside_window", nil
}

// Attest computes a SHA256 over canonical JSON bytes of a JSON-like value.
func Attest(v any) (Attestation, error) {
	b, err := canonicalJSONBytes(v)
	if err != nil {
		return Attestation{}, err
	}
	sum := sha256.Sum256(b)
	return Attestation{Algorithm: "sha256", Value: hex.EncodeToString(sum[:])}, nil
}

// ExtractChangeRecord extracts change-relevant fields from a document.
// This is heuristic and data-only.
func ExtractChangeRecord(doc map[string]any) (ChangeRecord, error) {
	if doc == nil {
		return ChangeRecord{Summary: map[string]string{"fields": "0"}}, nil
	}

	getS := func(path string) string {
		if v, ok := getPath(doc, path); ok {
			if s, ok := v.(string); ok {
				return normCollapse(s)
			}
		}
		return ""
	}

	cr := ChangeRecord{
		ActorID:   getS("actor_id"),
		RequestID: getS("request_id"),
		ObjectKey: getS("object_key"),
		Action:    getS("action"),
		Outcome:   getS("outcome"),
		TS:        getS("ts"),
	}

	// Collect field keys under "detail" or "meta" if present (deterministic).
	fields := make([]string, 0, 16)
	if v, ok := getPath(doc, "detail"); ok {
		if m, ok := v.(map[string]any); ok {
			for k := range m {
				fields = append(fields, "detail."+normCollapse(k))
			}
		}
	}
	if v, ok := getPath(doc, "meta"); ok {
		if m, ok := v.(map[string]any); ok {
			for k := range m {
				fields = append(fields, "meta."+normCollapse(k))
			}
		}
	}

	fields = normalizePathList(fields)
	cr.Fields = fields
	cr.Summary = map[string]string{
		"fields": fmt.Sprintf("%d", len(fields)),
	}

	return cr, nil
}

////////////////////////////////////////////////////////////////////////////////
// Internal helpers (deterministic)
////////////////////////////////////////////////////////////////////////////////

func normalizeRules(rules []MappingRule) []MappingRule {
	n := make([]MappingRule, 0, len(rules))
	for _, r := range rules {
		rr := r
		rr.ActionContains = strings.ToLower(normCollapse(rr.ActionContains))
		rr.OutcomeEquals = strings.ToLower(normCollapse(rr.OutcomeEquals))
		rr.Notes = normCollapse(rr.Notes)
		rr.Controls = normalizeControls(rr.Controls)
		if rr.ActionContains == "" {
			continue
		}
		n = append(n, rr)
	}
	
	sort.Slice(n, func(i, j int) bool {
		if n[i].ActionContains != n[j].ActionContains {
			return n[i].ActionContains < n[j].ActionContains
		}
		return n[i].OutcomeEquals < n[j].OutcomeEquals
	})

	return n
}

func normalizeControls(in []Control) []Control {
	if len(in) == 0 {
		return []Control{ControlUnknown}
	}
	m := make(map[Control]struct{}, len(in))
	for _, c := range in {
		if strings.TrimSpace(string(c)) == "" {
			continue
		}
		m[c] = struct{}{}
	}
	out := make([]Control, 0, len(m))
	for c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}

func normalizeRetention(p RetentionPolicy) RetentionPolicy {
	pp := p
	pp.Version = normCollapse(pp.Version)
	if pp.Version == "" {
		pp.Version = "v1"
	}
	if pp.MaxAge <= 0 {
		pp.MaxAge = 7 * 365 * 24 * time.Hour
	}
	
	if pp.PerControl != nil {
		tmp := make(map[Control]time.Duration, len(pp.PerControl))
		keys := make([]string, 0, len(pp.PerControl))
		for k := range pp.PerControl {
			keys = append(keys, string(k))
		}
		sort.Strings(keys)
		for _, k := range keys {
			c := Control(k)
			v := pp.PerControl[c]
			if v > 0 {
				tmp[c] = v
			}
		}
		pp.PerControl = tmp
	}

	return pp
}

type canonicalKV struct {
	K string `json:"k"`
	V any    `json:"v"`
}

func canonicalJSONBytes(v any) ([]byte, error) {
	c := canonicalizeAny(v)
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("%w: canonical json marshal: %v", ErrSOX, err)
	}
	return b, nil
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
		return normCollapse(fmt.Sprintf("%v", t))
	}
}

func getPath(root map[string]any, path string) (any, bool) {
	parts := strings.Split(normCollapse(path), ".")
	cur := any(root)
	for i := 0; i < len(parts); i++ {
		k := normCollapse(parts[i])
		if k == "" {
			return nil, false
		}
		last := i == len(parts)-1
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[k]
			if !ok {
				return nil, false
			}
			if last {
				return v, true
			}
			cur = v
		default:
			return nil, false
		}
	}
	return nil, false
}

func normalizePathList(in []string) []string {
	tmp := make([]string, 0, len(in))
	for _, p := range in {
		pn := normCollapse(p)
		if pn == "" {
			continue
		}
		tmp = append(tmp, pn)
	}
	sort.Strings(tmp)
	out := make([]string, 0, len(tmp))
	var last string
	for _, p := range tmp {
		if p != last {
			out = append(out, p)
			last = p
		}
	}
	return out
}

func normCollapse(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
