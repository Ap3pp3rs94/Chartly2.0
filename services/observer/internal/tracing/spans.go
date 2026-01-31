package tracing

// Span builder + normalization helpers (deterministic, stdlib-only).
//
// This file provides builder-style construction for Span values (as defined in jaeger.go),
// plus deterministic normalization and sorting utilities.
//
// Determinism guarantees:
//   - No randomness.
//   - No time.Now usage (caller provides timestamps).
//   - Tags and log fields are normalized (trim, remove NUL, collapse spaces) and stored in canonical form.
//   - Trace normalization sorts spans by (start time, span_id) deterministically.
//   - Logs are sorted by (ts, canonical fields) deterministically.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrSpan        = errors.New("span failed")
	ErrSpanInvalid = errors.New("span invalid")
)

type SpanBuilder struct {
	traceID      string
	spanID       string
	parentSpanID string
	op           string
	start        string
	end          string
	tags         map[string]string
	logs         []LogRecord
}

func NewSpanBuilder(traceID, spanID, op string) *SpanBuilder {
	return &SpanBuilder{
		traceID: normCollapse(traceID),
		spanID:  normCollapse(spanID),
		op:      normCollapse(op),
		tags:    make(map[string]string),
		logs:    make([]LogRecord, 0, 4),
	}
}

func (b *SpanBuilder) Parent(parentSpanID string) *SpanBuilder {
	b.parentSpanID = normCollapse(parentSpanID)
	return b
}

func (b *SpanBuilder) TimeWindow(start, end string) *SpanBuilder {
	b.start = normCollapse(start)
	b.end = normCollapse(end)
	return b
}

func (b *SpanBuilder) Tag(k, v string) *SpanBuilder {
	k = normCollapse(k)
	if k == "" {
		return b
	}
	if b.tags == nil {
		b.tags = make(map[string]string)
	}
	b.tags[k] = normCollapse(v)
	return b
}

func (b *SpanBuilder) Log(ts string, fields map[string]string) *SpanBuilder {
	lr := LogRecord{
		TS:     normCollapse(ts),
		Fields: normalizeStringMap(fields),
	}
	b.logs = append(b.logs, lr)
	return b
}

func (b *SpanBuilder) Build() (Span, error) {
	if b == nil {
		return Span{}, fmt.Errorf("%w: %w: builder nil", ErrSpan, ErrSpanInvalid)
	}
	tid := normCollapse(b.traceID)
	sid := normCollapse(b.spanID)
	op := normCollapse(b.op)
	st := normCollapse(b.start)
	et := normCollapse(b.end)

	if tid == "" || sid == "" || op == "" {
		return Span{}, fmt.Errorf("%w: %w: trace_id/span_id/operation required", ErrSpan, ErrSpanInvalid)
	}
	if st == "" || et == "" {
		return Span{}, fmt.Errorf("%w: %w: start/end required", ErrSpan, ErrSpanInvalid)
	}

	startT, err := parseRFC3339(st)
	if err != nil {
		return Span{}, fmt.Errorf("%w: %w: invalid start", ErrSpan, ErrSpanInvalid)
	}
	endT, err := parseRFC3339(et)
	if err != nil {
		return Span{}, fmt.Errorf("%w: %w: invalid end", ErrSpan, ErrSpanInvalid)
	}
	if endT.Before(startT) {
		return Span{}, fmt.Errorf("%w: %w: end before start", ErrSpan, ErrSpanInvalid)
	}

	sp := Span{
		TraceID:      tid,
		SpanID:       sid,
		ParentSpanID: normCollapse(b.parentSpanID),
		Operation:    op,
		Start:        startT.UTC().Format(time.RFC3339Nano),
		End:          endT.UTC().Format(time.RFC3339Nano),
		Tags:         normalizeStringMap(b.tags),
		Logs:         normalizeLogs(b.logs),
	}
	return sp, nil
}

// NormalizeTrace validates and normalizes a trace deterministically, sorting spans by (start, span_id).
func NormalizeTrace(t Trace) (Trace, error) {
	out := Trace{
		TraceID: normCollapse(t.TraceID),
		Spans:   make([]Span, 0, len(t.Spans)),
	}
	if out.TraceID == "" {
		return Trace{}, fmt.Errorf("%w: %w: trace_id required", ErrSpan, ErrSpanInvalid)
	}

	for _, sp := range t.Spans {
		ns, err := normalizeSpan(sp, out.TraceID)
		if err != nil {
			return Trace{}, err
		}
		out.Spans = append(out.Spans, ns)
	}

	sort.Slice(out.Spans, func(i, j int) bool {
		ti, _ := parseRFC3339(out.Spans[i].Start)
		tj, _ := parseRFC3339(out.Spans[j].Start)
		if ti.Before(tj) {
			return true
		}
		if ti.After(tj) {
			return false
		}
		return out.Spans[i].SpanID < out.Spans[j].SpanID
	})

	return out, nil
}

////////////////////////////////////////////////////////////////////////////////
// Internal normalization helpers
////////////////////////////////////////////////////////////////////////////////

func normalizeSpan(sp Span, traceID string) (Span, error) {
	s := Span{
		TraceID:      normCollapse(sp.TraceID),
		SpanID:       normCollapse(sp.SpanID),
		ParentSpanID: normCollapse(sp.ParentSpanID),
		Operation:    normCollapse(sp.Operation),
		Start:        normCollapse(sp.Start),
		End:          normCollapse(sp.End),
		Tags:         normalizeStringMap(sp.Tags),
		Logs:         normalizeLogs(sp.Logs),
	}
	if s.TraceID == "" {
		s.TraceID = traceID
	}
	if s.TraceID != traceID {
		return Span{}, fmt.Errorf("%w: %w: span trace_id mismatch", ErrSpan, ErrSpanInvalid)
	}
	if s.SpanID == "" || s.Operation == "" || s.Start == "" || s.End == "" {
		return Span{}, fmt.Errorf("%w: %w: span_id/operation/start/end required", ErrSpan, ErrSpanInvalid)
	}
	st, err := parseRFC3339(s.Start)
	if err != nil {
		return Span{}, fmt.Errorf("%w: %w: invalid start", ErrSpan, ErrSpanInvalid)
	}
	et, err := parseRFC3339(s.End)
	if err != nil {
		return Span{}, fmt.Errorf("%w: %w: invalid end", ErrSpan, ErrSpanInvalid)
	}
	if et.Before(st) {
		return Span{}, fmt.Errorf("%w: %w: end before start", ErrSpan, ErrSpanInvalid)
	}

	s.Start = st.UTC().Format(time.RFC3339Nano)
	s.End = et.UTC().Format(time.RFC3339Nano)
	return s, nil
}

func normalizeLogs(in []LogRecord) []LogRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]LogRecord, 0, len(in))
	for _, lr := range in {
		n := LogRecord{
			TS:     normCollapse(lr.TS),
			Fields: normalizeStringMap(lr.Fields),
		}
		if n.TS != "" {
			if _, err := parseRFC3339(n.TS); err != nil {
				continue
			}
		} else {
			n.TS = time.Unix(0, 0).UTC().Format(time.RFC3339Nano)
		}
		out = append(out, n)
	}

	sort.Slice(out, func(i, j int) bool {
		ti, _ := parseRFC3339(out[i].TS)
		tj, _ := parseRFC3339(out[j].TS)
		if ti.Before(tj) {
			return true
		}
		if ti.After(tj) {
			return false
		}
		return canonicalFields(out[i].Fields) < canonicalFields(out[j].Fields)
	})
	return out
}

func normalizeStringMap(m map[string]string) map[string]string {
	if m == nil || len(m) == 0 {
		return map[string]string{}
	}

	tmp := make(map[string]string, len(m))
	for k, v := range m {
		kk := normCollapse(k)
		if kk == "" {
			continue
		}
		tmp[kk] = normCollapse(v)
	}

	keys := make([]string, 0, len(tmp))
	for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = tmp[k]
	}
	return out
}

func canonicalFields(m map[string]string) string {
	if m == nil || len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(";")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(m[k])
	}
	return b.String()
}

func parseRFC3339(s string) (time.Time, error) {
	s = normCollapse(s)
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func normCollapse(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
