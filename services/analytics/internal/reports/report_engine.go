package reports

import (
	"bytes"

	"crypto/sha256"

	"encoding/hex"

	"encoding/json"

	"errors"

	"fmt"

	"sort"

	"strconv"

	"strings"

	"sync"

	"time"
)

type LoggerFn func(level, msg string, fields map[string]any) var ( ErrTemplateExists  = errors.New("template exists") ErrTemplateMissing = errors.New("template missing") ErrInvalidTemplate = errors.New("invalid template") ErrInvalidInput    = errors.New("invalid input") ErrRender          = errors.New("render failed") ) type Template struct {
	ID string `json:"id"`

	Title string `json:"title"`

	Subtitle string `json:"subtitle,omitempty"`

	Summary string `json:"summary,omitempty"`

	Sections []SectionTemplate `json:"sections"`

	Meta map[string]string `json:"meta,omitempty"`
}
type SectionTemplate struct {
	ID string `json:"id,omitempty"`

	Title string `json:"title,omitempty"`

	Kind string `json:"kind"` // text|chart|table|json

	Text string `json:"text,omitempty"`

	ChartKey string `json:"chart_key,omitempty"`

	TableKey string `json:"table_key,omitempty"`

	JSONKey string `json:"json_key,omitempty"`

	Meta map[string]string `json:"meta,omitempty"`
}
type BuildOptions struct {
	ReportID string `json:"report_id,omitempty"`

	TenantID string `json:"tenant_id,omitempty"`

	RequestID string `json:"request_id,omitempty"`

	GeneratedAt string `json:"generated_at,omitempty"` // RFC3339/RFC3339Nano if Strict

	Strict bool `json:"strict,omitempty"`

	Meta map[string]string `json:"meta,omitempty"` // overrides template meta

	Vars map[string]string `json:"vars,omitempty"` // overrides flattened vars
}
type Report struct {
	ID string `json:"id"`

	Title string `json:"title"`

	Subtitle string `json:"subtitle,omitempty"`

	Summary string `json:"summary,omitempty"`

	TenantID string `json:"tenant_id,omitempty"`

	RequestID string `json:"request_id,omitempty"`

	GeneratedAt string `json:"generated_at,omitempty"`

	Meta map[string]string `json:"meta,omitempty"`

	Sections []Section `json:"sections"`
}
type Section struct {
	ID string `json:"id"`

	Title string `json:"title,omitempty"`

	Kind string `json:"kind"` // text|chart|table|json

	Text string `json:"text,omitempty"`

	Chart map[string]any `json:"chart,omitempty"`

	Table *Table `json:"table,omitempty"`

	JSON any `json:"json,omitempty"`

	Meta map[string]string `json:"meta,omitempty"`
}
type Table struct {
	Columns []string `json:"columns"`

	Rows [][]any `json:"rows"`
}
type Engine struct {
	mu sync.RWMutex

	templates map[string]Template

	logger LoggerFn
}

func NewEngine(logger LoggerFn) *Engine {

	if logger == nil {

		logger = func(string, string, map[string]any) {}

	}
	return &Engine{

		templates: make(map[string]Template),

		logger: logger,
	}
}
func (e *Engine) Register(t Template) error {

	t = normalizeTemplate(t)
	if err := validateTemplate(t); err != nil {

		return err

	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.templates[t.ID]; ok {

		return ErrTemplateExists

	}
	e.templates[t.ID] = t

	return nil
}
func (e *Engine) Get(id string) (Template, bool) {

	id = strings.TrimSpace(id)
	e.mu.RLock()
	defer e.mu.RUnlock()
	t, ok := e.templates[id]

	return t, ok
}
func (e *Engine) IDs() []string {

	e.mu.RLock()
	defer e.mu.RUnlock()
	ids := make([]string, 0, len(e.templates))
	for id := range e.templates {

		ids = append(ids, id)

	}
	sort.Strings(ids)
	return ids
}

// Build constructs a Report from a registered Template and input.
// Library-only: does not imply any endpoint or persistence.
func (e *Engine) Build(templateID string, input map[string]any, opt BuildOptions) (Report, error) {

	templateID = strings.TrimSpace(templateID)
	if templateID == "" {

		return Report{}, fmt.Errorf("%w: empty template id", ErrInvalidInput)

	}
	t, ok := e.Get(templateID)
	if !ok {

		return Report{}, ErrTemplateMissing

	}
	if input == nil {

		input = map[string]any{}

	}
	if opt.Strict && strings.TrimSpace(opt.GeneratedAt) != "" {

		if _, err := parseRFC3339(opt.GeneratedAt); err != nil {

			return Report{}, fmt.Errorf("%w: generated_at must be RFC3339/RFC3339Nano", ErrInvalidInput)

		}

	}
	flat := FlattenVars(input)
	vars := make(map[string]string, len(flat)+16)
	for k, v := range flat {

		vars[k] = v

	}

	// Reserved vars

	vars["template_id"] = templateID

	if strings.TrimSpace(opt.TenantID) != "" {

		vars["tenant_id"] = strings.TrimSpace(opt.TenantID)

	}
	if strings.TrimSpace(opt.RequestID) != "" {

		vars["request_id"] = strings.TrimSpace(opt.RequestID)

	}
	if strings.TrimSpace(opt.GeneratedAt) != "" {

		vars["generated_at"] = strings.TrimSpace(opt.GeneratedAt)

	}
	if strings.TrimSpace(opt.ReportID) != "" {

		vars["report_id"] = strings.TrimSpace(opt.ReportID)

	}

	// opt.Vars override flattened vars + reserved defaults

	for k, v := range opt.Vars {

		k = strings.TrimSpace(k)
		if k == "" {

			continue

		}
		vars[k] = strings.TrimSpace(v)

	}

	// Deterministic report id if missing

	reportID := strings.TrimSpace(opt.ReportID)
	if reportID == "" {

		reportID = strings.TrimSpace(vars["report_id"])

	}
	if reportID == "" {

		reportID = deterministicReportID(templateID, opt.TenantID, opt.RequestID, opt.GeneratedAt)
		vars["report_id"] = reportID

	}
	meta := mergeStringMaps(t.Meta, opt.Meta)
	r := Report{

		ID: reportID,

		Title: expand(t.Title, vars),

		Subtitle: expand(t.Subtitle, vars),

		Summary: expand(t.Summary, vars),

		TenantID: strings.TrimSpace(opt.TenantID),

		RequestID: strings.TrimSpace(opt.RequestID),

		GeneratedAt: strings.TrimSpace(opt.GeneratedAt),

		Meta: meta,

		Sections: make([]Section, 0, len(t.Sections)),
	}

	// Fill from vars if missing

	if r.TenantID == "" {

		r.TenantID = strings.TrimSpace(vars["tenant_id"])

	}
	if r.RequestID == "" {

		r.RequestID = strings.TrimSpace(vars["request_id"])

	}
	if r.GeneratedAt == "" {

		r.GeneratedAt = strings.TrimSpace(vars["generated_at"])

	}
	for i, st := range t.Sections {

		kind := strings.ToLower(strings.TrimSpace(st.Kind))
		if kind == "" {

			kind = "text"

		}
		secID := strings.TrimSpace(st.ID)
		if secID == "" {

			secID = fmt.Sprintf("s_%03d", i+1)

		}
		sec := Section{

			ID: secID,

			Title: expand(st.Title, vars),

			Kind: kind,

			Meta: mergeStringMaps(st.Meta, nil),
		}
		switch kind {

		case "text":

			sec.Text = expand(st.Text, vars)
		case "chart":

			key := strings.TrimSpace(st.ChartKey)
			if key == "" {

				if opt.Strict {

					return Report{}, fmt.Errorf("%w: chart section %s missing chart_key", ErrInvalidTemplate, secID)

				}
				sec.Meta = mergeStringMaps(sec.Meta, map[string]string{"missing": "true", "missing_key": "chart_key"})
				r.Sections = append(r.Sections, sec)
				continue

			}
			val, ok := GetPath(input, key)
			if !ok {

				if opt.Strict {

					return Report{}, fmt.Errorf("%w: missing chart input at %s", ErrInvalidInput, key)

				}
				sec.Meta = mergeStringMaps(sec.Meta, map[string]string{"missing": "true", "path": key})
				r.Sections = append(r.Sections, sec)
				continue

			}
			spec, err := coerceJSONMap(val)
			if err != nil {

				if opt.Strict {

					return Report{}, err

				}
				sec.Meta = mergeStringMaps(sec.Meta, map[string]string{"invalid": "true", "path": key, "error": err.Error()})
				r.Sections = append(r.Sections, sec)
				continue

			}
			sec.Chart = spec

		case "table":

			key := strings.TrimSpace(st.TableKey)
			if key == "" {

				if opt.Strict {

					return Report{}, fmt.Errorf("%w: table section %s missing table_key", ErrInvalidTemplate, secID)

				}
				sec.Meta = mergeStringMaps(sec.Meta, map[string]string{"missing": "true", "missing_key": "table_key"})
				r.Sections = append(r.Sections, sec)
				continue

			}
			val, ok := GetPath(input, key)
			if !ok {

				if opt.Strict {

					return Report{}, fmt.Errorf("%w: missing table input at %s", ErrInvalidInput, key)

				}
				sec.Meta = mergeStringMaps(sec.Meta, map[string]string{"missing": "true", "path": key})
				r.Sections = append(r.Sections, sec)
				continue

			}
			tbl, err := coerceTable(val)
			if err != nil {

				if opt.Strict {

					return Report{}, err

				}
				sec.Meta = mergeStringMaps(sec.Meta, map[string]string{"invalid": "true", "path": key, "error": err.Error()})
				r.Sections = append(r.Sections, sec)
				continue

			}
			sec.Table = tbl

		case "json":

			key := strings.TrimSpace(st.JSONKey)
			if key == "" {

				// fallback to TableKey for compatibility

				key = strings.TrimSpace(st.TableKey)

			}
			if key == "" {

				if opt.Strict {

					return Report{}, fmt.Errorf("%w: json section %s missing json_key", ErrInvalidTemplate, secID)

				}
				sec.Meta = mergeStringMaps(sec.Meta, map[string]string{"missing": "true", "missing_key": "json_key"})
				r.Sections = append(r.Sections, sec)
				continue

			}
			val, ok := GetPath(input, key)
			if !ok {

				if opt.Strict {

					return Report{}, fmt.Errorf("%w: missing json input at %s", ErrInvalidInput, key)

				}
				sec.Meta = mergeStringMaps(sec.Meta, map[string]string{"missing": "true", "path": key})
				r.Sections = append(r.Sections, sec)
				continue

			}
			sec.JSON = val

		default:

			if opt.Strict {

				return Report{}, fmt.Errorf("%w: unknown section kind %q", ErrInvalidTemplate, kind)

			}
			sec.Meta = mergeStringMaps(sec.Meta, map[string]string{"invalid_kind": kind})

		}
		r.Sections = append(r.Sections, sec)

	}
	if err := validateReport(r); err != nil {

		return Report{}, err

	}
	return r, nil
}

////////////////////////////////////////////////////////////////////////////////
// Rendering (library only)
////////////////////////////////////////////////////////////////////////////////

type Renderer interface {
	Name()
	// string

	ContentType()
	// string

	Render(r Report) ([]byte, error)
}
type JSONRenderer struct {
	Indent bool
}

func (JSONRenderer) Name() string        { return "json" }
func (JSONRenderer) ContentType() string { return "application/json" }
func (jr JSONRenderer) Render(r Report) ([]byte, error) {

	if err := validateReport(r); err != nil {

		return nil, err

	}
	if jr.Indent {

		return json.MarshalIndent(r, "", "  ")

	}
	return json.Marshal(r)
}

type MarkdownRenderer struct {
	MaxTableRows int // default 50

	IncludeCharts bool // default true

	IncludeSection bool // include section meta details (default true)
}

func (MarkdownRenderer) Name() string        { return "markdown" }
func (MarkdownRenderer) ContentType() string { return "text/markdown" }
func (mr MarkdownRenderer) Render(r Report) ([]byte, error) {

	if err := validateReport(r); err != nil {

		return nil, err

	}
	maxRows := mr.MaxTableRows

	if maxRows <= 0 {

		maxRows = 50

	}
	includeCharts := mr.IncludeCharts

	if !mr.IncludeCharts && mr.IncludeCharts == false {

		// explicit false

	} else {

		includeCharts = true

	}
	includeMeta := mr.IncludeSection

	if mr.IncludeSection == false {

		includeMeta = false

	} else {

		includeMeta = true

	}
	var b bytes.Buffer

	title := strings.TrimSpace(r.Title)
	if title == "" {

		title = r.ID

	}
	b.WriteString("# ")
	b.WriteString(escapeMD(title))
	b.WriteString("\n\n")
	if strings.TrimSpace(r.Subtitle) != "" {

		b.WriteString("**")
		b.WriteString(escapeMD(r.Subtitle))
		b.WriteString("**\n\n")

	}
	if strings.TrimSpace(r.Summary) != "" {

		b.WriteString(escapeMD(r.Summary))
		b.WriteString("\n\n")

	}

	// metadata

	if len(r.Meta) > 0 || r.TenantID != "" || r.RequestID != "" || r.GeneratedAt != "" {

		b.WriteString("## Metadata\n\n")
		meta := mergeStringMaps(r.Meta, nil)
		if r.TenantID != "" {

			meta["tenant_id"] = r.TenantID

		}
		if r.RequestID != "" {

			meta["request_id"] = r.RequestID

		}
		if r.GeneratedAt != "" {

			meta["generated_at"] = r.GeneratedAt

		}
		keys := sortedKeys(meta)
		for _, k := range keys {

			b.WriteString("- **")
			b.WriteString(escapeMD(k))
			b.WriteString("**: ")
			b.WriteString(escapeMD(meta[k]))
			b.WriteString("\n")

		}
		b.WriteString("\n")

	}
	for i, s := range r.Sections {

		secTitle := strings.TrimSpace(s.Title)
		if secTitle == "" {

			secTitle = fmt.Sprintf("Section %d", i+1)

		}
		b.WriteString("## ")
		b.WriteString(escapeMD(secTitle))
		b.WriteString("\n\n")
		switch s.Kind {

		case "text":

			if strings.TrimSpace(s.Text) != "" {

				b.WriteString(escapeMD(s.Text))
				b.WriteString("\n\n")

			} else {

				b.WriteString("_No text._\n\n")

			}
		case "table":

			if s.Table != nil {

				writeMDTable(&b, *s.Table, maxRows)
				b.WriteString("\n")

			} else {

				b.WriteString("_No table data._\n\n")

			}
		case "chart":

			if includeCharts && s.Chart != nil {

				b.WriteString("```json\n")
				j, _ := json.MarshalIndent(s.Chart, "", "  ")
				b.Write(j)
				b.WriteString("\n```\n\n")

			} else {

				b.WriteString("_Chart spec omitted._\n\n")

			}
		case "json":

			b.WriteString("```json\n")
			j, _ := json.MarshalIndent(s.JSON, "", "  ")
			b.Write(j)
			b.WriteString("\n```\n\n")
		default:

			b.WriteString("_Unsupported section kind._\n\n")

		}
		if includeMeta && len(s.Meta) > 0 {

			b.WriteString("<details><summary>Section meta</summary>\n\n")
			keys := sortedKeys(s.Meta)
			for _, k := range keys {

				b.WriteString("- **")
				b.WriteString(escapeMD(k))
				b.WriteString("**: ")
				b.WriteString(escapeMD(s.Meta[k]))
				b.WriteString("\n")

			}
			b.WriteString("\n</details>\n\n")

		}

	}
	return b.Bytes(), nil
}
func writeMDTable(b *bytes.Buffer, t Table, maxRows int) {

	if len(t.Columns) == 0 {

		b.WriteString("_Empty table._\n\n")
		// return

	}
	cols := make([]string, len(t.Columns))
	copy(cols, t.Columns)
	b.WriteString("| ")
	for _, c := range cols {

		b.WriteString(escapeMD(c))
		b.WriteString(" | ")

	}
	b.WriteString("\n| ")
	for range cols {

		b.WriteString("--- | ")

	}
	b.WriteString("\n")
	n := len(t.Rows)
	if maxRows > 0 && n > maxRows {

		n = maxRows

	}
	for i := 0; i < n; i++ {

		b.WriteString("| ")
		row := t.Rows[i]

		for ci := 0; ci < len(cols); ci++ {

			var cell any

			if ci < len(row) {

				cell = row[ci]

			}
			b.WriteString(escapeMD(stringifyCell(cell)))
			b.WriteString(" | ")

		}
		b.WriteString("\n")

	}
	if len(t.Rows) > n {

		b.WriteString("\n_")
		b.WriteString(fmt.Sprintf("Showing %d of %d rows.", n, len(t.Rows)))
		b.WriteString("_\n\n")

	}
}
func stringifyCell(v any) string {

	if v == nil {

		return ""

	}
	switch t := v.(type) {

	case string:

		return t

	case bool:

		if t {

			return "true"

		}
		return "false"

	case float64:

		return fmt.Sprintf("%.6g", t)
	case float32:

		return fmt.Sprintf("%.6g", float64(t))
	case int:

		return strconv.Itoa(t)
	case int64:

		return strconv.FormatInt(t, 10)
	case int32:

		return strconv.FormatInt(int64(t), 10)
	case uint:

		return strconv.FormatUint(uint64(t), 10)
	case uint64:

		return strconv.FormatUint(t, 10)
	case uint32:

		return strconv.FormatUint(uint64(t), 10)
	default:

		b, err := json.Marshal(t)
		if err != nil {

			return ""

		}
		return string(b)

	}
}
func escapeMD(s string) string {

	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

////////////////////////////////////////////////////////////////////////////////
// Template normalization / validation
////////////////////////////////////////////////////////////////////////////////

func normalizeTemplate(t Template) Template {

	t.ID = strings.TrimSpace(t.ID)
	t.Title = strings.TrimSpace(t.Title)
	t.Subtitle = strings.TrimSpace(t.Subtitle)
	t.Summary = strings.TrimSpace(t.Summary)
	if t.Meta != nil {

		t.Meta = normalizeStringMap(t.Meta)

	}
	secs := make([]SectionTemplate, 0, len(t.Sections))
	for _, s := range t.Sections {

		s.ID = strings.TrimSpace(s.ID)
		s.Title = strings.TrimSpace(s.Title)
		s.Kind = strings.ToLower(strings.TrimSpace(s.Kind))
		s.ChartKey = strings.TrimSpace(s.ChartKey)
		s.TableKey = strings.TrimSpace(s.TableKey)
		s.JSONKey = strings.TrimSpace(s.JSONKey)
		if s.Meta != nil {

			s.Meta = normalizeStringMap(s.Meta)

		}
		if s.Kind == "" {

			s.Kind = "text"

		}
		secs = append(secs, s)

	}
	t.Sections = secs

	return t
}
func validateTemplate(t Template) error {

	if strings.TrimSpace(t.ID) == "" {

		return fmt.Errorf("%w: template id empty", ErrInvalidTemplate)

	}
	if strings.TrimSpace(t.Title) == "" {

		return fmt.Errorf("%w: template title empty", ErrInvalidTemplate)

	}
	for i, s := range t.Sections {

		kind := strings.ToLower(strings.TrimSpace(s.Kind))
		switch kind {

		case "text", "chart", "table", "json":

		default:

			return fmt.Errorf("%w: section %d unknown kind %q", ErrInvalidTemplate, i, s.Kind)

		}

	}
	return nil
}
func validateReport(r Report) error {

	if strings.TrimSpace(r.ID) == "" {

		return fmt.Errorf("%w: report id empty", ErrInvalidInput)

	}
	if strings.TrimSpace(r.Title) == "" {

		return fmt.Errorf("%w: report title empty", ErrInvalidInput)

	}
	for i, s := range r.Sections {

		if strings.TrimSpace(s.ID) == "" {

			return fmt.Errorf("%w: section %d id empty", ErrInvalidInput, i)

		}
		if strings.TrimSpace(s.Kind) == "" {

			return fmt.Errorf("%w: section %d kind empty", ErrInvalidInput, i)

		}

	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Placeholder expansion
////////////////////////////////////////////////////////////////////////////////

func expand(s string, vars map[string]string) string {

	if s == "" || vars == nil || len(vars) == 0 {

		return s

	}
	var out strings.Builder

	out.Grow(len(s))
	for {

		i := strings.Index(s, "{{")
		if i < 0 {

			out.WriteString(s)
			break

		}
		out.WriteString(s[:i])
		s = s[i+2:]

		j := strings.Index(s, "}}")
		if j < 0 {

			out.WriteString("{{")
			out.WriteString(s)
			break

		}
		key := strings.TrimSpace(s[:j])
		s = s[j+2:]

		if key == "" {

			continue

		}
		if v, ok := vars[key]; ok {

			out.WriteString(v)

		}

	}
	return out.String()
}

////////////////////////////////////////////////////////////////////////////////
// Deterministic ID generation
////////////////////////////////////////////////////////////////////////////////

func deterministicReportID(templateID, tenantID, requestID, generatedAt string) string {

	s := strings.TrimSpace(templateID) + "|" +

		strings.TrimSpace(tenantID) + "|" +

		strings.TrimSpace(requestID) + "|" +

		strings.TrimSpace(generatedAt)
	sum := sha256.Sum256([]byte(s))
	return "rpt_" + hex.EncodeToString(sum[:12])
}

////////////////////////////////////////////////////////////////////////////////
// Path navigation (dot + [idx])
////////////////////////////////////////////////////////////////////////////////

type pathSeg struct {
	key string

	hasIdx bool

	idx int
}

func parsePath(path string) ([]pathSeg, error) {

	path = strings.TrimSpace(path)
	if path == "" {

		return nil, ErrInvalidInput

	}
	parts := strings.Split(path, ".")
	out := make([]pathSeg, 0, len(parts))
	for _, p := range parts {

		p = strings.TrimSpace(p)
		if p == "" {

			return nil, ErrInvalidInput

		}
		if strings.Contains(p, "[") {

			k, rest, ok := strings.Cut(p, "[")
			if !ok || strings.TrimSpace(k) == "" {

				return nil, ErrInvalidInput

			}
			if !strings.HasSuffix(rest, "]") {

				return nil, ErrInvalidInput

			}
			idxStr := strings.TrimSuffix(rest, "]")
			i, err := strconv.Atoi(strings.TrimSpace(idxStr))
			if err != nil || i < 0 {

				return nil, ErrInvalidInput

			}
			out = append(out, pathSeg{key: k, hasIdx: true, idx: i})

		} else {

			out = append(out, pathSeg{key: p})

		}

	}
	return out, nil
}

// GetPath navigates map[string]any / []any structures. Safe: never panics.
func GetPath(root any, path string) (any, bool) {

	segs, err := parsePath(path)
	if err != nil || len(segs) == 0 {

		return nil, false

	}
	cur := root

	for _, s := range segs {

		m, ok := cur.(map[string]any)
		if !ok {

			return nil, false

		}
		next, ok := m[s.key]

		if !ok {

			return nil, false

		}
		if s.hasIdx {

			arr, ok := next.([]any)
			if !ok {

				return nil, false

			}
			if s.idx < 0 || s.idx >= len(arr) {

				return nil, false

			}
			cur = arr[s.idx]

		} else {

			cur = next

		}

	}
	return cur, true
}

////////////////////////////////////////////////////////////////////////////////
// Variable flattening for placeholders
////////////////////////////////////////////////////////////////////////////////

// FlattenVars returns a dot-path map of stringified primitives from input.
// Arrays are addressed as [idx]. Maps are traversed deterministically (sorted keys).
func FlattenVars(input map[string]any) map[string]string {

	out := make(map[string]string)
	if input == nil {

		return out

	}
	flattenAny(out, "", input)
	return out
}
func flattenAny(out map[string]string, prefix string, v any) {

	switch t := v.(type) {

	case map[string]any:

		keys := make([]string, 0, len(t))
		for k := range t {

			k2 := strings.TrimSpace(k)
			if k2 == "" {

				continue

			}
			keys = append(keys, k2)

		}
		sort.Strings(keys)
		for _, k := range keys {

			p := joinPath(prefix, k)
			flattenAny(out, p, t[k])

		}
	case []any:

		for i := 0; i < len(t); i++ {

			p := fmt.Sprintf("%s[%d]", prefix, i)
			flattenAny(out, p, t[i])

		}
	case string:

		out[prefix] = t

	case bool:

		if t {

			out[prefix] = "true"

		} else {

			out[prefix] = "false"

		}
	case float64:

		out[prefix] = fmt.Sprintf("%.6g", t)
	case float32:

		out[prefix] = fmt.Sprintf("%.6g", float64(t))
	case int:

		out[prefix] = strconv.Itoa(t)
	case int64:

		out[prefix] = strconv.FormatInt(t, 10)
	case int32:

		out[prefix] = strconv.FormatInt(int64(t), 10)
	case uint:

		out[prefix] = strconv.FormatUint(uint64(t), 10)
	case uint64:

		out[prefix] = strconv.FormatUint(t, 10)
	case uint32:

		out[prefix] = strconv.FormatUint(uint64(t), 10)
	case nil:

	// ignore

	default:

		b, err := json.Marshal(t)
		if err == nil && len(b) > 0 {

			out[prefix] = string(b)

		}

	}
}
func joinPath(prefix, key string) string {

	if prefix == "" {

		return key

	}
	return prefix + "." + key
}

////////////////////////////////////////////////////////////////////////////////
// Coercion helpers (chart/table)
////////////////////////////////////////////////////////////////////////////////

func coerceJSONMap(v any) (map[string]any, error) {

	switch t := v.(type) {

	case map[string]any:

		return t, nil

	case string:

		var m map[string]any

		if err := json.Unmarshal([]byte(t), &m); err != nil || m == nil {

			return nil, fmt.Errorf("%w: chart must be JSON object", ErrInvalidInput)

		}
		return m, nil

	case []byte:

		var m map[string]any

		if err := json.Unmarshal(t, &m); err != nil || m == nil {

			return nil, fmt.Errorf("%w: chart must be JSON object", ErrInvalidInput)

		}
		return m, nil

	default:

		return nil, fmt.Errorf("%w: chart value type unsupported", ErrInvalidInput)

	}
}
func coerceTable(v any) (*Table, error) {

	if v == nil {

		return nil, fmt.Errorf("%w: table is nil", ErrInvalidInput)

	}

	// Already a Table

	if t, ok := v.(Table); ok {

		cp := t

		normalizeTableInPlace(&cp)
		return &cp, nil

	}
	if tp, ok := v.(*Table); ok && tp != nil {

		cp := *tp

		normalizeTableInPlace(&cp)
		return &cp, nil

	}

	// map {columns, rows}
	if m, ok := v.(map[string]any); ok {

		colsAny, _ := m["columns"].([]any)
		rowsAny, _ := m["rows"].([]any)
		cols := make([]string, 0, len(colsAny))
		for _, c := range colsAny {

			if s, ok := c.(string); ok {

				s = strings.TrimSpace(s)
				if s != "" {

					cols = append(cols, s)

				}

			}

		}
		if len(cols) == 0 {

			if cs, ok := m["columns"].([]string); ok {

				for _, c := range cs {

					c = strings.TrimSpace(c)
					if c != "" {

						cols = append(cols, c)

					}

				}

			}

		}
		if len(cols) == 0 {

			return nil, fmt.Errorf("%w: table columns missing", ErrInvalidInput)

		}
		rows := make([][]any, 0, len(rowsAny))
		for _, r := range rowsAny {

			if ra, ok := r.([]any); ok {

				rows = append(rows, ra)
				continue

			}
			if ri, ok := r.([]interface{}); ok {

				row := make([]any, len(ri))
				for i := range ri {

					row[i] = ri[i]

				}
				rows = append(rows, row)
				continue

			}

		}
		tbl := &Table{Columns: cols, Rows: rows}
		normalizeTableInPlace(tbl)
		return tbl, nil

	}

	// []map[string]any rows

	if rows, ok := v.([]map[string]any); ok {

		return tableFromRowObjects(rows), nil

	}

	// []any of map[string]any

	if arr, ok := v.([]any); ok {

		rows := make([]map[string]any, 0, len(arr))
		for _, it := range arr {

			if m, ok := it.(map[string]any); ok {

				rows = append(rows, m)

			}

		}
		if len(rows) > 0 {

			return tableFromRowObjects(rows), nil

		}

	}
	return nil, fmt.Errorf("%w: unsupported table input type", ErrInvalidInput)
}
func tableFromRowObjects(rows []map[string]any) *Table {

	colSet := make(map[string]struct{})
	for _, r := range rows {

		for k := range r {

			k = strings.TrimSpace(k)
			if k == "" {

				continue

			}
			colSet[k] = struct{}{}

		}

	}
	cols := make([]string, 0, len(colSet))
	for k := range colSet {

		cols = append(cols, k)

	}
	sort.Strings(cols)
	outRows := make([][]any, 0, len(rows))
	for _, r := range rows {

		row := make([]any, len(cols))
		for i, c := range cols {

			row[i] = r[c]

		}
		outRows = append(outRows, row)

	}
	tbl := &Table{Columns: cols, Rows: outRows}
	normalizeTableInPlace(tbl)
	return tbl
}
func normalizeTableInPlace(t *Table) {

	if t == nil {

		return

	}
	seen := make(map[string]struct{}, len(t.Columns))
	cols := make([]string, 0, len(t.Columns))
	for _, c := range t.Columns {

		c = strings.TrimSpace(c)
		if c == "" {

			continue

		}
		if _, ok := seen[c]; ok {

			continue

		}
		seen[c] = struct{}{}
		cols = append(cols, c)

	}
	t.Columns = cols

	for i := 0; i < len(t.Rows); i++ {

		if t.Rows[i] == nil {

			t.Rows[i] = make([]any, len(t.Columns))
			continue

		}
		if len(t.Rows[i]) < len(t.Columns) {

			ext := make([]any, len(t.Columns))
			copy(ext, t.Rows[i])
			t.Rows[i] = ext

		}

	}
}

////////////////////////////////////////////////////////////////////////////////
// Utilities
////////////////////////////////////////////////////////////////////////////////

func mergeStringMaps(a, b map[string]string) map[string]string {

	out := make(map[string]string, 0)
	if a != nil {

		for k, v := range a {

			k = strings.TrimSpace(k)
			if k == "" {

				continue

			}
			out[k] = strings.TrimSpace(v)

		}

	}
	if b != nil {

		for k, v := range b {

			k = strings.TrimSpace(k)
			if k == "" {

				continue

			}
			out[k] = strings.TrimSpace(v)

		}

	}
	if len(out) == 0 {

		return nil

	}
	return out
}
func normalizeStringMap(m map[string]string) map[string]string {

	if m == nil {

		return nil

	}
	out := make(map[string]string, len(m))
	for k, v := range m {

		k = strings.TrimSpace(k)
		if k == "" {

			continue

		}
		out[k] = strings.TrimSpace(v)

	}
	if len(out) == 0 {

		return nil

	}
	return out
}
func sortedKeys(m map[string]string) []string {

	if m == nil {

		return nil

	}
	keys := make([]string, 0, len(m))
	for k := range m {

		k2 := strings.TrimSpace(k)
		if k2 == "" {

			continue

		}
		keys = append(keys, k2)

	}
	sort.Strings(keys)
	return keys
}
func parseRFC3339(ts string) (time.Time, error) {

	ts = strings.TrimSpace(ts)
	if ts == "" {

		return time.Time{}, errors.New("empty")

	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {

		return t.UTC(), nil

	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {

		return time.Time{}, err

	}
	return t.UTC(), nil
}
