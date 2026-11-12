package monitor

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"pier/internal/platform/sqlite"
)

func testHistory(t *testing.T) *HistoryStore {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &HistoryStore{DB: db}
}

func f(v float64) *float64 { return &v }
func i(v int64) *int64     { return &v }

// bucketStart is a fixed, bucket-aligned instant.
var bucketStart = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func TestCompactAveragesAndKeepsMaximum(t *testing.T) {
	ctx, store := context.Background(), testHistory(t)
	for minute, cpu := range []float64{10, 20, 90} {
		at := bucketStart.Add(time.Duration(minute) * time.Minute)
		sample := Sample{CPU: f(cpu), MemUsed: i(int64(100 * (minute + 1))), Load1: f(float64(minute))}
		if err := store.Record(ctx, at, map[string]Sample{"server:a": sample}); err != nil {
			t.Fatal(err)
		}
	}
	now := bucketStart.Add(20 * time.Minute)
	if err := store.Compact(ctx, now); err != nil {
		t.Fatal(err)
	}
	r, _ := ParseRange("7d")
	history, err := store.Query(ctx, "server:a", r, now)
	if err != nil {
		t.Fatal(err)
	}
	s := history.Series
	if len(s.T) != 1 || s.T[0] != bucketStart.Unix() || history.StepSecond != 900 {
		t.Fatalf("buckets: %+v", history)
	}
	if got := *s.CPU[0]; math.Abs(got-40) > 1e-9 {
		t.Fatalf("cpu avg = %v", got)
	}
	if *s.CPUMax[0] != 90 || *s.MemUsedMax[0] != 300 || *s.MemUsed[0] != 200 || *s.LoadMax[0] != 2 || *s.Load[0] != 1 {
		t.Fatalf("aggregates: %+v", s)
	}
	if s.DiskUsed != nil {
		t.Fatal("columns without data must be omitted")
	}
}

func TestCompactLeavesTheOpenBucketAlone(t *testing.T) {
	ctx, store := context.Background(), testHistory(t)
	_ = store.Record(ctx, bucketStart.Add(time.Minute), map[string]Sample{"server:a": {CPU: f(5)}})
	if err := store.Compact(ctx, bucketStart.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = store.DB.QueryRow(`SELECT COUNT(*) FROM metric_points_15m`).Scan(&count)
	if count != 0 {
		t.Fatalf("open bucket aggregated: %d", count)
	}
}

func TestRetentionDropsOldRawAfterAggregatingAndOldBuckets(t *testing.T) {
	ctx, store := context.Background(), testHistory(t)
	now := bucketStart.Add(48 * time.Hour)
	old := now.Add(-25 * time.Hour).Truncate(15 * time.Minute)
	recent := now.Add(-2 * time.Hour)
	for _, at := range []time.Time{old, recent} {
		_ = store.Record(ctx, at, map[string]Sample{"server:a": {CPU: f(1)}})
	}
	ancient := now.Add(-31 * 24 * time.Hour).Unix()
	_, _ = store.DB.Exec(`INSERT INTO metric_points_15m(subject,ts,cpu_avg) VALUES('server:a',?,1)`, ancient)
	if err := store.Compact(ctx, now); err != nil {
		t.Fatal(err)
	}
	count := func(table string) (n int) {
		_ = store.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
		return
	}
	if count("metric_points_raw") != 1 {
		t.Fatalf("raw rows = %d, want only the recent point", count("metric_points_raw"))
	}
	// The old raw point survives as a bucket; the 31 day old bucket is gone.
	if count("metric_points_15m") != 2 {
		t.Fatalf("bucket rows = %d", count("metric_points_15m"))
	}
	if err := store.Compact(ctx, now); err != nil {
		t.Fatal(err)
	}
	if count("metric_points_15m") != 2 {
		t.Fatal("compaction must be idempotent")
	}
}

func TestRecordReplacesTheSameMinuteAndSkipsEmptySamples(t *testing.T) {
	ctx, store := context.Background(), testHistory(t)
	at := bucketStart.Add(time.Minute)
	_ = store.Record(ctx, at, map[string]Sample{"server:a": {CPU: f(1)}, "server:empty": {}})
	_ = store.Record(ctx, at.Add(10*time.Second), map[string]Sample{"server:a": {CPU: f(2)}})
	r, _ := ParseRange("1h")
	history, _ := store.Query(ctx, "server:a", r, at.Add(time.Minute))
	if len(history.Series.T) != 1 || *history.Series.CPU[0] != 2 {
		t.Fatalf("%+v", history.Series)
	}
	empty, _ := store.Query(ctx, "server:empty", r, at.Add(time.Minute))
	if len(empty.Series.T) != 0 || empty.Series.T == nil {
		t.Fatal("empty series must be an empty array")
	}
}

func TestRangeSelectsResolution(t *testing.T) {
	want := map[string]time.Duration{"1h": time.Minute, "6h": time.Minute, "24h": time.Minute, "7d": 15 * time.Minute, "30d": 15 * time.Minute}
	for name, step := range want {
		r, ok := ParseRange(name)
		if !ok || r.Resolution != step {
			t.Fatalf("%s: %+v %v", name, r, ok)
		}
	}
	if _, ok := ParseRange("1y"); ok {
		t.Fatal("unknown range accepted")
	}
}

func TestQueryIsScopedToTheSubjectAndWindow(t *testing.T) {
	ctx, store := context.Background(), testHistory(t)
	now := bucketStart.Add(3 * time.Hour)
	_ = store.Record(ctx, now.Add(-30*time.Minute), map[string]Sample{"server:a": {CPU: f(1)}, "server:b": {CPU: f(9)}})
	_ = store.Record(ctx, now.Add(-2*time.Hour), map[string]Sample{"server:a": {CPU: f(7)}})
	r, _ := ParseRange("1h")
	history, _ := store.Query(ctx, "server:a", r, now)
	if len(history.Series.T) != 1 || *history.Series.CPU[0] != 1 {
		t.Fatalf("%+v", history.Series)
	}
}

// TestGrowthEstimate measures the on-disk cost of the history. It asserts
// only a generous ceiling; the logged numbers are documented in the notes.
func TestGrowthEstimate(t *testing.T) {
	ctx, store := context.Background(), testHistory(t)
	const subjects = 20
	samples := func() map[string]Sample {
		m := map[string]Sample{}
		for n := 0; n < subjects; n++ {
			m[SubjectKey("application", string(rune('a'+n))+"0123456789abcdef0123456789abcdef")] =
				Sample{CPU: f(12.34), MemUsed: i(123456789), Load1: f(0.5)}
		}
		return m
	}
	const minutes = 480
	for minute := 0; minute < minutes; minute++ {
		_ = store.Record(ctx, bucketStart.Add(time.Duration(minute)*time.Minute), samples())
	}
	_ = store.Compact(ctx, bucketStart.Add(minutes*time.Minute))
	size := func(table string) (n int64) {
		_ = store.DB.QueryRow(`SELECT COALESCE(SUM(pgsize),0) FROM dbstat WHERE name=?`, table).Scan(&n)
		return
	}
	rawRows, bucketRows := int64(subjects*minutes), int64(subjects*minutes/15)
	rawBytes, bucketBytes := size("metric_points_raw"), size("metric_points_15m")
	if rawBytes == 0 {
		t.Skip("dbstat unavailable")
	}
	rawPerRow, bucketPerRow := float64(rawBytes)/float64(rawRows), float64(bucketBytes)/float64(bucketRows)
	t.Logf("raw: %.1f B/row, 15m: %.1f B/row (%d-char subjects)", rawPerRow, bucketPerRow, len("application:a0123456789abcdef0123456789abcdef"))
	t.Logf("per subject steady state: raw %.0f KiB/24h + 15m %.0f KiB/30d", rawPerRow*1440/1024, bucketPerRow*2880/1024)
	if rawPerRow > 120 || bucketPerRow > 200 {
		t.Fatalf("rows too large: raw %.1f, bucket %.1f", rawPerRow, bucketPerRow)
	}
}
