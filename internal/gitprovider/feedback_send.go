package gitprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// errNothingToSend drops a report: the resource or its feedback is gone.
var errNothingToSend = errors.New("nothing to send")

// process sends one report and records the outcome. It never panics out:
// a broken report becomes a dead letter instead of stopping the queue.
func (q *Queue) process(ctx context.Context, report queuedReport) {
	ctx, cancel := context.WithTimeout(ctx, queueSendTimeout)
	defer cancel()
	checkRunID, err := q.safeSend(ctx, report)
	record := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		err = delivered(record, q.db, report, checkRunID, q.now())
	case errors.Is(err, errNothingToSend):
		err = discard(record, q.db, report)
	default:
		err = q.retryOrBury(record, report, err)
	}
	if err != nil {
		q.logger.Warn("git feedback queue write failed", "report", report.id, "error", err)
	}
}

func (q *Queue) safeSend(ctx context.Context, report queuedReport) (checkRunID int64, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = apperr.Invalid("Envio ao provedor Git inválido.")
		}
	}()
	return q.send(ctx, report)
}

func (q *Queue) retryOrBury(ctx context.Context, report queuedReport, cause error) error {
	attempts := report.attempts + 1
	dead := permanent(cause) || attempts >= q.policy.MaxAttempts
	message := safeMessage(cause)
	next := q.now().Add(q.policy.Delay(attempts))
	attributes := []any{"kind", report.kind, "resource", report.resourceID, "report", report.id,
		"operation", report.operation, "attempt", attempts, "error", message}
	if dead {
		q.logger.Error("git feedback gave up", attributes...)
	} else {
		q.logger.Warn("git feedback failed", attributes...)
	}
	return failed(ctx, q.db, report, dead, next, message, q.now())
}

// permanent tells failures a retry cannot fix: invalid reports and provider
// refusals other than authentication, timeouts, conflicts and rate limits.
func permanent(err error) bool {
	if status := statusOf(err); status != 0 {
		switch status {
		case http.StatusUnauthorized, http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
			return false
		}
		return status >= 400 && status < 500
	}
	var expected *apperr.Error
	return errors.As(err, &expected) && expected.Kind == apperr.KindInvalid
}

// safeMessage is the text kept and shown for a failure: only messages Pier
// wrote (provider name and HTTP status at most), never an upstream body,
// URL or credential, in the language of the instance.
func safeMessage(err error) string {
	language := i18n.Instance()
	var expected *apperr.Error
	switch {
	case errors.As(err, &expected):
		return i18n.Localize(expected, language)
	case errors.Is(err, context.DeadlineExceeded):
		return language.Text("O provedor Git não respondeu a tempo.")
	}
	return language.Text("Falha inesperada ao enviar ao provedor Git.")
}

func (q *Queue) send(ctx context.Context, report queuedReport) (int64, error) {
	repository, err := q.repositories(ctx, report.kind, report.resourceID)
	if err != nil {
		var missing *apperr.Error
		if errors.As(err, &missing) && missing.Kind == apperr.KindNotFound {
			return 0, errNothingToSend
		}
		return 0, err
	}
	feedback, err := q.service.Feedback(ctx, report.kind, report.resourceID, repository)
	if errors.Is(err, ErrNoFeedback) || errors.Is(err, ErrNoSelection) {
		return 0, errNothingToSend
	}
	if err != nil {
		return 0, err
	}
	switch report.operation {
	case operationRun:
		return q.sendRun(ctx, report, feedback)
	case operationComment:
		return 0, q.sendComment(ctx, report, feedback)
	}
	return 0, apperr.Invalid("Envio ao provedor Git inválido.")
}

// sendRun writes a GitHub check run when the reporter can, and a commit
// status otherwise (token providers, or an App without "checks: write").
func (q *Queue) sendRun(ctx context.Context, report queuedReport, feedback Feedback) (int64, error) {
	if !feedback.CommitStatus {
		return 0, errNothingToSend
	}
	var run RunReport
	if err := decodePayload(report.payload, &run); err != nil {
		return 0, err
	}
	if checks, ok := feedback.Reporter.(CheckRunReporter); ok {
		id, err := checks.UpsertCheckRun(ctx, run, report.checkRunID)
		if !errors.Is(err, ErrChecksUnavailable) {
			return id, err
		}
	}
	return 0, feedback.Reporter.SetCommitStatus(ctx, run.status())
}

func (q *Queue) sendComment(ctx context.Context, report queuedReport, feedback Feedback) error {
	if !feedback.PullRequestComments {
		return errNothingToSend
	}
	var comment CommentReport
	if err := decodePayload(report.payload, &comment); err != nil {
		return err
	}
	return feedback.Reporter.UpsertComment(ctx, comment.Comment)
}

func decodePayload(payload string, target any) error {
	if err := json.Unmarshal([]byte(payload), target); err != nil {
		return fmt.Errorf("%w: %w", apperr.Invalid("Envio ao provedor Git inválido."), err)
	}
	return nil
}
