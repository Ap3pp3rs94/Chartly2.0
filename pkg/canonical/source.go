package canonical

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Canonical Source Envelope (v0)
//
// Source = "Where data originates from" (identity) + "How it's behaving" (ops metadata).
// This is NOT a connector implementation and NOT a job definition.
//
// Key design choices:
// - Immutable identity: tenant + kind + external_ref (dedupe, discovery).
// - Operational metadata: status, health, ownership, quotas, schema expectations.
// - No reverse references: Sources PRODUCE events/artifacts; events/artifacts should carry SourceID,
//   not the other way around.
// - Deterministic identity vs mutable metadata hashing:
//   - IdentityBytes() => stable identity
//   - MetadataBytes() => mutable state (+ prev_hash)
// for tamper-evidence.
//
// Hybrid constructor: caller may override id/now; otherwise default generation is used.

type SourceID string
type SourceKind string
type SourceStatus string
type SourceHealth string
type SourceExpectedOutput string
const (
	SourceActive   SourceStatus = "active"
	SourcePaused   SourceStatus = "paused"
	SourceError    SourceStatus = "error"
	SourceDisabled SourceStatus = "disabled"

	HealthHealthy   SourceHealth = "healthy"
	HealthDegraded  SourceHealth = "degraded"
	HealthUnhealthy SourceHealth = "unhealthy"

	OutEvent    SourceExpectedOutput = "event"
	OutMetric   SourceExpectedOutput = "metric"
	OutEntity   SourceExpectedOutput = "entity"
	OutArtifact SourceExpectedOutput = "artifact"
	OutUnknown  SourceExpectedOutput = "unknown"
)

// Limits (defense-in-depth)
const (
	SourceMaxIDLen          = 128
	SourceMaxKindLen        = 64
	SourceMaxExternalRefLen = 1024
	SourceMaxNameLen        = 128
	SourceMaxNotesLen       = 2048
	SourceMaxLabels         = 32
	SourceMaxLabelKeyLen    = 64
	SourceMaxLabelValLen    = 256

	SourceMaxOwnerIDLen = 128
	SourceMaxTeamLen    = 128
	SourceMaxOnCallLen  = 128

	SourceMaxSchemaRefLen  = 256
	SourceMaxSchemaHashLen = 64 // hex sha256
	SourceMaxConfigHashLen = 64 // hex sha256

	// Rate limit / quotas (hard safety bounds)
	SourceMaxRPS         = 100000
	SourceMaxConcurrency = 100000
	SourceMaxDailyQuota  = 1_000_000_000

	SourceMaxErrorLen        = 512
	SourceMaxRelatedEntities = 50
)

// SourceMeta holds core metadata.
type SourceMeta struct {
	ID     SourceID   `json:"id"`
	Tenant TenantID   `json:"tenant"`
	Kind   SourceKind `json:"kind"`

	// ExternalRef is the immutable identity anchor.
	// Examples:
	// - api: https://api.shopify.com/store/abc
	// - db:  postgres://host:5432/db?schema=public
	// - file: s3://bucket/prefix  (or gs://...)
	// - stream: kafka://cluster/topic  (v0: strict allowlist; kafka optional policy can be added later)
	ExternalRef string `json:"external_ref"`

	// Human-friendly label (mutable)
	Name string `json:"name,omitempty"`

	Created time.Time `json:"created"` // UTC RFC3339Nano
	Updated time.Time `json:"updated"` // UTC RFC3339Nano

	Status SourceStatus `json:"status"`
}

// Ownership + paging metadata (operational reality).
type SourceOwnership struct {
	OwnerID        string `json:"owner_id,omitempty"` // user/service identifier
	Team           string `json:"team,omitempty"`
	OnCallRotation string `json:"oncall_rotation,omitempty"` // e.g. "data-platform-primary"
}

// Rate limiting / safety controls declared at the Source level.
// Connector-hub enforces these during ingestion.
type SourceLimits struct {
	RateLimitRPS   int `json:"rate_limit_rps,omitempty"`  // requests per second
	MaxConcurrency int `json:"max_concurrency,omitempty"` // concurrent in-flight calls
	DailyQuota     int `json:"daily_quota,omitempty"`     // max requests/day (or records/day depending on kind)
}

// Schema expectations (prevents guessing + detects drift).
type SourceSchema struct {
	ExpectedOutputKind SourceExpectedOutput `json:"expected_output_kind"`
	SchemaRef          string               `json:"schema_ref,omitempty"`  // e.g. "contracts/v1/canonical/event.schema.json"
	SchemaHash         string               `json:"schema_hash,omitempty"` // hex sha256 of schema content (optional)
}

// Config versioning/auditability (who changed it, when, what).
type SourceConfigAudit struct {
	ConfigVersion  int       `json:"config_version,omitempty"`
	ConfigHash     string    `json:"config_hash,omitempty"` // hex sha256 of normalized config blob
	LastModifiedBy string    `json:"last_modified_by,omitempty"`
	LastModifiedAt time.Time `json:"last_modified_at,omitempty"` // UTC; zero = unknown
}

// Health and stability tracking.
type SourceHealthState struct {
	HealthStatus        SourceHealth `json:"health_status"`
	ConsecutiveFailures int          `json:"consecutive_failures,omitempty"`
	LastSuccessAt       time.Time    `json:"last_success_at,omitempty"` // zero = never
	LastError           string       `json:"last_error,omitempty"`
	LastErrorAt         time.Time    `json:"last_error_at,omitempty"` // zero = never
	LastSeenAt          time.Time    `json:"last_seen_at,omitempty"`  // zero = never
}

// Source is the full envelope.
type Source struct {
	Meta SourceMeta `json:"meta"`

	// Optional semantic subject (what this source represents)
	Subject *EntityRef `json:"subject,omitempty"`

	Ownership SourceOwnership   `json:"ownership,omitempty"`
	Limits    SourceLimits      `json:"limits,omitempty"`
	Schema    SourceSchema      `json:"schema,omitempty"`
	Health    SourceHealthState `json:"health,omitempty"`

	// Low-cardinality tags (NOT access control)
	Labels map[string]string `json:"labels,omitempty"`

	// Optional: related entities only (bounded).
	// Do NOT attach produced events/artifacts here (reverse reference bomb).
	RelatedEntities []EntityRef `json:"related_entities,omitempty"`

	// Tamper-evidence (metadata only)
	PrevHash string `json:"prev_hash,omitempty"` // hex sha256
	Hash     string `json:"hash,omitempty"`      // hex sha256

	Notes       string            `json:"notes,omitempty"`
	ConfigAudit SourceConfigAudit `json:"config_audit,omitempty"`
}

// ---------- errors ----------

var (
	ErrSourceEmptyID          = errors.New("canonical: source id is required")
	ErrSourceEmptyKind        = errors.New("canonical: source kind is required")
	ErrSourceEmptyExternalRef = errors.New("canonical: external_ref is required")
	ErrSourceInvalidID        = errors.New("canonical: invalid source id")
	ErrSourceInvalidKind      = errors.New("canonical: invalid source kind")
	ErrSourceInvalidStatus    = errors.New("canonical: invalid source status")
	ErrSourceInvalidHealth    = errors.New("canonical: invalid source health_status")
	ErrSourceInvalidOutput    = errors.New("canonical: invalid expected_output_kind")
	ErrSourceTooManyLabels    = errors.New("canonical: too many labels")
	ErrSourceTooManyRefs      = errors.New("canonical: too many related entities")
	ErrSourceFieldTooBig      = errors.New("canonical: field exceeds max size")
	ErrSourceInvalidHash      = errors.New("canonical: invalid hash (expected hex sha256)")
	ErrSourceInvalidExternal  = errors.New("canonical: invalid external_ref")
	ErrSourceInvalidLimits    = errors.New("canonical: invalid limits")
	ErrSourceInvalidSchema    = errors.New("canonical: invalid schema")
	ErrSourceInvalidConfig    = errors.New("canonical: invalid config audit")
)

// ---------- ID / normalization helpers (prefixed to avoid collisions) ----------

func sourceNewRandomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
func sourceNormalizeKind(s string) SourceKind {
	return SourceKind(strings.ToLower(strings.TrimSpace(s)))
}
func sourceIsHexSha256(s string) bool {
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
func sourceValidateOpaqueToken(label, s string, max int) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("canonical: %s is required", label)

	}
	if len(s) > max {
		return fmt.Errorf("%w: %s too long", ErrSourceFieldTooBig, label)

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
func sourceValidateID(id SourceID) error {
	s := strings.TrimSpace(string(id))
	if s == "" {
		return ErrSourceEmptyID

	}
	return sourceValidateOpaqueToken("source id", s, SourceMaxIDLen)
}
func sourceValidateKind(k SourceKind) error {
	s := strings.TrimSpace(string(k))
	if s == "" {
		return ErrSourceEmptyKind

	}
	if len(s) > SourceMaxKindLen {
		return fmt.Errorf("%w: kind too long", ErrSourceInvalidKind)

	}
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-'
		if !ok || (i == 0 && (r < 'a' || r > 'z')) {
			return fmt.Errorf("%w: %q", ErrSourceInvalidKind, s)

		}
	}
	return nil
}
func sourceValidateStatus(s SourceStatus) error {
	switch strings.TrimSpace(string(s)) {
	case string(SourceActive), string(SourcePaused), string(SourceError), string(SourceDisabled):
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrSourceInvalidStatus, s)

	}
}
func sourceValidateHealth(h SourceHealth) error {
	switch strings.TrimSpace(string(h)) {
	case string(HealthHealthy), string(HealthDegraded), string(HealthUnhealthy):
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrSourceInvalidHealth, h)

	}
}
func sourceValidateOutput(o SourceExpectedOutput) error {
	switch strings.TrimSpace(string(o)) {
	case string(OutEvent), string(OutMetric), string(OutEntity), string(OutArtifact), string(OutUnknown), "":
		// allow empty => treated as unknown in Normalize()
		return nil
		// default:
		return fmt.Errorf("%w: %q", ErrSourceInvalidOutput, o)

	}
}

// ---------- external_ref validation by kind ----------

// Conservative v0 allowlist:
// - api: https://
// - file: s3://, gs://, https://
// - db: postgres://, mysql://, mssql://, sqlite:// (basic sanity only)
// - stream: kafka://, nats://, mqtt:// (optional; still validated as URL)
// If you want local-only schemes, add them later via service-level policy.
var (
	sourceDBSchemes     = map[string]bool{"postgres": true, "postgresql": true, "mysql": true, "mssql": true, "sqlite": true}
	sourceAPISchemes    = map[string]bool{"https": true}
	sourceFileSchemes   = map[string]bool{"s3": true, "gs": true, "https": true}
	sourceStreamSchemes = map[string]bool{"kafka": true, "nats": true, "mqtt": true}
)
var sourceTopicRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,256}$`)

func sourceValidateExternalRef(kind SourceKind, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ErrSourceEmptyExternalRef

	}
	if len(ref) > SourceMaxExternalRefLen {
		return fmt.Errorf("%w: external_ref too long", ErrSourceFieldTooBig)

	}
	if strings.Contains(ref, "\n") || strings.Contains(ref, "\r") || strings.Contains(ref, "\t") {
		return fmt.Errorf("%w: illegal whitespace", ErrSourceInvalidExternal)

	}
	k := strings.ToLower(strings.TrimSpace(string(kind)))

	// Special case: allow stream-like "topic" shorthand if kind explicitly says "stream_topic"
	// (kept minimal; you can extend kinds later).
	if k == "stream_topic" {
		if !sourceTopicRe.MatchString(ref) {
			return fmt.Errorf("%w: invalid topic format", ErrSourceInvalidExternal)

		}
		return nil

	}
	u, err := url.Parse(ref)
	if err != nil || strings.TrimSpace(u.Scheme) == "" {
		return fmt.Errorf("%w: must be a URI", ErrSourceInvalidExternal)

	}
	scheme := strings.ToLower(u.Scheme)
	switch k {
	case "api":
		if !sourceAPISchemes[scheme] {
			return fmt.Errorf("%w: scheme %q not allowed for api", ErrSourceInvalidExternal, scheme)

		}
		if strings.TrimSpace(u.Host) == "" {
			return fmt.Errorf("%w: api requires host", ErrSourceInvalidExternal)

		}
		if strings.TrimSpace(u.Path) == "" {
			// allow root path "/", but not empty
			u.Path = "/"

		}
		return nil

	case "file":
		if !sourceFileSchemes[scheme] {
			return fmt.Errorf("%w: scheme %q not allowed for file", ErrSourceInvalidExternal, scheme)

		} // For s3/gs, host is bucket, path is key/prefix
		if (scheme == "s3" || scheme == "gs") && strings.TrimSpace(u.Host) == "" {
			return fmt.Errorf("%w: %s requires bucket host", ErrSourceInvalidExternal, scheme)

		}
		return nil

	case "db":
		if !sourceDBSchemes[scheme] {
			return fmt.Errorf("%w: scheme %q not allowed for db", ErrSourceInvalidExternal, scheme)

		} // Minimal sanity:
		// - sqlite: path required
		// - others: host required (or allow unix socket via path; keep simple)
		if scheme == "sqlite" {
			if strings.TrimSpace(u.Path) == "" {
				return fmt.Errorf("%w: sqlite requires path", ErrSourceInvalidExternal)

			}
			return nil

		}
		if strings.TrimSpace(u.Host) == "" && strings.TrimSpace(u.Path) == "" {
			return fmt.Errorf("%w: db requires host or path", ErrSourceInvalidExternal)

		}
		return nil

	case "stream":
		if !sourceStreamSchemes[scheme] {
			return fmt.Errorf("%w: scheme %q not allowed for stream", ErrSourceInvalidExternal, scheme)

		} // Allow hostless for some brokers? Keep strict: require host.
		if strings.TrimSpace(u.Host) == "" {
			return fmt.Errorf("%w: stream requires host", ErrSourceInvalidExternal)

		}
		return nil

	default:
		// Unknown kind: require URI, but only allow https/s3/gs by default to reduce abuse.
		if scheme != "https" && scheme != "s3" && scheme != "gs" {
			return fmt.Errorf("%w: scheme %q not allowed for unknown kind", ErrSourceInvalidExternal, scheme)

		}
		return nil

	}
}

// ---------- Normalize / Validate ----------

func (s *Source) Normalize() {
	// Core
	s.Meta.Kind = sourceNormalizeKind(string(s.Meta.Kind))
	s.Meta.ExternalRef = strings.TrimSpace(s.Meta.ExternalRef)
	s.Meta.Name = strings.TrimSpace(s.Meta.Name)
	s.Meta.Status = SourceStatus(strings.ToLower(strings.TrimSpace(string(s.Meta.Status))))

	// Times
	if !s.Meta.Created.IsZero() {
		s.Meta.Created = s.Meta.Created.UTC()

	}
	if !s.Meta.Updated.IsZero() {
		s.Meta.Updated = s.Meta.Updated.UTC()

	} // Ownership
	s.Ownership.OwnerID = strings.TrimSpace(s.Ownership.OwnerID)
	s.Ownership.Team = strings.TrimSpace(s.Ownership.Team)
	s.Ownership.OnCallRotation = strings.TrimSpace(s.Ownership.OnCallRotation)

	// Limits
	// Keep as-is; validated later.

	// Schema
	if strings.TrimSpace(string(s.Schema.ExpectedOutputKind)) == "" {
		s.Schema.ExpectedOutputKind = OutUnknown
	} else {
		s.Schema.ExpectedOutputKind = SourceExpectedOutput(strings.ToLower(strings.TrimSpace(string(s.Schema.ExpectedOutputKind))))

	}
	s.Schema.SchemaRef = strings.TrimSpace(s.Schema.SchemaRef)
	s.Schema.SchemaHash = strings.TrimSpace(strings.ToLower(s.Schema.SchemaHash))

	// Health
	if strings.TrimSpace(string(s.Health.HealthStatus)) == "" {
		s.Health.HealthStatus = HealthHealthy
	} else {
		s.Health.HealthStatus = SourceHealth(strings.ToLower(strings.TrimSpace(string(s.Health.HealthStatus))))

	}
	if s.Health.ConsecutiveFailures < 0 {
		s.Health.ConsecutiveFailures = 0

	}
	s.Health.LastError = strings.TrimSpace(s.Health.LastError)
	if len(s.Health.LastError) > SourceMaxErrorLen {
		s.Health.LastError = s.Health.LastError[:SourceMaxErrorLen]

	}
	s.Health.LastErrorAt = s.Health.LastErrorAt.UTC()
	s.Health.LastSuccessAt = s.Health.LastSuccessAt.UTC()
	s.Health.LastSeenAt = s.Health.LastSeenAt.UTC()

	// Labels normalization
	if s.Labels != nil {
		clean := make(map[string]string, len(s.Labels))
		for k, v := range s.Labels {
			k2 := strings.ToLower(strings.TrimSpace(k))
			if k2 == "" || len(k2) > SourceMaxLabelKeyLen {
				continue

			}
			v2 := strings.TrimSpace(v)
			if len(v2) > SourceMaxLabelValLen {
				v2 = v2[:SourceMaxLabelValLen]

			}
			clean[k2] = v2

		}
		if len(clean) == 0 {
			s.Labels = nil
		} else {
			s.Labels = clean

		}

	} // RelatedEntities: keep bounded; sorting is applied in MetadataBytes().
	// Notes
	s.Notes = strings.TrimSpace(s.Notes)
	if len(s.Notes) > SourceMaxNotesLen {
		s.Notes = s.Notes[:SourceMaxNotesLen]

	} // Hash fields
	s.PrevHash = strings.TrimSpace(strings.ToLower(s.PrevHash))
	s.Hash = strings.TrimSpace(strings.ToLower(s.Hash))

	// Config audit
	s.ConfigAudit.ConfigHash = strings.TrimSpace(strings.ToLower(s.ConfigAudit.ConfigHash))
	s.ConfigAudit.LastModifiedBy = strings.TrimSpace(s.ConfigAudit.LastModifiedBy)
	if !s.ConfigAudit.LastModifiedAt.IsZero() {
		s.ConfigAudit.LastModifiedAt = s.ConfigAudit.LastModifiedAt.UTC()

	}
}
func (s Source) Validate() error {
	if err := sourceValidateID(s.Meta.ID); err != nil {
		return err

	}
	if err := ValidateTenantID(s.Meta.Tenant); err != nil {
		return err

	}
	if err := sourceValidateKind(s.Meta.Kind); err != nil {
		return err

	}
	if err := sourceValidateExternalRef(s.Meta.Kind, s.Meta.ExternalRef); err != nil {
		return err

	}
	if err := sourceValidateStatus(s.Meta.Status); err != nil {
		return err

	}
	if s.Meta.Created.IsZero() || s.Meta.Updated.IsZero() {
		return errors.New("canonical: created and updated times are required")

	}
	if s.Meta.Updated.Before(s.Meta.Created) {
		return errors.New("canonical: updated cannot be before created")

	}
	if len(s.Meta.Name) > SourceMaxNameLen {
		return fmt.Errorf("%w: name too long", ErrSourceFieldTooBig)

	} // Subject (optional)
	if s.Subject != nil {
		if err := s.Subject.Validate(); err != nil {
			return err

		}

	} // Ownership (optional)
	if len(s.Ownership.OwnerID) > SourceMaxOwnerIDLen {
		return fmt.Errorf("%w: owner_id too long", ErrSourceFieldTooBig)

	}
	if len(s.Ownership.Team) > SourceMaxTeamLen {
		return fmt.Errorf("%w: team too long", ErrSourceFieldTooBig)

	}
	if len(s.Ownership.OnCallRotation) > SourceMaxOnCallLen {
		return fmt.Errorf("%w: oncall_rotation too long", ErrSourceFieldTooBig)

	} // Limits
	if s.Limits.RateLimitRPS < 0 || s.Limits.MaxConcurrency < 0 || s.Limits.DailyQuota < 0 {
		return ErrSourceInvalidLimits

	}
	if s.Limits.RateLimitRPS > SourceMaxRPS || s.Limits.MaxConcurrency > SourceMaxConcurrency || s.Limits.DailyQuota > SourceMaxDailyQuota {
		return ErrSourceInvalidLimits

	} // Schema
	if err := sourceValidateOutput(s.Schema.ExpectedOutputKind); err != nil {
		return err

	}
	if len(s.Schema.SchemaRef) > SourceMaxSchemaRefLen {
		return fmt.Errorf("%w: schema_ref too long", ErrSourceFieldTooBig)

	}
	if s.Schema.SchemaHash != "" && !sourceIsHexSha256(s.Schema.SchemaHash) {
		return ErrSourceInvalidSchema

	} // Health
	if err := sourceValidateHealth(s.Health.HealthStatus); err != nil {
		return err

	}
	if s.Health.ConsecutiveFailures > 1_000_000 {
		return ErrSourceInvalidLimits

	} // Labels bounded
	if s.Labels != nil && len(s.Labels) > SourceMaxLabels {
		return ErrSourceTooManyLabels

	} // RelatedEntities bounded
	if len(s.RelatedEntities) > SourceMaxRelatedEntities {
		return ErrSourceTooManyRefs

	}
	for i := range s.RelatedEntities {
		if err := s.RelatedEntities[i].Validate(); err != nil {
			return fmt.Errorf("canonical: related_entities[%d]: %w", i, err)

		}

	} // Notes size already trimmed in Normalize, but enforce too.
	if len(s.Notes) > SourceMaxNotesLen {
		return fmt.Errorf("%w: notes too long", ErrSourceFieldTooBig)

	} // Config audit
	if s.ConfigAudit.ConfigVersion < 0 {
		return ErrSourceInvalidConfig

	}
	if s.ConfigAudit.ConfigHash != "" && !sourceIsHexSha256(s.ConfigAudit.ConfigHash) {
		return ErrSourceInvalidConfig

	}
	if len(s.ConfigAudit.ConfigHash) > 0 && len(s.ConfigAudit.ConfigHash) != SourceMaxConfigHashLen {
		return ErrSourceInvalidConfig

	}
	if len(s.ConfigAudit.LastModifiedBy) > SourceMaxOwnerIDLen {
		return ErrSourceInvalidConfig

	} // Hash fields
	if s.PrevHash != "" && !sourceIsHexSha256(s.PrevHash) {
		return fmt.Errorf("%w: prev_hash", ErrSourceInvalidHash)

	}
	if s.Hash != "" && !sourceIsHexSha256(s.Hash) {
		return fmt.Errorf("%w: hash", ErrSourceInvalidHash)

	} // Extra security: validate IP if present in labels (common footgun)
	// (No hard requirement, just prevent obviously invalid IP strings if used as label.)
	if s.Labels != nil {
		if ip, ok := s.Labels["ip"]; ok && strings.TrimSpace(ip) != "" {
			if net.ParseIP(strings.TrimSpace(ip)) == nil {
				return fmt.Errorf("canonical: labels.ip is not a valid IP")

			}
		}

	}
	return nil
}

// NewSourceHybrid constructs a Source with optional overrides.
// If id == "" => generated. If now.IsZero() => time.Now().UTC().
func NewSourceHybrid(tenant TenantID, kind string, externalRef string, id SourceID, now time.Time) (Source, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()

	}
	if strings.TrimSpace(string(id)) == "" {
		id = SourceID(sourceNewRandomID())

	}
	s := Source{
		Meta: SourceMeta{
			ID:          id,
			Tenant:      tenant,
			Kind:        sourceNormalizeKind(kind),
			ExternalRef: strings.TrimSpace(externalRef),
			Created:     now,
			Updated:     now,
			Status:      SourceActive,
		},
		Schema: SourceSchema{
			ExpectedOutputKind: OutUnknown,
		},
		Health: SourceHealthState{
			HealthStatus: HealthHealthy,
		},
	}
	s.Normalize()
	if err := s.Validate(); err != nil {
		return Source{}, err

	}
	return s, nil
}

// IdentityBytes returns deterministic bytes for dedupe/discovery.
// Same tenant + kind + external_ref => same identity.
func (s Source) IdentityBytes() ([]byte, error) {
	s2 := s
	s2.Normalize()
	if err := s2.Validate(); err != nil {
		return nil, err

	}
	canon := struct {
		Tenant      TenantID   `json:"tenant"`
		Kind        SourceKind `json:"kind"`
		ExternalRef string     `json:"external_ref"`
	}{
		Tenant:      s2.Meta.Tenant,
		Kind:        s2.Meta.Kind,
		ExternalRef: s2.Meta.ExternalRef,
	}
	return json.Marshal(canon)
}

// MetadataBytes returns deterministic bytes for tamper-evident hashing.
// Includes mutable fields + PrevHash; excludes Hash.
func (s Source) MetadataBytes() ([]byte, error) {
	s2 := s
	s2.Normalize()
	if err := s2.Validate(); err != nil {
		return nil, err

	}
	labels := s2.Labels
	if labels != nil {
		keys := make([]string, 0, len(labels))
		for k := range labels {
			keys = append(keys, k)

		}
		sort.Strings(keys)
		ordered := make(map[string]string, len(labels))
		for _, k := range keys {
			ordered[k] = labels[k]

		}
		labels = ordered

	} // Deterministic related entities ordering
	relEnt := append([]EntityRef(nil), s2.RelatedEntities...)
	sort.Slice(relEnt, func(i, j int) bool {
		if relEnt[i].Tenant != relEnt[j].Tenant {
			return relEnt[i].Tenant < relEnt[j].Tenant

		}
		if relEnt[i].Kind != relEnt[j].Kind {
			return relEnt[i].Kind < relEnt[j].Kind

		}
		return relEnt[i].ID < relEnt[j].ID
	})

	// Build stable payload (struct order deterministic).
	canon := struct {
		Meta struct {
			ID          SourceID     `json:"id"`
			Tenant      TenantID     `json:"tenant"`
			Kind        SourceKind   `json:"kind"`
			ExternalRef string       `json:"external_ref"`
			Name        string       `json:"name,omitempty"`
			Created     string       `json:"created"`
			Updated     string       `json:"updated"`
			Status      SourceStatus `json:"status"`
			PrevHash    string       `json:"prev_hash,omitempty"`
		} `json:"meta"`

		Subject         *EntityRef        `json:"subject,omitempty"`
		Ownership       SourceOwnership   `json:"ownership,omitempty"`
		Limits          SourceLimits      `json:"limits,omitempty"`
		Schema          SourceSchema      `json:"schema,omitempty"`
		Health          SourceHealthState `json:"health,omitempty"`
		Labels          map[string]string `json:"labels,omitempty"`
		RelatedEntities []EntityRef       `json:"related_entities,omitempty"`
		Notes           string            `json:"notes,omitempty"`
		ConfigAudit     SourceConfigAudit `json:"config_audit,omitempty"`
	}{}
	canon.Meta.ID = s2.Meta.ID
	canon.Meta.Tenant = s2.Meta.Tenant
	canon.Meta.Kind = s2.Meta.Kind
	canon.Meta.ExternalRef = s2.Meta.ExternalRef
	canon.Meta.Name = s2.Meta.Name
	canon.Meta.Created = s2.Meta.Created.UTC().Format(time.RFC3339Nano)
	canon.Meta.Updated = s2.Meta.Updated.UTC().Format(time.RFC3339Nano)
	canon.Meta.Status = s2.Meta.Status
	canon.Meta.PrevHash = s2.PrevHash

	canon.Subject = s2.Subject
	canon.Ownership = s2.Ownership
	canon.Limits = s2.Limits
	canon.Schema = s2.Schema
	canon.Health = s2.Health
	canon.Labels = labels
	if len(relEnt) > 0 {
		canon.RelatedEntities = relEnt

	}
	canon.Notes = s2.Notes
	canon.ConfigAudit = s2.ConfigAudit

	return json.Marshal(canon)
}

// ComputeHash sets PrevHash and Hash using MetadataBytes (SHA-256).
func (s *Source) ComputeHash(prevHash string) error {
	s.PrevHash = strings.TrimSpace(strings.ToLower(prevHash))
	s.Hash = ""
	s.Normalize()
	if err := s.Validate(); err != nil {
		return err

	}
	b, err := s.MetadataBytes()
	if err != nil {
		return err

	}
	sum := sha256.Sum256(b)
	s.Hash = hex.EncodeToString(sum[:])
	return nil
}

// VerifyHash recomputes metadata hash and compares.
func (s Source) VerifyHash() bool {
	h := strings.TrimSpace(strings.ToLower(s.Hash))
	if h == "" || !sourceIsHexSha256(h) {
		return false

	}
	b, err := s.MetadataBytes()
	if err != nil {
		return false

	}
	sum := sha256.Sum256(b)
	return bytes.Equal([]byte(h), []byte(hex.EncodeToString(sum[:])))
}

// PartitionKey is the primary shard key: "<tenant>/<kind>/<status>"
func (s Source) PartitionKey() (string, error) {
	s2 := s
	s2.Normalize()
	if err := s2.Validate(); err != nil {
		return "", err

	}
	return fmt.Sprintf("%s/%s/%s", s2.Meta.Tenant, s2.Meta.Kind, s2.Meta.Status), nil
}

// IndexKeys returns additional index keys for common queries.
// These are NOT storage instructions; they are deterministic strings storage can index.
func (s Source) IndexKeys() ([]string, error) {
	s2 := s
	s2.Normalize()
	if err := s2.Validate(); err != nil {
		return nil, err

	} // day buckets for activity queries (zero => "never")
	seenDay := "never"
	if !s2.Health.LastSeenAt.IsZero() {
		seenDay = s2.Health.LastSeenAt.UTC().Format("2006-01-02")

	}
	successDay := "never"
	if !s2.Health.LastSuccessAt.IsZero() {
		successDay = s2.Health.LastSuccessAt.UTC().Format("2006-01-02")

	}
	keys := []string{
		fmt.Sprintf("tenant/%s/health/%s", s2.Meta.Tenant, s2.Health.HealthStatus),
		fmt.Sprintf("tenant/%s/seen/%s", s2.Meta.Tenant, seenDay),
		fmt.Sprintf("tenant/%s/success/%s", s2.Meta.Tenant, successDay),
	} // deterministic order
	sort.Strings(keys)
	// return keys, nil
}

