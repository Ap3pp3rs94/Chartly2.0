package engine

import (
	"context"

	"crypto/sha256"

	"encoding/hex"

	"encoding/json"

	"sort"

	"strings"
)
type Enrichment struct {
	Key string `json:"key"`

	Value string `json:"value"`

	Source string `json:"source"`
}
type Enricher interface {
	Enrich(ctx context.Context, doc map[string]any, meta map[string]string) ([]Enrichment, error)
}
type StaticEnricher struct {
	values map[string]string
}

func NewStaticEnricher(values map[string]string) *StaticEnricher {

	cp := make(map[string]string)
for k, v := range values {

		k = strings.TrimSpace(k)
if k == "" {

			continue

		}
		cp[k] = v

	}
	return &StaticEnricher{values: cp}
}
func (e *StaticEnricher) Enrich(ctx context.Context, doc map[string]any, meta map[string]string) ([]Enrichment, error) {

	_ = ctx

	_ = doc

	_ = meta

	out := make([]Enrichment, 0, len(e.values))
keys := make([]string, 0, len(e.values))
for k := range e.values {

		keys = append(keys, k)

	}
	sort.Strings(keys)
for _, k := range keys {

		out = append(out, Enrichment{Key: k, Value: e.values[k], Source: "static"})

	}
	return out, nil
}

type DerivedEnricher struct{}

func (DerivedEnricher) Enrich(ctx context.Context, doc map[string]any, meta map[string]string) ([]Enrichment, error) {

	_ = ctx

	out := []Enrichment{

		{Key: "meta.tenant", Value: meta["tenant_id"], Source: "derived"},

		{Key: "meta.source", Value: meta["source_id"], Source: "derived"},

		{Key: "meta.connector", Value: meta["connector_id"], Source: "derived"},

		{Key: "meta.job", Value: meta["job_id"], Source: "derived"},
	}

	// Deterministic doc hash: canonicalize JSON with sorted keys recursively.

	canon := canonicalize(doc)
b, _ := json.Marshal(canon)
sum := sha256.Sum256(b)
	out = append(out, Enrichment{

		Key: "doc.hash",

		Value: hex.EncodeToString(sum[:]),

		Source: "derived",
	})
	return out, nil
}
func ApplyEnrichments(doc map[string]any, enrich []Enrichment) {

	if doc == nil {

		return

	}
	en := ensureMap(doc, "_enrich")
src := ensureMap(doc, "_enrich_sources")
for _, e := range enrich {

		k := strings.TrimSpace(e.Key)
if k == "" {

			continue

		}
		en[k] = e.Value

		src[k] = e.Source

	}
}
func ensureMap(doc map[string]any, key string) map[string]any {

	v, ok := doc[key]

	if ok {

		if m, ok2 := v.(map[string]any); ok2 {

			return m

		}

	}
	m := make(map[string]any)
doc[key] = m

	return m
}

// canonicalize converts maps to sorted-key representation and recurses through slices.
func canonicalize(v any) any {

	switch t := v.(type) {

	case map[string]any:

		keys := make([]string, 0, len(t))
for k := range t {

			keys = append(keys, k)

		}
		sort.Strings(keys)
		out := make([]any, 0, len(keys)*2)
for _, k := range keys {

			out = append(out, k)
out = append(out, canonicalize(t[k]))

		}

		// represent as array of [k1,v1,k2,v2...] to preserve order deterministically

		return out

	case []any:

		out := make([]any, len(t))
for i := range t {

			out[i] = canonicalize(t[i])

		}
		return out

	default:

		return v

	}
}
