package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

// Store persists applications, deployments and deployment logs.
type Store struct {
	db  *sql.DB
	box *secretbox.Box
}

// NewStore creates the store.
func NewStore(db *sql.DB, box *secretbox.Box) *Store { return &Store{db: db, box: box} }

const applicationColumns = `id, environment_id, server_id, name, description, config, status, current_image,
	created_at, updated_at`

func (s *Store) insert(ctx context.Context, app Application, webhookSecret string) error {
	config, err := json.Marshal(app.Config)
	if err != nil {
		return err
	}
	sealed, err := s.box.Seal(webhookSecret)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO applications
		(id, environment_id, server_id, name, description, config, webhook_secret, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		app.ID, app.EnvironmentID, app.ServerID, app.Name, app.Description, string(config), sealed, app.Status,
		sqlite.FormatTime(app.CreatedAt), sqlite.FormatTime(app.UpdatedAt))
	return err
}

func (s *Store) update(ctx context.Context, app Application) error {
	config, err := json.Marshal(app.Config)
	if err != nil {
		return err
	}
	return sqlite.RequireAffected(s.db.ExecContext(ctx,
		`UPDATE applications SET name = ?, description = ?, config = ?, updated_at = ? WHERE id = ?`,
		app.Name, app.Description, string(config), sqlite.FormatTime(time.Now()), app.ID))
}

// SetStatus records the runtime status observed on the server.
func (s *Store) SetStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE applications SET status = ? WHERE id = ? AND status <> ?`, status, id, status)
	return err
}

func (s *Store) setCurrentImage(ctx context.Context, id, image string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE applications SET current_image = ?, status = 'running' WHERE id = ?`, image, id)
	return err
}

func (s *Store) webhookSecret(ctx context.Context, id string) (string, error) {
	var sealed string
	err := s.db.QueryRowContext(ctx, `SELECT webhook_secret FROM applications WHERE id = ?`, id).Scan(&sealed)
	if err != nil {
		return "", sqlite.NotFoundIfNoRows(err)
	}
	return s.box.Open(sealed)
}

func (s *Store) setWebhookSecret(ctx context.Context, id, secret string) error {
	sealed, err := s.box.Seal(secret)
	if err != nil {
		return err
	}
	return sqlite.RequireAffected(s.db.ExecContext(ctx,
		`UPDATE applications SET webhook_secret = ? WHERE id = ?`, sealed, id))
}

func (s *Store) delete(ctx context.Context, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM applications WHERE id = ?`, id))
}

// Get loads one application.
func (s *Store) Get(ctx context.Context, id string) (Application, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id = ?`, id)
	app, err := scanApplication(row)
	return app, sqlite.NotFoundIfNoRows(err)
}

// List loads every application, optionally restricted to one environment.
func (s *Store) List(ctx context.Context, environmentID string) ([]Application, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+applicationColumns+` FROM applications
		WHERE (? = '' OR environment_id = ?) ORDER BY created_at`, environmentID, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	apps := []Application{}
	for rows.Next() {
		app, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanApplication(row rowScanner) (Application, error) {
	var app Application
	var config, createdAt, updatedAt string
	err := row.Scan(&app.ID, &app.EnvironmentID, &app.ServerID, &app.Name, &app.Description, &config,
		&app.Status, &app.CurrentImage, &createdAt, &updatedAt)
	if err != nil {
		return Application{}, err
	}
	if err := json.Unmarshal([]byte(config), &app.Config); err != nil {
		return Application{}, fmt.Errorf("decode config of application %s: %w", app.ID, err)
	}
	app.CreatedAt = sqlite.ParseTime(createdAt)
	app.UpdatedAt = sqlite.ParseTime(updatedAt)
	return app, nil
}

const deploymentColumns = `d.id, d.application_id, a.name, d.status, d.trigger, d.commit_sha, d.commit_message,
	d.image, d.rollback_image, d.source, d.ref, d.promoted_from, d.error, d.hints, d.retired_logs, d.suggested_port, d.created_at, d.started_at, d.finished_at`

// List responses omit potentially large retired tails; the detail endpoint loads them.
var deploymentSummaryColumns = strings.Replace(deploymentColumns, "d.retired_logs", "'[]'", 1)

const deploymentFrom = ` FROM deployments d JOIN applications a ON a.id = d.application_id `

func (s *Store) insertDeployment(ctx context.Context, deployment Deployment) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO deployments
		(id, application_id, status, trigger, rollback_image, source, ref, promoted_from, commit_sha, commit_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		deployment.ID, deployment.ApplicationID, deployment.Status, deployment.Trigger, deployment.RollbackImage,
		deployment.Source, deployment.Ref, deployment.PromotedFrom,
		deployment.CommitSHA, deployment.CommitMessage, sqlite.FormatTime(deployment.CreatedAt))
	return err
}

// GetDeployment loads one deployment.
func (s *Store) GetDeployment(ctx context.Context, id string) (Deployment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+deploymentColumns+deploymentFrom+`WHERE d.id = ?`, id)
	deployment, err := scanDeployment(row)
	return deployment, sqlite.NotFoundIfNoRows(err)
}

// ListDeployments returns the newest deployments, optionally of one application.
func (s *Store) ListDeployments(ctx context.Context, applicationID string, limit int) ([]Deployment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deploymentSummaryColumns+deploymentFrom+`
		WHERE (? = '' OR d.application_id = ?) ORDER BY d.created_at DESC LIMIT ?`,
		applicationID, applicationID, limit)
	if err != nil {
		return nil, err
	}
	return collectDeployments(rows)
}

func (s *Store) deploymentsByStatus(ctx context.Context, status DeploymentStatus) ([]Deployment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deploymentColumns+deploymentFrom+`
		WHERE d.status = ? ORDER BY d.created_at`, status)
	if err != nil {
		return nil, err
	}
	return collectDeployments(rows)
}

// queuedDeployment finds a waiting build of the same ref that a new request
// can join. Uploads, promotions and rollbacks carry their own input and are
// never shared.
func (s *Store) queuedDeployment(ctx context.Context, applicationID, ref string) (Deployment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+deploymentColumns+deploymentFrom+`
		WHERE d.application_id = ? AND d.status = ? AND d.rollback_image = '' AND d.ref = ?
		AND d.trigger NOT IN (?, ?) LIMIT 1`,
		applicationID, DeploymentQueued, ref, TriggerArchive, TriggerPromotion)
	deployment, err := scanDeployment(row)
	return deployment, sqlite.NotFoundIfNoRows(err)
}

// deploymentOfImage is the latest successful deployment that released image.
func (s *Store) deploymentOfImage(ctx context.Context, applicationID, image string) (Deployment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+deploymentColumns+deploymentFrom+`
		WHERE d.application_id = ? AND d.status = ? AND d.image = ? ORDER BY d.created_at DESC LIMIT 1`,
		applicationID, DeploymentFinished, image)
	deployment, err := scanDeployment(row)
	return deployment, sqlite.NotFoundIfNoRows(err)
}

// claimDeployment moves a queued deployment to in_progress; false means it
// was cancelled (or claimed) in the meantime.
func (s *Store) claimDeployment(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE deployments SET status = ?, started_at = ? WHERE id = ? AND status = ?`,
		DeploymentInProgress, sqlite.FormatTime(time.Now()), id, DeploymentQueued)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (s *Store) setDeploymentCommit(ctx context.Context, id, sha, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deployments SET commit_sha = ?, commit_message = ? WHERE id = ?`,
		sha, message, id)
	return err
}

func (s *Store) setDeploymentImage(ctx context.Context, id, image string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `UPDATE deployments SET image = ? WHERE id = ?`, image, id))
}

// finishDeployment sets a terminal status unless one was already recorded.
func (s *Store) finishDeployment(ctx context.Context, id string, status DeploymentStatus, failure string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deployments SET status = ?, error = ?, finished_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		status, failure, sqlite.FormatTime(time.Now()), id, DeploymentQueued, DeploymentInProgress)
	return err
}

func (s *Store) retainedImages(ctx context.Context, applicationID string, keep int) (retained, expired []string, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT image FROM deployments
		WHERE application_id = ? AND status = ? AND image <> ''
		GROUP BY image ORDER BY MAX(created_at) DESC`,
		applicationID, DeploymentFinished)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var image string
		if err := rows.Scan(&image); err != nil {
			return nil, nil, err
		}
		if len(retained) < keep {
			retained = append(retained, image)
		} else {
			expired = append(expired, image)
		}
	}
	return retained, expired, rows.Err()
}

func collectDeployments(rows *sql.Rows) ([]Deployment, error) {
	defer rows.Close()
	deployments := []Deployment{}
	for rows.Next() {
		deployment, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, deployment)
	}
	return deployments, rows.Err()
}

func scanDeployment(row rowScanner) (Deployment, error) {
	var deployment Deployment
	var createdAt, hints, retiredLogs string
	var startedAt, finishedAt sql.NullString
	err := row.Scan(&deployment.ID, &deployment.ApplicationID, &deployment.ApplicationName, &deployment.Status,
		&deployment.Trigger, &deployment.CommitSHA, &deployment.CommitMessage, &deployment.Image,
		&deployment.RollbackImage, &deployment.Source, &deployment.Ref, &deployment.PromotedFrom, &deployment.Error, &hints, &retiredLogs, &deployment.SuggestedPort, &createdAt, &startedAt, &finishedAt)
	if err != nil {
		return Deployment{}, err
	}
	if err := json.Unmarshal([]byte(hints), &deployment.Hints); err != nil {
		return Deployment{}, err
	}
	if err := json.Unmarshal([]byte(retiredLogs), &deployment.RetiredLogs); err != nil {
		return Deployment{}, err
	}
	deployment.CreatedAt = sqlite.ParseTime(createdAt)
	deployment.StartedAt = sqlite.ParseOptionalTime(startedAt)
	deployment.FinishedAt = sqlite.ParseOptionalTime(finishedAt)
	return deployment, nil
}

func (s *Store) appendLog(ctx context.Context, deploymentID string, level LogLevel, line string) (LogEntry, error) {
	entry := LogEntry{Level: level, Line: line, CreatedAt: time.Now()}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO deployment_logs (deployment_id, level, line, created_at) VALUES (?, ?, ?, ?)`,
		deploymentID, level, line, sqlite.FormatTime(entry.CreatedAt))
	if err != nil {
		return LogEntry{}, err
	}
	entry.ID, err = result.LastInsertId()
	return entry, err
}

// Logs returns the log lines of a deployment after the given id.
func (s *Store) Logs(ctx context.Context, deploymentID string, afterID int64) ([]LogEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, level, line, created_at FROM deployment_logs
		WHERE deployment_id = ? AND id > ? ORDER BY id`, deploymentID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []LogEntry{}
	for rows.Next() {
		var entry LogEntry
		var createdAt string
		if err := rows.Scan(&entry.ID, &entry.Level, &entry.Line, &createdAt); err != nil {
			return nil, err
		}
		entry.CreatedAt = sqlite.ParseTime(createdAt)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
