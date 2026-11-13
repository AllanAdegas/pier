package stack

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"pier/internal/application"
	"pier/internal/gitprovider"
)

const feedbackEnqueueTimeout = 5 * time.Second

// FeedbackQueue stores deploy reports for the Git provider (gitprovider.Queue).
type FeedbackQueue interface {
	EnqueueRun(ctx context.Context, report gitprovider.RunReport) error
}

// gitFeedback reports Git stack deploys like application deployments: a
// check run on GitHub, a commit status elsewhere. Unset, nothing is reported.
type gitFeedback struct {
	queue     FeedbackQueue
	publicURL func(context.Context) string
	logger    *slog.Logger
}

// SetGitFeedback installs the queue Git stack deploys report to; publicURL
// may return "" (no link to Pier).
func (s *Service) SetGitFeedback(queue FeedbackQueue, publicURL func(context.Context) string, logger *slog.Logger) {
	s.feedback = gitFeedback{queue: queue, publicURL: publicURL, logger: logger}
}

// gitRun is one deploy of a Git stack, from the moment its commit is known.
type gitRun struct {
	feedback gitFeedback
	stack    Stack
	sha      string
	id       string
}

func (s *Service) startGitRun(ctx context.Context, stack Stack, sha string) gitRun {
	run := gitRun{feedback: s.feedback, stack: stack, sha: sha, id: newRunID()}
	run.report(ctx, gitprovider.PhaseInProgress)
	return run
}

// finish reports the outcome of the deploy.
func (r gitRun) finish(ctx context.Context, err error) {
	phase := gitprovider.PhaseSucceeded
	if err != nil {
		phase = gitprovider.PhaseFailed
		if errors.Is(err, context.Canceled) {
			phase = gitprovider.PhaseCancelled
		}
	}
	r.report(ctx, phase)
}

// report never fails the deploy: errors are logged, without credentials.
func (r gitRun) report(ctx context.Context, phase gitprovider.Phase) {
	if r.feedback.queue == nil || !application.ValidCommit(r.sha) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), feedbackEnqueueTimeout)
	defer cancel()
	err := r.feedback.queue.EnqueueRun(ctx, gitprovider.RunReport{
		Kind: gitprovider.KindStack, ResourceID: r.stack.ID, SHA: r.sha, Run: r.id, Phase: phase,
		Context:     gitprovider.DeploymentContext("pier/", r.stack.Name),
		Description: gitprovider.PhaseDescription(phase),
		TargetURL:   application.PierURL(r.feedback.base(ctx), "/stacks/"+r.stack.ID),
	})
	if err != nil && r.feedback.logger != nil {
		r.feedback.logger.Warn("git feedback not queued", "stack", r.stack.ID, "error", err.Error())
	}
}

func (f gitFeedback) base(ctx context.Context) string {
	if f.publicURL == nil {
		return ""
	}
	return f.publicURL(ctx)
}

func newRunID() string {
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	return "stack-run-" + hex.EncodeToString(raw)
}
