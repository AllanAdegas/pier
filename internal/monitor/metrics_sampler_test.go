package monitor

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pier/internal/server"
)

type fakeServers []server.Server

func (f fakeServers) List(context.Context) ([]server.Server, error) { return f, nil }

type countingSource struct {
	calls   atomic.Int32
	release chan struct{}
	metrics server.Metrics
}

func (c *countingSource) Metrics(_ context.Context, _ string) (server.Metrics, error) {
	c.calls.Add(1)
	if c.release != nil {
		<-c.release
	}
	return c.metrics, nil
}

func testSampler(t *testing.T, servers fakeServers, source MetricsSource) *Sampler {
	t.Helper()
	return &Sampler{Servers: servers, Source: source, Store: testHistory(t),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return bucketStart.Add(time.Minute) }}
}

func sampleMetrics() server.Metrics {
	used := int64(2 << 30)
	return server.Metrics{
		CPUPercent: f(37.5), MemoryUsedBytes: &used, MemoryTotalBytes: 4 << 30,
		DiskUsedBytes: 10 << 30, DiskTotalBytes: 100 << 30, LoadAverage: []float64{0.7, 0.5, 0.4},
		Containers: []server.ContainerUsage{
			{ResourceType: "application", ResourceID: "app1", State: "running", CPUPercent: "1.50%", MemoryUsage: "10MiB / 1GiB"},
			{ResourceType: "stack", ResourceID: "st1", State: "running", CPUPercent: "2.00%", MemoryUsage: "1.5GiB / 4GiB"},
			{ResourceType: "stack", ResourceID: "st1", State: "running", CPUPercent: "3.00%", MemoryUsage: "512MiB / 4GiB"},
			{ResourceType: "database", ResourceID: "db1", State: "exited"},
			{ResourceType: "proxy", ResourceID: "", State: "running", CPUPercent: "9%", MemoryUsage: "5MiB / 1GiB"},
		},
	}
}

func TestSamplerRecordsHostAndResourcesOfReachableServersOnly(t *testing.T) {
	source := &countingSource{metrics: sampleMetrics()}
	sampler := testSampler(t, fakeServers{
		{ID: "s1", Status: server.StatusReachable},
		{ID: "s2", Status: server.StatusUnreachable},
		{ID: "s3", Status: server.StatusUnknown},
	}, source)
	sampler.SampleOnce(context.Background())
	if source.calls.Load() != 1 {
		t.Fatalf("collections = %d, want 1 (offline servers skipped)", source.calls.Load())
	}
	r, _ := ParseRange("1h")
	now := bucketStart.Add(2 * time.Minute)
	host, _ := sampler.Store.Query(context.Background(), "server:s1", r, now)
	if len(host.Series.T) != 1 || *host.Series.CPU[0] != 37.5 || *host.Series.DiskTotal[0] != float64(100<<30) || *host.Series.Load[0] != 0.7 {
		t.Fatalf("host: %+v", host.Series)
	}
	stack, _ := sampler.Store.Query(context.Background(), "stack:st1", r, now)
	if *stack.Series.CPU[0] != 5 || *stack.Series.MemUsed[0] != float64(2<<30) {
		t.Fatalf("stack sums its containers: %+v", stack.Series)
	}
	for _, subject := range []string{"database:db1", "proxy:", "server:s2"} {
		other, _ := sampler.Store.Query(context.Background(), subject, r, now)
		if len(other.Series.T) != 0 {
			t.Fatalf("%s must have no points", subject)
		}
	}
}

func TestSamplerRunsOneCollectionPerServerAtATime(t *testing.T) {
	source := &countingSource{metrics: sampleMetrics(), release: make(chan struct{})}
	sampler := testSampler(t, fakeServers{{ID: "s1", Status: server.StatusReachable}}, source)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() { defer wait.Done(); sampler.SampleOnce(context.Background()) }()
	for source.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	sampler.SampleOnce(context.Background()) // the first is still collecting: skipped
	close(source.release)
	wait.Wait()
	if source.calls.Load() != 1 {
		t.Fatalf("collections = %d, want 1", source.calls.Load())
	}
}

func TestSamplerSharesTheMemoizedSnapshotWithOtherCallers(t *testing.T) {
	// The sampler asks the same MetricsSource the API uses, never docker
	// itself: any number of ticks goes through Metrics(), which memoizes.
	source := &countingSource{metrics: sampleMetrics()}
	sampler := testSampler(t, fakeServers{{ID: "s1", Status: server.StatusReachable}}, source)
	sampler.SampleOnce(context.Background())
	sampler.SampleOnce(context.Background())
	if source.calls.Load() != 2 {
		t.Fatalf("calls = %d", source.calls.Load())
	}
}

func TestSamplerCompactsOncePerBucket(t *testing.T) {
	source := &countingSource{metrics: sampleMetrics()}
	sampler := testSampler(t, fakeServers{{ID: "s1", Status: server.StatusReachable}}, source)
	clock := bucketStart.Add(time.Minute)
	sampler.Now = func() time.Time { return clock }
	sampler.SampleOnce(context.Background())
	clock = bucketStart.Add(16 * time.Minute)
	sampler.compactIfDue(context.Background())
	first := sampler.lastCompact
	clock = clock.Add(5 * time.Minute)
	sampler.compactIfDue(context.Background())
	if !sampler.lastCompact.Equal(first) {
		t.Fatal("compacted again before a bucket elapsed")
	}
	var buckets int
	_ = sampler.Store.DB.QueryRow(`SELECT COUNT(*) FROM metric_points_15m`).Scan(&buckets)
	if buckets == 0 {
		t.Fatal("no aggregates written")
	}
}

func TestParseDockerUsage(t *testing.T) {
	cases := map[string]int64{"10MiB / 1GiB": 10 << 20, "1.5GiB / 4GiB": 3 << 29, "512B / 1GiB": 512, "2kB / 1GiB": 2000}
	for text, want := range cases {
		if got, ok := parseMemoryUsage(text); !ok || got != want {
			t.Fatalf("%q = %d %v", text, got, ok)
		}
	}
	for _, bad := range []string{"", "--", "NaNMiB / 1", "10XB / 1", "-5MiB / 1"} {
		if _, ok := parseMemoryUsage(bad); ok {
			t.Fatalf("%q accepted", bad)
		}
	}
	if v, ok := parsePercent(" 12.50% "); !ok || v != 12.5 {
		t.Fatal("percent")
	}
	if _, ok := parsePercent("--"); ok {
		t.Fatal("bad percent accepted")
	}
}
