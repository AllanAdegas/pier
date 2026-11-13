package gitprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"pier/internal/platform/apperr"
)

const (
	queueBatch       = 32
	queueWorkers     = 4
	queueSendTimeout = 60 * time.Second
	queueIdlePoll    = time.Minute
	queuePruneEvery  = time.Hour
	// queueRetention keeps delivered reports (and their check run ids) and
	// dead letters for a month.
	queueRetention = 30 * 24 * time.Hour
)

// RepositorySource returns the repository the source of a resource uses
// now; the sender resolves it at send time, so no credential that may be in
// the URL is ever copied into the queue. A not found error drops the report.
type RepositorySource func(ctx context.Context, kind, id string) (string, error)

// Queue is the durable outbox of Git provider feedback. Enqueue only writes a
// row (never calls a provider), so reporting never blocks or fails a
// deployment; Run sends the rows with retry, exponential backoff and jitter,
// and dead-letters a report after RetryPolicy.MaxAttempts. Reports survive a
// restart: rows pending when Pier stopped are sent when it starts again.
type Queue struct {
	service      *Service
	db           *sql.DB
	repositories RepositorySource
	logger       *slog.Logger
	policy       RetryPolicy

	wake chan struct{}

	mu       sync.Mutex
	inflight map[int64]bool
}

// NewQueue creates the queue of the service's database.
func NewQueue(service *Service, repositories RepositorySource, policy RetryPolicy, logger *slog.Logger) *Queue {
	return &Queue{
		service: service, db: service.db, repositories: repositories, logger: logger,
		policy: policy.normalized(), wake: make(chan struct{}, 1), inflight: map[int64]bool{},
	}
}

func (q *Queue) now() time.Time { return q.service.now() }

// EnqueueRun stores the state of a deployment run, when the resource reports
// commit statuses. It never talks to the provider.
func (q *Queue) EnqueueRun(ctx context.Context, report RunReport) error {
	if err := report.validate(); err != nil {
		return err
	}
	wanted, err := q.wants(ctx, report.Kind, report.ResourceID, func(s FeedbackSettings) bool { return s.CommitStatus })
	if err != nil || !wanted {
		return err
	}
	return q.store(ctx, reportRow{
		kind: report.Kind, resourceID: report.ResourceID, operation: operationRun, subject: report.subject(),
		runID: report.Run, phaseRank: report.Phase.rank(),
	}, report)
}

// EnqueueComment stores a pull request comment, when the resource reports them.
func (q *Queue) EnqueueComment(ctx context.Context, report CommentReport) error {
	if err := report.validate(); err != nil {
		return err
	}
	wanted, err := q.wants(ctx, report.Kind, report.ResourceID, func(s FeedbackSettings) bool { return s.PullRequestComments })
	if err != nil || !wanted {
		return err
	}
	return q.store(ctx, reportRow{
		kind: report.Kind, resourceID: report.ResourceID, operation: operationComment, subject: report.subject(),
	}, report)
}

// wants reads the settings (database only) so resources without feedback
// never fill the queue.
func (q *Queue) wants(ctx context.Context, kind, id string, enabled func(FeedbackSettings) bool) (bool, error) {
	settings, err := q.service.FeedbackSettings(ctx, kind, id)
	if err != nil {
		return false, err
	}
	return settings.Provider != ProviderNone && enabled(settings), nil
}

func (q *Queue) store(ctx context.Context, row reportRow, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	row.payload = string(raw)
	if err := upsert(ctx, q.db, row, q.now()); err != nil {
		return err
	}
	q.signal()
	return nil
}

func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// FailedReports lists the reports of a resource that are retrying or dead.
func (q *Queue) FailedReports(ctx context.Context, kind, id string) ([]FailedReport, error) {
	if !validKind(kind) || id == "" {
		return nil, invalid()
	}
	if err := q.service.requireResource(ctx, kind, id); err != nil {
		return nil, err
	}
	return failedReports(ctx, q.db, kind, id)
}

// Resend sends a failed report of the resource again, from its first attempt.
func (q *Queue) Resend(ctx context.Context, kind, id string, reportID int64) error {
	if !validKind(kind) || id == "" || reportID <= 0 {
		return apperr.NotFound("Envio")
	}
	found, err := resend(ctx, q.db, kind, id, reportID, q.now())
	if err != nil {
		return err
	}
	if !found {
		return apperr.NotFound("Envio")
	}
	q.signal()
	return nil
}

// Run sends due reports until ctx ends. Start it once.
func (q *Queue) Run(ctx context.Context) {
	lastPrune := time.Time{}
	for {
		if q.now().Sub(lastPrune) >= queuePruneEvery {
			if err := prune(ctx, q.db, q.now().Add(-queueRetention)); err != nil && ctx.Err() == nil {
				q.logger.Warn("git feedback prune failed", "error", err)
			}
			lastPrune = q.now()
		}
		q.Flush(ctx)
		timer := time.NewTimer(q.idleWait(ctx))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-q.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// idleWait is how long to sleep until the next report is due.
func (q *Queue) idleWait(ctx context.Context) time.Duration {
	next, found, err := nextDue(ctx, q.db)
	if err != nil || !found {
		return queueIdlePoll
	}
	return min(max(next.Sub(q.now()), 10*time.Millisecond), queueIdlePoll)
}

// Flush sends every report due now, in batches, and returns when none is
// due (failed ones wait for their next attempt).
func (q *Queue) Flush(ctx context.Context) {
	// seen stops a report whose outcome could not be recorded from being
	// sent again in the same flush.
	seen := map[int64]bool{}
	for ctx.Err() == nil {
		batch, err := due(ctx, q.db, q.now(), queueBatch)
		if err != nil {
			if ctx.Err() == nil {
				q.logger.Warn("git feedback queue read failed", "error", err)
			}
			return
		}
		batch = q.claim(unseen(batch, seen))
		if len(batch) == 0 {
			return
		}
		q.sendAll(ctx, batch)
	}
}

func unseen(batch []queuedReport, seen map[int64]bool) []queuedReport {
	fresh := batch[:0]
	for _, report := range batch {
		if !seen[report.id] {
			seen[report.id] = true
			fresh = append(fresh, report)
		}
	}
	return fresh
}

// claim keeps the reports no worker is sending (one process; the in-memory
// set is enough and starts empty after a restart).
func (q *Queue) claim(batch []queuedReport) []queuedReport {
	q.mu.Lock()
	defer q.mu.Unlock()
	claimed := batch[:0]
	for _, report := range batch {
		if !q.inflight[report.id] {
			q.inflight[report.id] = true
			claimed = append(claimed, report)
		}
	}
	return claimed
}

func (q *Queue) release(id int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.inflight, id)
}

func (q *Queue) sendAll(ctx context.Context, batch []queuedReport) {
	slots := make(chan struct{}, queueWorkers)
	var wg sync.WaitGroup
	for _, report := range batch {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			defer q.release(report.id)
			q.process(ctx, report)
		}()
	}
	wg.Wait()
}
