package canonical

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

// Canonical Event Envelope (v0)
//
// Purpose:
// A minimal, stable event format used across Chartly services for transport, storage, and audit.
//
// Key properties:
// - Multi-tenant safe: Tenant is REQUIRED and always part of the partition key.
// - JSON-first: Payload is json.RawMessage to avoid hard coupling.
// - Causality: trace/span/parent/correlation fields for observability.
// - Tamper-evidence ready: prev_hash + hash allow hash chaining (optional in v0).
//
// Hashing:
// - CanonicalBytes() returns stable JSON bytes (deterministic order) for hashing.
// - ComputeHash(prevHash) sets PrevHash + Hash fields.
// - Hash is SHA-256 over CanonicalBytes() with PrevHash included.
//
// String conventions:
// - IDs are validated as opaque tokens (similar to EntityID rules).
// - EventType is normalized lower-case with safe charset.

type EventID string
type TraceID string
type SpanID string
type CorrelationID string

// EventType is a stable category like "trade.signal.created" or "storage.chunk.written"
type EventType string

// EventMeta holds envelope metadata separate from payload.
// Keep this stable; add new fields carefully (backwards compatibility).
type EventMeta struct {
	ID       EventID   `json:"id"`
	Tenant   TenantID  `json:"tenant"`
	Type     EventType `json:"type"`
	Occurred time.Time `json:"occurred"` // UTC, RFC3339Nano
	Emitted  time.Time `json:"emitted"`  // UTC, RFC3339Nano (when this envelope was produced)

	// Subject is the primary entity this event refers to (optional but recommended).
	Subject *EntityRef `json:"subject,omitempty"`

	// Causality / tracing
	TraceID       TraceID       `json:"trace_id,omitempty"`
	SpanID        SpanID        `json:"span_id,omitempty"`
	ParentEventID EventID       `json:"parent_event_id,omitempty"`
	CorrelationID CorrelationID `json:"correlation_id,omitempty"`

	// Producer / origin
	Producer string `json:"producer,omitempty"` // e.g., "gateway", "storage", "analytics"
	Source   string `json:"source,omitempty"`   // e.g., "http", "cron", "import"
	Schema   string `json:"schema,omitempty"`   // optional payload schema/version tag

	// Tamper-evidence (optional in v0)
	PrevHash string `json:"prev_hash,omitempty"` // hex sha256
	Hash     string `json:"hash,omitempty"`      // hex sha256
}

// Event is the full envelope.
type Event struct {
	Meta       EventMeta           `json:"meta"`
	Payload    json.RawMessage     `json:"payload,omitempty"`
	Attributes map[string]string   `json:"attributes,omitempty"`
}

// NormalizeType lowercases and trims.
func NormalizeType(s string) EventType {
	return EventType(strings.ToLower(strings.TrimSpace(s)))
}

var (
	ErrEmptyEventID   = errors.New("canonical: event id is required")
	ErrEmptyEventType = errors.New("canonical: event type is required")
	ErrEmptyOccurred  = errors.New("canonical: occurred time is required")
	ErrEmptyEmitted   = errors.New("canonical: emitted time is required")
	ErrInvalidEventID = errors.New("canonical: invalid event id")
	ErrInvalidType    = errors.New("canonical: invalid event type")
	ErrInvalidHash    = errors.New("canonical: invalid hash (expected hex sha256)")
)

func validateOpaqueID(label string, s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("canonical: %s is required", label)
	}
	// Permissive opaque token: [A-Za-z0-9][A-Za-z0-9._-]{0,127}
	if len(s) > 128 {
		return fmt.Errorf("canonical: invalid %s (too long): %q", label, s)
	}
	for i, r := range s {
		ok := (r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.'
		if !ok || (i == 0 && (r == '_' || r == '-' || r == '.')) {
			return fmt.Errorf("canonical: invalid %s: %q", label, s)
		}
	}
	return nil
}

func validateEventType(t EventType) error {
	s := strings.TrimSpace(string(t))
	if s == "" {
		return ErrEmptyEventType
	}
	// event type pattern: starts with a-z, then [a-z0-9._-], 1..96 chars
	if len(s) > 96 {
		return fmt.Errorf("%w (too long): %q", ErrInvalidType, s)
	}
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-'
		if !ok || (i == 0 && (r < 'a' || r > 'z')) {
			return fmt.Errorf("%w: %q", ErrInvalidType, s)
		}
	}
	return nil
}

func isHexSha256(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

// Normalize enforces consistent casing/UTC times and cleans attribute keys.
func (e *Event) Normalize() {
	e.Meta.Type = NormalizeType(string(e.Meta.Type))

	if !e.Meta.Occurred.IsZero() {
		e.Meta.Occurred = e.Meta.Occurred.UTC()
	}
	if !e.Meta.Emitted.IsZero() {
		e.Meta.Emitted = e.Meta.Emitted.UTC()
	}

	if e.Attributes != nil {
		clean := make(map[string]string, len(e.Attributes))
		for k, v := range e.Attributes {
			k2 := strings.TrimSpace(strings.ToLower(k))
			if k2 == "" {
				continue
			}
			clean[k2] = strings.TrimSpace(v)
		}
		if len(clean) == 0 {
			e.Attributes = nil
		} else {
			e.Attributes = clean
		}
	}

	e.Meta.Producer = strings.TrimSpace(e.Meta.Producer)
	e.Meta.Source = strings.TrimSpace(e.Meta.Source)
	e.Meta.Schema = strings.TrimSpace(e.Meta.Schema)
	e.Meta.PrevHash = strings.TrimSpace(strings.ToLower(e.Meta.PrevHash))
	e.Meta.Hash = strings.TrimSpace(strings.ToLower(e.Meta.Hash))
}

// Validate checks the envelope is safe to route/store.
func (e Event) Validate() error {
	if err := validateOpaqueID("event id", string(e.Meta.ID)); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEventID, err)
	}
	if err := ValidateTenantID(e.Meta.Tenant); err != nil {
		return err
	}
	if err := validateEventType(e.Meta.Type); err != nil {
		return err
	}
	if e.Meta.Occurred.IsZero() {
		return ErrEmptyOccurred
	}
	if e.Meta.Emitted.IsZero() {
		return ErrEmptyEmitted
	}
	if e.Meta.Subject != nil {
		if err := e.Meta.Subject.Validate(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(string(e.Meta.TraceID)) != "" {
		if err := validateOpaqueID("trace id", string(e.Meta.TraceID)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(string(e.Meta.SpanID)) != "" {
		if err := validateOpaqueID("span id", string(e.Meta.SpanID)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(string(e.Meta.ParentEventID)) != "" {
		if err := validateOpaqueID("parent event id", string(e.Meta.ParentEventID)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(string(e.Meta.CorrelationID)) != "" {
		if err := validateOpaqueID("correlation id", string(e.Meta.CorrelationID)); err != nil {
			return err
		}
	}
	if !isHexSha256(strings.TrimSpace(strings.ToLower(e.Meta.PrevHash))) {
		return fmt.Errorf("%w: prev_hash", ErrInvalidHash)
	}
	if !isHexSha256(strings.TrimSpace(strings.ToLower(e.Meta.Hash))) {
		return fmt.Errorf("%w: hash", ErrInvalidHash)
	}
	return nil
}

// NewEvent constructs a normalized, validated event with a deterministic ID if id is empty.
// occurred and emitted must be provided by the caller; emitted defaults to occurred if zero.
func NewEvent(tenant TenantID, eventType string, occurred time.Time, payload json.RawMessage) (Event, error) {
	if occurred.IsZero() {
		return Event{}, ErrEmptyOccurred
	}
	if payload == nil {
		payload = json.RawMessage("null")
	}

	emitted := occurred
	e := Event{
		Meta: EventMeta{
			ID:       EventID(""),
			Tenant:   tenant,
			Type:     NormalizeType(eventType),
			Occurred: occurred.UTC(),
			Emitted:  emitted.UTC(),
		},
		Payload: payload,
	}

	e.Meta.ID = DeterministicEventID(e)
	e.Normalize()
	if err := e.Validate(); err != nil {
		return Event{}, err
	}
	return e, nil
}

// DeterministicEventID generates a stable event ID from core fields.
// This is a safe fallback when no external ID is provided.
func DeterministicEventID(e Event) EventID {
	seed := buildEventIDSeed(e)
	sum := sha256.Sum256(seed)
	return EventID(hex.EncodeToString(sum[:8])) // 16 hex chars
}

func buildEventIDSeed(e Event) []byte {
	// Stable seed: tenant|type|occurred|payload
	p := e.Payload
	if p == nil {
		p = json.RawMessage("null")
	}
	parts := []string{
		strings.TrimSpace(string(e.Meta.Tenant)),
		strings.TrimSpace(string(e.Meta.Type)),
		e.Meta.Occurred.UTC().Format(time.RFC3339Nano),
	}
	return []byte(strings.Join(parts, "|") + "|" + string(p))
}

// CanonicalBytes returns deterministic JSON bytes for hashing.
// This must remain stable across versions; do NOT include Meta.Hash itself in the canonical form.
// PrevHash is included because it affects hash chaining.
func (e Event) CanonicalBytes() ([]byte, error) {
	// Canonical attributes as ordered kv list
	type kv struct {
		K string `json:"k"`
		V string `json:"v"`
	}
	var attrs []kv
	if e.Attributes != nil {
		keys := make([]string, 0, len(e.Attributes))
		for k := range e.Attributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		attrs = make([]kv, 0, len(keys))
		for _, k := range keys {
			attrs = append(attrs, kv{K: k, V: e.Attributes[k]})
		}
	}

	// Subject alias for deterministic order
	type subjectAlias struct {
		Tenant TenantID   `json:"tenant"`
		Kind   EntityKind `json:"kind"`
		ID     EntityID   `json:"id"`
	}
	var subj *subjectAlias
	if e.Meta.Subject != nil {
		subj = &subjectAlias{
			Tenant: e.Meta.Subject.Tenant,
			Kind:   e.Meta.Subject.Kind,
			ID:     e.Meta.Subject.ID,
		}
	}

	// IMPORTANT: exclude Meta.Hash (self-referential)
	canon := struct {
		Meta struct {
			ID            EventID       `json:"id"`
			Tenant        TenantID      `json:"tenant"`
			Type          EventType     `json:"type"`
			Occurred      string        `json:"occurred"`
			Emitted       string        `json:"emitted"`
			Subject       *subjectAlias `json:"subject,omitempty"`
			TraceID       TraceID       `json:"trace_id,omitempty"`
			SpanID        SpanID        `json:"span_id,omitempty"`
			ParentEventID EventID       `json:"parent_event_id,omitempty"`
			CorrelationID CorrelationID `json:"correlation_id,omitempty"`
			Producer      string        `json:"producer,omitempty"`
			Source        string        `json:"source,omitempty"`
			Schema        string        `json:"schema,omitempty"`
			PrevHash      string        `json:"prev_hash,omitempty"`
		} `json:"meta"`
		Payload    json.RawMessage `json:"payload,omitempty"`
		Attributes []kv            `json:"attributes,omitempty"`
	}{}

	canon.Meta.ID = e.Meta.ID
	canon.Meta.Tenant = e.Meta.Tenant
	canon.Meta.Type = e.Meta.Type
	canon.Meta.Occurred = e.Meta.Occurred.UTC().Format(time.RFC3339Nano)
	canon.Meta.Emitted = e.Meta.Emitted.UTC().Format(time.RFC3339Nano)
	canon.Meta.Subject = subj
	canon.Meta.TraceID = e.Meta.TraceID
	canon.Meta.SpanID = e.Meta.SpanID
	canon.Meta.ParentEventID = e.Meta.ParentEventID
	canon.Meta.CorrelationID = e.Meta.CorrelationID
	canon.Meta.Producer = e.Meta.Producer
	canon.Meta.Source = e.Meta.Source
	canon.Meta.Schema = e.Meta.Schema
	canon.Meta.PrevHash = strings.TrimSpace(strings.ToLower(e.Meta.PrevHash))
	canon.Payload = e.Payload
	canon.Attributes = attrs

	return json.Marshal(canon)
}

// ComputeHash sets PrevHash and Hash fields (SHA-256) using CanonicalBytes.
func (e *Event) ComputeHash(prevHash string) error {
	e.Meta.PrevHash = strings.TrimSpace(strings.ToLower(prevHash))
	e.Meta.Hash = ""
	e.Normalize()
	if err := e.Validate(); err != nil {
		return err
	}
	b, err := e.CanonicalBytes()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	e.Meta.Hash = hex.EncodeToString(sum[:])
	return nil
}

// VerifyHash recomputes hash from current PrevHash and canonical bytes and compares.
func (e Event) VerifyHash() bool {
	if !isHexSha256(strings.TrimSpace(strings.ToLower(e.Meta.Hash))) || e.Meta.Hash == "" {
		return false
	}
	b, err := e.CanonicalBytes()
	if err != nil {
		return false
	}
	sum := sha256.Sum256(b)
	return strings.EqualFold(e.Meta.Hash, hex.EncodeToString(sum[:]))
}

// PartitionKey returns a deterministic partition key for storage/routing.
// Format: "<tenant>/<type>/<yyyy-mm-dd>"
func (e Event) PartitionKey() (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	day := e.Meta.Occurred.UTC().Format("2006-01-02")
	return fmt.Sprintf("%s/%s/%s", e.Meta.Tenant, e.Meta.Type, day), nil
}
