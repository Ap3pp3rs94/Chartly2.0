package timeseries

import (
	"context"
	"testing"
)

func TestCompactionToChunk(t *testing.T) {
	series := []SeriesPoints{
		{
			Key: SeriesKey{
				TenantID:   "t1",
				Namespace:  "ns",
				Metric:     "m1",
				EntityType: "entity",
				EntityID:   "id1",
			},
			Points: []Point{
				{TS: "2026-01-01T00:00:01Z", Value: 1},
				{TS: "2026-01-01T00:00:01Z", Value: 2}, // duplicate ts; KeepFirst => value 1
				{TS: "2026-01-01T00:00:05Z", Value: 5},
				{TS: "2026-01-01T00:00:20Z", Value: 20}, // out of window
			},
		},
		{
			Key: SeriesKey{
				TenantID:   "t1",
				Namespace:  "ns",
				Metric:     "m2",
				EntityType: "entity",
				EntityID:   "id2",
			},
			Points: []Point{
				{TS: "2026-01-01T00:00:02Z", Value: 2},
			},
		},
	}

	meta := ChunkMeta{
		TenantID:   "t1",
		Namespace:  "ns",
		ProducedAt: "2026-01-01T00:00:00Z",
		Start:      "2026-01-01T00:00:00Z",
		End:        "2026-01-01T00:00:10Z",
	}

	opts := CompactionOptions{
		Deduplicate:     true,
		DuplicatePolicy: KeepFirst,
		DropOutOfRange:  true,
	}

	sink := &memSink{}
	ref, res, err := CompactToChunk(context.Background(), sink, meta, "prefix", series, opts)
	if err != nil {
		t.Fatalf("compact to chunk: %v", err)
	}
	if sink.puts != 1 {
		t.Fatalf("expected 1 put, got %d", sink.puts)
	}
	if ref.Series != 2 {
		t.Fatalf("expected 2 series, got %d", ref.Series)
	}
	if ref.Points != 3 {
		t.Fatalf("expected 3 points, got %d", ref.Points)
	}
	if res.Dropped != 1 {
		t.Fatalf("expected 1 dropped (out of window), got %d", res.Dropped)
	}

	decoded, err := Decode(sink.data, ReaderOptions{})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Series) != 2 {
		t.Fatalf("decoded series count mismatch: %d", len(decoded.Series))
	}

	// Find series m1 and ensure duplicate resolution kept value=1 at ts=00:00:01Z.
	target := seriesKeyString(series[0].Key)
	found := false
	for _, s := range decoded.Series {
		if seriesKeyString(s.Key) != target {
			continue
		}
		found = true
		if len(s.Points) != 2 {
			t.Fatalf("expected 2 points in series m1, got %d", len(s.Points))
		}
		if s.Points[0].TS != "2026-01-01T00:00:01Z" || s.Points[0].Value != 1 {
			t.Fatalf("unexpected first point: %+v", s.Points[0])
		}
	}
	if !found {
		t.Fatalf("series m1 not found after decode")
	}
}
