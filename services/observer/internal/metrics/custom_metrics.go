package metrics

// Custom metrics registry (deterministic, stdlib-only).
//
// This file provides a small in-memory metrics registry for the observer service.
// It can define counters and gauges with optional label sets and export them into
// Prometheus exposition Family/Sample models (rendered by prometheus.go).
//
// Determinism guarantees:
//   - Label normalization is deterministic (sort by name, dedup by name).
//   - Storage uses canonical label strings, and exports are sorted deterministically.
//   - Families returned are sorted by family name.
//   - Samples within families are sorted by (name + canonical labels).
//
// This is DATA ONLY (registry/formatter). No HTTP handlers.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrMetrics        = errors.New("metrics failed")
	ErrMetricsInvalid = errors.New("metrics invalid")
)

type Registry struct {
	mu       sync.Mutex
	counters map[string]*Counter
	gauges   map[string]*Gauge
}

func NewRegistry() *Registry {
	return &Registry{
		counters: make(map[string]*Counter),
		gauges:   make(map[string]*Gauge),
	}
}

type Counter struct {
	name       string
	help       string
	baseLabels []Label

	mu     sync.Mutex
	values map[string]float64 // canonicalLabels -> value
}

type Gauge struct {
	name       string
	help       string
	baseLabels []Label

	mu     sync.Mutex
	values map[string]float64 // canonicalLabels -> value
}

func (r *Registry) Counter(name, help string, baseLabels []Label) *Counter {
	n := norm(name)
	if n == "" {
		n = "unnamed_counter"
	}
	h := strings.TrimSpace(help)

	r.mu.Lock()
	defer r.mu.Unlock()

	if c, ok := r.counters[n]; ok {
		return c
	}
	c := &Counter{
		name:       n,
		help:       h,
		baseLabels: normalizeLabelsLocal(baseLabels),
		values:     make(map[string]float64),
	}
	r.counters[n] = c
	return c
}

func (r *Registry) Gauge(name, help string, baseLabels []Label) *Gauge {
	n := norm(name)
	if n == "" {
		n = "unnamed_gauge"
	}
	h := strings.TrimSpace(help)

	r.mu.Lock()
	defer r.mu.Unlock()

	if g, ok := r.gauges[n]; ok {
		return g
	}
	g := &Gauge{
		name:       n,
		help:       h,
		baseLabels: normalizeLabelsLocal(baseLabels),
		values:     make(map[string]float64),
	}
	r.gauges[n] = g
	return g
}

func (c *Counter) Inc(labels []Label) {
	c.Add(1, labels)
}

func (c *Counter) Add(value float64, labels []Label) {
	if value == 0 {
		return
	}
	if value < 0 {
		return
	}
	ls := mergeLabels(c.baseLabels, labels)
	key := canonicalLabelsString(ls)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.values[key] += value
}

func (g *Gauge) Set(value float64, labels []Label) {
	ls := mergeLabels(g.baseLabels, labels)
	key := canonicalLabelsString(ls)

	g.mu.Lock()
	defer g.mu.Unlock()

	g.values[key] = value
}

// Families exports all registered metrics as Prometheus families deterministically.
func (r *Registry) Families() []Family {
	r.mu.Lock()
	counters := make([]*Counter, 0, len(r.counters))
	for _, c := range r.counters {
		counters = append(counters, c)
	}
	gauges := make([]*Gauge, 0, len(r.gauges))
	for _, g := range r.gauges {
		gauges = append(gauges, g)
	}
	r.mu.Unlock()

	sort.Slice(counters, func(i, j int) bool { return counters[i].name < counters[j].name })
	sort.Slice(gauges, func(i, j int) bool { return gauges[i].name < gauges[j].name })

	out := make([]Family, 0, len(counters)+len(gauges))
	for _, c := range counters {
		out = append(out, c.family())
	}
	for _, g := range gauges {
		out = append(out, g.family())
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *Counter) family() Family {
	c.mu.Lock()
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	samples := make([]Sample, 0, len(keys))
	for _, k := range keys {
		labels := parseCanonicalLabels(k)
		samples = append(samples, Sample{
			Name:   c.name,
			Labels: labels,
			Value:  c.values[k],
		})
	}
	c.mu.Unlock()

	sort.Slice(samples, func(i, j int) bool {
		ai := samples[i].Name + canonicalLabelsString(normalizeLabelsLocal(samples[i].Labels))
		aj := samples[j].Name + canonicalLabelsString(normalizeLabelsLocal(samples[j].Labels))
		return ai < aj
	})

	return Family{
		Name:    c.name,
		Help:    c.help,
		Type:    "counter",
		Samples: samples,
	}
}

func (g *Gauge) family() Family {
	g.mu.Lock()
	keys := make([]string, 0, len(g.values))
	for k := range g.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	samples := make([]Sample, 0, len(keys))
	for _, k := range keys {
		labels := parseCanonicalLabels(k)
		samples = append(samples, Sample{
			Name:   g.name,
			Labels: labels,
			Value:  g.values[k],
		})
	}
	g.mu.Unlock()

	sort.Slice(samples, func(i, j int) bool {
		ai := samples[i].Name + canonicalLabelsString(normalizeLabelsLocal(samples[i].Labels))
		aj := samples[j].Name + canonicalLabelsString(normalizeLabelsLocal(samples[j].Labels))
		return ai < aj
	})

	return Family{
		Name:    g.name,
		Help:    g.help,
		Type:    "gauge",
		Samples: samples,
	}
}

////////////////////////////////////////////////////////////////////////////////
// Label helpers (deterministic)
////////////////////////////////////////////////////////////////////////////////

func mergeLabels(base []Label, extra []Label) []Label {
	b := normalizeLabelsLocal(base)
	e := normalizeLabelsLocal(extra)

	tmp := make(map[string]string, len(b)+len(e))
	for _, l := range b {
		tmp[l.Name] = l.Value
	}
	for _, l := range e {
		tmp[l.Name] = l.Value
	}

	keys := make([]string, 0, len(tmp))
	for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Label, 0, len(keys))
	for _, k := range keys {
		out = append(out, Label{Name: k, Value: tmp[k]})
	}
	return out
}

func canonicalLabelsString(labels []Label) string {
	n := normalizeLabelsLocal(labels)
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

func parseCanonicalLabels(s string) []Label {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]Label, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := norm(kv[0])
		v := strings.TrimSpace(kv[1])
		if k == "" || !isLabelNameLocal(k) {
			continue
		}
		out = append(out, Label{Name: k, Value: v})
	}
	return normalizeLabelsLocal(out)
}

func normalizeLabelsLocal(labels []Label) []Label {
	if len(labels) == 0 {
		return nil
	}
	// Filter invalid names, normalize, then sort.
	tmp := make([]Label, 0, len(labels))
	for _, l := range labels {
		n := norm(l.Name)
		if n == "" || !isLabelNameLocal(n) {
			continue
		}
		v := strings.TrimSpace(strings.ReplaceAll(l.Value, "\x00", ""))
		tmp = append(tmp, Label{Name: n, Value: v})
	}
	sort.Slice(tmp, func(i, j int) bool { return tmp[i].Name < tmp[j].Name })

	// Dedup by name (last wins, but after sort duplicates adjacent; choose last).
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

func isLabelNameLocal(s string) bool {
	// Prom label: [a-zA-Z_][a-zA-Z0-9_]*
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

func norm(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Guard unused import errors if this file is copied standalone.
var _ = fmt.Sprintf
var _ = ErrMetrics
var _ = ErrMetricsInvalid
