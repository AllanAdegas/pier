package application

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"strings"
	"sync"
	"time"

	"pier/internal/gitprovider"
	"pier/internal/platform/i18n"
)

const (
	feedbackLanes       = 4
	feedbackLaneBacklog = 32
	feedbackTimeout     = 60 * time.Second
)

// FeedbackSource resolves how a resource reports to its Git provider.
type FeedbackSource interface {
	Feedback(ctx context.Context, kind, id, repository string) (gitprovider.Feedback, error)
}

// GitFeedback publishes commit statuses and the preview comment of pull
// requests. It implements PreviewReporter and DeploymentReporter: calls only
// enqueue work, so a slow or failing provider API never blocks or fails a
// deployment. Failures are logged (without credentials) and dropped.
// Reports of one resource run in order (pending before success).
type GitFeedback struct {
	source    FeedbackSource
	publicURL func(context.Context) string
	logger    *slog.Logger
	lanes     [feedbackLanes]chan feedbackTask
	pending   sync.WaitGroup
}

type feedbackTask struct {
	resource string
	run      func(context.Context) error
}

// NewGitFeedback starts the workers; publicURL may return "" (no links to Pier).
func NewGitFeedback(source FeedbackSource, publicURL func(context.Context) string, logger *slog.Logger) *GitFeedback {
	f := &GitFeedback{source: source, publicURL: publicURL, logger: logger}
	for index := range f.lanes {
		f.lanes[index] = make(chan feedbackTask, feedbackLaneBacklog)
		go f.work(f.lanes[index])
	}
	return f
}

func (f *GitFeedback) work(lane <-chan feedbackTask) {
	for task := range lane {
		f.runTask(task)
	}
}

func (f *GitFeedback) runTask(task feedbackTask) {
	defer f.pending.Done()
	defer func() {
		if recovered := recover(); recovered != nil {
			f.logger.Error("git feedback panicked", "resource", task.resource)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), feedbackTimeout)
	defer cancel()
	if err := task.run(ctx); err != nil && !errors.Is(err, gitprovider.ErrNoFeedback) &&
		!errors.Is(err, gitprovider.ErrNoSelection) {
		// Provider errors carry only the provider name and HTTP status.
		f.logger.Warn("git feedback failed", "resource", task.resource, "error", err.Error())
	}
}

// enqueue never blocks: with the lane full the report is dropped.
func (f *GitFeedback) enqueue(resource string, run func(context.Context) error) {
	hash := fnv.New32a()
	hash.Write([]byte(resource))
	f.pending.Add(1)
	select {
	case f.lanes[hash.Sum32()%feedbackLanes] <- feedbackTask{resource: resource, run: run}:
	default:
		f.pending.Done()
		f.logger.Warn("git feedback dropped: queue full", "resource", resource)
	}
}

// wait blocks until the queued reports ran (tests).
func (f *GitFeedback) wait() { f.pending.Wait() }

func (f *GitFeedback) feedback(ctx context.Context, app Application) (gitprovider.Feedback, error) {
	return f.source.Feedback(ctx, gitprovider.KindApplication, app.ID, app.Config.Source.Repository)
}

func (f *GitFeedback) deploymentURL(ctx context.Context, deploymentID string) string {
	base := strings.TrimRight(f.publicURL(ctx), "/")
	if base == "" || deploymentID == "" {
		return ""
	}
	return base + "/deployments/" + deploymentID
}

// ReportDeployment sets the commit status of a deployment of the application.
func (f *GitFeedback) ReportDeployment(_ context.Context, report DeploymentReport) {
	if !ValidCommit(report.Deployment.CommitSHA) {
		return
	}
	f.enqueue(report.Application.ID, func(ctx context.Context) error {
		feedback, err := f.feedback(ctx, report.Application)
		if err != nil || !feedback.CommitStatus {
			return err
		}
		state, description := deploymentState(report.Event)
		return feedback.Reporter.SetCommitStatus(ctx, gitprovider.CommitStatus{
			SHA: report.Deployment.CommitSHA, State: state, Context: "pier/" + statusName(report.Application.Name),
			Description: description, TargetURL: f.deploymentURL(ctx, report.Deployment.ID),
		})
	})
}

func deploymentState(event DeploymentEvent) (gitprovider.State, string) {
	language := i18n.Instance()
	switch event {
	case DeploymentEventSucceeded:
		return gitprovider.StateSuccess, language.Text("Deploy concluído no Pier.")
	case DeploymentEventFailed:
		return gitprovider.StateFailure, language.Text("O deploy falhou. Veja o log no Pier.")
	case DeploymentEventCancelled:
		return gitprovider.StateError, language.Text("Deploy cancelado ou interrompido.")
	}
	return gitprovider.StatePending, language.Text("Deploy em andamento no Pier.")
}

// statusName keeps the status context short and free of odd characters.
func statusName(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if runes := []rune(cleaned); len(runes) > 60 {
		cleaned = string(runes[:60])
	}
	return cleaned
}

// ReportPreview sets the preview status on the pull request head and keeps
// one comment with the preview URL up to date.
func (f *GitFeedback) ReportPreview(_ context.Context, report PreviewReport) {
	f.enqueue(report.Application.ID, func(ctx context.Context) error {
		feedback, err := f.feedback(ctx, report.Application)
		if err != nil {
			return err
		}
		var statusErr, commentErr error
		if feedback.CommitStatus && report.Event != PreviewEventDestroyed && ValidCommit(report.Preview.CommitSHA) {
			statusErr = feedback.Reporter.SetCommitStatus(ctx, f.previewStatus(ctx, report))
		}
		if feedback.PullRequestComments {
			commentErr = feedback.Reporter.UpsertComment(ctx, gitprovider.Comment{
				Number: report.Preview.Number, Marker: "pier:preview:" + strings.ToLower(report.Application.ID),
				Body: previewComment(report, f.deploymentURL(ctx, report.DeploymentID)),
			})
		}
		return errors.Join(statusErr, commentErr)
	})
}

func (f *GitFeedback) previewStatus(ctx context.Context, report PreviewReport) gitprovider.CommitStatus {
	language := i18n.Instance()
	status := gitprovider.CommitStatus{
		SHA: report.Preview.CommitSHA, Context: "pier/preview: " + statusName(report.Application.Name),
		State: gitprovider.StatePending, Description: language.Text("Preview em construção no Pier."),
		TargetURL: f.deploymentURL(ctx, report.DeploymentID),
	}
	switch report.Event {
	case PreviewEventReady:
		status.State, status.Description, status.TargetURL = gitprovider.StateSuccess, language.Text("Preview no ar."), report.Preview.URL
	case PreviewEventFailed:
		status.State, status.Description = gitprovider.StateFailure, language.Text("O preview falhou. Veja o log no Pier.")
	}
	return status
}

// previewComment never includes the failure reason: it may name internal
// paths or values, and the pull request can be public.
func previewComment(report PreviewReport, logURL string) string {
	language := i18n.Instance()
	title := language.Sprintf("**Pier** · preview de %s", report.Application.Name)
	var line string
	switch report.Event {
	case PreviewEventReady:
		line = language.Sprintf("Preview no ar: %s", report.Preview.URL)
	case PreviewEventFailed:
		line = language.Text("O deploy do preview falhou.")
	case PreviewEventDestroyed:
		line = language.Text("Preview removido.")
	default:
		line = language.Text("Construindo o preview…")
	}
	body := title + "\n\n" + line
	if commit := report.Preview.CommitSHA; len(commit) >= 8 && report.Event != PreviewEventDestroyed {
		body += "\n\n" + language.Sprintf("Commit: `%s`", commit[:8])
	}
	if logURL != "" && report.Event != PreviewEventDestroyed {
		body += "\n\n" + language.Sprintf("[Log do deploy](%s)", logURL)
	}
	return body
}
