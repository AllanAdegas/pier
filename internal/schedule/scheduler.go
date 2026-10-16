// Package schedule runs recurring work: database backups and the scheduled
// tasks of applications.
package schedule

import (
	"context"
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"
)

// Job is one recurring unit of work.
type Job struct {
	Name string
	Cron string
	Run  func()
}

// Source lists the jobs that should currently be scheduled.
type Source func(ctx context.Context) ([]Job, error)

// Scheduler keeps the cron table in sync with what the sources report.
type Scheduler struct {
	logger  *slog.Logger
	sources []Source

	mu      sync.Mutex
	engine  *cron.Cron
	entries []cron.EntryID
}

// NewScheduler creates a scheduler fed by the given sources.
func NewScheduler(logger *slog.Logger, sources ...Source) *Scheduler {
	return &Scheduler{logger: logger, sources: sources, engine: cron.New()}
}

// AddSource registers another source; call Reload afterwards.
func (s *Scheduler) AddSource(source Source) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sources = append(s.sources, source)
}

// Start begins firing jobs.
func (s *Scheduler) Start(ctx context.Context) {
	s.Reload(ctx)
	s.engine.Start()
}

// Stop waits for running jobs to finish.
func (s *Scheduler) Stop() { <-s.engine.Stop().Done() }

// Reload rebuilds the cron table from scratch. It is cheap and is called
// whenever a schedule is created, changed or removed.
func (s *Scheduler) Reload(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.entries {
		s.engine.Remove(entry)
	}
	s.entries = s.entries[:0]
	for _, source := range s.sources {
		jobs, err := source(ctx)
		if err != nil {
			s.logger.Error("load scheduled jobs", "error", err)
			continue
		}
		for _, job := range jobs {
			entry, err := s.engine.AddFunc(job.Cron, job.Run)
			if err != nil {
				s.logger.Warn("skip job with invalid cron", "job", job.Name, "cron", job.Cron, "error", err)
				continue
			}
			s.entries = append(s.entries, entry)
		}
	}
}
