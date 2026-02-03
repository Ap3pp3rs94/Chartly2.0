package canonical

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Canonical Case Envelope (v0)
//
// Purpose:
// A stable case/incident/audit-review object used across Chartly services.
//
// This is intentionally service-agnostic:
// - No storage decisions here.
// - No workflow engine dependencies.
// - Just a strict contract that can be routed, stored, audited, and later synced to an external system.
//
// Tamper-evidence:
// - PrevHash + Hash support chaining similar to canonical events.
// - Hash excludes itself, includes PrevHash, and is computed over CanonicalBytes().
//
// String form conventions:
// - CaseID is opaque and validated similarly to EventID/EntityID.
// - CaseType normalized lower-case, safe charset.
// - Tenant REQUIRED.

// type CaseID string
// type CaseType string

// type CaseSeverity string
// type CasePriority string
// type CaseStatus string

const (
	SeverityLow      CaseSeverity = "low"
	SeverityMedium   CaseSeverity = "medium"
	SeverityHigh     CaseSeverity = "high"
	SeverityCritical CaseSeverity = "critical"

	PriorityP3 CasePriority = "p3"
	PriorityP2 CasePriority = "p2"
	PriorityP1 CasePriority = "p1"
	PriorityP0 CasePriority = "p0"

	StatusOpen        CaseStatus = "open"
	StatusInvestigate CaseStatus = "investigate"
	StatusMitigated   CaseStatus = "mitigated"
	StatusResolved    CaseStatus = "resolved"
	StatusClosed      CaseStatus = "closed"
	StatusRejected    CaseStatus = "rejected"
)

// type EvidenceKind string

const (
	EvidenceEvent  EvidenceKind = "event"
	EvidenceEntity EvidenceKind = "entity"
	EvidenceURI    EvidenceKind = "uri"
	EvidenceNote   EvidenceKind = "note"
)

// CaseEvidence is a small, portable evidence record.
// It can reference an event/entity, a URI (e.g. object store), an optional sha256 for integrity,
// and an optional note.
type CaseEvidence struct {
	Kind   EvidenceKind `json:"kind"`
	Event  EventID      `json:"event_id,omitempty"`
	Entity *EntityRef   `json:"entity,omitempty"`
	URI    string       `json:"uri,omitempty"`
	SHA256 string       `json:"sha256,omitempty"` // hex sha256 of referenced blob (optional)
	Note   string       `json:"note,omitempty"`
}

// CaseAssignment holds assignee metadata without binding to any IAM system.
// Assignee could be a username, email, or service account string.
type CaseAssignment struct {
	Assignee string `json:"assignee,omitempty"`
	Team     string `json:"team,omitempty"`
}

// CaseMeta is the envelope metadata.
// Keep stable; add new fields carefully.
type CaseMeta struct {
	ID       CaseID       `json:"id"`
	Tenant   TenantID     `json:"tenant"`
	Type     CaseType     `json:"type"`
	Title    string       `json:"title"`
	Status   CaseStatus   `json:"status"`
	Severity CaseSeverity `json:"severity"`
	Priority CasePriority `json:"priority"`

	Created time.Time `json:"created"` // UTC RFC3339Nano
	Updated time.Time `json:"updated"` // UTC RFC3339Nano

	Opened *time.Time `json:"opened,omitempty"` // when it moved into active workflow
	Closed *time.Time `json:"closed,omitempty"` // when terminal

	Assignment CaseAssignment `json:"assignment,omitempty"`

	// Provenance
	Producer string `json:"producer,omitempty"` // e.g. "audit", "observer", "analytics"
	Source   string `json:"source,omitempty"`   // e.g. "rule", "manual", "import"

	// Optional tamper-evidence chain
	PrevHash string `json:"prev_hash,omitempty"` // hex sha256
	Hash     string `json:"hash,omitempty"`      // hex sha256
}

// Case is the full case object.
type Case struct {
	Meta CaseMeta `json:"meta"`

	// Primary subject this case is about (recommended).
	Subject *EntityRef `json:"subject,omitempty"`

	// Related links (optional)
	RelatedEntities []EntityRef  `json:"related_entities,omitempty"`
	RelatedEvents   []EventID    `json:"related_events,omitempty"`
	RelatedMetrics  []MetricName `json:"related_metrics,omitempty"`

	// Evidence records (optional)
	Evidence []CaseEvidence `json:"evidence,omitempty"`

	// Attributes are low-cardinality tags (NOT access control).
	Attributes map[string]string `json:"attributes,omitempty"`

	// Freeform narrative (optional)
	Summary string `json:"summary,omitempty"`
}

// NormalizeType lowercases and trims.
func NormalizeCaseType(s string) CaseType {
	return CaseType(strings.ToLower(strings.TrimSpace(s)))
}

var (
	ErrEmptyCaseID     = errors.New("canonical: case id is required")
	ErrEmptyCaseType   = errors.New("canonical: case type is required")
	ErrEmptyCaseTitl   = errors.New("canonical: case title is required")
	ErrInvalidCaseID   = errors.New("canonical: invalid case id")
	ErrInvalidCaseTy   = errors.New("canonical: invalid case type")
	ErrInvalidSeverity = errors.New("canonical: invalid case severity")
	ErrInvalidPriority = errors.New("canonical: invalid case priority")
	ErrInvalidStatus   = errors.New("canonical: invalid case status")
	ErrInvalidCaseHash = errors.New("canonical: invalid case hash (expected hex sha256)")
	ErrInvalidEvidence = errors.New("canonical: invalid case evidence")
)

func NewRandomCaseID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
func validateCaseID(id CaseID) error {
	s := strings.TrimSpace(string(id))
	if s == "" {
		return ErrEmptyCaseID
	}
	// Opaque token: [A-Za-z0-9][A-Za-z0-9._-]{0,127}
	if len(s) > 128 {
		return fmt.Errorf("%w (too long): %q", ErrInvalidCaseID, s)
	}
	for i, r := range s {
		ok := (r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.'
		if !ok || (i == 0 && (r == '_' || r == '-' || r == '.')) {
			return fmt.Errorf("%w: %q", ErrInvalidCaseID, s)
		}
	}
	return nil
}
func validateCaseType(t CaseType) error {
	s := strings.TrimSpace(string(t))
	if s == "" {
		return ErrEmptyCaseType
	}
	if len(s) > 96 {
		return fmt.Errorf("%w (too long): %q", ErrInvalidCaseTy, s)
	}
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-'
		if !ok || (i == 0 && (r < 'a' || r > 'z')) {
			return fmt.Errorf("%w: %q", ErrInvalidCaseTy, s)
		}
	}
	return nil
}
func validateSeverity(s CaseSeverity) error {
	switch strings.TrimSpace(string(s)) {
	case string(SeverityLow), string(SeverityMedium), string(SeverityHigh), string(SeverityCritical):
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidSeverity, s)
	}
}
func validatePriority(p CasePriority) error {
	switch strings.TrimSpace(string(p)) {
	case string(PriorityP3), string(PriorityP2), string(PriorityP1), string(PriorityP0):
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidPriority, p)
	}
}
func validateStatus(st CaseStatus) error {
	switch strings.TrimSpace(string(st)) {
	case string(StatusOpen), string(StatusInvestigate), string(StatusMitigated), string(StatusResolved), string(StatusClosed), string(StatusRejected):
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidStatus, st)
	}
}
func isHexSha256Case(s string) bool {
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

// Normalize enforces consistent casing/UTC times and cleans maps/slices.
func (c *Case) Normalize() {
	c.Meta.Type = NormalizeCaseType(string(c.Meta.Type))
	c.Meta.Title = strings.TrimSpace(c.Meta.Title)
	c.Meta.Producer = strings.TrimSpace(c.Meta.Producer)
	c.Meta.Source = strings.TrimSpace(c.Meta.Source)
	if !c.Meta.Created.IsZero() {
		c.Meta.Created = c.Meta.Created.UTC()
	}
	if !c.Meta.Updated.IsZero() {
		c.Meta.Updated = c.Meta.Updated.UTC()
	}
	if c.Meta.Opened != nil {
		t := c.Meta.Opened.UTC()
		c.Meta.Opened = &t
	}
	if c.Meta.Closed != nil {
		t := c.Meta.Closed.UTC()
		c.Meta.Closed = &t
	}
	c.Meta.PrevHash = strings.TrimSpace(strings.ToLower(c.Meta.PrevHash))
	c.Meta.Hash = strings.TrimSpace(strings.ToLower(c.Meta.Hash))
	c.Meta.Assignment.Assignee = strings.TrimSpace(c.Meta.Assignment.Assignee)
	c.Meta.Assignment.Team = strings.TrimSpace(c.Meta.Assignment.Team)
	if c.Attributes != nil {
		clean := make(map[string]string, len(c.Attributes))
		for k, v := range c.Attributes {
			k2 := strings.TrimSpace(strings.ToLower(k))
			if k2 == "" {
				continue
			}
			clean[k2] = strings.TrimSpace(v)
		}
		if len(clean) == 0 {
			c.Attributes = nil
		} else {
			c.Attributes = clean
		}
	}
	c.Summary = strings.TrimSpace(c.Summary)
}

// Validate checks case contract correctness.
func (c Case) Validate() error {
	if err := validateCaseID(c.Meta.ID); err != nil {
		return err
	}
	if err := ValidateTenantID(c.Meta.Tenant); err != nil {
		return err
	}
	if err := validateCaseType(c.Meta.Type); err != nil {
		return err
	}
	if strings.TrimSpace(c.Meta.Title) == "" {
		return ErrEmptyCaseTitl
	}
	if err := validateStatus(c.Meta.Status); err != nil {
		return err
	}
	if err := validateSeverity(c.Meta.Severity); err != nil {
		return err
	}
	if err := validatePriority(c.Meta.Priority); err != nil {
		return err
	}
	if c.Meta.Created.IsZero() || c.Meta.Updated.IsZero() {
		return errors.New("canonical: created and updated times are required")
	}
	if c.Meta.Updated.Before(c.Meta.Created) {
		return errors.New("canonical: updated cannot be before created")
	}
	if c.Meta.Closed != nil && c.Meta.Opened != nil && c.Meta.Closed.Before(*c.Meta.Opened) {
		return errors.New("canonical: closed cannot be before opened")
	}
	if c.Subject != nil {
		if err := c.Subject.Validate(); err != nil {
			return err
		}
	}
	for i := range c.RelatedEntities {
		if err := c.RelatedEntities[i].Validate(); err != nil {
			return fmt.Errorf("canonical: related_entities[%d]: %w", i, err)
		}
	}
	for i := range c.RelatedEvents {
		if strings.TrimSpace(string(c.RelatedEvents[i])) == "" {
			return fmt.Errorf("canonical: related_events[%d]: empty", i)
		}
		if err := validateOpaqueID("event id", string(c.RelatedEvents[i])); err != nil {
			return fmt.Errorf("canonical: related_events[%d]: %w", i, err)
		}
	}
	for i := range c.RelatedMetrics {
		if err := ValidateMetricName(c.RelatedMetrics[i]); err != nil {
			return fmt.Errorf("canonical: related_metrics[%d]: %w", i, err)
		}
	}
	for i := range c.Evidence {
		if err := validateEvidence(c.Evidence[i]); err != nil {
			return fmt.Errorf("canonical: evidence[%d]: %w", i, err)
		}
	}
	if !isHexSha256Case(c.Meta.PrevHash) {
		return fmt.Errorf("%w: prev_hash", ErrInvalidCaseHash)
	}
	if !isHexSha256Case(c.Meta.Hash) {
		return fmt.Errorf("%w: hash", ErrInvalidCaseHash)
	}
	return nil
}
func validateEvidence(ev CaseEvidence) error {
	kind := strings.TrimSpace(string(ev.Kind))
	switch kind {
	case string(EvidenceEvent), string(EvidenceEntity), string(EvidenceURI), string(EvidenceNote):
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidEvidence, ev.Kind)
	}
	sh := strings.TrimSpace(strings.ToLower(ev.SHA256))
	if sh != "" && !isHexSha256Case(sh) {
		return fmt.Errorf("%w: invalid sha256", ErrInvalidEvidence)
	}
	switch EvidenceKind(kind) {
	case EvidenceEvent:
		if strings.TrimSpace(string(ev.Event)) == "" {
			return fmt.Errorf("%w: event evidence requires event_id", ErrInvalidEvidence)
		}
		if err := validateOpaqueID("event id", string(ev.Event)); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidEvidence, err)
		}
	case EvidenceEntity:
		if ev.Entity == nil {
			return fmt.Errorf("%w: entity evidence requires entity", ErrInvalidEvidence)
		}
		if err := ev.Entity.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidEvidence, err)
		}
	case EvidenceURI:
		if strings.TrimSpace(ev.URI) == "" {
			return fmt.Errorf("%w: uri evidence requires uri", ErrInvalidEvidence)
		}
	case EvidenceNote:
		if strings.TrimSpace(ev.Note) == "" {
			return fmt.Errorf("%w: note evidence requires note", ErrInvalidEvidence)
		}
	}
	return nil
}

// NewCase constructs a normalized, validated case with deterministic defaults.
// created/updated default to Unix epoch if zero (callers should supply real timestamps).
func NewCase(tenant TenantID, caseType string, title string, created time.Time, updated time.Time) (Case, error) {
	if created.IsZero() {
		created = time.Unix(0, 0).UTC()
	}
	if updated.IsZero() {
		updated = created
	}
	c := Case{
		Meta: CaseMeta{
			ID:       CaseID(NewRandomCaseID()),
			Tenant:   tenant,
			Type:     NormalizeCaseType(caseType),
			Title:    strings.TrimSpace(title),
			Status:   StatusOpen,
			Severity: SeverityLow,
			Priority: PriorityP3,
			Created:  created.UTC(),
			Updated:  updated.UTC(),
		},
	}
	c.Normalize()
	if err := c.Validate(); err != nil {
		return Case{}, err
	}
	return c, nil
}

// TransitionStatus enforces a basic lifecycle.
// Allowed transitions (v0):
// open -> investigate|rejected|closed
// investigate -> mitigated|resolved|rejected|closed
// mitigated -> investigate|resolved|closed
// resolved -> closed
// rejected -> closed
// closed -> (no transitions)
//
// now is caller-provided (no time.Now usage). If zero, Unix epoch is used.
func (c *Case) TransitionStatus(next CaseStatus, now time.Time) error {
	c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	next = CaseStatus(strings.TrimSpace(strings.ToLower(string(next))))
	if err := validateStatus(next); err != nil {
		return err
	}
	cur := c.Meta.Status
	allowed := map[CaseStatus][]CaseStatus{
		StatusOpen:        {StatusInvestigate, StatusRejected, StatusClosed},
		StatusInvestigate: {StatusMitigated, StatusResolved, StatusRejected, StatusClosed},
		StatusMitigated:   {StatusInvestigate, StatusResolved, StatusClosed},
		StatusResolved:    {StatusClosed},
		StatusRejected:    {StatusClosed},
		StatusClosed:      {},
	}
	ok := false
	for _, st := range allowed[cur] {
		if st == next {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("canonical: invalid status transition %q -> %q", cur, next)
	}
	if now.IsZero() {
		now = time.Unix(0, 0).UTC()
	}
	c.Meta.Status = next
	c.Meta.Updated = now.UTC()
	if c.Meta.Opened == nil && (next == StatusInvestigate || next == StatusMitigated || next == StatusResolved) {
		t := now.UTC()
		c.Meta.Opened = &t
	}
	if next == StatusClosed {
		t := now.UTC()
		c.Meta.Closed = &t
	}
	c.Normalize()
	return c.Validate()
}

// CanonicalBytes returns deterministic JSON bytes for hashing.
// IMPORTANT: excludes Meta.Hash (self-referential), includes PrevHash.
func (c Case) CanonicalBytes() ([]byte, error) {
	type entityAlias struct {
		Tenant TenantID   `json:"tenant"`
		Kind   EntityKind `json:"kind"`
		ID     EntityID   `json:"id"`
	}
	aliasEntity := func(r EntityRef) entityAlias {
		return entityAlias{Tenant: r.Tenant, Kind: r.Kind, ID: r.ID}
	}
	var subj *entityAlias
	if c.Subject != nil {
		a := aliasEntity(*c.Subject)
		subj = &a
	}
	relEnt := make([]entityAlias, 0, len(c.RelatedEntities))
	for _, r := range c.RelatedEntities {
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
	relEvt := append([]EventID(nil), c.RelatedEvents...)
	sort.Slice(relEvt, func(i, j int) bool { return relEvt[i] < relEvt[j] })
	relMet := append([]MetricName(nil), c.RelatedMetrics...)
	sort.Slice(relMet, func(i, j int) bool { return relMet[i] < relMet[j] })
	ev := append([]CaseEvidence(nil), c.Evidence...)
	sort.Slice(ev, func(i, j int) bool {
		if ev[i].Kind != ev[j].Kind {
			return ev[i].Kind < ev[j].Kind
		}
		if ev[i].URI != ev[j].URI {
			return ev[i].URI < ev[j].URI
		}
		if ev[i].Event != ev[j].Event {
			return ev[i].Event < ev[j].Event
		}
		ni := strings.TrimSpace(ev[i].Note)
		nj := strings.TrimSpace(ev[j].Note)
		if ni != nj {
			return ni < nj
		}
		si, sj := "", ""
		if ev[i].Entity != nil {
			si = ev[i].Entity.String()
		}
		if ev[j].Entity != nil {
			sj = ev[j].Entity.String()
		}
		return si < sj
	})
	attrs := c.Attributes
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
	type evidenceAlias struct {
		Kind   EvidenceKind `json:"kind"`
		Event  EventID      `json:"event_id,omitempty"`
		Entity *entityAlias `json:"entity,omitempty"`
		URI    string       `json:"uri,omitempty"`
		SHA256 string       `json:"sha256,omitempty"`
		Note   string       `json:"note,omitempty"`
	}
	evAlias := make([]evidenceAlias, 0, len(ev))
	for i := range ev {
		var ent *entityAlias
		if ev[i].Entity != nil {
			a := aliasEntity(*ev[i].Entity)
			ent = &a
		}
		evAlias = append(evAlias, evidenceAlias{
			Kind:   EvidenceKind(strings.TrimSpace(strings.ToLower(string(ev[i].Kind)))),
			Event:  ev[i].Event,
			Entity: ent,
			URI:    strings.TrimSpace(ev[i].URI),
			SHA256: strings.TrimSpace(strings.ToLower(ev[i].SHA256)),
			Note:   strings.TrimSpace(ev[i].Note),
		})
	}
	canon := struct {
		Meta struct {
			ID       CaseID       `json:"id"`
			Tenant   TenantID     `json:"tenant"`
			Type     CaseType     `json:"type"`
			Title    string       `json:"title"`
			Status   CaseStatus   `json:"status"`
			Severity CaseSeverity `json:"severity"`
			Priority CasePriority `json:"priority"`

			Created string  `json:"created"`
			Updated string  `json:"updated"`
			Opened  *string `json:"opened,omitempty"`
			Closed  *string `json:"closed,omitempty"`

			Assignment CaseAssignment `json:"assignment,omitempty"`
			Producer   string         `json:"producer,omitempty"`
			Source     string         `json:"source,omitempty"`

			PrevHash string `json:"prev_hash,omitempty"`
		} `json:"meta"`

		Subject         *entityAlias      `json:"subject,omitempty"`
		RelatedEntities []entityAlias     `json:"related_entities,omitempty"`
		RelatedEvents   []EventID         `json:"related_events,omitempty"`
		RelatedMetrics  []MetricName      `json:"related_metrics,omitempty"`
		Evidence        []evidenceAlias   `json:"evidence,omitempty"`
		Attributes      map[string]string `json:"attributes,omitempty"`
		Summary         string            `json:"summary,omitempty"`
	}{}
	canon.Meta.ID = c.Meta.ID
	canon.Meta.Tenant = c.Meta.Tenant
	canon.Meta.Type = c.Meta.Type
	canon.Meta.Title = strings.TrimSpace(c.Meta.Title)
	canon.Meta.Status = c.Meta.Status
	canon.Meta.Severity = c.Meta.Severity
	canon.Meta.Priority = c.Meta.Priority
	canon.Meta.Created = c.Meta.Created.UTC().Format(time.RFC3339Nano)
	canon.Meta.Updated = c.Meta.Updated.UTC().Format(time.RFC3339Nano)
	if c.Meta.Opened != nil {
		s := c.Meta.Opened.UTC().Format(time.RFC3339Nano)
		canon.Meta.Opened = &s
	}
	if c.Meta.Closed != nil {
		s := c.Meta.Closed.UTC().Format(time.RFC3339Nano)
		canon.Meta.Closed = &s
	}
	canon.Meta.Assignment = c.Meta.Assignment
	canon.Meta.Producer = strings.TrimSpace(c.Meta.Producer)
	canon.Meta.Source = strings.TrimSpace(c.Meta.Source)
	canon.Meta.PrevHash = strings.TrimSpace(strings.ToLower(c.Meta.PrevHash))
	canon.Subject = subj
	if len(relEnt) > 0 {
		canon.RelatedEntities = relEnt
	}
	if len(relEvt) > 0 {
		canon.RelatedEvents = relEvt
	}
	if len(relMet) > 0 {
		canon.RelatedMetrics = relMet
	}
	if len(evAlias) > 0 {
		canon.Evidence = evAlias
	}
	canon.Attributes = attrs
	canon.Summary = strings.TrimSpace(c.Summary)
	return json.Marshal(canon)
}

// ComputeHash sets PrevHash and Hash using CanonicalBytes (SHA-256).
func (c *Case) ComputeHash(prevHash string) error {
	c.Meta.PrevHash = strings.TrimSpace(strings.ToLower(prevHash))
	c.Meta.Hash = ""
	c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := c.CanonicalBytes()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	c.Meta.Hash = hex.EncodeToString(sum[:])
	// return nil
}

// VerifyHash recomputes hash from current PrevHash and canonical bytes and compares.
func (c Case) VerifyHash() bool {
	h := strings.TrimSpace(strings.ToLower(c.Meta.Hash))
	if h == "" || !isHexSha256Case(h) {
		return false
	}
	b, err := c.CanonicalBytes()
	if err != nil {
		return false
	}
	sum := sha256.Sum256(b)
	return bytes.Equal([]byte(h), []byte(hex.EncodeToString(sum[:])))
}

// PartitionKey returns a deterministic partition key.
// Format: "<tenant>/<type>/<status>/<yyyy-mm-dd>" using created day.
func (c Case) PartitionKey() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	day := c.Meta.Created.UTC().Format("2006-01-02")
	return fmt.Sprintf("%s/%s/%s/%s", c.Meta.Tenant, c.Meta.Type, c.Meta.Status, day), nil
}
