package gitprovider

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestQueueRetriesAcrossRestartAndResendIsScoped(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	if _, err := s.db.Exec(`INSERT INTO git_provider_selections VALUES('application','app',123,'acme/app')`); err != nil {
		t.Fatal(err)
	}
	code, calls := 500, 0
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			io.WriteString(w, `{"token":"secret","expires_at":"2099-01-01T00:00:00Z"}`)
			return
		}
		calls++
		w.WriteHeader(code)
		io.WriteString(w, "secret upstream body")
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	policy := RetryPolicy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: time.Minute, Jitter: func() float64 { return 0 }}
	makeQueue := func() *Queue {
		return NewQueue(s, func(context.Context, string, string) (string, error) { return "https://github.com/acme/app.git", nil }, policy, logger)
	}
	q := makeQueue()
	report := RunReport{Kind: KindApplication, ResourceID: "app", SHA: sha, Context: "pier/app", Run: "run1", Phase: PhaseSucceeded}
	if err := q.EnqueueRun(t.Context(), report); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("enqueue contacted provider")
	}
	q.Flush(t.Context())
	rows, err := q.FailedReports(t.Context(), KindApplication, "app")
	if err != nil || len(rows) != 1 || rows[0].Attempts != 1 || strings.Contains(rows[0].LastError, "secret") {
		t.Fatal(rows, err)
	}
	q = makeQueue()
	now = now.Add(time.Minute)
	q.Flush(t.Context())
	rows, _ = q.FailedReports(t.Context(), KindApplication, "app")
	if len(rows) != 1 || rows[0].State != ReportDead {
		t.Fatal(rows)
	}
	if err := q.Resend(t.Context(), KindStack, "stack", rows[0].ID); err == nil {
		t.Fatal("cross-resource resend accepted")
	}
	code = 201
	// A forbidden check endpoint falls back to statuses; the fallback succeeds.
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			io.WriteString(w, `{"token":"secret","expires_at":"2099-01-01T00:00:00Z"}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/check-runs") {
			w.WriteHeader(403)
			return
		}
		w.WriteHeader(code)
	})
	if err := q.Resend(t.Context(), KindApplication, "app", rows[0].ID); err != nil {
		t.Fatal(err)
	}
	q.Flush(t.Context())
	rows, _ = q.FailedReports(t.Context(), KindApplication, "app")
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestQueueRevisionAndPhaseOrdering(t *testing.T) {
	s := fixture(t)
	ctx := t.Context()
	now := time.Now()
	row := reportRow{kind: KindStack, resourceID: "stack", operation: operationRun, subject: "subject", runID: "run", phaseRank: 2, payload: "building"}
	if err := upsert(ctx, s.db, row, now); err != nil {
		t.Fatal(err)
	}
	batch, err := due(ctx, s.db, now, 10)
	if err != nil || len(batch) != 1 {
		t.Fatal(batch, err)
	}
	old := batch[0]
	row.phaseRank = 3
	row.payload = "finished"
	if err := upsert(ctx, s.db, row, now); err != nil {
		t.Fatal(err)
	}
	if err := delivered(ctx, s.db, old, 42, now); err != nil {
		t.Fatal(err)
	}
	row.phaseRank = 1
	row.payload = "stale"
	if err := upsert(ctx, s.db, row, now); err != nil {
		t.Fatal(err)
	}
	batch, err = due(ctx, s.db, now, 10)
	if err != nil || len(batch) != 1 || batch[0].payload != "finished" || batch[0].checkRunID != 42 {
		t.Fatal(batch, err)
	}
	if _, err := s.db.Exec(`DELETE FROM stacks WHERE id='stack'`); err != nil {
		t.Fatal(err)
	}
	batch, err = due(ctx, s.db, now, 10)
	if err != nil || len(batch) != 0 {
		t.Fatal(batch, err)
	}
}

func TestRetryPolicyCapsAndJitters(t *testing.T) {
	if delay := DefaultRetryPolicy().Delay(1); delay < 7500*time.Millisecond || delay > 15*time.Second {
		t.Fatal(delay)
	}
	p := RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 8 * time.Second, Jitter: func() float64 { return 0 }}.normalized()
	for attempts, want := range map[int]time.Duration{1: time.Second / 2, 2: time.Second, 3: 2 * time.Second, 20: 4 * time.Second} {
		if got := p.Delay(attempts); got != want {
			t.Errorf("attempt %d: %v, want %v", attempts, got, want)
		}
	}
}
