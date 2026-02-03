package rbac

// RBAC policy engine (deterministic, library-only).
//
// This engine evaluates whether a principal is allowed to perform a permissioned action
// within a tenant based on role assignments and role->permission expansions.
//
// Determinism guarantees:
//   - Role IDs are normalized, deduped, and sorted.
//   - Effective permissions are deduped and sorted.
//   - Evaluation uses sorted grants for stable MatchedGrant selection.
//   - No randomness; no time.Now usage (caller provides "now").
//
// Note:
// - This file does not load YAML catalogs. It operates on in-memory role definitions.

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
	ErrPolicyExpired = errors.New("policy expired")
)

type Principal struct {
	ID   string `json:"id"`
	Type string `json:"type"` // e.g. "user", "service"
}
type Assignment struct {
	TenantID  string            `json:"tenant_id"`
	Principal Principal         `json:"principal"`
	RoleIDs   []string          `json:"role_ids"`
	IssuedAt  string            `json:"issued_at"`            // RFC3339Nano (caller-provided)
	ExpiresAt string            `json:"expires_at,omitempty"` // RFC3339Nano optional
	Meta      map[string]string `json:"meta,omitempty"`
}
type Decision struct {
	Allowed        bool         `json:"allowed"`
	Reason         string       `json:"reason"`
	MatchedGrant   Permission   `json:"matched_grant,omitempty"`
	EffectiveRoles []string     `json:"effective_roles,omitempty"`
	EffectivePerms []Permission `json:"effective_perms,omitempty"`
}
type Engine struct {
	roles map[string]Role
}

func NewEngine(roles map[string]Role) (*Engine, error) {
	if roles == nil {
		return nil, fmt.Errorf("%w: %w: roles required", ErrPolicy, ErrPolicyInvalid)
	}

	// Copy roles map defensively.
	cp := make(map[string]Role, len(roles))
	keys := make([]string, 0, len(roles))
	for k := range roles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r := roles[k]
		r.ID = norm(r.ID)
		if r.ID == "" {
			// If key is the id, use it.
			r.ID = norm(k)
		}

		// Normalize inherits deterministically.
		r.Inherits = normalizeStringSlice(r.Inherits)

		// Normalize permissions via Parse to ensure strict format.
		perms := make([]Permission, 0, len(r.Permissions))
		for _, p := range r.Permissions {
			pp, err := Parse(string(p))
			if err != nil {
				continue
			}
			perms = append(perms, pp)
		}
		perms = normalizePermSlice(perms)
		r.Permissions = perms

		cp[r.ID] = r
	}
	return &Engine{roles: cp}, nil
}

// Compile expands roles from an assignment into effective roles and permissions.
func (e *Engine) Compile(tenantID string, asg Assignment) ([]string, []Permission, error) {
	if e == nil {
		return nil, nil, fmt.Errorf("%w: engine nil", ErrPolicyInvalid)
	}
	tid := norm(tenantID)
	if tid == "" {
		tid = norm(asg.TenantID)
	}
	if tid == "" {
		return nil, nil, fmt.Errorf("%w: tenant_id required", ErrPolicyInvalid)
	}
	roleIDs := normalizeStringSlice(asg.RoleIDs)
	if len(roleIDs) == 0 {
		return nil, nil, fmt.Errorf("%w: no roles", ErrPolicyInvalid)
	}
	permSet := NewSet()
	effectiveRoles := make([]string, 0, len(roleIDs))
	for _, rid := range roleIDs {
		perms, err := ExpandRole(rid, e.roles)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range perms {
			permSet = permSet.Add(p)
		}
		effectiveRoles = append(effectiveRoles, rid)
	}
	effectiveRoles = normalizeStringSlice(effectiveRoles)
	return effectiveRoles, permSet.List(), nil
}

// Decide evaluates whether assignment allows the requested permission.
// now is caller-provided (no time.Now usage).
func (e *Engine) Decide(tenantID string, asg Assignment, want Permission, now time.Time) (Decision, error) {
	if e == nil {
		return Decision{}, fmt.Errorf("%w: engine nil", ErrPolicyInvalid)
	}
	tid := norm(tenantID)
	if tid == "" {
		tid = norm(asg.TenantID)
	}
	if tid == "" {
		return Decision{}, fmt.Errorf("%w: tenant_id required", ErrPolicyInvalid)
	}

	// Validate principal.
	asg.Principal.ID = norm(asg.Principal.ID)
	asg.Principal.Type = norm(asg.Principal.Type)
	if asg.Principal.ID == "" || asg.Principal.Type == "" {
		return Decision{}, fmt.Errorf("%w: principal required", ErrPolicyInvalid)
	}

	// Validate time bounds.
	if norm(asg.IssuedAt) == "" {
		return Decision{}, fmt.Errorf("%w: issued_at required", ErrPolicyInvalid)
	}
	iat, err := parseRFC3339(asg.IssuedAt)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: invalid issued_at", ErrPolicyInvalid)
	}
	if !now.IsZero() && now.Before(iat) {
		return Decision{}, fmt.Errorf("%w: assignment not yet valid", ErrPolicyExpired)
	}
	if norm(asg.ExpiresAt) != "" {
		exp, err := parseRFC3339(asg.ExpiresAt)
		if err != nil {
			return Decision{}, fmt.Errorf("%w: invalid expires_at", ErrPolicyInvalid)
		}
		if !now.IsZero() && !now.Before(exp) {
			return Decision{}, fmt.Errorf("%w: assignment expired", ErrPolicyExpired)
		}
	}
	w, err := Parse(string(want))
	if err != nil {
		return Decision{}, fmt.Errorf("%w: invalid want permission", ErrPolicyInvalid)
	}
	roles, perms, err := e.Compile(tid, asg)
	if err != nil {
		return Decision{}, err
	}
	matched := Permission("")
	allowed := false
	for _, g := range perms {
		if Match(g, w) {
			allowed = true
			matched = g
			break // first match is deterministic because perms is sorted
		}
	}
	reason := "denied"
	if allowed {
		reason = "allowed"
	}
	return Decision{
		Allowed:        allowed,
		Reason:         reason,
		MatchedGrant:   matched,
		EffectiveRoles: roles,
		EffectivePerms: perms,
	}, nil
}

////////////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////////////

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	tmp := make([]string, 0, len(in))
	for _, s := range in {
		n := norm(s)
		if n == "" {
			continue
		}
		tmp = append(tmp, n)
	}
	sort.Strings(tmp)
	out := make([]string, 0, len(tmp))
	// var last string
	for _, s := range tmp {
		if s != last {
			out = append(out, s)
			last = s
		}
	}
	return out
}
func normalizePermSlice(in []Permission) []Permission {
	if len(in) == 0 {
		return nil
	}
	tmp := make([]Permission, 0, len(in))
	for _, p := range in {
		pp, err := Parse(string(p))
		if err != nil {
			continue
		}
		tmp = append(tmp, pp)
	}
	sort.Slice(tmp, func(i, j int) bool { return tmp[i] < tmp[j] })
	out := make([]Permission, 0, len(tmp))
	// var last Permission
	for _, p := range tmp {
		if p != last {
			out = append(out, p)
			last = p
		}
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
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	// return s
}
