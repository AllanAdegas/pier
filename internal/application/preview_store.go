package application

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"pier/internal/platform/sqlite"
)

// previewSettings returns the saved settings, or the defaults (disabled).
func (s *Store) previewSettings(ctx context.Context, applicationID string) (PreviewSettings, error) {
	settings := DefaultPreviewSettings()
	err := s.db.QueryRowContext(ctx, `SELECT enabled, domain_template, max_previews, allow_forks, ttl_days
		FROM application_preview_settings WHERE application_id = ?`, applicationID).
		Scan(&settings.Enabled, &settings.DomainTemplate, &settings.MaxPreviews, &settings.AllowForks, &settings.TTLDays)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultPreviewSettings(), nil
	}
	return settings, err
}

func (s *Store) savePreviewSettings(ctx context.Context, applicationID string, settings PreviewSettings) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO application_preview_settings
		(application_id, enabled, domain_template, max_previews, allow_forks, ttl_days, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (application_id) DO UPDATE SET enabled = excluded.enabled,
		domain_template = excluded.domain_template, max_previews = excluded.max_previews,
		allow_forks = excluded.allow_forks, ttl_days = excluded.ttl_days, updated_at = excluded.updated_at`,
		applicationID, settings.Enabled, settings.DomainTemplate, settings.MaxPreviews, settings.AllowForks,
		settings.TTLDays, sqlite.FormatTime(time.Now()))
	return err
}

const previewColumns = `p.id, p.application_id, p.number, p.provider, p.branch, p.commit_sha, p.title, p.author,
	p.fork, p.url, p.status, p.image, p.last_deployment_id, p.created_at, p.updated_at, p.last_activity_at,
	d.status, d.created_at, d.finished_at`

const previewFrom = ` FROM application_previews p LEFT JOIN deployments d ON d.id = p.last_deployment_id `

// previews lists the previews of one application, or of all with an empty id.
func (s *Store) previews(ctx context.Context, applicationID string) ([]Preview, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+previewColumns+previewFrom+`
		WHERE (? = '' OR p.application_id = ?) ORDER BY p.application_id, p.number`, applicationID, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	previews := []Preview{}
	for rows.Next() {
		preview, err := scanPreview(rows)
		if err != nil {
			return nil, err
		}
		previews = append(previews, preview)
	}
	return previews, rows.Err()
}

func (s *Store) preview(ctx context.Context, applicationID, id string) (Preview, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+previewColumns+previewFrom+`WHERE p.application_id = ? AND p.id = ?`,
		applicationID, id)
	preview, err := scanPreview(row)
	return preview, sqlite.NotFoundIfNoRows(err)
}

func (s *Store) previewByNumber(ctx context.Context, applicationID string, number int) (Preview, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+previewColumns+previewFrom+`WHERE p.application_id = ? AND p.number = ?`,
		applicationID, number)
	preview, err := scanPreview(row)
	return preview, sqlite.NotFoundIfNoRows(err)
}

func (s *Store) countPreviews(ctx context.Context, applicationID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_previews WHERE application_id = ?`,
		applicationID).Scan(&count)
	return count, err
}

func (s *Store) insertPreview(ctx context.Context, preview Preview) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO application_previews
		(id, application_id, number, provider, branch, commit_sha, title, author, fork, url, status,
		created_at, updated_at, last_activity_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		preview.ID, preview.ApplicationID, preview.Number, preview.Provider, preview.Branch, preview.CommitSHA,
		preview.Title, preview.Author, preview.Fork, preview.URL, preview.Status,
		sqlite.FormatTime(preview.CreatedAt), sqlite.FormatTime(preview.UpdatedAt), sqlite.FormatTime(preview.LastActivityAt))
	return err
}

// updatePreviewHead records a new head commit (and what may change with it).
func (s *Store) updatePreviewHead(ctx context.Context, preview Preview) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `UPDATE application_previews SET branch = ?, commit_sha = ?,
		title = ?, author = ?, fork = ?, url = ?, status = ?, updated_at = ?, last_activity_at = ? WHERE id = ?`,
		preview.Branch, preview.CommitSHA, preview.Title, preview.Author, preview.Fork, preview.URL, preview.Status,
		sqlite.FormatTime(time.Now()), sqlite.FormatTime(preview.LastActivityAt), preview.ID))
}

func (s *Store) setPreviewDeployment(ctx context.Context, id, deploymentID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE application_previews SET last_deployment_id = ?, status = ?, updated_at = ?
		WHERE id = ? AND status <> ?`, deploymentID, PreviewDeploying, sqlite.FormatTime(time.Now()), id, PreviewDestroying)
	return err
}

// setPreviewStatus changes the status unless the preview is being destroyed.
func (s *Store) setPreviewStatus(ctx context.Context, id string, status PreviewStatus) error {
	_, err := s.db.ExecContext(ctx, `UPDATE application_previews SET status = ?, updated_at = ?
		WHERE id = ? AND status <> ?`, status, sqlite.FormatTime(time.Now()), id, PreviewDestroying)
	return err
}

// claimPreviewDestruction marks the preview as being destroyed; false means
// another destruction got it first.
func (s *Store) claimPreviewDestruction(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE application_previews SET status = ?, updated_at = ?
		WHERE id = ? AND status <> ?`, PreviewDestroying, sqlite.FormatTime(time.Now()), id, PreviewDestroying)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// abandonPreviewDestruction marks a preview whose destruction failed, so it
// can be destroyed again.
func (s *Store) abandonPreviewDestruction(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE application_previews SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?`, PreviewFailed, sqlite.FormatTime(time.Now()), id, PreviewDestroying)
	return err
}

func (s *Store) setPreviewImage(ctx context.Context, id, image string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE application_previews SET image = ?, updated_at = ? WHERE id = ?`,
		image, sqlite.FormatTime(time.Now()), id)
	return err
}

func (s *Store) deletePreview(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM application_previews WHERE id = ?`, id)
	return err
}

// activePreviewDeployments are the deployments of a preview that have not ended.
func (s *Store) activePreviewDeployments(ctx context.Context, applicationID, ref string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM deployments WHERE application_id = ? AND trigger = ? AND ref = ?
		AND status IN (?, ?)`, applicationID, TriggerPreview, ref, DeploymentQueued, DeploymentInProgress)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanPreview(row rowScanner) (Preview, error) {
	var preview Preview
	var createdAt, updatedAt, activityAt string
	var deploymentStatus, deploymentCreated, deploymentFinished sql.NullString
	err := row.Scan(&preview.ID, &preview.ApplicationID, &preview.Number, &preview.Provider, &preview.Branch,
		&preview.CommitSHA, &preview.Title, &preview.Author, &preview.Fork, &preview.URL, &preview.Status,
		&preview.Image, &preview.LastDeploymentID, &createdAt, &updatedAt, &activityAt,
		&deploymentStatus, &deploymentCreated, &deploymentFinished)
	if err != nil {
		return Preview{}, err
	}
	preview.CreatedAt = sqlite.ParseTime(createdAt)
	preview.UpdatedAt = sqlite.ParseTime(updatedAt)
	preview.LastActivityAt = sqlite.ParseTime(activityAt)
	if deploymentStatus.Valid {
		preview.LastDeployment = &PreviewDeployment{
			Status: DeploymentStatus(deploymentStatus.String), CreatedAt: sqlite.ParseTime(deploymentCreated.String),
			FinishedAt: sqlite.ParseOptionalTime(deploymentFinished),
		}
	}
	return preview, nil
}
