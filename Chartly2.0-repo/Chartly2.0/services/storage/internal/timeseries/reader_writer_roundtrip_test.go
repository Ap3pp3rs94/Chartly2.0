package timeseries

import (
	"context"
	"sort"
	"testing"
)

type memSink struct {
	puts        int
	tenantID    string
	objectKey   string
	contentType string
	data        []byte
	meta        map[string]string
}

func (m *memSink) Put(ctx context.Context, tenantID string, objectKey string, contentType string, data []byte, meta map[string]string) error {
	_ = ctx
	m.puts++
	m.tenantID = tenantID
	m.objectKey = objectKey
	m.contentType = contentType
	m.data = append([]byte(nil), data...)
	if meta != nil {
		m.meta = make(map[string]string, len(meta))
		for k, v := range meta {
			m.meta[k] = v
		}
	}
	return nil
}
func TestCHTS1RoundTrip(t *testing.T) {
	w := NewWriter(WriterOptions{
		CompressBody:      false,
		MaxPointsPerChunk: 1000,
		MaxSeriesPerChunk: 100,
		AllowNaN:          false,
	})
	keyA := SeriesKey{
		TenantID:   "t1",
		Namespace:  "ns",
		Metric:     "m1",
		EntityType: "entity",
		EntityID:   "id1",
		Tags:       map[string]string{"b": "2", "a": "1"},
	}
	keyB := SeriesKey{
		TenantID:   "t1",
		Namespace:  "ns",
		Metric:     "m2",
		EntityType: "entity",
		EntityID:   "id2",
	}
	pointsA := []Point{
		{TS: "2026-01-01T00:00:02Z", Value: 2},
		{TS: "2026-01-01T00:00:01Z", Value: 1},
	}
	pointsB := []Point{
		{TS: "2026-01-01T00:00:03Z", Value: 30},
		{TS: "2026-01-01T00:00:04Z", Value: 40},
	}
	if err := w.AddSeriesPoints(keyA, pointsA); err != nil {
		t.Fatalf("add series A: %v", err)
	}
	if err := w.AddSeriesPoints(keyB, pointsB); err != nil {
		t.Fatalf("add series B: %v", err)
	}
	meta := ChunkMeta{
		TenantID:   "t1",
		Namespace:  "ns",
		ProducedAt: "2026-01-01T00:00:00Z",
		Start:      "2026-01-01T00:00:00Z",
		End:        "2026-01-01T00:01:00Z",
	}
	sink := &memSink{}
	ref, err := w.Flush(context.Background(), sink, meta, "prefix")
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if sink.puts != 1 {
		t.Fatalf("expected 1 put, got %d", sink.puts)
	}
	if len(sink.data) == 0 {
		t.Fatalf("expected non-empty chunk data")
	}
	if ref.Series != 2 {
		t.Fatalf("expected 2 series, got %d", ref.Series)
	}
	if ref.Points != 4 {
		t.Fatalf("expected 4 points, got %d", ref.Points)
	}
	decoded, err := Decode(sink.data, ReaderOptions{})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Meta.TenantID != meta.TenantID || decoded.Meta.Namespace != meta.Namespace {
		t.Fatalf("meta mismatch")
	}
	if len(decoded.Series) != 2 {
		t.Fatalf("decoded series count mismatch: %d", len(decoded.Series))
	}

	// Ensure deterministic series ordering.
	gotKeys := make([]string, 0, len(decoded.Series))
	for _, s := range decoded.Series {
		gotKeys = append(gotKeys, seriesKeyString(s.Key))
	}
	wantKeys := []string{seriesKeyString(keyA), seriesKeyString(keyB)}
	sort.Strings(wantKeys)
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("series order mismatch: got=%v want=%v", gotKeys, wantKeys)
		}
	}

	// Verify points are in ascending time order per series.
	for _, s := range decoded.Series {
		for i := 1; i < len(s.Points); i++ {
			if s.Points[i-1].TS > s.Points[i].TS {
				t.Fatalf("points not sorted for series %s", seriesKeyString(s.Key))
			}
		}
	}
}
func TestCHTS1RoundTripGzip(t *testing.T) {
	w := NewWriter(WriterOptions{
		CompressBody: true,
	})
	key := SeriesKey{
		TenantID:   "t1",
		Namespace:  "ns",
		Metric:     "m1",
		EntityType: "entity",
	}
	if err := w.AddPoint(key, Point{TS: "2026-01-01T00:00:00Z", Value: 1}); err != nil {
		t.Fatalf("add point: %v", err)
	}
	meta := ChunkMeta{
		TenantID:   "t1",
		Namespace:  "ns",
		ProducedAt: "2026-01-01T00:00:00Z",
		Start:      "2026-01-01T00:00:00Z",
		End:        "2026-01-01T00:01:00Z",
	}
	sink := &memSink{}
	if _, err := w.Flush(context.Background(), sink, meta, "prefix"); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(sink.data) == 0 {
		t.Fatalf("expected non-empty chunk data")
	}
	if _, err := Decode(sink.data, ReaderOptions{}); err != nil {
		t.Fatalf("decode gzip: %v", err)
	}
}
