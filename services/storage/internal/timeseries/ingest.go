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

"bytes"

"compress/gzip"

"context"

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

Meta   ChunkMeta   `json:"meta"`

Series []SeriesSet `json:"series"`
}

type SeriesSet struct {

Key    SeriesKey `json:"key"`

Points []Point   `json:"points"`
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

opts   IngestOptions

writer *Writer
}

func NewIngestor(opts IngestOptions) *Ingestor {

o := normalizeIngestOptions(opts)

return &Ingestor{


opts:   o,


writer: NewWriter(o.Writer),

}
}

func (i *Ingestor) Reset() {

if i.writer != nil {


		i.writer.Reset()

}
}

// Ingest reads a JSON (or gzip JSON) payload, encodes it into a CHTS1 chunk,
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


raw, err := io.ReadAll(io.LimitReader(r, max+1))

if err != nil {


return IngestPayload{}, nil, fmt.Errorf("%w: read failed", ErrDecode)

}

if int64(len(raw)) > max {


return IngestPayload{}, nil, fmt.Errorf("%w: input exceeds max bytes", ErrTooLarge)

}


data := raw

warnings := make([]string, 0, 2)


if opts.AllowGzip && looksLikeGzip(raw) {


		dec, err := gunzipAll(raw, max)


		if err != nil {


			return IngestPayload{}, warnings, fmt.Errorf("%w: gzip decode failed", ErrDecode)


		}


		data = dec


		warnings = append(warnings, "payload_gzip_decoded")

}


var payload IngestPayload

dec := json.NewDecoder(bytes.NewReader(data))

if opts.DisallowUnknownFields {


		dec.DisallowUnknownFields()

}

if err := dec.Decode(&payload); err != nil {


		return IngestPayload{}, warnings, fmt.Errorf("%w: invalid json", ErrDecode)

}

	// Ensure there is no trailing junk
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return IngestPayload{}, warnings, fmt.Errorf("%w: trailing data", ErrDecode)
	} else if err != io.EOF {
		return IngestPayload{}, warnings, fmt.Errorf("%w: trailing data", ErrDecode)
	}


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

func looksLikeGzip(b []byte) bool {

if len(b) < 2 {


		return false

}

return b[0] == 0x1f && b[1] == 0x8b
}

func gunzipAll(b []byte, max int64) ([]byte, error) {

zr, err := gzip.NewReader(bytes.NewReader(b))

if err != nil {


		return nil, err

}


defer zr.Close()


out, err := io.ReadAll(io.LimitReader(zr, max+1))

if err != nil {


		return nil, err

}

if int64(len(out)) > max {


		return nil, fmt.Errorf("%w: decompressed exceeds max bytes", ErrTooLarge)

}

return out, nil
}

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


// Normalize points (TS/Meta trims) but do not sort here; Writer sorts


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
