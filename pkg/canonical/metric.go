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

// Canonical Metric Envelope (v0)
//
// Purpose:
// A stable metric format used across Chartly services for analytics ingestion and storage.
//
// Properties:
// - Multi-tenant safe: Tenant REQUIRED.
// - Prometheus-compatible semantics but JSON-first transport.
// - Optional linkage to EntityRef (subject)
// and EventID.
//
// Hashing:
// - CanonicalBytes()
// returns deterministic JSON bytes.
// - ComputeHash()
// sets Hash as SHA-256 over canonical bytes.
//
// Partitioning:
// - PartitionKey: "<tenant>/<name>/<yyyy-mm-dd>"

// type MetricName string
// type MetricType string

const (
	MetricGauge     MetricType = "gauge"
	MetricCounter   MetricType = "counter"
	MetricHistogram MetricType = "histogram"
	MetricSummary   MetricType = "summary"
)

// HistogramBucket represents a cumulative bucket (Prometheus-style).
type HistogramBucket struct {
	Le    float64 `json:"le"`    // upper bound
	Count uint64  `json:"count"` // cumulative count <= le
}

// SummaryQuantile represents a quantile estimate.
type SummaryQuantile struct {
	Q     float64 `json:"q"`     // 0..1
	Value float64 `json:"value"` // estimated value at quantile
}

// MetricValue supports either scalar or structured forms.
// Exactly one of Scalar/Histogram/Summary should be set.
type MetricValue struct {
	Scalar    *float64          `json:"scalar,omitempty"`    // gauge/counter
	Histogram []HistogramBucket `json:"histogram,omitempty"` // histogram
	Summary   []SummaryQuantile `json:"summary,omitempty"`   // summary
}

// MetricMeta carries envelope metadata.
type MetricMeta struct {
	ID       string     `json:"id,omitempty"` // optional; can be derived by storage
	Tenant   TenantID   `json:"tenant"`
	Name     MetricName `json:"name"`
	Type     MetricType `json:"type"`
	Observed time.Time  `json:"observed"` // UTC RFC3339Nano

	// Optional linkage
	Subject *EntityRef `json:"subject,omitempty"`
	EventID EventID    `json:"event_id,omitempty"`

	// Producer info
	Producer string `json:"producer,omitempty"` // e.g. "analytics", "gateway"
	Source   string `json:"source,omitempty"`   // e.g. "http", "cron"
	Unit     string `json:"unit,omitempty"`     // e.g. "ms", "bytes", "count"
}

// Metric is the full envelope.
type Metric struct {
	Meta   MetricMeta        `json:"meta"`
	Val    MetricValue       `json:"val"`
	Labels map[string]string `json:"labels,omitempty"`
	// Optional tamper-evident hash (v0 optional)
	Hash string `json:"hash,omitempty"` // hex sha256
}

var (
	ErrEmptyMetricName = errors.New("canonical: metric name is required")
	ErrInvalidMetric   = errors.New("canonical: invalid metric")
	ErrInvalidMetricTy = errors.New("canonical: invalid metric type")
	ErrInvalidMetricNm = errors.New("canonical: invalid metric name")
	ErrInvalidMetricHv = errors.New("canonical: invalid metric histogram")
	ErrInvalidMetricSv = errors.New("canonical: invalid metric summary")
	ErrInvalidMetricHs = errors.New("canonical: invalid metric hash (expected hex sha256)")
)

// NormalizeName lowercases and trims.
func NormalizeName(s string) MetricName {
	return MetricName(strings.ToLower(strings.TrimSpace(s)))
}

// ValidateMetricName enforces a safe metric name charset.
// Pattern (roughly Prometheus-ish): [a-z][a-z0-9:_-]{0,127}
func ValidateMetricName(n MetricName) error {
	s := strings.TrimSpace(string(n))
	if s == "" {
		return ErrEmptyMetricName
	}
	if len(s) > 128 {
		return fmt.Errorf("%w (too long): %q", ErrInvalidMetricNm, s)
	}
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == ':'
		if !ok || (i == 0 && (r < 'a' || r > 'z')) {
			return fmt.Errorf("%w: %q", ErrInvalidMetricNm, s)
		}
	}
	return nil
}
func ValidateMetricType(t MetricType) error {
	switch strings.TrimSpace(string(t)) {
	case string(MetricGauge), string(MetricCounter), string(MetricHistogram), string(MetricSummary):
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidMetricTy, t)
	}
}
func isHexSha256Metric(s string) bool {
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

// Normalize enforces consistent casing/UTC times and cleans labels.
func (m *Metric) Normalize() {
	m.Meta.Name = NormalizeName(string(m.Meta.Name))
	m.Meta.Producer = strings.TrimSpace(m.Meta.Producer)
	m.Meta.Source = strings.TrimSpace(m.Meta.Source)
	m.Meta.Unit = strings.TrimSpace(m.Meta.Unit)
	if !m.Meta.Observed.IsZero() {
		m.Meta.Observed = m.Meta.Observed.UTC()
	}
	if m.Labels != nil {
		clean := make(map[string]string, len(m.Labels))
		for k, v := range m.Labels {
			k2 := strings.TrimSpace(strings.ToLower(k))
			if k2 == "" {
				continue
			}
			clean[k2] = strings.TrimSpace(v)
		}
		if len(clean) == 0 {
			m.Labels = nil
		} else {
			m.Labels = clean
		}
	}
	m.Hash = strings.TrimSpace(strings.ToLower(m.Hash))
}

// Validate checks metric envelope correctness.
func (m Metric) Validate() error {
	if err := ValidateTenantID(m.Meta.Tenant); err != nil {
		return err
	}
	if err := ValidateMetricName(m.Meta.Name); err != nil {
		return err
	}
	if err := ValidateMetricType(m.Meta.Type); err != nil {
		return err
	}
	if m.Meta.Observed.IsZero() {
		return fmt.Errorf("%w: observed time is required", ErrInvalidMetric)
	}
	if m.Meta.Subject != nil {
		if err := m.Meta.Subject.Validate(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(string(m.Meta.EventID)) != "" {
		if err := validateOpaqueID("event id", string(m.Meta.EventID)); err != nil {
			return err
		}
	}
	switch m.Meta.Type {
	case MetricGauge, MetricCounter:
		if m.Val.Scalar == nil || m.Val.Histogram != nil || m.Val.Summary != nil {
			return fmt.Errorf("%w: gauge/counter require scalar only", ErrInvalidMetric)
		}
		if m.Meta.Type == MetricCounter && *m.Val.Scalar < 0 {
			return fmt.Errorf("%w: counter cannot be negative", ErrInvalidMetric)
		}
	case MetricHistogram:
		if m.Val.Scalar != nil || m.Val.Summary != nil || len(m.Val.Histogram) == 0 {
			return fmt.Errorf("%w: histogram requires histogram buckets only", ErrInvalidMetric)
		}
		if err := validateHistogram(m.Val.Histogram); err != nil {
			return err
		}
	case MetricSummary:
		if m.Val.Scalar != nil || m.Val.Histogram != nil || len(m.Val.Summary) == 0 {
			return fmt.Errorf("%w: summary requires summary quantiles only", ErrInvalidMetric)
		}
		if err := validateSummary(m.Val.Summary); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: %q", ErrInvalidMetricTy, m.Meta.Type)
	}
	if !isHexSha256Metric(strings.TrimSpace(strings.ToLower(m.Hash))) {
		return ErrInvalidMetricHs
	}
	return nil
}
func validateHistogram(b []HistogramBucket) error {
	prevLe := -1.0e308
	var prevCount uint64 = 0
	for i, bk := range b {
		if bk.Le < prevLe {
			return fmt.Errorf("%w: buckets not sorted (index %d)", ErrInvalidMetricHv, i)
		}
		if i > 0 && bk.Le == prevLe {
			return fmt.Errorf("%w: duplicate le (index %d)", ErrInvalidMetricHv, i)
		}
		if bk.Count < prevCount {
			return fmt.Errorf("%w: counts must be non-decreasing (index %d)", ErrInvalidMetricHv, i)
		}
		prevLe = bk.Le
		prevCount = bk.Count
	}
	return nil
}
func validateSummary(q []SummaryQuantile) error {
	prevQ := -1.0
	for i, sq := range q {
		if sq.Q < 0 || sq.Q > 1 {
			return fmt.Errorf("%w: quantile out of range (index %d)", ErrInvalidMetricSv, i)
		}
		if sq.Q < prevQ {
			return fmt.Errorf("%w: quantiles not sorted (index %d)", ErrInvalidMetricSv, i)
		}
		if i > 0 && sq.Q == prevQ {
			return fmt.Errorf("%w: duplicate quantile (index %d)", ErrInvalidMetricSv, i)
		}
		prevQ = sq.Q
	}
	return nil
}

// NewMetric constructs a normalized, validated scalar metric.
func NewMetric(tenant TenantID, name string, mtype MetricType, observed time.Time, scalar float64) (Metric, error) {
	if observed.IsZero() {
		return Metric{}, fmt.Errorf("%w: observed time is required", ErrInvalidMetric)
	}
	m := Metric{
		Meta: MetricMeta{
			Tenant:   tenant,
			Name:     NormalizeName(name),
			Type:     mtype,
			Observed: observed.UTC(),
		},
		Val: MetricValue{Scalar: &scalar},
	}
	m.Normalize()
	if err := m.Validate(); err != nil {
		return Metric{}, err
	}
	return m, nil
}

// CanonicalBytes returns deterministic JSON bytes for hashing.
// IMPORTANT: exclude Hash itself.
func (m Metric) CanonicalBytes() ([]byte, error) {
	type subjectAlias struct {
		Tenant TenantID   `json:"tenant"`
		Kind   EntityKind `json:"kind"`
		ID     EntityID   `json:"id"`
	}
	var subj *subjectAlias
	if m.Meta.Subject != nil {
		subj = &subjectAlias{
			Tenant: m.Meta.Subject.Tenant,
			Kind:   m.Meta.Subject.Kind,
			ID:     m.Meta.Subject.ID,
		}
	}
	labels := m.Labels
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
	}

	// Ensure deterministic bucket/quantile order by re-sorting (in case caller didn't).
	val := m.Val
	if val.Histogram != nil {
		b := append([]HistogramBucket(nil), val.Histogram...)
		sort.Slice(b, func(i, j int) bool { return b[i].Le < b[j].Le })
		val.Histogram = b
	}
	if val.Summary != nil {
		q := append([]SummaryQuantile(nil), val.Summary...)
		sort.Slice(q, func(i, j int) bool { return q[i].Q < q[j].Q })
		val.Summary = q
	}
	canon := struct {
		Meta struct {
			ID       string        `json:"id,omitempty"`
			Tenant   TenantID      `json:"tenant"`
			Name     MetricName    `json:"name"`
			Type     MetricType    `json:"type"`
			Observed string        `json:"observed"`
			Subject  *subjectAlias `json:"subject,omitempty"`
			EventID  EventID       `json:"event_id,omitempty"`
			Producer string        `json:"producer,omitempty"`
			Source   string        `json:"source,omitempty"`
			Unit     string        `json:"unit,omitempty"`
		} `json:"meta"`
		Val    MetricValue       `json:"val"`
		Labels map[string]string `json:"labels,omitempty"`
	}{}
	canon.Meta.ID = strings.TrimSpace(m.Meta.ID)
	canon.Meta.Tenant = m.Meta.Tenant
	canon.Meta.Name = m.Meta.Name
	canon.Meta.Type = m.Meta.Type
	canon.Meta.Observed = m.Meta.Observed.UTC().Format(time.RFC3339Nano)
	canon.Meta.Subject = subj
	canon.Meta.EventID = m.Meta.EventID
	canon.Meta.Producer = m.Meta.Producer
	canon.Meta.Source = m.Meta.Source
	canon.Meta.Unit = m.Meta.Unit

	canon.Val = val
	canon.Labels = labels

	return json.Marshal(canon)
}

// ComputeHash sets Hash as SHA-256 over CanonicalBytes.
func (m *Metric) ComputeHash() error {
	m.Hash = ""
	m.Normalize()
	if err := m.Validate(); err != nil {
		return err
	}
	b, err := m.CanonicalBytes()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	m.Hash = hex.EncodeToString(sum[:])
	// return nil
}

// VerifyHash recomputes hash and compares.
func (m Metric) VerifyHash() bool {
	if !isHexSha256Metric(strings.TrimSpace(strings.ToLower(m.Hash))) || m.Hash == "" {
		return false
	}
	b, err := m.CanonicalBytes()
	if err != nil {
		return false
	}
	sum := sha256.Sum256(b)
	return strings.EqualFold(m.Hash, hex.EncodeToString(sum[:]))
}

// PartitionKey returns "<tenant>/<name>/<yyyy-mm-dd>".
func (m Metric) PartitionKey() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	day := m.Meta.Observed.UTC().Format("2006-01-02")
	return fmt.Sprintf("%s/%s/%s", m.Meta.Tenant, m.Meta.Name, day), nil
}
