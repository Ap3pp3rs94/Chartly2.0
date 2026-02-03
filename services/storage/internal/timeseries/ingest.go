package timeseries

// Ingest provides a deterministic, production-grade ingestion layer for time-series chunks.
// It accepts a JSON payload (optionally gzip-compressed), validates and normalizes inputs,
// then uses Writer to encode and flush a CHTS1 chunk to the provided Sink.
//
// Payload shape (JSON):
//   {
//     "meta": { ChunkMeta ... },
//     "series": [
//       { "key": { SeriesKey ... }, "points": [ {"ts":"...","value":1.23,"meta":{...}} ] }
//     ]
//   }
//
// Determinism:
// - Input maps are normalized (trim + NUL removal).
// - Series are processed in sorted order by SeriesKey string.
// - Points are passed to Writer, which sorts and dedupes deterministically.
// - No randomness, no time.Now.

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

var (
	ErrInvalidPayload = errors.New("invalid payload")
	ErrDecode         = errors.New("decode failed")
	ErrIngest         = errors.New("ingest failed")
)

type IngestPayload struct {
	Meta ChunkMeta `json:"meta"`

	Series []SeriesSet `json:"series"`
}
type SeriesSet struct {
	Key SeriesKey `json:"key"`

	Points []Point `json:"points"`
}
type IngestOptions struct {

	// MaxBytes caps raw input bytes read (compressed or uncompressed). Default 64 MiB.

	MaxBytes int64

	// AllowGzip enables gzip payloads (detected by header). Default true.

	AllowGzip bool

	// DisallowUnknownFields forces strict JSON decoding. Default true.

	DisallowUnknownFields bool

	// Writer options used for chunk encoding.

	Writer WriterOptions
}
type Ingestor struct {
	opts IngestOptions

	writer *Writer
}

func NewIngestor(opts IngestOptions) *Ingestor {

	o := normalizeIngestOptions(opts)
	return &Ingestor{

		opts: o,

		writer: NewWriter(o.Writer),
	}
}
func (i *Ingestor) Reset() {

	if i.writer != nil {

		i.writer.Reset()

	}
}

// Ingest reads a JSON (or gzip JSON)
// payload, encodes it into a CHTS1 chunk,
// and writes it to the provided Sink exactly once.
func (i *Ingestor) Ingest(ctx context.Context, r io.Reader, sink Sink, objectKeyPrefix string) (ChunkRef, []string, error) {

	if i == nil {

		return ChunkRef{}, nil, fmt.Errorf("%w: nil ingestor", ErrIngest)

	}
	if r == nil {

		return ChunkRef{}, nil, fmt.Errorf("%w: nil reader", ErrInvalidPayload)

	}
	payload, warns, err := decodePayload(r, i.opts)
	if err != nil {

		return ChunkRef{}, warns, err

	}
	return i.IngestPayload(ctx, payload, sink, objectKeyPrefix, warns)
}

// IngestPayload ingests a pre-parsed payload.
func (i *Ingestor) IngestPayload(ctx context.Context, payload IngestPayload, sink Sink, objectKeyPrefix string, warnings []string) (ChunkRef, []string, error) {

	if i == nil || i.writer == nil {

		return ChunkRef{}, warnings, fmt.Errorf("%w: writer not initialized", ErrIngest)

	}

	// Normalize meta and series deterministically

	payload.Meta = normalizeChunkMeta(payload.Meta)
	payload.Series = normalizeSeriesSets(payload.Series)

	// Validate that series tenant/namespace match meta early (deterministic).

	for _, s := range payload.Series {

		k := normalizeSeriesKey(s.Key)
		if k.TenantID != payload.Meta.TenantID {

			return ChunkRef{}, warnings, fmt.Errorf("%w: series tenant mismatch", ErrInvalidPayload)

		}
		if k.Namespace != payload.Meta.Namespace {

			return ChunkRef{}, warnings, fmt.Errorf("%w: series namespace mismatch", ErrInvalidPayload)

		}

	}

	// Reset writer state and add all series/points.

	// Writer is concurrency-safe, but we reset to avoid mixing chunks.

	i.writer.Reset()
	for _, s := range payload.Series {

		if err := i.writer.AddSeriesPoints(s.Key, s.Points); err != nil {

			return ChunkRef{}, warnings, fmt.Errorf("%w: %v", ErrIngest, err)

		}

	}

	// Flush chunk

	ref, err := i.writer.Flush(ctx, sink, payload.Meta, objectKeyPrefix)
	if err != nil {

		return ChunkRef{}, warnings, err

	}

	// Ensure warnings are deterministic order

	sort.Strings(warnings)
	return ref, warnings, nil
}

////////////////////////////////////////////////////////////////////////////////
// Decode
////////////////////////////////////////////////////////////////////////////////

func decodePayload(r io.Reader, opts IngestOptions) (IngestPayload, []string, error) {

	max := opts.MaxBytes
	if max <= 0 {
		max = 64 * 1024 * 1024
	}
	warnings := make([]string, 0, 2)

	// Limit raw bytes read (compressed or uncompressed).
	lr := &io.LimitedReader{R: r, N: max + 1}
	br := bufio.NewReader(lr)

	// Detect gzip by magic header without consuming stream.
	hdr, _ := br.Peek(2)
	useGzip := opts.AllowGzip && len(hdr) >= 2 && hdr[0] == 0x1f && hdr[1] == 0x8b

	var reader io.Reader = br
	var gz *gzip.Reader
	var err error

	if useGzip {
		gz, err = gzip.NewReader(br)
		if err != nil {
			return IngestPayload{}, warnings, fmt.Errorf("%w: gzip decode failed", ErrDecode)
		}
		defer gz.Close()
		reader = gz
		warnings = append(warnings, "payload_gzip_decoded")
	}

	// Limit decompressed bytes too.
	dlr := &io.LimitedReader{R: reader, N: max + 1}
	hasher := sha256.New()
	tee := io.TeeReader(dlr, hasher)
	dec := json.NewDecoder(tee)
	if opts.DisallowUnknownFields {
		dec.DisallowUnknownFields()
	}
	var payload IngestPayload
	if err := dec.Decode(&payload); err != nil {
		if lr.N <= 0 || dlr.N <= 0 {
			return IngestPayload{}, warnings, fmt.Errorf("%w: input exceeds max bytes", ErrTooLarge)
		}
		return IngestPayload{}, warnings, fmt.Errorf("%w: invalid json", ErrDecode)
	}

	// Ensure there is no trailing junk
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return IngestPayload{}, warnings, fmt.Errorf("%w: trailing data", ErrDecode)
	} else if err != io.EOF {
		return IngestPayload{}, warnings, fmt.Errorf("%w: trailing data", ErrDecode)
	}

	// Enforce size limits deterministically.
	if lr.N <= 0 || dlr.N <= 0 {
		return IngestPayload{}, warnings, fmt.Errorf("%w: input exceeds max bytes", ErrTooLarge)
	}

	// Attach payload checksum to meta for traceability (deterministic).
	sum := hex.EncodeToString(hasher.Sum(nil))
	if payload.Meta.Meta == nil {
		payload.Meta.Meta = make(map[string]string)
	}
	payload.Meta.Meta["ingest.payload_sha256"] = sum

	// Basic shape validation (more validation happens in Writer/Flush).

	if strings.TrimSpace(payload.Meta.TenantID) == "" || strings.TrimSpace(payload.Meta.Namespace) == "" {

		return IngestPayload{}, warnings, fmt.Errorf("%w: meta tenant_id/namespace required", ErrInvalidPayload)

	}
	if strings.TrimSpace(payload.Meta.Start) == "" || strings.TrimSpace(payload.Meta.End) == "" {

		return IngestPayload{}, warnings, fmt.Errorf("%w: meta start/end required", ErrInvalidPayload)

	}
	if len(payload.Series) == 0 {

		return IngestPayload{}, warnings, fmt.Errorf("%w: no series", ErrInvalidPayload)

	}
	return payload, warnings, nil
}

// (gzip helpers removed; streaming decode handles gzip directly)

////////////////////////////////////////////////////////////////////////////////
// Normalization helpers
////////////////////////////////////////////////////////////////////////////////

func normalizeIngestOptions(o IngestOptions) IngestOptions {

	if o.MaxBytes <= 0 {

		o.MaxBytes = 64 * 1024 * 1024

	}
	if o.AllowGzip == false {

		// keep false

	} else {

		o.AllowGzip = true

	}
	if o.DisallowUnknownFields == false {

		// keep false

	} else {

		o.DisallowUnknownFields = true

	}

	// Writer defaults handled by NewWriter via normalizeWriterOptions

	return o
}
func normalizeSeriesSets(ss []SeriesSet) []SeriesSet {

	out := make([]SeriesSet, 0, len(ss))
	for _, s := range ss {

		s.Key = normalizeSeriesKey(s.Key)

		// Normalize points (TS/Meta trims), but do not sort here; Writer sorts.

		if s.Points != nil {

			pts := make([]Point, len(s.Points))
			for i := range s.Points {

				p := s.Points[i]

				p.TS = normalizeString(p.TS)
				p.Meta = normalizeStringMap(p.Meta)
				pts[i] = p

			}
			s.Points = pts

		}
		out = append(out, s)

	}

	// Sort by series key string deterministically

	sort.Slice(out, func(i, j int) bool {

		return seriesKeyString(out[i].Key) < seriesKeyString(out[j].Key)

	})
	return out
}
