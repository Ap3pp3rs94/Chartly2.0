package ml

import (
	"crypto/sha256"

	"encoding/hex"

	"encoding/json"

	"errors"

	"fmt"

	"sort"

	"strconv"

	"strings"

	"unicode/utf8"
)
var (
	ErrModelExists = errors.New("model exists")
	ErrModelMissing  = errors.New("model missing")
	ErrInvalidSpec   = errors.New("invalid model spec")
	ErrInvalidConfig = errors.New("invalid model config")
	ErrRegistryClosed = errors.New("registry closed")
)
type Task string

const (
	TaskAnomaly Task = "anomaly"

	TaskForecast Task = "forecast"

	TaskAggregation Task = "aggregation"

	TaskTransform Task = "transform"
)
type ModelSpec struct {
	ID string `json:"id"`

	Version string `json:"version,omitempty"`

	Task Task `json:"task"`

	Title string `json:"title,omitempty"`

	Description string `json:"description,omitempty"`

	Priority int `json:"priority,omitempty"` // higher = preferred

	Deterministic bool `json:"deterministic,omitempty"`

	Tags []string `json:"tags,omitempty"`

	Defaults map[string]any `json:"defaults,omitempty"` // default config (merged under overrides)
	InputSchema map[string]any `json:"input_schema,omitempty"` // JSON-schema-like (subset) for cfg validation (optional)
	OutputSchema map[string]any `json:"output_schema,omitempty"`

	Meta map[string]string `json:"meta,omitempty"`
}
type Requirements struct {
	Task Task `json:"task"`

	MustTags []string `json:"must_tags,omitempty"`

	PreferTags []string `json:"prefer_tags,omitempty"`

	MinVersion string `json:"min_version,omitempty"`

	MaxVersion string `json:"max_version,omitempty"`

	RequireDeterministic *bool `json:"require_deterministic,omitempty"`
}
type Model interface {
	Spec() ModelSpec

	Run(input any) (any, error)
}
type FactoryFn func(cfg map[string]any) (Model, error)

type Registry struct {
	mu syncRW

	closed bool

	entries map[Task]map[string]entry // key: id@version

	aliases map[Task]map[string]string // alias -> id@version
}
type entry struct {
	spec ModelSpec

	factory FactoryFn
}

func NewRegistry() *Registry {

	return &Registry{

		entries: make(map[Task]map[string]entry),

		aliases: make(map[Task]map[string]string),
	}
}
func (r *Registry) Close() {

	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}
func (r *Registry) Register(spec ModelSpec, factory FactoryFn) error {

	spec = normalizeSpec(spec)
	if err := validateSpec(spec); err != nil {

		return err

	}
	if factory == nil {

		return fmt.Errorf("%w: factory is nil", ErrInvalidSpec)

	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {

		return ErrRegistryClosed

	}
	if _, ok := r.entries[spec.Task]; !ok {

		r.entries[spec.Task] = make(map[string]entry)

	}
	k := modelKey(spec.ID, spec.Version)
	if _, exists := r.entries[spec.Task][k]; exists {

		return ErrModelExists

	}
	r.entries[spec.Task][k] = entry{spec: spec, factory: factory}
	return nil
}
func (r *Registry) Alias(task Task, alias string, id string, version string) error {

	task = Task(strings.TrimSpace(string(task)))
	alias = strings.TrimSpace(alias)
	id = strings.TrimSpace(id)
	version = normalizeVersion(version)
	if task == "" || alias == "" || id == "" {

		return fmt.Errorf("%w: empty task/alias/id", ErrInvalidSpec)

	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {

		return ErrRegistryClosed

	}

	// Must point to an existing model

	k := modelKey(id, version)
	if _, ok := r.entries[task]; !ok {

		return ErrModelMissing

	}
	if _, ok := r.entries[task][k]; !ok {

		return ErrModelMissing

	}
	if _, ok := r.aliases[task]; !ok {

		r.aliases[task] = make(map[string]string)

	}
	r.aliases[task][alias] = k

	return nil
}
func (r *Registry) Get(task Task, id string, version string) (ModelSpec, FactoryFn, bool) {

	task = Task(strings.TrimSpace(string(task)))
	id = strings.TrimSpace(id)
	version = normalizeVersion(version)
	if task == "" || id == "" {

		return ModelSpec{}, nil, false

	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	k := modelKey(id, version)
	m, ok := r.entries[task]

	if !ok {

		return ModelSpec{}, nil, false

	}
	e, ok := m[k]

	if !ok {

		return ModelSpec{}, nil, false

	}
	return e.spec, e.factory, true
}
func (r *Registry) Resolve(task Task, ref string) (ModelSpec, FactoryFn, bool) {

	task = Task(strings.TrimSpace(string(task)))
	ref = strings.TrimSpace(ref)
	if task == "" || ref == "" {

		return ModelSpec{}, nil, false

	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1)
// alias

	if am, ok := r.aliases[task]; ok {

		if k, ok := am[ref]; ok {

			if em, ok := r.entries[task]; ok {

				if e, ok := em[k]; ok {

					return e.spec, e.factory, true

				}

			}

		}

	}

	// 2)
// id@version

	id, ver, hasAt := strings.Cut(ref, "@")
	id = strings.TrimSpace(id)
	ver = normalizeVersion(ver)
	if id == "" {

		return ModelSpec{}, nil, false

	}
	if em, ok := r.entries[task]; ok {

		if hasAt {

			k := modelKey(id, ver)
			if e, ok := em[k]; ok {

				return e.spec, e.factory, true

			}
			return ModelSpec{}, nil, false

		}

		// 3) if only id provided, pick highest-precedence version (Priority desc, Version desc)
		bestKey := ""

		var best entry

		found := false

		for k, e := range em {

			if !strings.HasPrefix(k, id+"@") {

				continue

			}
			if !found {

				bestKey, best, found = k, e, true

				continue

			}
			if betterEntry(e, best) {

				bestKey, best = k, e

			} else if equalEntry(e, best) {

				// tie-break by key

				if k < bestKey {

					bestKey, best = k, e

				}

			}

		}
		if found {

			return best.spec, best.factory, true

		}

	}
	return ModelSpec{}, nil, false
}
func (r *Registry) List(task Task) []ModelSpec {

	task = Task(strings.TrimSpace(string(task)))
	if task == "" {

		return nil

	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	em, ok := r.entries[task]

	if !ok || len(em) == 0 {

		return nil

	}
	out := make([]ModelSpec, 0, len(em))
	for _, e := range em {

		out = append(out, e.spec)

	}
	sort.Slice(out, func(i, j int) bool {

		ai := out[i]

		aj := out[j]

		if ai.Priority != aj.Priority {

			return ai.Priority > aj.Priority

		}
		if ai.ID != aj.ID {

			return ai.ID < aj.ID

		}

		// version desc

		vi, oki := ParseSemVer(ai.Version)
		vj, okj := ParseSemVer(aj.Version)
		if oki && okj {

			c := CompareSemVer(vi, vj)
			if c != 0 {

				return c > 0

			}

		} else if oki != okj {

			// parseable versions come first

			return oki

		} else if ai.Version != aj.Version {

			return ai.Version > aj.Version

		}
		return false

	})
	return out
}
func (r *Registry) Select(req Requirements) (ModelSpec, FactoryFn, error) {

	req = normalizeReq(req)
	if req.Task == "" {

		return ModelSpec{}, nil, fmt.Errorf("%w: task required", ErrInvalidConfig)

	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	em, ok := r.entries[req.Task]

	if !ok || len(em) == 0 {

		return ModelSpec{}, nil, ErrModelMissing

	}

	// Pre-parse version constraints

	var minV, maxV SemVer

	var hasMin, hasMax bool

	if strings.TrimSpace(req.MinVersion) != "" {

		if v, ok := ParseSemVer(req.MinVersion); ok {

			minV, hasMin = v, true

		} else {

			return ModelSpec{}, nil, fmt.Errorf("%w: invalid min_version", ErrInvalidConfig)

		}

	}
	if strings.TrimSpace(req.MaxVersion) != "" {

		if v, ok := ParseSemVer(req.MaxVersion); ok {

			maxV, hasMax = v, true

		} else {

			return ModelSpec{}, nil, fmt.Errorf("%w: invalid max_version", ErrInvalidConfig)

		}

	}
	type candidate struct {
		key string

		entry entry

		score int

		verOK bool

		tagOK bool

		detOK bool
	}
	cands := make([]candidate, 0, len(em))
	for k, e := range em {

		if !tagsContainAll(e.spec.Tags, req.MustTags) {

			continue

		}
		if req.RequireDeterministic != nil && e.spec.Deterministic != *req.RequireDeterministic {

			continue

		}

		// Version constraints (only if spec version parseable and constraints present)
		if hasMin || hasMax {

			v, ok := ParseSemVer(e.spec.Version)
			if !ok {

				continue

			}
			if hasMin && CompareSemVer(v, minV) < 0 {

				continue

			}
			if hasMax && CompareSemVer(v, maxV) > 0 {

				continue

			}

		}
		score := e.spec.Priority

		// PreferTags: +10 per match (deterministic)
		for _, t := range req.PreferTags {

			if hasTag(e.spec.Tags, t) {

				score += 10

			}

		}
		cands = append(cands, candidate{key: k, entry: e, score: score})

	}
	if len(cands) == 0 {

		return ModelSpec{}, nil, ErrModelMissing

	}
	sort.Slice(cands, func(i, j int) bool {

		ai := cands[i]

		aj := cands[j]

		if ai.score != aj.score {

			return ai.score > aj.score

		}

		// Stable tie-break by spec precedence (priority already in score; use version desc, then id, then key)
		si := ai.entry.spec

		sj := aj.entry.spec

		if si.ID != sj.ID {

			return si.ID < sj.ID

		}
		vi, oki := ParseSemVer(si.Version)
		vj, okj := ParseSemVer(sj.Version)
		if oki && okj {

			c := CompareSemVer(vi, vj)
			if c != 0 {

				return c > 0

			}

		} else if oki != okj {

			return oki

		} else if si.Version != sj.Version {

			return si.Version > sj.Version

		}
		return ai.key < aj.key

	})
	best := cands[0].entry

	return best.spec, best.factory, nil
}

// New resolves a model reference (id, id@version, or alias), merges defaults under cfg,
// validates cfg against spec.InputSchema (if present), then constructs the model via factory.
func (r *Registry) New(task Task, ref string, cfg map[string]any) (Model, error) {

	task = Task(strings.TrimSpace(string(task)))
	ref = strings.TrimSpace(ref)
	if task == "" || ref == "" {

		return nil, fmt.Errorf("%w: task/ref required", ErrInvalidConfig)

	}
	spec, factory, ok := r.Resolve(task, ref)
	if !ok {

		return nil, ErrModelMissing

	}
	if factory == nil {

		return nil, fmt.Errorf("%w: factory missing", ErrInvalidSpec)

	}
	merged := DeepMerge(cloneMap(spec.Defaults), cfg)
	if spec.InputSchema != nil && len(spec.InputSchema) > 0 {

		if err := ValidateConfigAgainstSchema(spec.InputSchema, merged); err != nil {

			return nil, err

		}

	}
	m, err := factory(merged)
	if err != nil {

		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)

	}
	if m == nil {

		return nil, fmt.Errorf("%w: factory returned nil model", ErrInvalidConfig)

	}
	return m, nil
}

////////////////////////////////////////////////////////////////////////////////
// Normalization + validation
////////////////////////////////////////////////////////////////////////////////

func normalizeSpec(s ModelSpec) ModelSpec {

	s.ID = strings.TrimSpace(s.ID)
	s.Version = normalizeVersion(s.Version)
	s.Task = Task(strings.TrimSpace(string(s.Task)))
	s.Title = strings.TrimSpace(s.Title)
	s.Description = strings.TrimSpace(s.Description)

	// Tags: trim, lowercase, unique, stable sort

	if len(s.Tags) > 0 {

		set := make(map[string]struct{}, len(s.Tags))
		tags := make([]string, 0, len(s.Tags))
		for _, t := range s.Tags {

			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" {

				continue

			}
			if _, ok := set[t]; ok {

				continue

			}
			set[t] = struct{}{}
			tags = append(tags, t)

		}
		sort.Strings(tags)
		s.Tags = tags

	}

	// Meta: trim keys/values, drop empties

	if s.Meta != nil {

		out := make(map[string]string, len(s.Meta))
		keys := make([]string, 0, len(s.Meta))
		for k := range s.Meta {

			k2 := strings.TrimSpace(k)
			if k2 == "" {

				continue

			}
			keys = append(keys, k2)

		}
		sort.Strings(keys)
		for _, k := range keys {

			v := strings.TrimSpace(s.Meta[k])
			out[k] = v

		}
		if len(out) == 0 {

			s.Meta = nil

		} else {

			s.Meta = out

		}

	}

	// Defaults: normalize keys, ensure JSON-marshalable deterministically

	if s.Defaults != nil {

		s.Defaults = normalizeMapKeys(s.Defaults)
		if len(s.Defaults) == 0 {

			s.Defaults = nil

		}

	}

	// Schemas: keep as-is but normalize map keys for stability

	if s.InputSchema != nil {

		s.InputSchema = normalizeMapKeys(s.InputSchema)
		if len(s.InputSchema) == 0 {

			s.InputSchema = nil

		}

	}
	if s.OutputSchema != nil {

		s.OutputSchema = normalizeMapKeys(s.OutputSchema)
		if len(s.OutputSchema) == 0 {

			s.OutputSchema = nil

		}

	}
	return s
}
func validateSpec(s ModelSpec) error {

	if s.ID == "" {

		return fmt.Errorf("%w: id required", ErrInvalidSpec)

	}
	if s.Task == "" {

		return fmt.Errorf("%w: task required", ErrInvalidSpec)

	}
	switch s.Task {

	case TaskAnomaly, TaskForecast, TaskAggregation, TaskTransform:

	default:

		return fmt.Errorf("%w: unknown task %q", ErrInvalidSpec, string(s.Task))

	}

	// Version can be empty (unversioned), but if present should be semver-ish or at least stable string.

	if strings.ContainsAny(s.ID, " \t\r\n") {

		return fmt.Errorf("%w: id must not contain whitespace", ErrInvalidSpec)

	}
	return nil
}
func normalizeReq(r Requirements) Requirements {

	r.Task = Task(strings.TrimSpace(string(r.Task)))
	r.MinVersion = normalizeVersion(r.MinVersion)
	r.MaxVersion = normalizeVersion(r.MaxVersion)
	r.MustTags = normalizeTags(r.MustTags)
	r.PreferTags = normalizeTags(r.PreferTags)
	return r
}
func normalizeTags(tags []string) []string {

	if len(tags) == 0 {

		return nil

	}
	set := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {

		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {

			continue

		}
		if _, ok := set[t]; ok {

			continue

		}
		set[t] = struct{}{}
		out = append(out, t)

	}
	sort.Strings(out)
	if len(out) == 0 {

		return nil

	}
	return out
}
func normalizeVersion(v string) string {

	v = strings.TrimSpace(v)
	if v == "" {

		return ""

	}

	// accept v-prefix but store without it for stability

	if strings.HasPrefix(strings.ToLower(v), "v") && len(v) > 1 && isDigit(rune(v[1])) {

		v = v[1:]

	}
	return v
}
func modelKey(id, version string) string {

	id = strings.TrimSpace(id)
	version = normalizeVersion(version)
	if version == "" {

		version = "0"

	}
	return id + "@" + version
}
func hasTag(tags []string, t string) bool {

	t = strings.ToLower(strings.TrimSpace(t))
	if t == "" {

		return false

	}

	// tags are already normalized/sorted in specs, but do linear scan for simplicity

	for i := range tags {

		if tags[i] == t {

			return true

		}

	}
	return false
}
func tagsContainAll(have []string, must []string) bool {

	if len(must) == 0 {

		return true

	}
	for _, t := range must {

	if !hasTag(have, t) {

			return false

		}

	}
	return true
}
func betterEntry(a, b entry) bool {

	// true if a has higher precedence than b (Priority desc, Version desc)
	if a.spec.Priority != b.spec.Priority {

		return a.spec.Priority > b.spec.Priority

	}
	av, aok := ParseSemVer(a.spec.Version)
	bv, bok := ParseSemVer(b.spec.Version)
	if aok && bok {

		c := CompareSemVer(av, bv)
		if c != 0 {

			return c > 0

		}

	} else if aok != bok {

		return aok

	} else if a.spec.Version != b.spec.Version {

		return a.spec.Version > b.spec.Version

	}
	return a.spec.ID < b.spec.ID
}
func equalEntry(a, b entry) bool {

	return a.spec.Priority == b.spec.Priority && a.spec.ID == b.spec.ID && a.spec.Version == b.spec.Version
}

////////////////////////////////////////////////////////////////////////////////
// Deterministic instance id
////////////////////////////////////////////////////////////////////////////////

// DeterministicInstanceID returns a stable id for (spec, cfg)
// using sha256 of canonical JSON.
// Returns "mdl_" + 24 hex chars.
func DeterministicInstanceID(spec ModelSpec, cfg map[string]any) (string, error) {

	spec = normalizeSpec(spec)
	payload := map[string]any{

		"id": spec.ID,

		"version": spec.Version,

		"task": string(spec.Task),

		"cfg": normalizeMapKeys(cfg),
	}
	b, err := json.Marshal(payload)
	if err != nil {

		return "", fmt.Errorf("%w: cfg not json-marshalable", ErrInvalidConfig)

	}
	sum := sha256.Sum256(b)
	return "mdl_" + hex.EncodeToString(sum[:12]), nil
}

////////////////////////////////////////////////////////////////////////////////
// Deep merge + cloning
////////////////////////////////////////////////////////////////////////////////

// DeepMerge merges overrides into base (both may be nil). Nested map[string]any values are merged recursively.
// Non-map values replace base values.
func DeepMerge(base map[string]any, overrides map[string]any) map[string]any {

	if base == nil && overrides == nil {

		return nil

	}
	out := cloneMap(base)
	for k, v := range overrides {

		k2 := strings.TrimSpace(k)
		if k2 == "" {

			continue

		}
		if bv, ok := out[k2]; ok {

			mb, okb := bv.(map[string]any)
			mv, okv := v.(map[string]any)
			if okb && okv {

				out[k2] = DeepMerge(mb, mv)
				continue

			}

		}
		out[k2] = v

	}
	if len(out) == 0 {

		return nil

	}
	return out
}
func cloneMap(m map[string]any) map[string]any {

	if m == nil {

		return nil

	}
	out := make(map[string]any, len(m))
	for k, v := range m {

		k2 := strings.TrimSpace(k)
		if k2 == "" {

			continue

		}
		out[k2] = v

	}
	if len(out) == 0 {

		return nil

	}
	return out
}
func normalizeMapKeys(m map[string]any) map[string]any {

	if m == nil {

		return nil

	}
	out := make(map[string]any, len(m))
	for k, v := range m {

		k2 := strings.TrimSpace(k)
		if k2 == "" {

			continue

		}
		switch vv := v.(type) {

		case map[string]any:
			out[k2] = normalizeMapKeys(vv)
		case []any:
			out[k2] = normalizeSlice(vv)
		default:
			out[k2] = v

		}

	}
	if len(out) == 0 {

		return nil

	}
	return out
}
func normalizeSlice(s []any) []any {

	if s == nil {

		return nil

	}
	out := make([]any, len(s))
	for i := range s {

		switch vv := s[i].(type) {

		case map[string]any:
			out[i] = normalizeMapKeys(vv)
		case []any:
			out[i] = normalizeSlice(vv)
		default:
			out[i] = s[i]

		}

	}
	return out
}

////////////////////////////////////////////////////////////////////////////////
// SemVer parsing (minimal, deterministic)
////////////////////////////////////////////////////////////////////////////////

type SemVer struct {
	Major int

	Minor int

	Patch int

	Pre string // prerelease (raw; compared lexicographically)
Valid bool
}

func ParseSemVer(s string) (SemVer, bool) {

	s = normalizeVersion(s)
	if strings.TrimSpace(s) == "" {

		return SemVer{}, false

	}

	// Split build metadata away

	if i := strings.IndexByte(s, '+'); i >= 0 {

		s = s[:i]

	}
	pre := ""

	if i := strings.IndexByte(s, '-'); i >= 0 {

		pre = s[i+1:]

		s = s[:i]

	}
	parts := strings.Split(s, ".")
	parsePart := func(i int) (int, bool) {

		if i >= len(parts) {

			return 0, true

		}
		p := strings.TrimSpace(parts[i])
		if p == "" {

			return 0, false

		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {

			return 0, false

		}
		return n, true

	}
	maj, ok := parsePart(0)
	if !ok {

		return SemVer{}, false

	}
	min, ok := parsePart(1)
	if !ok {

		return SemVer{}, false

	}
	pat, ok := parsePart(2)
	if !ok {

		return SemVer{}, false

	}
	return SemVer{Major: maj, Minor: min, Patch: pat, Pre: pre, Valid: true}, true
}

// CompareSemVer returns -1 if a<b, 0 if equal, +1 if a>b.
// Release (no Pre)
// is considered greater than prerelease with same numbers.
func CompareSemVer(a, b SemVer) int {

	if !a.Valid && !b.Valid {

		return 0

	}
	if a.Valid != b.Valid {

		if a.Valid {

			return 1

		}
		return -1

	}
	if a.Major != b.Major {

		return cmpInt(a.Major, b.Major)

	}
	if a.Minor != b.Minor {

		return cmpInt(a.Minor, b.Minor)

	}
	if a.Patch != b.Patch {

		return cmpInt(a.Patch, b.Patch)

	}

	// prerelease: empty > non-empty

	ae := strings.TrimSpace(a.Pre) == ""

	be := strings.TrimSpace(b.Pre) == ""

	if ae && be {

		return 0

	}
	if ae != be {

		if ae {

			return 1

		}
		return -1

	}
	if a.Pre < b.Pre {

		return -1

	}
	if a.Pre > b.Pre {

		return 1

	}
	return 0
}
func cmpInt(a, b int) int {

	if a < b {

		return -1

	}
	if a > b {

		return 1

	}
	return 0
}
func isDigit(r rune) bool { return r >= '0' && r <= '9' }

////////////////////////////////////////////////////////////////////////////////
// Config validation (subset of JSON Schema)
////////////////////////////////////////////////////////////////////////////////

// ValidateConfigAgainstSchema validates cfg against a JSON-schema-like object.
// Supported (subset):
// - { "type":"object", "required":[...], "properties":{k:{...}}, "additionalProperties":bool }
// - properties: { "type":"string|number|integer|boolean|object|array", "enum":[...], "min":..., "max":... }
// - array: { "items": { ... } }
// This is intentionally minimal and deterministic.
func ValidateConfigAgainstSchema(schema map[string]any, cfg map[string]any) error {

	if schema == nil {

		return nil

	}

	// Top-level type must be object (if specified)
	typ, _ := schema["type"].(string)
	if typ != "" && typ != "object" {

		// For config validation we expect object schema

		return fmt.Errorf("%w: schema type must be object", ErrInvalidConfig)

	}
	if cfg == nil {

		cfg = map[string]any{}

	}

	// required

	if reqAny, ok := schema["required"]; ok {

		if reqList, ok := reqAny.([]any); ok {

			for _, it := range reqList {

				k, ok := it.(string)
				if !ok {

					continue

				}
				k = strings.TrimSpace(k)
				if k == "" {

					continue

				}
				if _, ok := cfg[k]; !ok {

					return fmt.Errorf("%w: missing required %q", ErrInvalidConfig, k)

				}

			}

		} else if reqList, ok := schema["required"].([]string); ok {

			for _, k := range reqList {

				k = strings.TrimSpace(k)
				if k == "" {

					continue

				}
				if _, ok := cfg[k]; !ok {

					return fmt.Errorf("%w: missing required %q", ErrInvalidConfig, k)

				}

			}

		}

	}
	props, _ := schema["properties"].(map[string]any)
addl := true

	if ap, ok := schema["additionalProperties"].(bool); ok {

		addl = ap

	}

	// If additionalProperties=false, reject unknown keys

	if !addl && props != nil {

		for k := range cfg {

			if _, ok := props[k]; !ok {

				return fmt.Errorf("%w: unknown config key %q", ErrInvalidConfig, k)

			}

		}

	}

	// Validate known properties

	for k, ps := range props {

		pm, ok := ps.(map[string]any)
		if !ok {

			continue

		}
		val, exists := cfg[k]

		if !exists {

			continue

		}
		if err := validateValueAgainstSchema(pm, val, "cfg."+k); err != nil {

			return err

		}

	}

	// Ensure cfg is JSON-marshalable (deterministic check)
	if _, err := json.Marshal(cfg); err != nil {

		return fmt.Errorf("%w: cfg not json-marshalable", ErrInvalidConfig)

	}
	return nil
}
func validateValueAgainstSchema(schema map[string]any, v any, path string) error {

	typ, _ := schema["type"].(string)
	typ = strings.TrimSpace(typ)

	// enum

	if enumAny, ok := schema["enum"]; ok {

		if enums, ok := enumAny.([]any); ok {

			okEnum := false

			for _, e := range enums {

				if equalsJSONScalar(e, v) {

					okEnum = true

					break

				}

			}
			if !okEnum {

				return fmt.Errorf("%w: %s not in enum", ErrInvalidConfig, path)

			}

		}

	}
	switch typ {

	case "":

		// no type constraint

		return nil

	case "string":

		s, ok := v.(string)
		if !ok {

			return fmt.Errorf("%w: %s must be string", ErrInvalidConfig, path)

		}

		// optional minLength/maxLength

		if ml, ok := asInt(schema["minLength"]); ok && utf8Len(s) < ml {

			return fmt.Errorf("%w: %s too short", ErrInvalidConfig, path)

		}
		if mx, ok := asInt(schema["maxLength"]); ok && utf8Len(s) > mx {

			return fmt.Errorf("%w: %s too long", ErrInvalidConfig, path)

		}
		return nil

	case "boolean":

		if _, ok := v.(bool); !ok {

			return fmt.Errorf("%w: %s must be boolean", ErrInvalidConfig, path)

		}
		return nil

	case "number":

		if !isNumber(v) {

			return fmt.Errorf("%w: %s must be number", ErrInvalidConfig, path)

		}
		return nil

	case "integer":

		if !isInteger(v) {

			return fmt.Errorf("%w: %s must be integer", ErrInvalidConfig, path)

		}
		return nil

	case "object":

		m, ok := v.(map[string]any)
		if !ok {

			return fmt.Errorf("%w: %s must be object", ErrInvalidConfig, path)

		}

		// recurse (subset)
		return ValidateConfigAgainstSchema(schema, m)
	case "array":

		arr, ok := v.([]any)
		if !ok {

			return fmt.Errorf("%w: %s must be array", ErrInvalidConfig, path)

		}
		itm, _ := schema["items"].(map[string]any)
		if itm != nil {

			for i := range arr {

				if err := validateValueAgainstSchema(itm, arr[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {

					return err

				}

			}

		}
		return nil

	case "number|integer":

		if !isNumber(v) {

			return fmt.Errorf("%w: %s must be number", ErrInvalidConfig, path)

		}
		return nil

	case "string|number":

		if _, ok := v.(string); ok {

			return nil

		}
		if !isNumber(v) {

			return fmt.Errorf("%w: %s must be string or number", ErrInvalidConfig, path)

		}
		return nil

	default:

		return fmt.Errorf("%w: %s unknown schema type %q", ErrInvalidConfig, path, typ)

	}
}
func equalsJSONScalar(a, b any) bool {

	switch x := a.(type) {

	case string:

		y, ok := b.(string)
		return ok && x == y

	case bool:

		y, ok := b.(bool)
		return ok && x == y

	case float64:

		// json numbers decode as float64; also accept ints

		switch y := b.(type) {

		case float64:

			return x == y

		case int:

			return x == float64(y)
		case int64:

			return x == float64(y)
		case uint64:

			return x == float64(y)
		default:

			return false

		}
	default:

		return false

	}
}
func isNumber(v any) bool {

	switch v.(type) {

	case float64, float32, int, int32, int64, uint, uint32, uint64:

		return true

	default:

		return false

	}
}
func isInteger(v any) bool {

	switch v.(type) {

	case int, int32, int64, uint, uint32, uint64:

		return true

	default:

		return false

	}
}
func asInt(v any) (int, bool) {

	switch t := v.(type) {

	case int:

		return t, true

	case int64:

		if t > int64(^uint(0)>>1) {

			return 0, false

		}
		return int(t), true

	case float64:

		if t != float64(int(t)) {

			return 0, false

		}
		return int(t), true

	default:

		return 0, false

	}
}
func utf8Len(s string) int {

	return utf8.RuneCountInString(s)
}

////////////////////////////////////////////////////////////////////////////////
// Minimal RWMutex wrapper (avoid importing sync in signature lists)
////////////////////////////////////////////////////////////////////////////////

type syncRW struct {
	mu  chan struct{}
	rmu chan struct{}

	// readers count protected by rmu

	readers int
}

func (s *syncRW) init() {

	if s.mu == nil {

		s.mu = make(chan struct{}, 1)
		s.mu <- struct{}{}

	}
	if s.rmu == nil {

		s.rmu = make(chan struct{}, 1)
		s.rmu <- struct{}{}

	}
}
func (s *syncRW) Lock() {

	s.init()

	<-s.mu
}
func (s *syncRW) Unlock() {

	s.init()
	s.mu <- struct{}{}
}
func (s *syncRW) RLock() {

	s.init()

	<-s.rmu

	s.readers++

	if s.readers == 1 {

		<-s.mu

	}
	s.rmu <- struct{}{}
}
func (s *syncRW) RUnlock() {

	s.init()

	<-s.rmu

	s.readers--

	if s.readers == 0 {

		s.mu <- struct{}{}

	}
	s.rmu <- struct{}{}
}
