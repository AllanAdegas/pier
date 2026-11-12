package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"pier/internal/server"
)

// SampleInterval is how often every reachable server is sampled.
const SampleInterval = time.Minute

// MetricsSource is the memoized snapshot of a server. server.Service shares
// one `docker stats` run between the panel, the alerts and this sampler.
type MetricsSource interface {
	Metrics(ctx context.Context, serverID string) (server.Metrics, error)
}

// ServerLister enumerates the servers and their last known reachability.
type ServerLister interface {
	List(ctx context.Context) ([]server.Server, error)
}

// Sampler records the metrics history of every reachable server.
type Sampler struct {
	Servers  ServerLister
	Source   MetricsSource
	Store    *HistoryStore
	Interval time.Duration
	Logger   *slog.Logger
	Now      func() time.Time

	mu          sync.Mutex
	inFlight    map[string]bool
	lastCompact time.Time
}

// Run samples every interval until ctx is cancelled. Compaction runs once
// per aggregation bucket.
func (s *Sampler) Run(ctx context.Context) {
	interval := s.Interval
	if interval <= 0 {
		interval = SampleInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.SampleOnce(ctx)
		s.compactIfDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// SampleOnce collects every reachable server in parallel and waits. A server
// whose previous collection is still running is skipped, so one slow host
// never accumulates work.
func (s *Sampler) SampleOnce(ctx context.Context) {
	servers, err := s.Servers.List(ctx)
	if err != nil {
		s.Logger.Warn("metrics: list servers", "error", err)
		return
	}
	var wait sync.WaitGroup
	for _, target := range servers {
		if target.Status != server.StatusReachable || !s.begin(target.ID) {
			continue
		}
		wait.Add(1)
		go func(id string) {
			defer wait.Done()
			defer s.finish(id)
			s.sampleServer(ctx, id)
		}(target.ID)
	}
	wait.Wait()
}

func (s *Sampler) begin(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight == nil {
		s.inFlight = map[string]bool{}
	}
	if s.inFlight[id] {
		return false
	}
	s.inFlight[id] = true
	return true
}

func (s *Sampler) finish(id string) {
	s.mu.Lock()
	delete(s.inFlight, id)
	s.mu.Unlock()
}

func (s *Sampler) sampleServer(ctx context.Context, id string) {
	metrics, err := s.Source.Metrics(ctx, id)
	if err != nil {
		return // an unreachable or failing host is a gap, not a zero
	}
	samples := resourceSamples(metrics.Containers)
	samples[SubjectKey("server", id)] = hostSample(metrics)
	if err := s.Store.Record(ctx, s.now(), samples); err != nil {
		s.Logger.Warn("metrics: record", "server", id, "error", err)
	}
}

func (s *Sampler) compactIfDue(ctx context.Context) {
	now := s.now()
	if !s.lastCompact.IsZero() && now.Sub(s.lastCompact) < bucketInterval {
		return
	}
	if err := s.Store.Compact(ctx, now); err != nil {
		s.Logger.Warn("metrics: compact", "error", err)
		return
	}
	s.lastCompact = now
}

func (s *Sampler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
