package access

import (
	"context"
	"database/sql"
	"errors"
)

// projectQueries resolve the project that owns a resource from persisted
// rows (resource -> parent -> environment -> project). Request input never
// names the project directly. Unknown kinds fail closed.
var projectQueries = map[string]string{
	"projects":       `SELECT id FROM projects WHERE id=?`,
	"environments":   `SELECT project_id FROM environments WHERE id=?`,
	"applications":   resourceProject("applications"),
	"databases":      resourceProject("databases"),
	"stacks":         resourceProject("stacks"),
	"deployments":    parentProject("deployments", "application_id", "applications"),
	"tasks":          parentProject("scheduled_tasks", "application_id", "applications"),
	"backups":        parentProject("backup_executions", "database_id", "databases"),
	"volume-backups": volumeBackupProject,
}

func resourceProject(table string) string {
	return `SELECT e.project_id FROM ` + table + ` r JOIN environments e ON e.id=r.environment_id WHERE r.id=?`
}

func parentProject(table, column, parent string) string {
	return `SELECT e.project_id FROM ` + table + ` r JOIN ` + parent + ` p ON p.id=r.` + column +
		` JOIN environments e ON e.id=p.environment_id WHERE r.id=?`
}

// volumeBackupProject follows the polymorphic owner of a volume backup.
const volumeBackupProject = `SELECT e.project_id FROM volume_backups v
	LEFT JOIN applications a ON v.resource_type='application' AND a.id=v.resource_id
	LEFT JOIN stacks s ON v.resource_type='stack' AND s.id=v.resource_id
	JOIN environments e ON e.id=COALESCE(a.environment_id,s.environment_id) WHERE v.id=?`

// KnownKind reports whether Project can resolve kind.
func KnownKind(kind string) bool { _, ok := projectQueries[kind]; return ok }

// Project returns the project owning the resource. A missing resource is
// denied like a foreign one, so callers cannot probe for existence.
func (s *Service) Project(ctx context.Context, kind, id string) (string, error) {
	query, ok := projectQueries[kind]
	if !ok || id == "" {
		return "", denied()
	}
	var project string
	err := s.db.QueryRowContext(ctx, query, id).Scan(&project)
	if errors.Is(err, sql.ErrNoRows) {
		return "", denied()
	}
	return project, err
}

// OwnToken deliberately hides whether a foreign token exists.
func (s *Service) OwnToken(ctx context.Context, user, id string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE id=? AND user_id=?`, id, user).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return denied()
	}
	return nil
}
