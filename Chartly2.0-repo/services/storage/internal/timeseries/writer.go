package timeseries

// CHTS1  Chartly TimeSeries Chunk Format (v1)
//
// This package provides a deterministic, production-grade encoder for time-series chunks.
// It is designed to be used by the storage service to persist time-series data in a stable,
// reproducible form (same input => same output bytes), enabling content-addressable storage,
// caching, and integrity verification.
//
// On-wire format (high level):
//   Header:
//     Magic      : 5 bytes  "CHTS1"
//     Version    : uint16   (1)
//     Flags      : uint16   (bit0=gzip body)
//     MetaLen    : uint32   length in bytes of MetaJSON
//     MetaJSON   : canonical JSON (sorted keys)
//   Body:
//     SeriesCount: uint32
//     For each series (sorted by deterministic SeriesKey string):
//       SeriesKeyLen : uint32
//       SeriesKeyJSON: canonical JSON (sorted keys)
//       PointsCount  : uint32
//       BaseTS       : int64 (unix nanos)
//       For each point (sorted by TS asc):
//         DeltaTS    : varint (signed)
//         relative to previous TS (first delta = 0)
//         Value      : float64 (IEEE)
//       SeriesCRC32  : uint32 over (SeriesKeyJSON + point encoding bytes)
//     BodyCRC32   : uint32 over entire body (SeriesCount..last SeriesCRC32)
//
// Determinism guarantees:
//   - No randomness, no time.Now.
//   - All map keys are serialized in lexicographic order at all depths.
//   - Series are sorted deterministically; points sorted deterministically.
//   - Optional gzip body compression uses a fixed ModTime (unix epoch)
// and stable settings.

import (
	"bytes"
	"compress/gzip"
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidKey   = errors.New("invalid series key")
	ErrInvalidPoint = errors.New("invalid point")
	ErrInvalidMeta  = errors.New("invalid chunk meta")
	ErrTooLarge     = errors.New("chunk too large")
	ErrEncode       = errors.New("encode failed")
	ErrSink         = errors.New("sink failed")
)

type SeriesKey struct {
	TenantID string `json:"tenant_id"`

	Namespace string `json:"namespace"`

	Metric string `json:"metric"`

	EntityType string `json:"entity_type"`

	EntityID string `json:"entity_id,omitempty"`

	Tags map[string]string `json:"tags,omitempty"`
}
type Point struct {
	TS string `json:"ts"`

	Value float64 `json:"value"`

	Meta map[string]string `json:"meta,omitempty"`
}
type ChunkMeta struct {
	TenantID string `json:"tenant_id"`

	Namespace string `json:"namespace"`

	SourceID string `json:"source_id,omitempty"`

	ProducedAt string `json:"produced_at,omitempty"`

	Start string `json:"start"`

	End string `json:"end"`

	SchemaVersion string `json:"schema_version,omitempty"`

	Meta map[string]string `json:"meta,omitempty"`
}
type ChunkRef struct {
	ObjectKey string `json:"object_key"`

	ContentType string `json:"content_type"`

	Bytes int64 `json:"bytes"`

	SHA256 string `json:"sha256"`

	Start string `json:"start"`

	End string `json:"end"`

	Series int `json:"series"`

	Points int `json:"points"`
}
type WriterOptions struct {
	CompressBody      bool
	MaxPointsPerChunk int
	MaxSeriesPerChunk int
	AllowNaN          bool
}
type Sink interface {
	Put(ctx context.Context, tenantID string, objectKey string, contentType string, data []byte, meta map[string]string) error
}
type Writer struct {
	mu   sync.Mutex
	opts WriterOptions

	// seriesKeyStr -> series buffer

	series map[string]seriesBuf
}
type seriesBuf struct {
	key SeriesKey

	points []pointBuf
}
type pointBuf struct {
	ts  time.Time
	tsS string
	val float64

	meta map[string]string
}

func NewWriter(opts WriterOptions) *Writer {

	o := normalizeWriterOptions(opts)
	return &Writer{

		opts: o,

		series: make(map[string]seriesBuf),
	}
}
func (w *Writer) Reset() {

	w.mu.Lock()
	defer w.mu.Unlock()
	w.series = make(map[string]seriesBuf)
}
func (w *Writer) Stats() map[string]any {

	w.mu.Lock()
	defer w.mu.Unlock()
	series := len(w.series)
	points := 0

	for _, sb := range w.series {

		points += len(sb.points)

	}

	// deterministic key order is not guaranteed by encoding/json anyway, but keep a stable set.

	return map[string]any{

		"series": series,

		"points": points,

		"compress_body": w.opts.CompressBody,

		"max_points_per_chunk": w.opts.MaxPointsPerChunk,

		"max_series_per_chunk": w.opts.MaxSeriesPerChunk,
	}
}
func (w *Writer) AddPoint(key SeriesKey, p Point) error {

	return w.AddSeriesPoints(key, []Point{p})
}
func (w *Writer) AddSeriesPoints(key SeriesKey, points []Point) error {

	k := normalizeSeriesKey(key)
	if err := validateSeriesKey(k); err != nil {

		return err

	}
	if points == nil || len(points) == 0 {

		return nil

	}
	pbs := make([]pointBuf, 0, len(points))
	for i := range points {

		pn := normalizePoint(points[i])
		if err := validatePoint(pn, w.opts.AllowNaN); err != nil {

			return err

		}
		ts, err := parseRFC3339Point(pn.TS)
		if err != nil {

			return err

		}
		pbs = append(pbs, pointBuf{

			ts: ts,

			tsS: pn.TS,

			val: pn.Value,

			meta: normalizeStringMap(pn.Meta),
		})

	}
	w.mu.Lock()
	defer w.mu.Unlock()
	sk := seriesKeyString(k)
	sb := w.series[sk]

	if sb.key.TenantID == "" {

		sb.key = k

	}

	// Append; sorting/dedupe happens at Flush.

	sb.points = append(sb.points, pbs...)
	w.series[sk] = sb

	return nil
}
func (w *Writer) Flush(ctx context.Context, sink Sink, meta ChunkMeta, objectKeyPrefix string) (ChunkRef, error) {

	if sink == nil {

		return ChunkRef{}, fmt.Errorf("%w: sink is nil", ErrSink)

	}
	m := normalizeChunkMeta(meta)
	if err := validateChunkMeta(m); err != nil {

		return ChunkRef{}, err

	}
	startT, err := parseRFC3339Meta(m.Start)
	if err != nil {

		return ChunkRef{}, err

	}
	endT, err := parseRFC3339Meta(m.End)
	if err != nil {

		return ChunkRef{}, err

	}
	if !endT.After(startT) {

		return ChunkRef{}, fmt.Errorf("%w: end must be after start", ErrInvalidMeta)

	}

	// Snapshot under lock

	w.mu.Lock()
	seriesSnapshot := make([]seriesBuf, 0, len(w.series))
	for _, sb := range w.series {

		seriesSnapshot = append(seriesSnapshot, deepCopySeriesBuf(sb))

	}
	w.mu.Unlock()

	// Validate tenant/namespace match

	for i := range seriesSnapshot {

		if seriesSnapshot[i].key.TenantID != m.TenantID {

			return ChunkRef{}, fmt.Errorf("%w: series tenant mismatch", ErrInvalidMeta)

		}
		if seriesSnapshot[i].key.Namespace != m.Namespace {

			return ChunkRef{}, fmt.Errorf("%w: series namespace mismatch", ErrInvalidMeta)

		}

	}

	// Sort series deterministically

	sort.Slice(seriesSnapshot, func(i, j int) bool {

		return seriesKeyString(seriesSnapshot[i].key) < seriesKeyString(seriesSnapshot[j].key)

	})

	// Apply caps with deterministic truncation and warnings.

	warnings := make([]string, 0, 8)
	opts := w.opts

	if opts.MaxSeriesPerChunk > 0 && len(seriesSnapshot) > opts.MaxSeriesPerChunk {

		warnings = append(warnings, fmt.Sprintf("series_truncated:showing=%d of=%d", opts.MaxSeriesPerChunk, len(seriesSnapshot)))
		seriesSnapshot = seriesSnapshot[:opts.MaxSeriesPerChunk]

	}
	totalPts := 0

	for i := range seriesSnapshot {

		// Sort points deterministically (sharded for large series).
		seriesSnapshot[i].points = sortPointsDeterministic(seriesSnapshot[i].points)

		// Dedupe exact timestamp deterministically: keep the first (after sort).

		dedup := make([]pointBuf, 0, len(seriesSnapshot[i].points))
		// var last time.Time

		hasLast := false

		for _, p := range seriesSnapshot[i].points {

			if !hasLast || !p.ts.Equal(last) {

				dedup = append(dedup, p)
				last = p.ts

				hasLast = true

			}

		}
		seriesSnapshot[i].points = dedup

		totalPts += len(dedup)

	}
	if opts.MaxPointsPerChunk > 0 && totalPts > opts.MaxPointsPerChunk {

		// Deterministic truncation: walk series in order and cap points cumulatively.

		remain := opts.MaxPointsPerChunk

		orig := totalPts

		for i := range seriesSnapshot {

			if remain <= 0 {

				seriesSnapshot[i].points = nil

				// continue

			}
			if len(seriesSnapshot[i].points) > remain {

				seriesSnapshot[i].points = seriesSnapshot[i].points[:remain]

				remain = 0

			} else {

				remain -= len(seriesSnapshot[i].points)

			}

		}

		// Drop empty series at end to reduce bloat deterministically.

		pruned := make([]seriesBuf, 0, len(seriesSnapshot))
		for _, sb := range seriesSnapshot {

			if len(sb.points) > 0 {

				pruned = append(pruned, sb)

			}

		}
		seriesSnapshot = pruned

		warnings = append(warnings, fmt.Sprintf("points_truncated:showing=%d of=%d", opts.MaxPointsPerChunk, orig))

	}
	if len(seriesSnapshot) == 0 {

		return ChunkRef{}, fmt.Errorf("%w: no data to flush", ErrTooLarge)

	}

	// Attach warnings to meta deterministically

	m.Meta = normalizeStringMap(m.Meta)
	for i := range warnings {

		m.Meta[fmt.Sprintf("writer_warning.%03d", i+1)] = warnings[i]

	}
	if strings.TrimSpace(m.SchemaVersion) == "" {

		m.SchemaVersion = "v1"

	}
	metaJSON, err := canonicalJSON(m)
	if err != nil {

		return ChunkRef{}, fmt.Errorf("%w: meta json: %v", ErrEncode, err)

	}

	// Encode body (optionally gzip)
	bodyRaw, counts, err := encodeBody(seriesSnapshot)
	if err != nil {

		return ChunkRef{}, err

	}
	body := bodyRaw

	flags := uint16(0)
	if opts.CompressBody {

		comp, err := gzipDeterministic(bodyRaw)
		if err != nil {

			return ChunkRef{}, fmt.Errorf("%w: gzip: %v", ErrEncode, err)

		}
		body = comp

		flags |= 1

	}

	// Build full blob

	blob, err := encodeBlob(metaJSON, flags, body)
	if err != nil {

		return ChunkRef{}, err

	}
	sha := sha256.Sum256(blob)
	shaHex := hex.EncodeToString(sha[:])
	objKey := deterministicObjectKey(objectKeyPrefix, m, seriesSnapshot)
	ct := "application/x-chartly-tschunk"

	putMeta := map[string]string{

		"content_type": ct,

		"sha256": shaHex,

		"tenant_id": m.TenantID,

		"namespace": m.Namespace,

		"start": m.Start,

		"end": m.End,

		"schema_version": m.SchemaVersion,
	}

	// include meta keys deterministically

	metaKeys := make([]string, 0, len(m.Meta))
	for k := range m.Meta {

		metaKeys = append(metaKeys, k)

	}
	sort.Strings(metaKeys)
	for _, k := range metaKeys {

		putMeta["meta."+k] = m.Meta[k]

	}
	if err := sink.Put(ctx, m.TenantID, objKey, ct, blob, putMeta); err != nil {

		return ChunkRef{}, fmt.Errorf("%w: %v", ErrSink, err)

	}
	return ChunkRef{

		ObjectKey:   objKey,
		ContentType: ct,
		Bytes:       int64(len(blob)),
		SHA256:      shaHex,
		Start:       m.Start,
		End:         m.End,
		Series:      counts.series,
		Points:      counts.points,
	}, nil
}

////////////////////////////////////////////////////////////////////////////////
// Encoding
////////////////////////////////////////////////////////////////////////////////

type bodyCounts struct {
	series int
	points int
}

func encodeBody(series []seriesBuf) ([]byte, bodyCounts, error) {

	var buf bytes.Buffer

	var tmp [8]byte

	maxU32 := int(^uint32(0))

	// SeriesCount uint32

	if len(series) > maxU32 {

		return nil, bodyCounts{}, fmt.Errorf("%w: too many series", ErrTooLarge)

	}
	binary.LittleEndian.PutUint32(tmp[:4], uint32(len(series)))
	buf.Write(tmp[:4])
	totalPts := 0

	for _, sb := range series {

		// SeriesKeyJSON

		skJSON, err := canonicalJSON(normalizeSeriesKey(sb.key))
		if err != nil {

			return nil, bodyCounts{}, fmt.Errorf("%w: series key json: %v", ErrEncode, err)

		}
		if len(skJSON) > maxU32 {

			return nil, bodyCounts{}, fmt.Errorf("%w: series key too large", ErrTooLarge)

		}
		binary.LittleEndian.PutUint32(tmp[:4], uint32(len(skJSON)))
		buf.Write(tmp[:4])
		buf.Write(skJSON)

		// PointsCount

		if len(sb.points) > maxU32 {

			return nil, bodyCounts{}, fmt.Errorf("%w: too many points", ErrTooLarge)

		}
		binary.LittleEndian.PutUint32(tmp[:4], uint32(len(sb.points)))
		buf.Write(tmp[:4])
		if len(sb.points) == 0 {

			// BaseTS

			binary.LittleEndian.PutUint64(tmp[:8], uint64(0))
			buf.Write(tmp[:8])

			// SeriesCRC32

			binary.LittleEndian.PutUint32(tmp[:4], 0)
			buf.Write(tmp[:4])
			continue

		}

		// BaseTS int64 (unix nanos)
		base := sb.points[0].ts.UTC().UnixNano()
		binary.LittleEndian.PutUint64(tmp[:8], uint64(base))
		buf.Write(tmp[:8])
		seriesBlockStart := buf.Len()

		// Encode points: deltaTS varint + float64

		prev := base

		for i := range sb.points {

			ts := sb.points[i].ts.UTC().UnixNano()
			delta := ts - prev

			if i == 0 {

				delta = 0

			}
			var vb [binary.MaxVarintLen64]byte

			n := binary.PutVarint(vb[:], delta)
			buf.Write(vb[:n])
			var fb [8]byte

			binary.LittleEndian.PutUint64(fb[:], math.Float64bits(sb.points[i].val))
			buf.Write(fb[:])
			prev = ts

			totalPts++

		}

		// CRC32 over (SeriesKeyJSON + points encoding bytes)

		// We compute it deterministically using a new crc32 and known slices.

		crc := crc32.NewIEEE()
		_, _ = crc.Write(skJSON)
		seriesBlock := buf.Bytes()[seriesBlockStart:buf.Len()]

		_, _ = crc.Write(seriesBlock)
		binary.LittleEndian.PutUint32(tmp[:4], crc.Sum32())
		buf.Write(tmp[:4])

	}

	// BodyCRC32 over entire body (SeriesCount..last SeriesCRC32)
	bodyBytes := buf.Bytes()
	bodyCRC := crc32.ChecksumIEEE(bodyBytes)
	binary.LittleEndian.PutUint32(tmp[:4], bodyCRC)
	buf.Write(tmp[:4])
	return buf.Bytes(), bodyCounts{series: len(series), points: totalPts}, nil
}
func encodeBlob(metaJSON []byte, flags uint16, body []byte) ([]byte, error) {

	if len(metaJSON) > int(^uint32(0)) {

		return nil, fmt.Errorf("%w: meta too large", ErrTooLarge)

	}
	var buf bytes.Buffer

	// Magic

	buf.WriteString("CHTS1")
	var tmp [8]byte

	// Version uint16 = 1

	binary.LittleEndian.PutUint16(tmp[:2], 1)
	buf.Write(tmp[:2])

	// Flags uint16

	binary.LittleEndian.PutUint16(tmp[:2], flags)
	buf.Write(tmp[:2])

	// MetaLen uint32

	binary.LittleEndian.PutUint32(tmp[:4], uint32(len(metaJSON)))
	buf.Write(tmp[:4])

	// MetaJSON

	buf.Write(metaJSON)

	// Body

	buf.Write(body)
	return buf.Bytes(), nil
}
func gzipDeterministic(in []byte) ([]byte, error) {

	var buf bytes.Buffer

	zw, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {

		return nil, err

	}
	zw.Name = ""

	zw.Comment = ""

	zw.ModTime = time.Unix(0, 0).UTC()
	zw.OS = 255

	if _, err := zw.Write(in); err != nil {

		_ = zw.Close()
		return nil, err

	}
	if err := zw.Close(); err != nil {

		return nil, err

	}
	return buf.Bytes(), nil
}

////////////////////////////////////////////////////////////////////////////////
// Canonical JSON (sorted keys at all depths)
////////////////////////////////////////////////////////////////////////////////

func canonicalJSON(v any) ([]byte, error) {

	n := normalizeJSONValue(v)
	return json.Marshal(n)
}
func normalizeJSONValue(v any) any {

	switch t := v.(type) {

	case nil:
		return nil
	case string:
		return normalizeString(t)
	case bool:
		return t
	case float64:
		// keep as-is; encoding/json is deterministic for same float64 bits.
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case map[string]string:

		return normalizeStringMap(t)
	case map[string]any:

		return normalizeAnyMap(t)
	case []any:

		out := make([]any, len(t))
		for i := range t {

			out[i] = normalizeJSONValue(t[i])

		}
		return out

	default:

		// Try to marshal/unmarshal into interface{}
		// to canonicalize structs.

		b, err := json.Marshal(t)
		if err != nil {

			// fallback: string formatting

			return normalizeString(fmt.Sprintf("%v", t))

		}
		var anyv any

		if err := json.Unmarshal(b, &anyv); err != nil {

			return normalizeString(string(b))

		}
		return normalizeJSONValue(anyv)

	}
}
func normalizeAnyMap(m map[string]any) map[string]any {

	if m == nil {

		return nil

	}
	keys := make([]string, 0, len(m))
	for k := range m {

		k2 := normalizeString(k)
		if k2 == "" {

			continue

		}
		keys = append(keys, k2)

	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, k := range keys {

		out[k] = normalizeJSONValue(m[k])

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
	keys := make([]string, 0, len(m))
	for k := range m {

		k2 := normalizeString(k)
		if k2 == "" {

			continue

		}
		keys = append(keys, k2)

	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, k := range keys {

		out[k] = normalizeString(m[k])

	}
	if len(out) == 0 {

		return nil

	}
	return out
}
func normalizeString(s string) string {

	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

////////////////////////////////////////////////////////////////////////////////
// Normalization + validation
////////////////////////////////////////////////////////////////////////////////

func normalizeSeriesKey(k SeriesKey) SeriesKey {

	k.TenantID = normalizeString(k.TenantID)
	k.Namespace = normalizeString(k.Namespace)
	k.Metric = normalizeString(k.Metric)
	k.EntityType = normalizeString(k.EntityType)
	k.EntityID = normalizeString(k.EntityID)
	k.Tags = normalizeStringMap(k.Tags)
	return k
}
func validateSeriesKey(k SeriesKey) error {

	if k.TenantID == "" || k.Namespace == "" {

		return fmt.Errorf("%w: tenant_id/namespace required", ErrInvalidKey)

	}
	if k.Metric == "" || k.EntityType == "" {

		return fmt.Errorf("%w: metric/entity_type required", ErrInvalidKey)

	}
	return nil
}
func normalizePoint(p Point) Point {

	p.TS = normalizeString(p.TS)

	// Value stays as-is

	p.Meta = normalizeStringMap(p.Meta)
	return p
}
func validatePoint(p Point, allowNaN bool) error {

	if p.TS == "" {

		return fmt.Errorf("%w: ts required", ErrInvalidPoint)

	}
	if !allowNaN {

		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {

			return fmt.Errorf("%w: NaN/Inf not allowed", ErrInvalidPoint)

		}

	}
	return nil
}
func normalizeChunkMeta(m ChunkMeta) ChunkMeta {

	m.TenantID = normalizeString(m.TenantID)
	m.Namespace = normalizeString(m.Namespace)
	m.SourceID = normalizeString(m.SourceID)
	m.ProducedAt = normalizeString(m.ProducedAt)
	m.Start = normalizeString(m.Start)
	m.End = normalizeString(m.End)
	m.SchemaVersion = normalizeString(m.SchemaVersion)
	m.Meta = normalizeStringMap(m.Meta)
	return m
}
func validateChunkMeta(m ChunkMeta) error {

	if m.TenantID == "" || m.Namespace == "" {

		return fmt.Errorf("%w: tenant_id/namespace required", ErrInvalidMeta)

	}
	if m.Start == "" || m.End == "" {

		return fmt.Errorf("%w: start/end required", ErrInvalidMeta)

	}
	if m.ProducedAt != "" {

		if _, err := parseRFC3339Meta(m.ProducedAt); err != nil {

			return err

		}

	}
	return nil
}
func parseRFC3339Point(s string) (time.Time, error) {

	s = normalizeString(s)
	if s == "" {

		return time.Time{}, fmt.Errorf("%w: ts required", ErrInvalidPoint)

	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {

		return t.UTC(), nil

	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {

		return time.Time{}, fmt.Errorf("%w: invalid rfc3339 ts", ErrInvalidPoint)

	}
	return t.UTC(), nil
}
func parseRFC3339Meta(s string) (time.Time, error) {

	s = normalizeString(s)
	if s == "" {

		return time.Time{}, fmt.Errorf("%w: ts required", ErrInvalidMeta)

	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {

		return t.UTC(), nil

	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {

		return time.Time{}, fmt.Errorf("%w: invalid rfc3339 ts", ErrInvalidMeta)

	}
	return t.UTC(), nil
}

////////////////////////////////////////////////////////////////////////////////
// Deterministic series key + object key
////////////////////////////////////////////////////////////////////////////////

func seriesKeyString(k SeriesKey) string {

	k = normalizeSeriesKey(k)

	// Tags canonical string: k1=v1;k2=v2...

	tags := ""

	if len(k.Tags) > 0 {

		keys := make([]string, 0, len(k.Tags))
		for kk := range k.Tags {

			keys = append(keys, kk)

		}
		sort.Strings(keys)
		var b strings.Builder

		for i, kk := range keys {

			if i > 0 {

				b.WriteString(";")

			}
			b.WriteString(kk)
			b.WriteString("=")
			b.WriteString(k.Tags[kk])

		}
		tags = b.String()

	}
	return strings.Join([]string{

		k.TenantID,
		k.Namespace,
		k.Metric,
		k.EntityType,
		k.EntityID,
		tags,
	}, "|")
}
func deterministicObjectKey(prefix string, meta ChunkMeta, series []seriesBuf) string {

	prefix = strings.TrimSpace(prefix)
	if prefix == "" {

		prefix = "ts"

	}

	// Key suffix derived deterministically from meta + series index

	seed := buildKeySeed(meta, series)
	sum := sha256.Sum256(seed)
	hex24 := hex.EncodeToString(sum[:])[:24]

	return strings.TrimRight(prefix, "/") + "/tschunk_" + hex24 + ".bin"
}
func buildKeySeed(meta ChunkMeta, series []seriesBuf) []byte {

	// canonical json of meta (already normalized) + series key strings

	mj, _ := canonicalJSON(meta)
	var b bytes.Buffer

	b.Write(mj)
	b.WriteByte('\n')
	for _, sb := range series {

		b.WriteString(seriesKeyString(sb.key))
		b.WriteByte('\n')

		// include point count for stability

		b.WriteString(fmt.Sprintf("%d\n", len(sb.points)))

	}
	return b.Bytes()
}

////////////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////////////

func deepCopySeriesBuf(sb seriesBuf) seriesBuf {

	out := seriesBuf{

		key: sb.key,
	}
	if sb.points != nil {

		out.points = make([]pointBuf, len(sb.points))
		for i := range sb.points {

			out.points[i] = pointBuf{

				ts: sb.points[i].ts,

				tsS: sb.points[i].tsS,

				val: sb.points[i].val,

				meta: normalizeStringMap(sb.points[i].meta),
			}

		}

	}
	return out
}
func canonicalMetaString(m map[string]string) string {

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
func normalizeWriterOptions(opts WriterOptions) WriterOptions {

	o := opts

	if o.MaxPointsPerChunk <= 0 {

		o.MaxPointsPerChunk = 200000

	}
	if o.MaxSeriesPerChunk <= 0 {

		o.MaxSeriesPerChunk = 5000

	}
	return o
}

// sortPointsDeterministic sorts points deterministically. For large series, it shards
// the input into smaller slices, sorts each, then performs a stable k-way merge.
func sortPointsDeterministic(points []pointBuf) []pointBuf {
	if len(points) <= 1 {
		return points
	}
	const shardSize = 50000
	if len(points) <= shardSize {
		sort.Slice(points, func(i, j int) bool {
			return comparePoints(points[i], points[j]) < 0
		})
		return points
	}

	// Shard + sort
	shards := make([][]pointBuf, 0, (len(points)+shardSize-1)/shardSize)
	for i := 0; i < len(points); i += shardSize {
		end := i + shardSize
		if end > len(points) {
			end = len(points)
		}
		sh := points[i:end]
		sort.Slice(sh, func(a, b int) bool {
			return comparePoints(sh[a], sh[b]) < 0
		})
		shards = append(shards, sh)
	}

	// Merge
	out := make([]pointBuf, 0, len(points))
	h := &pointHeap{shards: shards}
	for i := range shards {
		if len(shards[i]) > 0 {
			h.items = append(h.items, heapItem{shard: i, idx: 0})
		}
	}
	heap.Init(h)
	for h.Len() > 0 {
		it := heap.Pop(h).(heapItem)
		p := h.shards[it.shard][it.idx]
		out = append(out, p)
		it.idx++
		if it.idx < len(h.shards[it.shard]) {
			heap.Push(h, it)
		}
	}
	return out
}
func comparePoints(a, b pointBuf) int {
	if a.ts.Before(b.ts) {
		return -1
	}
	if a.ts.After(b.ts) {
		return 1
	}
	if a.val < b.val {
		return -1
	}
	if a.val > b.val {
		return 1
	}
	ma := canonicalMetaString(a.meta)
	mb := canonicalMetaString(b.meta)
	if ma < mb {
		return -1
	}
	if ma > mb {
		return 1
	}
	return 0
}

type heapItem struct {
	shard int
	idx   int
}
type pointHeap struct {
	shards [][]pointBuf
	items  []heapItem
}

func (h pointHeap) Len() int { return len(h.items) }
func (h pointHeap) Less(i, j int) bool {
	a := h.shards[h.items[i].shard][h.items[i].idx]
	b := h.shards[h.items[j].shard][h.items[j].idx]
	return comparePoints(a, b) < 0
}
func (h pointHeap) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *pointHeap) Push(x any) {
	h.items = append(h.items, x.(heapItem))
}
func (h *pointHeap) Pop() any {
	n := len(h.items)
	it := h.items[n-1]
	h.items = h.items[:n-1]
	return it
}
