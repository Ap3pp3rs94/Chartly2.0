package metrics

// Prometheus exposition format renderer (deterministic, stdlib-only).
//
// This package renders metric families into Prometheus text exposition format.
// It is a formatter only (DATA ONLY) and does not implement HTTP handlers.
//
// Determinism guarantees:
//   - Families are sorted by Name.
//   - Samples are sorted by (metric name + canonical labels string).
//   - Labels are sorted by label Name.
//   - Escaping is applied deterministically.
//
// References:
//   - Prometheus text exposition format (v0.0.4). This implementation uses the commonly accepted subset.

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrProm        = errors.New("prometheus render failed")
	ErrPromInvalid = errors.New("prometheus invalid")
)

type Label struct {
	Name  string
	Value string
}

type Sample struct {
	Name        string
	Labels      []Label
	Value       float64
	TimestampMS int64 // 0 means omit
}

type Family struct {
	Name    string
	Help    string
	Type    string // "counter"|"gauge"|"histogram"|"summary"
	Samples []Sample
}

// Render produces Prometheus text exposition for the provided families.
// Output is deterministic as long as inputs are the same.
func Render(families []Family) (string, error) {
	// Normalize and validate families.
	fs := make([]Family, 0, len(families))
	for _, f := range families {
		nf, err := normalizeFamily(f)
		if err != nil {
			return "", err
		}
		// Allow empty families (will render HELP/TYPE only if present).
		fs = append(fs, nf)
	}

	// Sort families by Name
	sort.Slice(fs, func(i, j int) bool { return fs[i].Name < fs[j].Name })

	var b strings.Builder
	for _, f := range fs {
		if f.Help != "" {
			b.WriteString("# HELP ")
			b.WriteString(f.Name)
			b.WriteString(" ")
			b.WriteString(escapeHelp(f.Help))
			b.WriteString("\n")
		}
		if f.Type != "" {
			b.WriteString("# TYPE ")
			b.WriteString(f.Name)
			b.WriteString(" ")
			b.WriteString(f.Type)
			b.WriteString("\n")
		}

		// Sort samples deterministically by (Name + canonical labels)
		samples := make([]Sample, len(f.Samples))
		copy(samples, f.Samples)
		sort.Slice(samples, func(i, j int) bool {
			ai := samples[i].Name + canonicalLabels(samples[i].Labels)
			aj := samples[j].Name + canonicalLabels(samples[j].Labels)
			return ai < aj
		})

		for _, s := range samples {
			ns, err := normalizeSample(s)
			if err != nil {
				return "", err
			}
			b.WriteString(ns.Name)
			lbl := renderLabels(ns.Labels)
			if lbl != "" {
				b.WriteString(lbl)
			}
			b.WriteString(" ")
			b.WriteString(strconv.FormatFloat(ns.Value, 'g', -1, 64))
			if ns.TimestampMS != 0 {
				b.WriteString(" ")
				b.WriteString(strconv.FormatInt(ns.TimestampMS, 10))
			}
			b.WriteString("\n")
		}
	}

	return b.String(), nil
}

////////////////////////////////////////////////////////////////////////////////
// Normalization + validation
////////////////////////////////////////////////////////////////////////////////

func normalizeFamily(f Family) (Family, error) {
	n := Family{
		Name:    norm(f.Name),
		Help:    normKeepSpace(f.Help),
		Type:    strings.ToLower(norm(f.Type)),
		Samples: f.Samples,
	}
	if n.Name == "" {
		return Family{}, fmt.Errorf("%w: %w: family name required", ErrProm, ErrPromInvalid)
	}
	if !isMetricName(n.Name) {
		return Family{}, fmt.Errorf("%w: %w: invalid family name", ErrProm, ErrPromInvalid)
	}
	if n.Type != "" {
		switch n.Type {
		case "counter", "gauge", "histogram", "summary":
			// ok
		default:
			return Family{}, fmt.Errorf("%w: %w: invalid family type", ErrProm, ErrPromInvalid)
		}
	}
	return n, nil
}

func normalizeSample(s Sample) (Sample, error) {
	n := Sample{
		Name:        norm(s.Name),
		Labels:      normalizeLabels(s.Labels),
		Value:       s.Value,
		TimestampMS: s.TimestampMS,
	}
	if n.Name == "" {
		return Sample{}, fmt.Errorf("%w: %w: sample name required", ErrProm, ErrPromInvalid)
	}
	if !isMetricName(n.Name) {
		return Sample{}, fmt.Errorf("%w: %w: invalid sample metric name", ErrProm, ErrPromInvalid)
	}
	if n.TimestampMS < 0 {
		return Sample{}, fmt.Errorf("%w: %w: negative timestamp", ErrProm, ErrPromInvalid)
	}
	return n, nil
}

func normalizeLabels(labels []Label) []Label {
	if len(labels) == 0 {
		return nil
	}
	// Filter invalid names, normalize, then sort.
	tmp := make([]Label, 0, len(labels))
	for _, l := range labels {
		ln := norm(l.Name)
		if ln == "" {
			continue
		}
		if !isLabelName(ln) {
			continue
		}
		tmp = append(tmp, Label{Name: ln, Value: normKeepSpace(l.Value)})
	}
	sort.Slice(tmp, func(i, j int) bool { return tmp[i].Name < tmp[j].Name })

	// Dedup deterministically: last wins, duplicates are adjacent due to sort.
	out := make([]Label, 0, len(tmp))
	for i := 0; i < len(tmp); {
		j := i + 1
		for j < len(tmp) && tmp[j].Name == tmp[i].Name {
			j++
		}
		out = append(out, tmp[j-1])
		i = j
	}
	return out
}

////////////////////////////////////////////////////////////////////////////////
// Rendering helpers
////////////////////////////////////////////////////////////////////////////////

func renderLabels(labels []Label) string {
	if len(labels) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("{")
	for i, l := range labels {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(l.Name)
		b.WriteString("=\"")
		b.WriteString(escapeLabelValue(l.Value))
		b.WriteString("\"")
	}
	b.WriteString("}")
	return b.String()
}

func canonicalLabels(labels []Label) string {
	n := normalizeLabels(labels)
	if len(n) == 0 {
		return ""
	}
	var b strings.Builder
	for i, l := range n {
		if i > 0 {
			b.WriteString(";")
		}
		b.WriteString(l.Name)
		b.WriteString("=")
		b.WriteString(l.Value)
	}
	return b.String()
}

func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

////////////////////////////////////////////////////////////////////////////////
// Name validation (basic)
////////////////////////////////////////////////////////////////////////////////

func isMetricName(s string) bool {
	// Prometheus metric name: [a-zA-Z_:][a-zA-Z0-9_:]*
	if s == "" {
		return false
	}
	r0 := s[0]
	if !isAlpha(r0) && r0 != '_' && r0 != ':' {
		return false
	}
	for i := 1; i < len(s); i++ {
		r := s[i]
		if !isAlpha(r) && !isDigit(r) && r != '_' && r != ':' {
			return false
		}
	}
	return true
}

func isLabelName(s string) bool {
	// Label name: [a-zA-Z_][a-zA-Z0-9_]*
	if s == "" {
		return false
	}
	r0 := s[0]
	if !isAlpha(r0) && r0 != '_' {
		return false
	}
	for i := 1; i < len(s); i++ {
		r := s[i]
		if !isAlpha(r) && !isDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

////////////////////////////////////////////////////////////////////////////////
// String normalization
////////////////////////////////////////////////////////////////////////////////

func norm(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	return s
}

func normKeepSpace(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}
