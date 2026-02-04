package charts

import (
	"encoding/json"

	"errors"

	"fmt"

	"math"

	"sort"

	"strings"

	"sync"

	"time"
)
type Spec = map[string]any

type LoggerFn func(level, msg string, fields map[string]any)

type AxisOptions struct {
	Label string `json:"label,omitempty"`

	Type string `json:"type,omitempty"` // time|number|category|log

	Unit string `json:"unit,omitempty"`

	Min *float64 `json:"min,omitempty"`

	Max *float64 `json:"max,omitempty"`

	LogBase float64 `json:"log_base,omitempty"`
}
type SeriesOptions struct {
	Kind string `json:"kind,omitempty"` // line|bar|area|scatter|histogram|table

	Stack string `json:"stack,omitempty"`

	Unit string `json:"unit,omitempty"`

	Color string `json:"color,omitempty"`

	LineWidth float64 `json:"line_width,omitempty"`

	PointSize float64 `json:"point_size,omitempty"`

	Opacity float64 `json:"opacity,omitempty"`

	Meta map[string]string `json:"meta,omitempty"`
}
type XYPoint struct {
	X any `json:"x"`

	Y float64 `json:"y"`

	Label string `json:"label,omitempty"`

	Meta map[string]any `json:"meta,omitempty"`
}
type AnomalyMarker struct {
	Ts string `json:"ts,omitempty"`

	X any `json:"x,omitempty"`

	Y float64 `json:"y"`

	Score float64 `json:"score,omitempty"`

	Direction string `json:"direction,omitempty"` // high|low|change

	Reason string `json:"reason,omitempty"`

	Meta map[string]string `json:"meta,omitempty"`
}
type Builder struct {
	mu sync.Mutex

	spec Spec

	maxPoints int

	logger LoggerFn
}

func NewBuilder(chartType, title string) *Builder {

	chartType = strings.TrimSpace(chartType)
if chartType == "" {

		chartType = "line"

	}
	title = strings.TrimSpace(title)
b := &Builder{

		spec: make(Spec),

		maxPoints: 0,

		logger: func(string, string, map[string]any) {},
	}
	b.spec["version"] = "v1"

	b.spec["type"] = chartType

	b.spec["title"] = title

	b.spec["meta"] = make(map[string]any)
b.spec["axes"] = map[string]any{

		"x": map[string]any{"type": "category"},

		"y": map[string]any{"type": "number"},
	}
b.spec["series"] = make([]any, 0)
b.spec["annotations"] = make([]any, 0)
return b
}
func (b *Builder) WithLogger(fn LoggerFn) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
if fn != nil {

		b.logger = fn

	}
	return b
}
func (b *Builder) WithID(id string) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
id = strings.TrimSpace(id)
if id != "" {

		b.spec["id"] = id

	}
	return b
}
func (b *Builder) WithSubtitle(s string) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
s = strings.TrimSpace(s)
if s != "" {

		b.spec["subtitle"] = s

	}
	return b
}
func (b *Builder) WithDescription(s string) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
s = strings.TrimSpace(s)
if s != "" {

		b.spec["description"] = s

	}
	return b
}
func (b *Builder) WithTheme(theme string) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
theme = strings.TrimSpace(theme)
if theme != "" {

		b.spec["theme"] = theme

	}
	return b
}
func (b *Builder) WithMaxPoints(n int) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
if n < 0 {

		n = 0

	}
	b.maxPoints = n

	return b
}
func (b *Builder) WithMeta(k, v string) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
k = strings.TrimSpace(k)
if k == "" {

		return b

	}
	meta, _ := b.spec["meta"].(map[string]any)
if meta == nil {

		meta = make(map[string]any)
b.spec["meta"] = meta

	}
meta[k] = strings.TrimSpace(v)
return b
}
func (b *Builder) WithAxis(axis string, opts AxisOptions) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
axis = strings.ToLower(strings.TrimSpace(axis))
if axis != "x" && axis != "y" {

		return b

	}
	axes, _ := b.spec["axes"].(map[string]any)
if axes == nil {

		axes = make(map[string]any)
b.spec["axes"] = axes

	}
	m := make(map[string]any)
if strings.TrimSpace(opts.Label) != "" {

		m["label"] = strings.TrimSpace(opts.Label)

	}
	if strings.TrimSpace(opts.Type) != "" {

		m["type"] = strings.TrimSpace(opts.Type)

	}
	if strings.TrimSpace(opts.Unit) != "" {

		m["unit"] = strings.TrimSpace(opts.Unit)

	}
	if opts.Min != nil {

		m["min"] = *opts.Min

	}
	if opts.Max != nil {

		m["max"] = *opts.Max

	}
	if opts.LogBase > 0 {

		m["log_base"] = opts.LogBase

	}
	axes[axis] = m

	return b
}
func (b *Builder) AddSeries(name string, pts []XYPoint, opts SeriesOptions) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
name = strings.TrimSpace(name)
if name == "" {

		b.logger("warn", "chart_add_series_skipped", map[string]any{"reason": "empty_name"})
		return b

	}
	kind := strings.ToLower(strings.TrimSpace(opts.Kind))
if kind == "" {

		// default aligns with chart type

		if ct, _ := b.spec["type"].(string); strings.TrimSpace(ct) != "" {

			kind = strings.ToLower(strings.TrimSpace(ct))

		} else {

			kind = "line"

		}

	}
	data := make([]XYPoint, 0, len(pts))
for _, p := range pts {

		// allow any X; drop NaN Y

		if math.IsNaN(p.Y) || math.IsInf(p.Y, 0) {

			continue

		}
		data = append(data, p)

	}
	if b.maxPoints > 0 && len(data) > b.maxPoints {

		data = downsample(data, b.maxPoints)

	}
	series := make(map[string]any)
series["name"] = name

	series["kind"] = kind

	if strings.TrimSpace(opts.Unit) != "" {

		series["unit"] = strings.TrimSpace(opts.Unit)

	}
	if strings.TrimSpace(opts.Stack) != "" {

		series["stack"] = strings.TrimSpace(opts.Stack)

	}
	style := make(map[string]any)
if strings.TrimSpace(opts.Color) != "" {

		style["color"] = strings.TrimSpace(opts.Color)

	}
	if opts.LineWidth > 0 {

		style["line_width"] = opts.LineWidth

	}
	if opts.PointSize > 0 {

		style["point_size"] = opts.PointSize

	}
	if opts.Opacity > 0 {

		style["opacity"] = opts.Opacity

	}
	if len(style) > 0 {

		series["style"] = style

	}
	if opts.Meta != nil && len(opts.Meta) > 0 {

		series["meta"] = sortedStringMap(opts.Meta)

	}

	// encode points

	outPts := make([]any, 0, len(data))
for _, p := range data {

		pm := make(map[string]any)
pm["x"] = p.X

		pm["y"] = p.Y

		if strings.TrimSpace(p.Label) != "" {

			pm["label"] = strings.TrimSpace(p.Label)

		}
		if p.Meta != nil && len(p.Meta) > 0 {

			pm["meta"] = sortedAnyMap(p.Meta)

		}
		outPts = append(outPts, pm)

	}
	series["data"] = outPts

	arr, _ := b.spec["series"].([]any)
if arr == nil {

		arr = make([]any, 0)

	}
	arr = append(arr, series)
b.spec["series"] = arr

	return b
}
func (b *Builder) AddAnomalies(name string, marks []AnomalyMarker) *Builder {

	b.mu.Lock()
defer b.mu.Unlock()
name = strings.TrimSpace(name)
if name == "" {

		name = "anomalies"

	}

	// stable sort by X (numeric/time if possible), then score desc, then reason asc

	cp := make([]AnomalyMarker, 0, len(marks))
for _, m := range marks {

		cp = append(cp, m)

	}
	sort.SliceStable(cp, func(i, j int) bool {

		xi, okI := xToFloat(cp[i].X, cp[i].Ts)
xj, okJ := xToFloat(cp[j].X, cp[j].Ts)
if okI && okJ {

			if xi == xj {

				if cp[i].Score == cp[j].Score {

					return strings.TrimSpace(cp[i].Reason) < strings.TrimSpace(cp[j].Reason)

				}
				return cp[i].Score > cp[j].Score

			}
			return xi < xj

		}

		// fallback: by Ts string

		if cp[i].Ts != cp[j].Ts {

			return cp[i].Ts < cp[j].Ts

		}
		if cp[i].Score == cp[j].Score {

			return strings.TrimSpace(cp[i].Reason) < strings.TrimSpace(cp[j].Reason)

		}
		return cp[i].Score > cp[j].Score

	})
ann, _ := b.spec["annotations"].([]any)
if ann == nil {

		ann = make([]any, 0)

	}

	// Add annotations array entries

	for _, m := range cp {

		a := make(map[string]any)
a["name"] = name

		if m.Ts != "" {

			a["ts"] = m.Ts

		}
		if m.X != nil {

			a["x"] = m.X

		}
		a["y"] = m.Y

		if m.Score != 0 {

			a["score"] = m.Score

		}
		if strings.TrimSpace(m.Direction) != "" {

			a["direction"] = strings.TrimSpace(m.Direction)

		}
		if strings.TrimSpace(m.Reason) != "" {

			a["reason"] = strings.TrimSpace(m.Reason)

		}
		if m.Meta != nil && len(m.Meta) > 0 {

			a["meta"] = sortedStringMap(m.Meta)

		}
		ann = append(ann, a)

	}
	b.spec["annotations"] = ann

	// Also add a dedicated scatter series for anomalies for easy plotting in many clients.

	pts := make([]XYPoint, 0, len(cp))
for _, m := range cp {

		x := m.X

		if x == nil && m.Ts != "" {

			x = m.Ts

		}
		pts = append(pts, XYPoint{

			X: x,

			Y: m.Y,

			Label: m.Reason,

			Meta: map[string]any{

				"score": m.Score,

				"direction": m.Direction,
			},
		})

	}

	// Since we're already holding the lock, call internal addSeries helper directly

	kind := "scatter"

	series := make(map[string]any)
series["name"] = name

	series["kind"] = kind

	series["meta"] = map[string]any{"role": "anomaly_overlay"}
	outPts := make([]any, 0, len(pts))
for _, p := range pts {

		pm := make(map[string]any)
pm["x"] = p.X

		pm["y"] = p.Y

		if strings.TrimSpace(p.Label) != "" {

			pm["label"] = strings.TrimSpace(p.Label)

		}
		if p.Meta != nil && len(p.Meta) > 0 {

			pm["meta"] = sortedAnyMap(p.Meta)

		}
		outPts = append(outPts, pm)

	}
	series["data"] = outPts

	arr, _ := b.spec["series"].([]any)
if arr == nil {

		arr = make([]any, 0)

	}
	arr = append(arr, series)
b.spec["series"] = arr

	return b
}
func (b *Builder) Build() (Spec, error) {

	b.mu.Lock()
defer b.mu.Unlock()
ct, _ := b.spec["type"].(string)
ct = strings.TrimSpace(ct)
if ct == "" {

		return nil, errors.New("chart type missing")

	}
	title, _ := b.spec["title"].(string)
if strings.TrimSpace(title) == "" {

		return nil, errors.New("chart title missing")

	}

	// Make a deep-ish copy to avoid mutation after build. Ensure meta is stable (sorted map).

	out := make(Spec, len(b.spec))
for k, v := range b.spec {

		out[k] = v

	}

	// Ensure meta is deterministically ordered in JSON by converting to map with sorted insertion.

	if meta, ok := out["meta"].(map[string]any); ok && meta != nil {

		out["meta"] = sortedAnyMap(meta)

	}

	// Ensure annotations sorted (they already are), but enforce deterministic stable ordering

	if ann, ok := out["annotations"].([]any); ok && ann != nil {

		// nothing to do; entries are added in deterministic order from AddAnomalies

		out["annotations"] = ann

	}
	return out, nil
}
func ToJSON(spec Spec, indent bool) ([]byte, error) {

	if spec == nil {

		return nil, errors.New("spec is nil")

	}
	if indent {

		return json.MarshalIndent(spec, "", "  ")

	}
	return json.Marshal(spec)
}

////////////////////////////////////////////////////////////////////////////////
// Downsampling
////////////////////////////////////////////////////////////////////////////////

func downsample(pts []XYPoint, maxN int) []XYPoint {

if maxN <= 0 || len(pts) <= maxN {

		cp := make([]XYPoint, len(pts))
copy(cp, pts)
		return cp

	}
	if maxN < 3 {

		// preserve head/tail only

		return []XYPoint{pts[0], pts[len(pts)-1]}

	}

	// Prefer LTTB if we can produce numeric x

	xs := make([]float64, len(pts))
okAll := true

	for i := range pts {

		x, ok := xToFloat(pts[i].X, "")
if !ok {

			okAll = false

			break

		}
		xs[i] = x

	}
	if okAll {

		return downsampleLTTB(pts, xs, maxN)

	}

	// Fallback: deterministic stride sampling + preserve endpoints

	out := make([]XYPoint, 0, maxN)
out = append(out, pts[0])
need := maxN - 2

	step := float64(len(pts)-2) / float64(need+1)
for i := 1; i <= need; i++ {

		idx := 1 + int(math.Floor(float64(i)
*step))
if idx <= 0 {

			idx = 1

		}
		if idx >= len(pts)-1 {

			idx = len(pts) - 2

		}
		out = append(out, pts[idx])

	}
out = append(out, pts[len(pts)-1])
return out
}

// Largest-Triangle-Three-Buckets downsampling.
// Deterministic, preserves first and last.
func downsampleLTTB(pts []XYPoint, xs []float64, threshold int) []XYPoint {

	n := len(pts)
if threshold >= n || threshold == 0 {

		cp := make([]XYPoint, n)
copy(cp, pts)
		return cp

	}
	if threshold < 3 {

		return []XYPoint{pts[0], pts[n-1]}

	}
	sampled := make([]XYPoint, 0, threshold)
sampled = append(sampled, pts[0])

	// buckets excluding first and last

	every := float64(n-2) / float64(threshold-2)
a := 0 // index of previously selected

	for i := 0; i < threshold-2; i++ {

		// bucket range [rangeStart, rangeEnd)
rangeStart := int(math.Floor(float64(i)
*every)) + 1

		rangeEnd := int(math.Floor(float64(i+1)
*every)) + 1

		if rangeEnd >= n {

			rangeEnd = n - 1

		}
		avgRangeStart := int(math.Floor(float64(i+1)
*every)) + 1

		avgRangeEnd := int(math.Floor(float64(i+2)
*every)) + 1

		if avgRangeEnd >= n {

			avgRangeEnd = n

		}
		if avgRangeStart >= n {

			avgRangeStart = n - 1

		}
		if avgRangeEnd <= avgRangeStart {

			avgRangeEnd = avgRangeStart + 1

			if avgRangeEnd > n {

				avgRangeEnd = n

			}

		}

		// avg for next bucket

		var avgX, avgY float64

		avgCount := 0

		for j := avgRangeStart; j < avgRangeEnd; j++ {

			avgX += xs[j]

			avgY += pts[j].Y

			avgCount++

		}
		if avgCount > 0 {

			avgX /= float64(avgCount)
avgY /= float64(avgCount)

		} else {

			avgX = xs[minInt(n-1, avgRangeStart)]

			avgY = pts[minInt(n-1, avgRangeStart)].Y

		}

		// find point in current bucket that forms max triangle with a and avg

		ax := xs[a]

		ay := pts[a].Y

		maxArea := -1.0

		maxIdx := rangeStart

		for j := rangeStart; j < rangeEnd; j++ {

			// area = abs((ax-avgX)
*(y-ay) - (ax-x)
*(avgY-ay)) / 2

			// constant /2 ignored

			area := math.Abs((ax-avgX)
*(pts[j].Y-ay) - (ax-xs[j])
*(avgY-ay))
if area > maxArea {

				maxArea = area

				maxIdx = j

				continue

			}

			// tie-breaker: earlier index (stable determinism)
if area == maxArea && j < maxIdx {

				maxIdx = j

			}

		}
		sampled = append(sampled, pts[maxIdx])
a = maxIdx

	}
sampled = append(sampled, pts[n-1])
return sampled
}

////////////////////////////////////////////////////////////////////////////////
// Helpers (deterministic ordering + timestamp parsing)
////////////////////////////////////////////////////////////////////////////////

func xToFloat(x any, tsFallback string) (float64, bool) {

	if x == nil {

		if strings.TrimSpace(tsFallback) == "" {

			return 0, false

		}
		t, err := parseRFC3339(tsFallback)
if err != nil {

			return 0, false

		}
		return float64(t.UnixNano()) / 1e9, true

	}
	switch v := x.(type) {

	case float64:

		if math.IsNaN(v) || math.IsInf(v, 0) {

			return 0, false

		}
		return v, true

	case float32:

		f := float64(v)
if math.IsNaN(f) || math.IsInf(f, 0) {

			return 0, false

		}
		return f, true

	case int:

		return float64(v), true

	case int64:

		return float64(v), true

	case int32:

		return float64(v), true

	case uint:

		return float64(v), true

	case uint64:

		return float64(v), true

	case uint32:

		return float64(v), true

	case string:

		s := strings.TrimSpace(v)
if s == "" {

			return 0, false

		}

		// try time first

		if t, err := parseRFC3339(s); err == nil {

			return float64(t.UnixNano()) / 1e9, true

		}

		// try numeric

		if f, ok := parseFloatLoose(s); ok {

			return f, true

		}
		return 0, false

	default:

		return 0, false

	}
}
func parseRFC3339(ts string) (time.Time, error) {

	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {

		return t.UTC(), nil

	}
	t, err := time.Parse(time.RFC3339, ts)
if err != nil {

		return time.Time{}, err

	}
	return t.UTC(), nil
}
func parseFloatLoose(s string) (float64, bool) {

	// deterministic, minimal parser: allow digits, sign, dot

	s = strings.TrimSpace(s)
if s == "" {

		return 0, false

	}

	// use strconv-free approach? Keep stdlib only; strconv is stdlib but we can avoid extra import.

	// We do a very small parse using fmt.Sscanf deterministically.

	var f float64

	_, err := fmt.Sscanf(s, "%f", &f)
if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {

		return 0, false

	}
	return f, true
}
func sortedStringMap(m map[string]string) map[string]any {

	keys := make([]string, 0, len(m))
for k := range m {

		k2 := strings.TrimSpace(k)
if k2 == "" {

			continue

		}
		keys = append(keys, k2)

	}
	sort.Strings(keys)
out := make(map[string]any, len(keys))
for _, k := range keys {

		out[k] = strings.TrimSpace(m[k])

	}
	return out
}
func sortedAnyMap(m map[string]any) map[string]any {

	keys := make([]string, 0, len(m))
for k := range m {

		k2 := strings.TrimSpace(k)
if k2 == "" {

			continue

		}
		keys = append(keys, k2)

	}
	sort.Strings(keys)
out := make(map[string]any, len(keys))
for _, k := range keys {

		out[k] = m[k]

	}
	return out
}
func minInt(a, b int) int {

	if a < b {

		return a

	}
	return b
}
