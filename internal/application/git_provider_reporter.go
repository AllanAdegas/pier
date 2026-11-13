package application

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"pier/internal/gitprovider"
)

// feedbackEnqueueTimeout bounds the database write of one report, the only
// work done on the deployment path.
const feedbackEnqueueTimeout = 5 * time.Second

// FeedbackQueue stores reports for the Git provider (gitprovider.Queue). It
// only writes them down; sending, retries and dead letters happen elsewhere.
type FeedbackQueue interface {
	EnqueueRun(ctx context.Context, report gitprovider.RunReport) error
	EnqueueComment(ctx context.Context, report gitprovider.CommentReport) error
}

// GitFeedback turns deployments and previews into reports on the durable
// queue. It implements PreviewReporter and DeploymentReporter: a failure to
// enqueue is logged (never with a credential) and never reaches the
// deployment.
type GitFeedback struct {
	queue     FeedbackQueue
	publicURL func(context.Context) string
	logger    *slog.Logger
}

// NewGitFeedback reports to queue; publicURL may return "" (no links to Pier).
func NewGitFeedback(queue FeedbackQueue, publicURL func(context.Context) string, logger *slog.Logger) *GitFeedback {
	return &GitFeedback{queue: queue, publicURL: publicURL, logger: logger}
}

// enqueue runs one queue write with a bounded time, detached from the
// caller's cancellation, and swallows its error after logging it.
func (f *GitFeedback) enqueue(ctx context.Context, resource string, write func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), feedbackEnqueueTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			f.logger.Error("git feedback enqueue panicked", "resource", resource)
		}
	}()
	if err := write(ctx); err != nil {
		f.logger.Warn("git feedback not queued", "resource", resource, "error", err.Error())
	}
}

// PierURL joins the public URL of the instance and a panel path; "" when no
// public URL is set.
func PierURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return ""
	}
	return base + path
}

func (f *GitFeedback) deploymentURL(ctx context.Context, deploymentID string) string {
	if deploymentID == "" {
		return ""
	}
	return PierURL(f.publicURL(ctx), "/deployments/"+deploymentID)
}
