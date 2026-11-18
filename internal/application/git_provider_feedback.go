package application

import (
	"context"
	"errors"
	"strings"

	"pier/internal/gitprovider"
	"pier/internal/platform/i18n"
)

// ReportDeployment queues the state of a deployment of the application on
// its commit (a check run on GitHub, a commit status elsewhere).
func (f *GitFeedback) ReportDeployment(ctx context.Context, report DeploymentReport) {
	if !ValidCommit(report.Deployment.CommitSHA) {
		return
	}
	phase := deploymentPhase(report.Event)
	run := gitprovider.RunReport{
		Kind: gitprovider.KindApplication, ResourceID: report.Application.ID, SHA: report.Deployment.CommitSHA,
		Context: gitprovider.DeploymentContext("pier/", report.Application.Name), Run: report.Deployment.ID,
		Phase: phase, Description: gitprovider.PhaseDescription(phase),
	}
	f.enqueue(ctx, report.Application.ID, func(ctx context.Context) error {
		run.TargetURL = f.deploymentURL(ctx, report.Deployment.ID)
		return f.queue.EnqueueRun(ctx, run)
	})
}

func deploymentPhase(event DeploymentEvent) gitprovider.Phase {
	switch event {
	case DeploymentEventBuilding:
		return gitprovider.PhaseInProgress
	case DeploymentEventSucceeded:
		return gitprovider.PhaseSucceeded
	case DeploymentEventFailed:
		return gitprovider.PhaseFailed
	case DeploymentEventCancelled:
		return gitprovider.PhaseCancelled
	}
	return gitprovider.PhaseQueued
}

// ReportPreview queues the preview state on the pull request head and the
// one comment with the preview URL.
func (f *GitFeedback) ReportPreview(ctx context.Context, report PreviewReport) {
	f.enqueue(ctx, report.Application.ID, func(ctx context.Context) error {
		logURL := f.deploymentURL(ctx, report.DeploymentID)
		var runErr error
		if report.Event != PreviewEventDestroyed && ValidCommit(report.Preview.CommitSHA) && report.DeploymentID != "" {
			runErr = f.queue.EnqueueRun(ctx, previewRun(report, logURL))
		}
		commentErr := f.queue.EnqueueComment(ctx, gitprovider.CommentReport{
			Kind: gitprovider.KindApplication, ResourceID: report.Application.ID,
			Comment: gitprovider.Comment{
				Number: report.Preview.Number, Marker: "pier:preview:" + strings.ToLower(report.Application.ID),
				Body: previewComment(report, logURL),
			},
		})
		return errors.Join(runErr, commentErr)
	})
}

func previewRun(report PreviewReport, logURL string) gitprovider.RunReport {
	language := i18n.Instance()
	run := gitprovider.RunReport{
		Kind: gitprovider.KindApplication, ResourceID: report.Application.ID, SHA: report.Preview.CommitSHA,
		Context: gitprovider.DeploymentContext("pier/preview: ", report.Application.Name), Run: report.DeploymentID,
		Phase: gitprovider.PhaseInProgress, Description: language.Text("Preview em construção no Pier."), TargetURL: logURL,
	}
	switch report.Event {
	case PreviewEventReady:
		run.Phase, run.Description, run.TargetURL = gitprovider.PhaseSucceeded, language.Text("Preview no ar."), report.Preview.URL
	case PreviewEventFailed:
		run.Phase, run.Description = gitprovider.PhaseFailed, language.Text("O preview falhou. Veja o log no Pier.")
	}
	return run
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
