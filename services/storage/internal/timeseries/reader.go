package timeseries

// CHTS1  Chartly TimeSeries Chunk Reader (v1)
//
// This file implements a deterministic, production-grade decoder for the CHTS1
// on-wire format emitted by writer.go. It performs:
//
//   1) Header validation:
//        - Magic "CHTS1"
//        - Version=1
//        - Flags (bit0 = gzip body)
//        - MetaLen + MetaJSON (canonical JSON)
//
//   2) Meta parsing:
//        - MetaJSON is unmarshaled into ChunkMeta
//        - Meta is normalized and time fields are validated (RFC3339/RFC3339Nano)
//
//   3) BODY handling:
//        - Remaining bytes are BODY (optionally gzip compressed)
//        - If gzip flag is set, decompress BODY only
//
//   4) Integrity checks:
//        - BodyCRC32: last 4 bytes of decoded BODY; verifies crc32 over BODY bytes
//          excluding the trailing BodyCRC32 field. Coverage is bytes AFTER SeriesCount
//          through the final SeriesCRC32 (inclusive).
//        - SeriesCRC32: validates each series block CRC32 over (SeriesKeyJSON + encoded points bytes).
//
//   5) Deterministic output:
//        - Series are returned sorted by deterministic SeriesKey string (same helper as writer.go)
//        - Points are returned in ascending time order as encoded
//
// Library-only:
//   - No HTTP handlers
//   - No filesystem writes (this is a decoder/reader library)
//   - No network calls
//
// Multi-tenant safety:
//   - DecodeFrom requires explicit tenantID and objectKey and reads from a Source.
//   - Tenant boundaries are enforced at higher layers; this decoder is tenant-safe by design
//     by keeping tenant identity explicit in meta and key structures.

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

var (
	ErrDecodeInvalid  = errors.New("decode invalid")
	ErrDecodeCRC      = errors.New("decode crc mismatch")
	ErrDecodeTooLarge = errors.New("decode too large")
	ErrDecodeMeta     = errors.New("decode meta")
	ErrDecodeBody     = errors.New("decode body")
	ErrDecodeSource   = errors.New("decode source")
)

type ReaderOptions struct {
	MaxBytes  int64 // cap total blob size; default 64MiB
	MaxSeries int   // default 10000
	MaxPoints int   // default 500000
	AllowNaN  bool  // default false (reject NaN/Inf unless true)
}

type DecodedPoint struct {
	TS    string  // RFC3339Nano
	Value float64 // numeric
}

type DecodedSeries struct {
	Key    SeriesKey
	Points []DecodedPoint
	Start  string // RFC3339Nano (first point)
	End    string // RFC3339Nano (last point; consumer may treat as exclusive-ish)
}

type DecodedChunk struct {
	Meta   ChunkMeta
	Series []DecodedSeries
	Ref    ChunkRef // computed from bytes (sha256) + counts
}

type Source interface {
	Get(ctx context.Context, tenantID, objectKey string) (contentType string, data []byte, meta map[string]string, err error)
}

func Decode(data []byte, opts ReaderOptions) (DecodedChunk, error) {
	o := normalizeReaderOptions(opts)

	if data == nil {
		return DecodedChunk{}, fmt.Errorf("%w: nil data", ErrDecodeInvalid)
	}

	if o.MaxBytes > 0 && int64(len(data)) > o.MaxBytes {
		return DecodedChunk{}, fmt.Errorf("%w: max bytes exceeded (%d>%d)", ErrDecodeTooLarge, len(data), o.MaxBytes)
	}

	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])

	r := bytes.NewReader(data)

	// Header: Magic(5) + Version(u16) + Flags(u16) + MetaLen(u32) + MetaJSON
	magic := make([]byte, 5)
	if _, err := io.ReadFull(r, magic); err != nil {
		return DecodedChunk{}, fmt.Errorf("%w: header magic: %v", ErrDecodeInvalid, err)
	}
	if string(magic) != "CHTS1" {
		return DecodedChunk{}, fmt.Errorf("%w: bad magic", ErrDecodeInvalid)
	}

	var b2 [2]byte
	if _, err := io.ReadFull(r, b2[:]); err != nil {
		return DecodedChunk{}, fmt.Errorf("%w: header version: %v", ErrDecodeInvalid, err)
	}
	version := binary.LittleEndian.Uint16(b2[:])
	if version != 1 {
		return DecodedChunk{}, fmt.Errorf("%w: unsupported version=%d", ErrDecodeInvalid, version)
	}

	if _, err := io.ReadFull(r, b2[:]); err != nil {
		return DecodedChunk{}, fmt.Errorf("%w: header flags: %v", ErrDecodeInvalid, err)
	}
	flags := binary.LittleEndian.Uint16(b2[:])

	var b4 [4]byte
	if _, err := io.ReadFull(r, b4[:]); err != nil {
		return DecodedChunk{}, fmt.Errorf("%w: header metalen: %v", ErrDecodeInvalid, err)
	}
	metaLen := binary.LittleEndian.Uint32(b4[:])
	if metaLen == 0 {
		return DecodedChunk{}, fmt.Errorf("%w: meta missing", ErrDecodeMeta)
	}
	if o.MaxBytes > 0 && int64(metaLen) > o.MaxBytes {
		return DecodedChunk{}, fmt.Errorf("%w: meta too large", ErrDecodeTooLarge)
	}

	metaJSON := make([]byte, metaLen)
	if _, err := io.ReadFull(r, metaJSON); err != nil {
		return DecodedChunk{}, fmt.Errorf("%w: meta read: %v", ErrDecodeMeta, err)
	}

	var meta ChunkMeta
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return DecodedChunk{}, fmt.Errorf("%w: meta json: %v", ErrDecodeMeta, err)
	}
	meta = normalizeChunkMeta(meta)
	if err := validateChunkMetaForDecode(meta); err != nil {
		return DecodedChunk{}, err
	}

	// Remaining bytes are BODY (optionally gzipped).
	bodyRaw, err := io.ReadAll(r)
	if err != nil {
		return DecodedChunk{}, fmt.Errorf("%w: body read: %v", ErrDecodeBody, err)
	}
	if len(bodyRaw) == 0 {
		return DecodedChunk{}, fmt.Errorf("%w: empty body", ErrDecodeInvalid)
	}
	if o.MaxBytes > 0 && int64(len(bodyRaw)) > o.MaxBytes {
		return DecodedChunk{}, fmt.Errorf("%w: body too large", ErrDecodeTooLarge)
	}

	const flagGzipBody = uint16(1)
	body := bodyRaw
	if (flags & flagGzipBody) != 0 {
		dec, err := gunzipBody(bodyRaw, o.MaxBytes)
		if err != nil {
			return DecodedChunk{}, fmt.Errorf("%w: gunzip: %v", ErrDecodeBody, err)
		}
		body = dec
	}

	series, counts, err := decodeBody(body, o)
	if err != nil {
		return DecodedChunk{}, err
	}

	// Deterministic ordering: sort series by the same stable key as writer.
	sort.Slice(series, func(i, j int) bool {
		return seriesKeyString(series[i].Key) < seriesKeyString(series[j].Key)
	})

	ref := ChunkRef{
		ObjectKey:   "",
		ContentType: "application/x-chartly-tschunk",
		Bytes:       int64(len(data)),
		SHA256:      shaHex,
		Start:       meta.Start,
		End:         meta.End,
		Series:      counts.series,
		Points:      counts.points,
	}

	return DecodedChunk{
		Meta:   meta,
		Series: series,
		Ref:    ref,
	}, nil
}

func DecodeFrom(ctx context.Context, tenantID string, objectKey string, src Source, opts ReaderOptions) (DecodedChunk, error) {
	if src == nil {
		return DecodedChunk{}, fmt.Errorf("%w: nil source", ErrDecodeSource)
	}

	tenantID = normalizeString(tenantID)
	objectKey = strings.TrimSpace(objectKey)
	if tenantID == "" || objectKey == "" {
		return DecodedChunk{}, fmt.Errorf("%w: tenantID/objectKey required", ErrDecodeInvalid)
	}

	_, data, _, err := src.Get(ctx, tenantID, objectKey)
	if err != nil {
		return DecodedChunk{}, fmt.Errorf("%w: get: %v", ErrDecodeSource, err)
	}

	ch, err := Decode(data, opts)
	if err != nil {
		return DecodedChunk{}, err
	}

	ch.Ref.ObjectKey = objectKey
	return ch, nil
}

type decodeCounts struct {
	series int
	points int
}

func decodeBody(body []byte, opts ReaderOptions) ([]DecodedSeries, decodeCounts, error) {
	if body == nil || len(body) == 0 {
		return nil, decodeCounts{}, fmt.Errorf("%w: empty body", ErrDecodeInvalid)
	}
	if opts.MaxBytes > 0 && int64(len(body)) > opts.MaxBytes {
		return nil, decodeCounts{}, fmt.Errorf("%w: body max bytes exceeded", ErrDecodeTooLarge)
	}

	// Must contain at least SeriesCount(4) + BodyCRC32(4).
	if len(body) < 8 {
		return nil, decodeCounts{}, fmt.Errorf("%w: body too small", ErrDecodeInvalid)
	}

	// Verify BodyCRC32:
	// - BodyCRC32 is the last 4 bytes of BODY.
	// - Coverage is everything before it, excluding SeriesCount (first 4 bytes).
	bodyWithoutCRC := body[:len(body)-4]
	wantBodyCRC := binary.LittleEndian.Uint32(body[len(body)-4:])
	if len(bodyWithoutCRC) < 4 {
		return nil, decodeCounts{}, fmt.Errorf("%w: body too small", ErrDecodeInvalid)
	}
	gotBodyCRC := crc32.ChecksumIEEE(bodyWithoutCRC[4:])
	if gotBodyCRC != wantBodyCRC {
		return nil, decodeCounts{}, fmt.Errorf("%w: body crc mismatch", ErrDecodeCRC)
	}

	r := bytes.NewReader(bodyWithoutCRC)

	var b4 [4]byte
	if _, err := io.ReadFull(r, b4[:]); err != nil {
		return nil, decodeCounts{}, fmt.Errorf("%w: seriescount: %v", ErrDecodeBody, err)
	}
	seriesCount := int(binary.LittleEndian.Uint32(b4[:]))
	if seriesCount < 0 {
		return nil, decodeCounts{}, fmt.Errorf("%w: invalid series count", ErrDecodeInvalid)
	}
	if opts.MaxSeries > 0 && seriesCount > opts.MaxSeries {
		return nil, decodeCounts{}, fmt.Errorf("%w: too many series (%d>%d)", ErrDecodeTooLarge, seriesCount, opts.MaxSeries)
	}

	out := make([]DecodedSeries, 0, seriesCount)
	totalPts := 0

	for si := 0; si < seriesCount; si++ {
		// SeriesKeyLen
		if _, err := io.ReadFull(r, b4[:]); err != nil {
			return nil, decodeCounts{}, fmt.Errorf("%w: series key len: %v", ErrDecodeBody, err)
		}
		keyLen := int(binary.LittleEndian.Uint32(b4[:]))
		if keyLen <= 0 {
			return nil, decodeCounts{}, fmt.Errorf("%w: series key len invalid", ErrDecodeInvalid)
		}
		if opts.MaxBytes > 0 && int64(keyLen) > opts.MaxBytes {
			return nil, decodeCounts{}, fmt.Errorf("%w: series key too large", ErrDecodeTooLarge)
		}

		keyJSON := make([]byte, keyLen)
		if _, err := io.ReadFull(r, keyJSON); err != nil {
			return nil, decodeCounts{}, fmt.Errorf("%w: series key json: %v", ErrDecodeBody, err)
		}

		var key SeriesKey
		if err := json.Unmarshal(keyJSON, &key); err != nil {
			return nil, decodeCounts{}, fmt.Errorf("%w: series key decode: %v", ErrDecodeBody, err)
		}
		key = normalizeSeriesKey(key)
		if err := validateSeriesKey(key); err != nil {
			return nil, decodeCounts{}, fmt.Errorf("%w: %v", ErrDecodeBody, err)
		}

		// PointsCount
		if _, err := io.ReadFull(r, b4[:]); err != nil {
			return nil, decodeCounts{}, fmt.Errorf("%w: pointscount: %v", ErrDecodeBody, err)
		}
		pointsCount := int(binary.LittleEndian.Uint32(b4[:]))
		if pointsCount < 0 {
			return nil, decodeCounts{}, fmt.Errorf("%w: invalid points count", ErrDecodeInvalid)
		}
		if opts.MaxPoints > 0 && (totalPts+pointsCount) > opts.MaxPoints {
			return nil, decodeCounts{}, fmt.Errorf("%w: too many points (%d>%d)", ErrDecodeTooLarge, totalPts+pointsCount, opts.MaxPoints)
		}

		// BaseTS int64 (unix nanos)
		var b8 [8]byte
		if _, err := io.ReadFull(r, b8[:]); err != nil {
			return nil, decodeCounts{}, fmt.Errorf("%w: base ts: %v", ErrDecodeBody, err)
		}
		base := int64(binary.LittleEndian.Uint64(b8[:]))

		// For SeriesCRC32 verification: accumulate exact encoded points bytes in decode order.
		var rawPoints bytes.Buffer
		points := make([]DecodedPoint, 0, pointsCount)
		prev := base

		for pi := 0; pi < pointsCount; pi++ {
			delta, rawDelta, err := readVarintWithRaw(r)
			if err != nil {
				return nil, decodeCounts{}, fmt.Errorf("%w: delta ts: %v", ErrDecodeBody, err)
			}
			rawPoints.Write(rawDelta)

			var fb [8]byte
			if _, err := io.ReadFull(r, fb[:]); err != nil {
				return nil, decodeCounts{}, fmt.Errorf("%w: value: %v", ErrDecodeBody, err)
			}
			rawPoints.Write(fb[:])

			var tsN int64
			if pi == 0 {
				tsN = base
			} else {
				tsN = prev + delta
			}
			prev = tsN

			val := math.Float64frombits(binary.LittleEndian.Uint64(fb[:]))
			if !opts.AllowNaN && (math.IsNaN(val) || math.IsInf(val, 0)) {
				return nil, decodeCounts{}, fmt.Errorf("%w: NaN/Inf not allowed", ErrDecodeBody)
			}

			ts := time.Unix(0, tsN).UTC().Format(time.RFC3339Nano)
			points = append(points, DecodedPoint{TS: ts, Value: val})
			totalPts++
		}

		// SeriesCRC32
		if _, err := io.ReadFull(r, b4[:]); err != nil {
			return nil, decodeCounts{}, fmt.Errorf("%w: series crc: %v", ErrDecodeBody, err)
		}
		wantSeriesCRC := binary.LittleEndian.Uint32(b4[:])
		crc := crc32.NewIEEE()
		_, _ = crc.Write(keyJSON)
		_, _ = crc.Write(rawPoints.Bytes())
		gotSeriesCRC := crc.Sum32()
		if gotSeriesCRC != wantSeriesCRC {
			return nil, decodeCounts{}, fmt.Errorf("%w: series crc mismatch", ErrDecodeCRC)
		}

		ds := DecodedSeries{Key: key, Points: points}
		if len(points) > 0 {
			ds.Start = points[0].TS
			ds.End = points[len(points)-1].TS
		}
		out = append(out, ds)
	}

	// Ensure no trailing bytes remain.
	if r.Len() != 0 {
		return nil, decodeCounts{}, fmt.Errorf("%w: trailing bytes in body", ErrDecodeInvalid)
	}

	return out, decodeCounts{series: len(out), points: totalPts}, nil
}

func normalizeReaderOptions(opts ReaderOptions) ReaderOptions {
	o := opts
	if o.MaxBytes <= 0 {
		o.MaxBytes = 64 * 1024 * 1024 // 64MiB
	}
	if o.MaxSeries <= 0 {
		o.MaxSeries = 10000
	}
	if o.MaxPoints <= 0 {
		o.MaxPoints = 500000
	}
	return o
}

func gunzipBody(in []byte, maxBytes int64) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(in))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	var r io.Reader = zr
	if maxBytes > 0 {
		r = io.LimitReader(zr, maxBytes+1)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(out)) > maxBytes {
		return nil, fmt.Errorf("%w: gunzip exceeded max bytes", ErrDecodeTooLarge)
	}
	return out, nil
}

func readVarintWithRaw(r *bytes.Reader) (int64, []byte, error) {
	// Read bytes until varint terminates; record raw bytes for CRC verification.
	var raw [binary.MaxVarintLen64]byte
	n := 0
	for n < len(raw) {
		b, err := r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		raw[n] = b
		n++
		if b < 0x80 {
			break
		}
	}
	v, m := binary.Varint(raw[:n])
	if m <= 0 {
		return 0, nil, fmt.Errorf("invalid varint")
	}
	return v, raw[:n], nil
}

func validateChunkMetaForDecode(m ChunkMeta) error {
	if strings.TrimSpace(m.TenantID) == "" || strings.TrimSpace(m.Namespace) == "" {
		return fmt.Errorf("%w: tenant_id/namespace required", ErrDecodeMeta)
	}
	if strings.TrimSpace(m.Start) == "" || strings.TrimSpace(m.End) == "" {
		return fmt.Errorf("%w: start/end required", ErrDecodeMeta)
	}

startT, err := parseRFC3339MetaDecode(m.Start)
	if err != nil {
		return err
	}
endT, err := parseRFC3339MetaDecode(m.End)
	if err != nil {
		return err
	}
	if !endT.After(startT) {
		return fmt.Errorf("%w: end must be after start", ErrDecodeMeta)
	}
	if strings.TrimSpace(m.ProducedAt) != "" {
		if _, err := parseRFC3339MetaDecode(m.ProducedAt); err != nil {
			return err
		}
	}
	return nil
}

func parseRFC3339MetaDecode(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	if s == "" {
		return time.Time{}, fmt.Errorf("%w: ts required", ErrDecodeMeta)
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%w: invalid rfc3339 ts", ErrDecodeMeta)
}
