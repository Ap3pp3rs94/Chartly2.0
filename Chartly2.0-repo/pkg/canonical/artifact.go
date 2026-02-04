package canonical

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Canonical Artifact Envelope (v0)
//
// Purpose:
// A stable artifact/blob contract to link storage outputs to events, entities, and cases.
//
// Critical invariants (production-safe):
// - Tenant REQUIRED.
// - URI REQUIRED (address where artifact can be retrieved).
// - Content.SHA256 REQUIRED (integrity, dedupe, content addressing).
// - Related ref arrays bounded to avoid storage/serialization bombs.
// - Hash chain (PrevHash/Hash)
// is a METADATA hash, not a content hash.
//   Content identity is Content.SHA256.
//
// Hashing split:
// - ContentHash(): returns Content.SHA256 (hash of blob bytes).
// - IdentityBytes(): stable identity bytes for "same content" comparisons.
// - MetadataBytes(): includes mutable metadata (title/notes/attrs/links)
// for tamper-evidence chaining.
// - ComputeHash(prevHash): sets PrevHash + Hash over MetadataBytes().
//
// URI validation:
// - ValidateURI()
// is strict by default: allow https://, s3://, gs:// only.
// - ValidateURIWithPolicy()
allows callers to opt-in to additional schemes (e.g., file:// for local).

type ArtifactID string
type ArtifactKind string
// URIPolicy controls which URI schemes are allowed.
// Canonical should be strict by default; callers can loosen explicitly for local-only use cases.
type URIPolicy struct {
	AllowHTTPS bool
	AllowS3    bool
	AllowGS    bool
	AllowFile  bool // only if the calling service explicitly opts-in (typically local)
}

// DefaultURIPolicy is intentionally strict.
var DefaultURIPolicy = URIPolicy{
	AllowHTTPS: true,
	AllowS3:    true,
	AllowGS:    true,
	AllowFile:  false,
}

// Limits (defense-in-depth against payload bombs)
const (
	MaxRelatedRefs    = 100
	MaxAttributes     = 64
	MaxAttrKeyLen     = 64
	MaxAttrValLen     = 256
	MaxTitleLen       = 256
	MaxNotesLen       = 4096
	MaxURILen         = 2048
	MaxContentTypeLen = 128
)
type ArtifactMeta struct {
	ID       ArtifactID   `json:"id"`
	Tenant   TenantID     `json:"tenant"`
	Kind     ArtifactKind `json:"kind"`
	Title    string       `json:"title,omitempty"`
	Created  time.Time    `json:"created"`            // UTC RFC3339Nano (artifact creation time)
Observed time.Time    `json:"observed,omitempty"` // UTC RFC3339Nano (when envelope recorded)
Producer string       `json:"producer,omitempty"` // e.g. "analytics", "storage"
	Source   string       `json:"source,omitempty"`   // e.g. "http", "batch", "import"
}

// ArtifactContent describes addressing + integrity.
type ArtifactContent struct {
	URI         string `json:"uri"`                    // required
	SHA256      string `json:"sha256"`                 // REQUIRED (hex sha256 of blob bytes)
SizeBytes   int64  `json:"size_bytes,omitempty"`   // optional
	ContentType string `json:"content_type,omitempty"` // optional (e.g. "application/pdf")
}
type Artifact struct {
	Meta    ArtifactMeta    `json:"meta"`
	Content ArtifactContent `json:"content"`

	// Optional linkage
	Subject         *EntityRef  `json:"subject,omitempty"`
	RelatedEntities []EntityRef `json:"related_entities,omitempty"`
	RelatedEvents   []EventID   `json:"related_events,omitempty"`
	RelatedCases    []CaseID    `json:"related_cases,omitempty"`

	// Low-cardinality tags (NOT access control)
Attributes map[string]string `json:"attributes,omitempty"`

	// Optional tamper-evidence chain (metadata hash)
PrevHash string `json:"prev_hash,omitempty"` // hex sha256
	Hash     string `json:"hash,omitempty"`      // hex sha256

	Notes string `json:"notes,omitempty"`
}

var (
	ErrEmptyArtifactID   = errors.New("canonical: artifact id is required")
ErrEmptyArtifactKind = errors.New("canonical: artifact kind is required")
ErrEmptyArtifactURI  = errors.New("canonical: artifact uri is required")
ErrEmptyArtifactSHA  = errors.New("canonical: artifact sha256 is required")
ErrInvalidArtifactID   = errors.New("canonical: invalid artifact id")
ErrInvalidArtifactKind = errors.New("canonical: invalid artifact kind")
ErrInvalidArtifactURI  = errors.New("canonical: invalid artifact uri")
ErrInvalidArtifactHash = errors.New("canonical: invalid artifact hash (expected hex sha256)")
ErrInvalidArtifactSHA  = errors.New("canonical: invalid artifact sha256 (expected hex sha256)")
ErrTooManyRelatedRefs  = errors.New("canonical: too many related references")
ErrTooManyAttributes   = errors.New("canonical: too many attributes")
ErrArtifactFieldTooBig = errors.New("canonical: artifact field exceeds max size")
)
func NewRandomArtifactID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
return hex.EncodeToString(b[:])
}
func NormalizeArtifactKind(s string) ArtifactKind {
	return ArtifactKind(strings.ToLower(strings.TrimSpace(s)))
}
func validateArtifactID(id ArtifactID) error {
	s := strings.TrimSpace(string(id))
if s == "" {
		return ErrEmptyArtifactID

	} // Opaque token: [A-Za-z0-9][A-Za-z0-9._-]{0,127}
	for i, r := range s {
		ok := (r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.'
		if !ok || (i == 0 && (r == '_' || r == '-' || r == '.')) {
			return fmt.Errorf("%w: %q", ErrInvalidArtifactID, s)

		}
	}
	if len(s) > 128 {
		return fmt.Errorf("%w (too long): %q", ErrInvalidArtifactID, s)

	}
	return nil
}
func validateArtifactKind(k ArtifactKind) error {
	s := strings.TrimSpace(string(k))
if s == "" {
		return ErrEmptyArtifactKind

	}
	if len(s) > 96 {
		return fmt.Errorf("%w (too long): %q", ErrInvalidArtifactKind, s)

	}
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-'
		if !ok || (i == 0 && (r < 'a' || r > 'z')) {
			return fmt.Errorf("%w: %q", ErrInvalidArtifactKind, s)

		}
	}
	return nil
}
func isHexSha256(s string) bool {
	if s == "" {
		return false

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
func ValidateURI(raw string) error {
	return ValidateURIWithPolicy(raw, DefaultURIPolicy)
}
func ValidateURIWithPolicy(raw string, policy URIPolicy) error {
	raw = strings.TrimSpace(raw)
if raw == "" {
		return ErrEmptyArtifactURI

	}
	if len(raw) > MaxURILen {
		return fmt.Errorf("%w: %v (uri too long)", ErrArtifactFieldTooBig, ErrInvalidArtifactURI)

	}
	u, err := url.Parse(raw)
if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArtifactURI, err)

	}
	if strings.TrimSpace(u.Scheme) == "" {
		return fmt.Errorf("%w: missing scheme", ErrInvalidArtifactURI)

	}
	scheme := strings.ToLower(u.Scheme)
allowed := false
	switch scheme {
	case "https":
		allowed = policy.AllowHTTPS
	case "s3":
		allowed = policy.AllowS3
	case "gs":
		allowed = policy.AllowGS
	case "file":
		allowed = policy.AllowFile
	default:
		allowed = false

	}
	if !allowed {
		return fmt.Errorf("%w: scheme %q not allowed", ErrInvalidArtifactURI, scheme)

	} // Require some path-like component. For s3/gs, host is typically bucket; for https, host required.
	if scheme == "https" && strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("%w: https requires host", ErrInvalidArtifactURI)

	}
	if scheme != "file" && strings.TrimSpace(u.Path) == "" {
		return fmt.Errorf("%w: missing path", ErrInvalidArtifactURI)

	} // Disallow obvious footguns in the URI string (defense-in-depth)
if strings.Contains(raw, "\n") || strings.Contains(raw, "\r") || strings.Contains(raw, "\t") {
		return fmt.Errorf("%w: illegal whitespace", ErrInvalidArtifactURI)

	}
	return nil
}
func (a *Artifact) Normalize() {
	a.Meta.Kind = NormalizeArtifactKind(string(a.Meta.Kind))
a.Meta.Title = strings.TrimSpace(a.Meta.Title)
a.Meta.Producer = strings.TrimSpace(a.Meta.Producer)
a.Meta.Source = strings.TrimSpace(a.Meta.Source)
if !a.Meta.Created.IsZero() {
		a.Meta.Created = a.Meta.Created.UTC()

	}
	if !a.Meta.Observed.IsZero() {
		a.Meta.Observed = a.Meta.Observed.UTC()

	}
	a.Content.URI = strings.TrimSpace(a.Content.URI)
a.Content.SHA256 = strings.TrimSpace(strings.ToLower(a.Content.SHA256))
a.Content.ContentType = strings.TrimSpace(a.Content.ContentType)
a.PrevHash = strings.TrimSpace(strings.ToLower(a.PrevHash))
a.Hash = strings.TrimSpace(strings.ToLower(a.Hash))
if a.Attributes != nil {
		clean := make(map[string]string, len(a.Attributes))
for k, v := range a.Attributes {
			k2 := strings.TrimSpace(strings.ToLower(k))
if k2 == "" {
				continue

			}
			if len(k2) > MaxAttrKeyLen {
				// drop oversize keys deterministically
				// continue

			}
			v2 := strings.TrimSpace(v)
if len(v2) > MaxAttrValLen {
				v2 = v2[:MaxAttrValLen]

			}
			clean[k2] = v2

		}
		if len(clean) == 0 {
			a.Attributes = nil
		} else {
			a.Attributes = clean

		}

	}
	a.Notes = strings.TrimSpace(a.Notes)
}
func (a Artifact) Validate() error {
	if err := validateArtifactID(a.Meta.ID); err != nil {
		return err

	}
	if err := ValidateTenantID(a.Meta.Tenant); err != nil {
		return err

	}
	if err := validateArtifactKind(a.Meta.Kind); err != nil {
		return err

	}
	if a.Meta.Created.IsZero() {
		return errors.New("canonical: artifact created time is required")

	}
	if len(a.Meta.Title) > MaxTitleLen {
		return fmt.Errorf("%w: title too long", ErrArtifactFieldTooBig)

	}
	if len(a.Notes) > MaxNotesLen {
		return fmt.Errorf("%w: notes too long", ErrArtifactFieldTooBig)

	}
	if len(a.Content.ContentType) > MaxContentTypeLen {
		return fmt.Errorf("%w: content_type too long", ErrArtifactFieldTooBig)

	} // STRICT: URI must pass default strict policy unless caller explicitly uses ValidateURIWithPolicy.
	if err := ValidateURI(a.Content.URI); err != nil {
		return err

	} // STRICT: SHA256 REQUIRED always (integrity + dedupe + content addressing)
if strings.TrimSpace(a.Content.SHA256) == "" {
		return ErrEmptyArtifactSHA

	}
	if !isHexSha256(a.Content.SHA256) {
		return ErrInvalidArtifactSHA

	}
	if a.Content.SizeBytes < 0 {
		return errors.New("canonical: size_bytes cannot be negative")

	}
	if a.Subject != nil {
		if err := a.Subject.Validate(); err != nil {
			return err

		}

	}
	if len(a.RelatedEntities) > MaxRelatedRefs ||
		len(a.RelatedEvents) > MaxRelatedRefs ||
		len(a.RelatedCases) > MaxRelatedRefs {
		return ErrTooManyRelatedRefs

	}
	for i := range a.RelatedEntities {
		if err := a.RelatedEntities[i].Validate(); err != nil {
			return fmt.Errorf("canonical: related_entities[%d]: %w", i, err)

		}
	}
	for i := range a.RelatedEvents {
		if strings.TrimSpace(string(a.RelatedEvents[i])) == "" {
			return fmt.Errorf("canonical: related_events[%d]: empty", i)

		}
		if err := validateOpaqueID("event id", string(a.RelatedEvents[i])); err != nil {
			return fmt.Errorf("canonical: related_events[%d]: %w", i, err)

		}
	}
	for i := range a.RelatedCases {
		if err := validateCaseID(a.RelatedCases[i]); err != nil {
			return fmt.Errorf("canonical: related_cases[%d]: %w", i, err)

		}

	}
	if a.Attributes != nil && len(a.Attributes) > MaxAttributes {
		return ErrTooManyAttributes

	} // PrevHash/Hash are optional but must be valid if present.
	if a.PrevHash != "" && !isHexSha256(a.PrevHash) {
		return fmt.Errorf("%w: prev_hash", ErrInvalidArtifactHash)

	}
	if a.Hash != "" && !isHexSha256(a.Hash) {
		return fmt.Errorf("%w: hash", ErrInvalidArtifactHash)

	}
	return nil
}

// ContentHash returns the required content hash (sha256 of blob bytes).
func (a Artifact) ContentHash() (string, error) {
	aNorm := a
	aNorm.Normalize()
if err := aNorm.Validate(); err != nil {
		return "", err

	}
	return aNorm.Content.SHA256, nil
}

// NewArtifact constructs a normalized, validated artifact with defaults.
func NewArtifact(tenant TenantID, kind string, uri string, sha256Hex string) (Artifact, error) {
	now := time.Now().UTC()
a := Artifact{
		Meta: ArtifactMeta{
			ID:       ArtifactID(NewRandomArtifactID()),
			Tenant:   tenant,
			Kind:     NormalizeArtifactKind(kind),
			Created:  now,
			Observed: now,
		},
		Content: ArtifactContent{
			URI:    strings.TrimSpace(uri),
			SHA256: strings.TrimSpace(strings.ToLower(sha256Hex)),
		},
	}
	a.Normalize()
if err := a.Validate(); err != nil {
		return Artifact{}, err

	}
	return a, nil
}

// IdentityBytes returns deterministic bytes for "same artifact content" comparisons.
// This is for content-addressing and dedupe; it intentionally excludes mutable fields
// like Title/Notes/Attributes and excludes Related* arrays.
func (a Artifact) IdentityBytes() ([]byte, error) {
	// Validate against strict rules first.
	aNorm := a
	aNorm.Normalize()
if err := aNorm.Validate(); err != nil {
		return nil, err

	}
	type entityAlias struct {
		Tenant TenantID   `json:"tenant"`
		Kind   EntityKind `json:"kind"`
		ID     EntityID   `json:"id"`
	}
	var subj *entityAlias
	if aNorm.Subject != nil {
		x := entityAlias{Tenant: aNorm.Subject.Tenant, Kind: aNorm.Subject.Kind, ID: aNorm.Subject.ID}
		subj = &x

	}
	canon := struct {
		Tenant TenantID     `json:"tenant"`
		Kind   ArtifactKind `json:"kind"`
		// Content-addressing fields
		SHA256      string `json:"sha256"`
		SizeBytes   int64  `json:"size_bytes,omitempty"`
		ContentType string `json:"content_type,omitempty"`
		// Optional subject identity
		Subject *entityAlias `json:"subject,omitempty"`
	}{
		Tenant:      aNorm.Meta.Tenant,
		Kind:        aNorm.Meta.Kind,
		SHA256:      aNorm.Content.SHA256,
		SizeBytes:   aNorm.Content.SizeBytes,
		ContentType: aNorm.Content.ContentType,
		Subject:     subj,
	}
	return json.Marshal(canon)
}

// MetadataBytes returns deterministic bytes for tamper-evident hashing.
// Includes mutable fields (Title/Notes/Attributes/Related refs)
// and PrevHash,
// excludes Hash itself.
func (a Artifact) MetadataBytes() ([]byte, error) {
	aNorm := a
	aNorm.Normalize()
if err := aNorm.Validate(); err != nil {
		return nil, err

	}
	type entityAlias struct {
		Tenant TenantID   `json:"tenant"`
		Kind   EntityKind `json:"kind"`
		ID     EntityID   `json:"id"`
	}
	aliasEntity := func(r EntityRef) entityAlias {
		return entityAlias{Tenant: r.Tenant, Kind: r.Kind, ID: r.ID}

	}
	var subj *entityAlias
	if aNorm.Subject != nil {
		x := aliasEntity(*aNorm.Subject)
subj = &x

	}
	relEnt := make([]entityAlias, 0, len(aNorm.RelatedEntities))
for _, r := range aNorm.RelatedEntities {
		relEnt = append(relEnt, aliasEntity(r))

	}
	sort.Slice(relEnt, func(i, j int) bool {
		if relEnt[i].Tenant != relEnt[j].Tenant {
			return relEnt[i].Tenant < relEnt[j].Tenant

		}
		if relEnt[i].Kind != relEnt[j].Kind {
			return relEnt[i].Kind < relEnt[j].Kind

		}
		return relEnt[i].ID < relEnt[j].ID
	})
relEvt := append([]EventID(nil), aNorm.RelatedEvents...)
sort.Slice(relEvt, func(i, j int) bool { return relEvt[i] < relEvt[j] })
relCase := append([]CaseID(nil), aNorm.RelatedCases...)
sort.Slice(relCase, func(i, j int) bool { return relCase[i] < relCase[j] })
attrs := aNorm.Attributes
	if attrs != nil {
		keys := make([]string, 0, len(attrs))
for k := range attrs {
			keys = append(keys, k)

		}
		sort.Strings(keys)
ordered := make(map[string]string, len(attrs))
for _, k := range keys {
			ordered[k] = attrs[k]

		}
		attrs = ordered

	}
	canon := struct {
		Meta struct {
			ID       ArtifactID   `json:"id"`
			Tenant   TenantID     `json:"tenant"`
			Kind     ArtifactKind `json:"kind"`
			Title    string       `json:"title,omitempty"`
			Created  string       `json:"created"`
			Observed string       `json:"observed,omitempty"`
			Producer string       `json:"producer,omitempty"`
			Source   string       `json:"source,omitempty"`
		} `json:"meta"`

		Content struct {
			URI         string `json:"uri"`
			SHA256      string `json:"sha256"`
			SizeBytes   int64  `json:"size_bytes,omitempty"`
			ContentType string `json:"content_type,omitempty"`
		} `json:"content"`

		Subject         *entityAlias      `json:"subject,omitempty"`
		RelatedEntities []entityAlias     `json:"related_entities,omitempty"`
		RelatedEvents   []EventID         `json:"related_events,omitempty"`
		RelatedCases    []CaseID          `json:"related_cases,omitempty"`
		Attributes      map[string]string `json:"attributes,omitempty"`
		Notes           string            `json:"notes,omitempty"`

		PrevHash string `json:"prev_hash,omitempty"`
	}{}
	canon.Meta.ID = aNorm.Meta.ID
	canon.Meta.Tenant = aNorm.Meta.Tenant
	canon.Meta.Kind = aNorm.Meta.Kind
	canon.Meta.Title = aNorm.Meta.Title
	canon.Meta.Created = aNorm.Meta.Created.UTC().Format(time.RFC3339Nano)
if !aNorm.Meta.Observed.IsZero() {
		canon.Meta.Observed = aNorm.Meta.Observed.UTC().Format(time.RFC3339Nano)

	}
	canon.Meta.Producer = aNorm.Meta.Producer
	canon.Meta.Source = aNorm.Meta.Source

	canon.Content.URI = aNorm.Content.URI
	canon.Content.SHA256 = aNorm.Content.SHA256
	canon.Content.SizeBytes = aNorm.Content.SizeBytes
	canon.Content.ContentType = aNorm.Content.ContentType

	canon.Subject = subj
	if len(relEnt) > 0 {
		canon.RelatedEntities = relEnt

	}
	if len(relEvt) > 0 {
		canon.RelatedEvents = relEvt

	}
	if len(relCase) > 0 {
		canon.RelatedCases = relCase

	}
	canon.Attributes = attrs
	canon.Notes = aNorm.Notes

	canon.PrevHash = aNorm.PrevHash

	return json.Marshal(canon)
}

// ComputeHash sets PrevHash and Hash using MetadataBytes (SHA-256).
func (a *Artifact) ComputeHash(prevHash string) error {
	a.PrevHash = strings.TrimSpace(strings.ToLower(prevHash))
a.Hash = ""
	a.Normalize()
if err := a.Validate(); err != nil {
		return err

	}
	b, err := a.MetadataBytes()
if err != nil {
		return err

	}
	sum := sha256.Sum256(b)
a.Hash = hex.EncodeToString(sum[:])
return nil
}

// VerifyHash recomputes metadata hash and compares.
func (a Artifact) VerifyHash() bool {
	h := strings.TrimSpace(strings.ToLower(a.Hash))
if h == "" || !isHexSha256(h) {
		return false

	}
	b, err := a.MetadataBytes()
if err != nil {
		return false

	}
	sum := sha256.Sum256(b)
return bytes.Equal([]byte(h), []byte(hex.EncodeToString(sum[:])))
}

// PartitionKey returns "<tenant>/<kind>/<yyyy-mm-dd>" using created day.
func (a Artifact) PartitionKey() (string, error) {
	aNorm := a
	aNorm.Normalize()
if err := aNorm.Validate(); err != nil {
		return "", err

	}
	day := aNorm.Meta.Created.UTC().Format("2006-01-02")
return fmt.Sprintf("%s/%s/%s", aNorm.Meta.Tenant, aNorm.Meta.Kind, day), nil
}

