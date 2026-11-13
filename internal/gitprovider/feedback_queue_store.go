package gitprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Report states in git_provider_reports.
const (
	ReportPending   = "pending"
	ReportDelivered = "delivered"
	ReportDead      = "dead"
)

const (
	operationRun     = "run"
	operationComment = "comment"
)

// queuedReport is one row of the queue, as the worker sends it.
type queuedReport struct {
	id         int64
	kind       string
	resourceID string
	operation  string
	payload    string
	runID      string
	checkRunID int64
	revision   int64
	attempts   int
}

// FailedReport is a report that is retrying or was given up, as the panel
// lists it. It has no payload body and no credential.
type FailedReport struct {
	ID            int64     `json:"id"`
	Operation     string    `json:"operation"`
	State         string    `json:"state"`
	SHA           string    `json:"sha,omitempty"`
	Context       string    `json:"context,omitempty"`
	PullRequest   int       `json:"pull_request,omitempty"`
	Phase         Phase     `json:"phase,omitempty"`
	Attempts      int       `json:"attempts"`
	LastError     string    `json:"last_error"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type reportRow struct {
	kind, resourceID, operation, subject, payload, runID string
	phaseRank                                            int
}

func millis(t time.Time) int64 { return t.UnixMilli() }

// upsert stores a report; a newer report of the same subject replaces the
// payload and is sent from scratch. A report of an earlier phase of the same
// run is ignored, so an older state never overwrites a newer one. The check
// run is kept while the run is the same.
func upsert(ctx context.Context, db *sql.DB, row reportRow, now time.Time) error {
	_, err := db.ExecContext(ctx, `INSERT INTO git_provider_reports
		(kind,resource_id,operation,subject,payload,run_id,phase_rank,state,attempts,next_attempt_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,'pending',0,?,?,?)
		ON CONFLICT(kind,resource_id,subject) DO UPDATE SET
			check_run_id = CASE WHEN git_provider_reports.run_id = excluded.run_id THEN git_provider_reports.check_run_id ELSE 0 END,
			operation=excluded.operation, payload=excluded.payload, run_id=excluded.run_id, phase_rank=excluded.phase_rank,
			revision=git_provider_reports.revision+1, state='pending', attempts=0, last_error='',
			next_attempt_at=excluded.next_attempt_at, updated_at=excluded.updated_at
		WHERE excluded.run_id <> git_provider_reports.run_id OR excluded.phase_rank >= git_provider_reports.phase_rank`,
		row.kind, row.resourceID, row.operation, row.subject, row.payload, row.runID, row.phaseRank,
		millis(now), millis(now), millis(now))
	return err
}

// due returns pending reports whose time has come, oldest first.
func due(ctx context.Context, db *sql.DB, now time.Time, limit int) ([]queuedReport, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,kind,resource_id,operation,payload,run_id,check_run_id,revision,attempts
		FROM git_provider_reports WHERE state='pending' AND next_attempt_at<=? ORDER BY next_attempt_at,id LIMIT ?`,
		millis(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []queuedReport
	for rows.Next() {
		var r queuedReport
		if err := rows.Scan(&r.id, &r.kind, &r.resourceID, &r.operation, &r.payload, &r.runID, &r.checkRunID,
			&r.revision, &r.attempts); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// nextDue is when the earliest pending report is due (ok false: none).
func nextDue(ctx context.Context, db *sql.DB) (time.Time, bool, error) {
	var next sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT MIN(next_attempt_at) FROM git_provider_reports WHERE state='pending'`).Scan(&next)
	if err != nil || !next.Valid {
		return time.Time{}, false, err
	}
	return time.UnixMilli(next.Int64), true, nil
}

// delivered settles the revision that was sent. When a newer revision
// arrived meanwhile, only the check run id is kept and the row stays pending.
func delivered(ctx context.Context, db *sql.DB, r queuedReport, checkRunID int64, now time.Time) error {
	result, err := db.ExecContext(ctx, `UPDATE git_provider_reports SET state='delivered', check_run_id=?, last_error='',
		updated_at=? WHERE id=? AND revision=?`, checkRunID, millis(now), r.id, r.revision)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed > 0 || checkRunID == 0 {
		return nil
	}
	_, err = db.ExecContext(ctx, `UPDATE git_provider_reports SET check_run_id=? WHERE id=? AND run_id=?`,
		checkRunID, r.id, r.runID)
	return err
}

// failed records a failed attempt: retried at next, or dead.
func failed(ctx context.Context, db *sql.DB, r queuedReport, dead bool, next time.Time, message string, now time.Time) error {
	state := ReportPending
	if dead {
		state = ReportDead
	}
	_, err := db.ExecContext(ctx, `UPDATE git_provider_reports SET state=?, attempts=attempts+1, next_attempt_at=?,
		last_error=?, updated_at=? WHERE id=? AND revision=?`, state, millis(next), message, millis(now), r.id, r.revision)
	return err
}

// discard removes a report there is nothing to send for any more.
func discard(ctx context.Context, db *sql.DB, r queuedReport) error {
	_, err := db.ExecContext(ctx, `DELETE FROM git_provider_reports WHERE id=? AND revision=?`, r.id, r.revision)
	return err
}

// prune forgets delivered and dead reports not touched since before.
func prune(ctx context.Context, db *sql.DB, before time.Time) error {
	_, err := db.ExecContext(ctx, `DELETE FROM git_provider_reports WHERE state IN ('delivered','dead') AND updated_at<?`,
		millis(before))
	return err
}

// failedReports lists the reports of a resource that failed at least once
// and are not delivered, newest first.
func failedReports(ctx context.Context, db *sql.DB, kind, id string) ([]FailedReport, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,operation,state,payload,attempts,last_error,next_attempt_at,updated_at
		FROM git_provider_reports WHERE kind=? AND resource_id=? AND (state='dead' OR state='pending' AND attempts>0)
		ORDER BY updated_at DESC, id DESC LIMIT 100`, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FailedReport{}
	for rows.Next() {
		var report FailedReport
		var payload string
		var next, updated int64
		if err := rows.Scan(&report.ID, &report.Operation, &report.State, &payload, &report.Attempts,
			&report.LastError, &next, &updated); err != nil {
			return nil, err
		}
		report.NextAttemptAt, report.UpdatedAt = time.UnixMilli(next).UTC(), time.UnixMilli(updated).UTC()
		describe(&report, payload)
		out = append(out, report)
	}
	return out, rows.Err()
}

// describe copies the identifying fields of a payload, never its body.
func describe(report *FailedReport, payload string) {
	if report.Operation == operationComment {
		var comment CommentReport
		if json.Unmarshal([]byte(payload), &comment) == nil {
			report.PullRequest = comment.Comment.Number
		}
		return
	}
	var run RunReport
	if json.Unmarshal([]byte(payload), &run) == nil {
		report.SHA, report.Context, report.Phase = run.SHA, run.Context, run.Phase
	}
}

// resend makes a failed report of the resource due now, from scratch.
func resend(ctx context.Context, db *sql.DB, kind, id string, reportID int64, now time.Time) (bool, error) {
	result, err := db.ExecContext(ctx, `UPDATE git_provider_reports SET state='pending', attempts=0, next_attempt_at=?,
		updated_at=?, revision=revision+1 WHERE id=? AND kind=? AND resource_id=? AND (state='dead' OR state='pending' AND attempts>0)`,
		millis(now), millis(now), reportID, kind, id)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed > 0, err
}
