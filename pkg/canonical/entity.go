package canonical

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Canonical Entity Contract (v0)
//
// Goal:
// A minimal, stable identifier/ref model used across Chartly services.
//
// Design Notes:
// - TenantID is REQUIRED for EntityRef to prevent cross-tenant leakage.
// - Kind is a normalized string (lowercase, trimmed)
// validated against a safe charset.
// - EntityID is an opaque identifier (ULID/UUID/KSUID/etc). We validate charset/length only,
//   not semantic meaning (services may choose their own generator).
//
// String form (EntityRef):
//   "<tenant>/<kind>/<id>"
//
// Examples:
//   "local/stream/01J0MZ3K9J6TQ4P5QJ0K6W3D9K"
//   "tenant-a/device/550e8400e29b41d4a716446655440000"

// type TenantID string
// type EntityID string
// type EntityKind string

// Entity is an object description (optional payload lives elsewhere).
// Many systems will store only EntityRef, but Entity is useful for typed metadata.
type Entity struct {
	Tenant TenantID   `json:"tenant"`
	Kind   EntityKind `json:"kind"`
	ID     EntityID   `json:"id"`
	// Optional display/diagnostic fields (non-authoritative).
	Name string `json:"name,omitempty"`
	// Arbitrary tags can be attached for indexing/search; not for access control.
	Tags map[string]string `json:"tags,omitempty"`
}

// EntityRef is the canonical reference used in events, storage keys, and audit trails.
type EntityRef struct {
	Tenant TenantID   `json:"tenant"`
	Kind   EntityKind `json:"kind"`
	ID     EntityID   `json:"id"`
}

func (r EntityRef) String() string {
	return fmt.Sprintf("%s/%s/%s", r.Tenant, r.Kind, r.ID)
}

// MarshalText allows EntityRef to be used cleanly in logs, map keys, etc.
func (r EntityRef) MarshalText() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return []byte(r.String()), nil
}

// UnmarshalText parses "<tenant>/<kind>/<id>".
func (r *EntityRef) UnmarshalText(b []byte) error {
	parsed, err := ParseEntityRef(string(b))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// MarshalJSON ensures validation before emitting.
func (r EntityRef) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	type alias EntityRef
	return json.Marshal(alias(r))
}

// UnmarshalJSON validates after decoding.
func (r *EntityRef) UnmarshalJSON(b []byte) error {
	type alias EntityRef
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	tmp := EntityRef(a)
	if err := tmp.Validate(); err != nil {
		return err
	}
	*r = tmp
	return nil
}

// NormalizeKind lowercases and trims a kind string.
func NormalizeKind(s string) EntityKind {
	return EntityKind(strings.ToLower(strings.TrimSpace(s)))
}

// Common errors are exported for easy checks in callers.
var (
	ErrEmptyTenant      = errors.New("canonical: tenant id is required")
	ErrEmptyKind        = errors.New("canonical: entity kind is required")
	ErrEmptyID          = errors.New("canonical: entity id is required")
	ErrInvalidTenant    = errors.New("canonical: invalid tenant id")
	ErrInvalidKind      = errors.New("canonical: invalid entity kind")
	ErrInvalidID        = errors.New("canonical: invalid entity id")
	ErrInvalidRefFormat = errors.New("canonical: invalid entity ref format (expected <tenant>/<kind>/<id>)")
)

// Validation constraints:
// - TenantID: lowercase recommended; allow [a-z0-9][a-z0-9-_]{0,62}  (1..63 chars)
// - Kind:     [a-z][a-z0-9._-]{0,63}                                (1..64 chars)
// - ID:       [A-Za-z0-9][A-Za-z0-9_-]{0,127}                        (1..128 chars)
//
// These are intentionally permissive to support multiple ID formats.
// Services should still generate IDs deterministically and avoid embedding PII.

var (
	tenantRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	kindRe   = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	idRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
)

// ValidateTenantID validates TenantID.
func ValidateTenantID(t TenantID) error {
	s := strings.TrimSpace(string(t))
	if s == "" {
		return ErrEmptyTenant
	}
	if !tenantRe.MatchString(s) {
		return fmt.Errorf("%w: %q", ErrInvalidTenant, s)
	}
	return nil
}

// ValidateKind validates EntityKind (after normalization).
func ValidateKind(k EntityKind) error {
	s := strings.TrimSpace(string(k))
	if s == "" {
		return ErrEmptyKind
	}
	if !kindRe.MatchString(s) {
		return fmt.Errorf("%w: %q", ErrInvalidKind, s)
	}
	return nil
}

// ValidateEntityID validates EntityID.
func ValidateEntityID(id EntityID) error {
	s := strings.TrimSpace(string(id))
	if s == "" {
		return ErrEmptyID
	}
	if !idRe.MatchString(s) {
		return fmt.Errorf("%w: %q", ErrInvalidID, s)
	}
	return nil
}

// Validate ensures the EntityRef is safe to use everywhere.
func (r EntityRef) Validate() error {
	if err := ValidateTenantID(r.Tenant); err != nil {
		return err
	}
	// Kind should be normalized by callers, but we still validate raw.
	if err := ValidateKind(r.Kind); err != nil {
		return err
	}
	if err := ValidateEntityID(r.ID); err != nil {
		return err
	}
	return nil
}

// NewEntityRef creates a validated reference with kind normalization.
func NewEntityRef(tenant TenantID, kind string, id EntityID) (EntityRef, error) {
	ref := EntityRef{
		Tenant: TenantID(strings.TrimSpace(string(tenant))),
		Kind:   NormalizeKind(kind),
		ID:     EntityID(strings.TrimSpace(string(id))),
	}
	if err := ref.Validate(); err != nil {
		return EntityRef{}, err
	}
	return ref, nil
}

// ParseEntityRef parses "<tenant>/<kind>/<id>" into EntityRef.
func ParseEntityRef(s string) (EntityRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return EntityRef{}, ErrInvalidRefFormat
	}
	// Split into exactly 3 parts to prevent ambiguity and injection.
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return EntityRef{}, ErrInvalidRefFormat
	}
	ref := EntityRef{
		Tenant: TenantID(strings.TrimSpace(parts[0])),
		Kind:   NormalizeKind(parts[1]),
		ID:     EntityID(strings.TrimSpace(parts[2])),
	}
	if err := ref.Validate(); err != nil {
		return EntityRef{}, err
	}
	return ref, nil
}
