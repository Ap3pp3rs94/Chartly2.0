package telemetry

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "math"
    "sort"
    "strings"
)

// Metrics contracts (v0):
// - Pure interfaces + helpers only (no backend bindings).
// - Caller supplies timestamps implicitly by call ordering; no time.Now in this layer.
// - Labels are bounded and normalized to prevent cardinality bombs.

type Labels map[string]string

const (
    MaxLabelPairs  = 32
    MaxLabelKeyLen = 64
    MaxLabelValLen = 256
)

var (
    ErrInvalidLabels = errors.New("telemetry: invalid labels")
    ErrInvalidValue  = errors.New("telemetry: invalid metric value")
)

// DefaultHistogramBuckets returns a shared recommended bucket set.
// These are similar in spirit to common latency buckets (seconds)
// used in practice.
// Callers may supply custom buckets; this is a safe default to reduce drift.
func DefaultHistogramBuckets() []float64 {
    // seconds: 5ms .. 10s
    return []float64{
        0.005, 0.01, 0.025, 0.05,
        0.1, 0.25, 0.5, 1.0,
        2.5, 5.0, 10.0,
    }
}

// NormalizeLabels returns a bounded, normalized copy of labels.
// - Lowercases and trims keys
// - Trims values
// - Enforces key/value charset (ASCII)
//   to avoid downstream exporter rejections
// - Drops invalid/empty/oversize keys
// - Truncates oversize values
// - Deterministically limits to MaxLabelPairs by sorted key order
func NormalizeLabels(in Labels) (Labels, error) {
    if in == nil || len(in) == 0 {
        return nil, nil
    }
    keys := make([]string, 0, len(in))
    for k := range in {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    out := make(map[string]string, len(in))
    for _, k := range keys {
        k2 := strings.ToLower(strings.TrimSpace(k))
        if k2 == "" || len(k2) > MaxLabelKeyLen {
            continue
        }
        if !isValidLabelKey(k2) {
            return nil, fmt.Errorf("%w: invalid label key charset %q", ErrInvalidLabels, k2)
        }
        v := strings.TrimSpace(in[k])
        if len(v) > MaxLabelValLen {
            v = v[:MaxLabelValLen]
        }
        if !isValidLabelValue(v) {
            return nil, fmt.Errorf("%w: invalid label value charset for key %q", ErrInvalidLabels, k2)
        }
        out[k2] = v
        if len(out) >= MaxLabelPairs {
            break
        }
    }
    if len(out) == 0 {
        return nil, nil
    }
    if len(out) > MaxLabelPairs {
        return nil, fmt.Errorf("%w: too many labels", ErrInvalidLabels)
    }
    return Labels(out), nil
}

// Fingerprint returns a deterministic sha256 over normalized labels.
// If labels are nil/empty, returns empty string.
func (l Labels) Fingerprint() (string, error) {
    n, err := NormalizeLabels(l)
    if err != nil {
        return "", err
    }
    if n == nil || len(n) == 0 {
        return "", nil
    }
    keys := make([]string, 0, len(n))
    for k := range n {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    h := sha256.New()
    write := func(s string) {
        _, _ = h.Write([]byte(s))
        _, _ = h.Write([]byte{0})
    }
    for _, k := range keys {
        write(k)
        write(n[k])
    }
    return hex.EncodeToString(h.Sum(nil)), nil
}

// Meter is the minimal metrics interface used across Chartly services.
// Implementations may export to Prometheus, OTel, logs, etc.
type Meter interface {
    IncCounter(ctx context.Context, name string, delta int64, labels Labels) error
    SetGauge(ctx context.Context, name string, value float64, labels Labels) error
    ObserveHistogram(ctx context.Context, name string, value float64, buckets []float64, labels Labels) error
}

// NopMeter is a safe no-op meter.
type NopMeter struct{}

// NopMeterInstance is a convenience singleton.
var NopMeterInstance = NopMeter{}

func (NopMeter) IncCounter(ctx context.Context, name string, delta int64, labels Labels) error {
    return nil
}
func (NopMeter) SetGauge(ctx context.Context, name string, value float64, labels Labels) error {
    return nil
}
func (NopMeter) ObserveHistogram(ctx context.Context, name string, value float64, buckets []float64, labels Labels) error {
    return nil
}

// ValidateMetricName enforces a safe name charset.
// Pattern: [a-z][a-z0-9:_-]{0,127}
func ValidateMetricName(name string) error {
    name = strings.TrimSpace(name)
    if name == "" {
        return fmt.Errorf("%w: metric name required", ErrInvalidValue)
    }
    if len(name) > 128 {
        return fmt.Errorf("%w: metric name too long", ErrInvalidValue)
    }
    for i, r := range name {
        ok := (r >= 'a' && r <= 'z') ||
            (r >= '0' && r <= '9') ||
            r == '_' || r == '-' || r == ':'
        if !ok || (i == 0 && (r < 'a' || r > 'z')) {
            return fmt.Errorf("%w: invalid metric name %q", ErrInvalidValue, name)
        }
    }
    return nil
}

func ValidateFloat(value float64) error {
    if math.IsNaN(value) || math.IsInf(value, 0) {
        return fmt.Errorf("%w: non-finite float", ErrInvalidValue)
    }
    return nil
}

// ValidateBuckets ensures histogram buckets are strictly increasing and finite.
func ValidateBuckets(b []float64) error {
    if len(b) == 0 {
        return fmt.Errorf("%w: buckets required", ErrInvalidValue)
    }
    prev := b[0]
    if err := ValidateFloat(prev); err != nil {
        return err
    }
    for i := 1; i < len(b); i++ {
        if err := ValidateFloat(b[i]); err != nil {
            return err
        }
        if b[i] <= prev {
            return fmt.Errorf("%w: buckets must be strictly increasing", ErrInvalidValue)
        }
        prev = b[i]
    }
    return nil
}

// Safe wrappers: normalize labels and validate names/values/buckets before calling the meter.

func IncCounter(m Meter, ctx context.Context, name string, delta int64, labels Labels) error {
    if m == nil {
        m = NopMeterInstance
    }
    if err := ValidateMetricName(name); err != nil {
        return err
    }
    nl, err := NormalizeLabels(labels)
    if err != nil {
        return err
    }
    return m.IncCounter(ctx, name, delta, nl)
}

func SetGauge(m Meter, ctx context.Context, name string, value float64, labels Labels) error {
    if m == nil {
        m = NopMeterInstance
    }
    if err := ValidateMetricName(name); err != nil {
        return err
    }
    if err := ValidateFloat(value); err != nil {
        return err
    }
    nl, err := NormalizeLabels(labels)
    if err != nil {
        return err
    }
    return m.SetGauge(ctx, name, value, nl)
}

func ObserveHistogram(m Meter, ctx context.Context, name string, value float64, buckets []float64, labels Labels) error {
    if m == nil {
        m = NopMeterInstance
    }
    if err := ValidateMetricName(name); err != nil {
        return err
    }
    if err := ValidateFloat(value); err != nil {
        return err
    }
    if err := ValidateBuckets(buckets); err != nil {
        return err
    }
    nl, err := NormalizeLabels(labels)
    if err != nil {
        return err
    }
    return m.ObserveHistogram(ctx, name, value, buckets, nl)
}

// ---- charset policy (defensive for exporters like Prometheus) ----
//
// Keys (post-normalization) allowed: [a-z0-9_-.:]
// Values allowed: ASCII printable + space, restricted to [A-Za-z0-9 _-.:]
// Both reject control chars and non-ASCII to reduce downstream surprises.

func isValidLabelKey(k string) bool {
    for _, r := range k {
        switch {
        case r >= 'a' && r <= 'z':
        case r >= '0' && r <= '9':
        case r == '_' || r == '-' || r == '.' || r == ':':
        default:
            return false
        }
    }
    return true
}

func isValidLabelValue(v string) bool {
    for _, r := range v {
        switch {
        case r >= 'A' && r <= 'Z':
        case r >= 'a' && r <= 'z':
        case r >= '0' && r <= '9':
        case r == ' ' || r == '_' || r == '-' || r == '.' || r == ':':
        default:
            return false
        }
    }
    return true
}
