package gitprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"pier/internal/platform/i18n"
)

// checksDeniedFor is how long an installation that refused check runs
// reports through commit statuses before check runs are tried again.
const checksDeniedFor = time.Hour

// ErrChecksUnavailable means the GitHub App cannot write check runs (an App
// created before Pier asked for "checks: write"): report a commit status.
var ErrChecksUnavailable = errors.New("GitHub check runs not permitted")

// CheckRunReporter writes GitHub check runs. id is the check run of the same
// deployment run already created (0: none); the result is the check run id.
type CheckRunReporter interface {
	UpsertCheckRun(ctx context.Context, report RunReport, id int64) (int64, error)
}

type checkRunOutput struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

type checkRunInput struct {
	Name        string         `json:"name,omitempty"`
	HeadSHA     string         `json:"head_sha,omitempty"`
	Status      string         `json:"status"`
	Conclusion  string         `json:"conclusion,omitempty"`
	ExternalID  string         `json:"external_id,omitempty"`
	DetailsURL  string         `json:"details_url,omitempty"`
	StartedAt   string         `json:"started_at,omitempty"`
	CompletedAt string         `json:"completed_at,omitempty"`
	Output      checkRunOutput `json:"output"`
}

func (r *githubReporter) checkKey() tokenKey {
	return tokenKey{installation: r.selection.InstallationID, repository: r.selection.Repository, purpose: purposeCheck}
}

// UpsertCheckRun creates the check run of a deployment run, or moves the one
// already created to the phase of report (queued, in progress, completed).
func (r *githubReporter) UpsertCheckRun(ctx context.Context, report RunReport, id int64) (int64, error) {
	if err := validStatus(report.status()); err != nil {
		return 0, err
	}
	if r.service.checksDenied(r.selection.InstallationID) {
		return 0, ErrChecksUnavailable
	}
	input := r.service.checkRunInput(report)
	base := repositoryPath(r.selection.Repository) + "/check-runs"
	var created struct {
		ID int64 `json:"id"`
	}
	err := r.service.withInstallationToken(ctx, r.checkKey(), func(token string) error {
		if id > 0 {
			err := r.service.github(ctx, http.MethodPatch, fmt.Sprintf("%s/%d", base, id), token, input, nil)
			if statusOf(err) != http.StatusNotFound {
				created.ID = id
				return err
			}
			// The check run is gone (deleted with the commit): start another.
		}
		input.Name, input.HeadSHA = report.Context, report.SHA
		return r.service.github(ctx, http.MethodPost, base, token, input, &created)
	})
	if refusedChecks(err) {
		r.service.denyChecks(r.selection.InstallationID)
		return 0, ErrChecksUnavailable
	}
	if err != nil {
		return 0, err
	}
	if created.ID <= 0 {
		return 0, fmt.Errorf("GitHub returned no check run id")
	}
	return created.ID, nil
}

// refusedChecks tells a missing "checks: write" permission: the token mint
// is refused for it (403/422) or the API answers 403 to the installation.
func refusedChecks(err error) bool {
	switch mintStatus(err) {
	case http.StatusForbidden, http.StatusUnprocessableEntity:
		return true
	case 0:
		return statusOf(err) == http.StatusForbidden
	}
	return false
}

func (s *Service) checkRunInput(report RunReport) checkRunInput {
	now := s.now().UTC().Format(time.RFC3339)
	input := checkRunInput{
		Status: "queued", ExternalID: report.Run, DetailsURL: report.TargetURL,
		Output: checkRunOutput{Title: truncate(report.Description, 140), Summary: checkSummary(report)},
	}
	switch {
	case report.Phase == PhaseInProgress:
		input.Status, input.StartedAt = "in_progress", now
	case report.Phase.Finished():
		input.Status, input.CompletedAt, input.Conclusion = "completed", now, checkConclusion(report.Phase)
	}
	return input
}

func checkConclusion(phase Phase) string {
	switch phase {
	case PhaseSucceeded:
		return "success"
	case PhaseFailed:
		return "failure"
	}
	return "cancelled"
}

// checkSummary never carries a failure reason: the pull request may be
// public and the reason may name internal paths or values.
func checkSummary(report RunReport) string {
	language := i18n.Instance()
	summary := report.Description + "\n\n" + language.Sprintf("Commit: `%s`", report.SHA[:12])
	if report.TargetURL != "" {
		summary += "\n\n" + language.Sprintf("[Detalhes](%s)", report.TargetURL)
	}
	return summary
}

func (s *Service) checksDenied(installation int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, found := s.checksRefused[installation]
	return found && s.now().Before(until)
}

func (s *Service) denyChecks(installation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checksRefused[installation] = s.now().Add(checksDeniedFor)
}
