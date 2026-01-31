package timeseries

// Time-series compaction utilities (deterministic, library-only).
//
// This file provides a compaction/merge layer that can take decoded chunks (DecodedChunk/DecodedSeries)
// or explicit series point sets and produce a deterministically merged output, optionally flushing
// through the existing Writer + Sink abstraction.
//
// Design goals:
//   - Determinism: same input -> same output ordering and byte content when passed through Writer.Flush.
//   - Safety: no panics, strict validation, multi-tenant/namespace enforcement.
//   - Practicality: supports de-duplication, caps, and stable truncation rules.
//
// What this does NOT do:
//   - No HTTP handlers, no filesystem writes, no network calls.
//   - No "smart" time bucketing or downsampling (future feature; out of scope).
//
// Duplicate policy:
//   - Points are grouped by timestamp within a series; duplicates are resolved deterministically.
//   - Default policy keeps the first point after deterministic sort (TS asc, Value asc, Meta canonical asc).
//   - Optional policy can keep the last point after that same ordering.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrCompact        = errors.New("compact failed")
	ErrCompactInvalid = errors.New("compact invalid")
	ErrCompactTooLarge = errors.New("compact too large")
)

// DuplicatePolicy defines how compaction handles multiple points with the same timestamp in a series.
type DuplicatePolicy string

const (
	// KeepFirst keeps the first point after deterministic sort (stable default).
	KeepFirst DuplicatePolicy = "keep_first"
	// KeepLast keeps the last point after deterministic sort (still deterministic).
	KeepLast DuplicatePolicy = "keep_last"
)

type CompactionOptions struct {
	// Caps and limits. If <=0, defaults are applied.
	MaxSeriesPerChunk  int
	MaxPointsPerChunk  int
	MaxPointsPerSeries int

	// Numeric validation (reject NaN/Inf unless true).
	AllowNaN bool

	// Duplicate handling.
	Deduplicate     bool
	DuplicatePolicy DuplicatePolicy

	// If true, points outside [meta.Start, meta.End) will be dropped (deterministically) when producing a chunk.
	// If false, they are kept but warnings are emitted.
	DropOutOfRange bool
}

type SeriesPoints struct {
	Key    SeriesKey
	Points []Point
}

// CompactResult includes deterministic warnings and counts for debugging/telemetry.
type CompactResult struct {
	Warnings map[string]string // deterministic keys: compact_warning.001, ...
	Series   int
	Points   int
	Dropped  int
}

// CompactDecodedChunks merges multiple decoded chunks into a deterministic series+points set.
// It enforces that all chunks share the same tenant and namespace; otherwise it returns an error.
func CompactDecodedChunks(chunks []DecodedChunk, opts CompactionOptions) ([]SeriesPoints, CompactResult, error) {
	o := normalizeCompactionOptions(opts)

	if len(chunks) == 0 {
		return nil, CompactResult{}, fmt.Errorf("%w: %w: no chunks", ErrCompact, ErrCompactInvalid)
	}

	tenant := normalizeString(chunks[0].Meta.TenantID)
	ns := normalizeString(chunks[0].Meta.Namespace)
	if tenant == "" || ns == "" {
		return nil, CompactResult{}, fmt.Errorf("%w: %w: missing tenant/namespace", ErrCompact, ErrCompactInvalid)
	}

	// Collect series into a list (avoid maps for determinism; we will sort+merge).
	var collected []SeriesPoints

	for i := range chunks {
		m := chunks[i].Meta
		if normalizeString(m.TenantID) != tenant || normalizeString(m.Namespace) != ns {
			return nil, CompactResult{}, fmt.Errorf("%w: %w: mixed tenant/namespace", ErrCompact, ErrCompactInvalid)
		}

		for _, ds := range chunks[i].Series {
			k := normalizeSeriesKey(ds.Key)
			if err := validateSeriesKey(k); err != nil {
				return nil, CompactResult{}, fmt.Errorf("%w: %w: %v", ErrCompact, ErrCompactInvalid, err)
			}

			pts := make([]Point, 0, len(ds.Points))
			for _, dp := range ds.Points {
				pts = append(pts, Point{TS: normalizeString(dp.TS), Value: dp.Value, Meta: nil})
			}

			collected = append(collected, SeriesPoints{Key: k, Points: pts})
		}
	}

	return CompactSeriesPoints(collected, o)
}

// CompactSeriesPoints merges an explicit list of series+points into a deterministic compacted output.
// It does NOT require a meta window; it simply merges and applies deterministic caps/dedupe.
// Use CompactToChunk to enforce a ChunkMeta window and flush via Writer+Sink.
func CompactSeriesPoints(in []SeriesPoints, opts CompactionOptions) ([]SeriesPoints, CompactResult, error) {
	o := normalizeCompactionOptions(opts)

	if len(in) == 0 {
		return nil, CompactResult{}, fmt.Errorf("%w: %w: no data", ErrCompact, ErrCompactInvalid)
	}

	// Normalize+validate and explode into per-series buffers.
	// We use a map internally but NEVER iterate without sorting keys.
	byKey := make(map[string]*seriesBuf, len(in))

	for i := range in {
		k := normalizeSeriesKey(in[i].Key)
		if err := validateSeriesKey(k); err != nil {
			return nil, CompactResult{}, fmt.Errorf("%w: %w: %v", ErrCompact, ErrCompactInvalid, err)
		}

		sk := seriesKeyString(k)
		sb := byKey[sk]
		if sb == nil {
			sb = &seriesBuf{key: k, points: nil}
			byKey[sk] = sb
		}

		// Do not mutate caller points.
		for _, p := range in[i].Points {
			pn := normalizePoint(p)
			if err := validatePoint(pn, o.AllowNaN); err != nil {
				return nil, CompactResult{}, fmt.Errorf("%w: %w: %v", ErrCompact, ErrCompactInvalid, err)
			}
			ts, err := parseRFC3339Point(pn.TS)
			if err != nil {
				return nil, CompactResult{}, fmt.Errorf("%w: %w: %v", ErrCompact, ErrCompactInvalid, err)
			}
			sb.points = append(sb.points, pointBuf{ts: ts, tsS: pn.TS, val: pn.Value, meta: normalizeStringMap(pn.Meta)})
		}
	}

	// Deterministic series ordering.
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Apply MaxSeriesPerChunk deterministically (truncate after sorting).
	warnings := make([]string, 0, 8)
	if o.MaxSeriesPerChunk > 0 && len(keys) > o.MaxSeriesPerChunk {
		warnings = append(warnings, fmt.Sprintf("series_truncated:showing=%d of=%d", o.MaxSeriesPerChunk, len(keys)))
		keys = keys[:o.MaxSeriesPerChunk]
	}

	out := make([]SeriesPoints, 0, len(keys))
	totalPts := 0
	dropped := 0

	// Sort points, dedupe, apply per-series caps.
	for _, sk := range keys {
		sb := byKey[sk]
		if sb == nil {
			continue
		}

		// Deterministic point ordering: TS asc, Value asc, Meta canonical string asc.
		sort.Slice(sb.points, func(i, j int) bool {
			a := sb.points[i]
			b := sb.points[j]
			if a.ts.Before(b.ts) {
				return true
			}
			if a.ts.After(b.ts) {
				return false
			}
			if a.val < b.val {
				return true
			}
			if a.val > b.val {
				return false
			}
			return canonicalMetaString(a.meta) < canonicalMetaString(b.meta)
		})

		comp := sb.points
		if o.Deduplicate && len(comp) > 1 {
			comp = dedupePointsByTS(comp, o.DuplicatePolicy)
		}

		// Per-series cap
		if o.MaxPointsPerSeries > 0 && len(comp) > o.MaxPointsPerSeries {
			warnings = append(warnings, fmt.Sprintf("series_points_truncated:%s:showing=%d of=%d", sk, o.MaxPointsPerSeries, len(comp)))
			dropped += (len(comp) - o.MaxPointsPerSeries)
			comp = comp[:o.MaxPointsPerSeries]
		}

		// Convert back to exported Point (Meta preserved).
		pts := make([]Point, 0, len(comp))
		for i := range comp {
			pts = append(pts, Point{
				TS:    comp[i].ts.UTC().Format(time.RFC3339Nano),
				Value: comp[i].val,
				Meta:  normalizeStringMap(comp[i].meta),
			})
		}

		totalPts += len(pts)
		out = append(out, SeriesPoints{Key: sb.key, Points: pts})
	}

	// Global point cap across chunk: deterministic truncation across series in sorted order.
	if o.MaxPointsPerChunk > 0 && totalPts > o.MaxPointsPerChunk {
		orig := totalPts
		remain := o.MaxPointsPerChunk
		for i := range out {
			if remain <= 0 {
				dropped += len(out[i].Points)
				out[i].Points = nil
				continue
			}

			if len(out[i].Points) > remain {
				dropped += len(out[i].Points) - remain
				out[i].Points = out[i].Points[:remain]
				remain = 0
			} else {
				remain -= len(out[i].Points)
			}
		}

		// Drop empty series deterministically.
		pruned := make([]SeriesPoints, 0, len(out))
		for _, sp := range out {
			if len(sp.Points) > 0 {
				pruned = append(pruned, sp)
			}
		}
		out = pruned

		warnings = append(warnings, fmt.Sprintf("points_truncated:showing=%d of=%d", o.MaxPointsPerChunk, orig))
		totalPts = o.MaxPointsPerChunk
	}

	if len(out) == 0 {
		return nil, CompactResult{}, fmt.Errorf("%w: %w: no data after compaction", ErrCompact, ErrCompactTooLarge)
	}

	// Build deterministic warning map.
	wm := make(map[string]string, len(warnings))
	for i, w := range warnings {
		wm[fmt.Sprintf("compact_warning.%03d", i+1)] = w
	}

	return out, CompactResult{
		Warnings: wm,
		Series:   len(out),
		Points:   totalPts,
		Dropped:  dropped,
	}, nil
}

// CompactToChunk compacts series and flushes them through Writer into a Sink as a single CHTS1 chunk.
// - Enforces meta.TenantID and meta.Namespace across all series.
// - Enforces [meta.Start, meta.End) window by either dropping or warning depending on opts.DropOutOfRange.
// - Uses WriterOptions caps derived from CompactionOptions in a deterministic manner.
func CompactToChunk(ctx context.Context, sink Sink, meta ChunkMeta, objectKeyPrefix string, series []SeriesPoints, opts CompactionOptions) (ChunkRef, CompactResult, error) {
	o := normalizeCompactionOptions(opts)

	if sink == nil {
		return ChunkRef{}, CompactResult{}, fmt.Errorf("%w: %w: sink is nil", ErrCompact, ErrSink)
	}

	m := normalizeChunkMeta(meta)
	if err := validateChunkMeta(m); err != nil {
		return ChunkRef{}, CompactResult{}, fmt.Errorf("%w: %w: %v", ErrCompact, ErrCompactInvalid, err)
	}

	startT, err := parseRFC3339MetaWindow(m.Start)
	if err != nil {
		return ChunkRef{}, CompactResult{}, err
	}
	endT, err := parseRFC3339MetaWindow(m.End)
	if err != nil {
		return ChunkRef{}, CompactResult{}, err
	}
	if !endT.After(startT) {
		return ChunkRef{}, CompactResult{}, fmt.Errorf("%w: %w: end must be after start", ErrCompact, ErrCompactInvalid)
	}

	// Compact first (merge/dedupe/caps).
	compactSeries, res, err := CompactSeriesPoints(series, o)
	if err != nil {
		return ChunkRef{}, CompactResult{}, fmt.Errorf("%w: %v", ErrCompact, err)
	}

	// Enforce tenant/namespace and window rules deterministically.
	winDropped := 0
	winWarn := make([]string, 0, 4)

	for i := range compactSeries {
		k := normalizeSeriesKey(compactSeries[i].Key)
		if k.TenantID != normalizeString(m.TenantID) || k.Namespace != normalizeString(m.Namespace) {
			return ChunkRef{}, CompactResult{}, fmt.Errorf("%w: %w: series tenant/namespace mismatch", ErrCompact, ErrCompactInvalid)
		}

		if len(compactSeries[i].Points) == 0 {
			continue
		}

		kept := make([]Point, 0, len(compactSeries[i].Points))
		outside := 0

		for _, p := range compactSeries[i].Points {
			ts, e := parseRFC3339Point(p.TS)
			if e != nil {
				return ChunkRef{}, CompactResult{}, fmt.Errorf("%w: %w: %v", ErrCompact, ErrCompactInvalid, e)
			}
			if ts.Before(startT) || !ts.Before(endT) {
				outside++
				if o.DropOutOfRange {
					continue
				}
			}
			kept = append(kept, p)
		}

		if outside > 0 && !o.DropOutOfRange {
			winWarn = append(winWarn, fmt.Sprintf("points_outside_meta_window:%s:count=%d", seriesKeyString(k), outside))
		}
		if o.DropOutOfRange {
			winDropped += outside
		}

		compactSeries[i].Points = kept
	}

	// Prune empty series deterministically.
	pruned := make([]SeriesPoints, 0, len(compactSeries))
	for _, sp := range compactSeries {
		if len(sp.Points) > 0 {
			pruned = append(pruned, sp)
		}
	}
	compactSeries = pruned
	if len(compactSeries) == 0 {
		return ChunkRef{}, CompactResult{}, fmt.Errorf("%w: %w: no data in window", ErrCompact, ErrCompactTooLarge)
	}

	// Merge warnings deterministically into meta.Meta
	m.Meta = normalizeStringMap(m.Meta)

	// Existing compaction warnings first.
	wkeys := make([]string, 0, len(res.Warnings))
	for k := range res.Warnings {
		wkeys = append(wkeys, k)
	}
	sort.Strings(wkeys)
	for _, k := range wkeys {
		m.Meta[k] = res.Warnings[k]
	}

	// Window warnings next.
	sort.Strings(winWarn)
	for i := range winWarn {
		m.Meta[fmt.Sprintf("compact_window_warning.%03d", i+1)] = winWarn[i]
	}

	if strings.TrimSpace(m.SchemaVersion) == "" {
		m.SchemaVersion = "v1"
	}

	wo := WriterOptions{
		CompressBody:      false, // compactor does not decide compression by default
		MaxPointsPerChunk: o.MaxPointsPerChunk,
		MaxSeriesPerChunk: o.MaxSeriesPerChunk,
		AllowNaN:          o.AllowNaN,
	}
	w := NewWriter(wo)

	for _, sp := range compactSeries {
		if err := w.AddSeriesPoints(sp.Key, sp.Points); err != nil {
			return ChunkRef{}, CompactResult{}, fmt.Errorf("%w: %w: %v", ErrCompact, ErrCompactInvalid, err)
		}
	}

	ref, err := w.Flush(ctx, sink, m, objectKeyPrefix)
	if err != nil {
		return ChunkRef{}, CompactResult{}, fmt.Errorf("%w: %v", ErrCompact, err)
	}

	res.Dropped += winDropped
	if res.Warnings == nil {
		res.Warnings = map[string]string{}
	}
	for i := range winWarn {
		res.Warnings[fmt.Sprintf("compact_window_warning.%03d", i+1)] = winWarn[i]
	}

	return ref, res, nil
}

////////////////////////////////////////////////////////////////////////////////
// Internal helpers (deterministic)
////////////////////////////////////////////////////////////////////////////////

func normalizeCompactionOptions(opts CompactionOptions) CompactionOptions {
	o := opts

	if o.MaxSeriesPerChunk <= 0 {
		o.MaxSeriesPerChunk = 5000
	}
	if o.MaxPointsPerChunk <= 0 {
		o.MaxPointsPerChunk = 200000
	}
	if o.MaxPointsPerSeries <= 0 {
		o.MaxPointsPerSeries = 0
	}

	if strings.TrimSpace(string(o.DuplicatePolicy)) == "" {
		o.DuplicatePolicy = KeepFirst
	}
	if o.DuplicatePolicy != KeepFirst && o.DuplicatePolicy != KeepLast {
		o.DuplicatePolicy = KeepFirst
	}

	// Deduplicate: default false unless explicitly set by caller.
	o.Deduplicate = opts.Deduplicate

	return o
}

func dedupePointsByTS(sorted []pointBuf, policy DuplicatePolicy) []pointBuf {
	if len(sorted) <= 1 {
		return sorted
	}

	out := make([]pointBuf, 0, len(sorted))
	i := 0
	for i < len(sorted) {
		j := i + 1
		for j < len(sorted) && sorted[j].ts.Equal(sorted[i].ts) {
			j++
		}

		// range [i, j) shares same timestamp; deterministic ordering already applied.
		if policy == KeepLast {
			out = append(out, sorted[j-1])
		} else {
			out = append(out, sorted[i])
		}

		i = j
	}

	return out
}

func parseRFC3339MetaWindow(s string) (time.Time, error) {
	s = normalizeString(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("%w: %w: meta time required", ErrCompact, ErrCompactInvalid)
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%w: %w: invalid meta time", ErrCompact, ErrCompactInvalid)
}
