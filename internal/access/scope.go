package access

import "context"

// Project resolves ownership from persisted rows. Unknown kinds fail closed.
func (s *Service) Project(ctx context.Context, kind, id string) (string, error) {
	var query string
	switch kind {
	case "projects":
		query = `SELECT id FROM projects WHERE id=?`
	case "environments":
		query = `SELECT project_id FROM environments WHERE id=?`
	case "applications", "databases", "stacks":
		query = `SELECT e.project_id FROM ` + kind + ` r JOIN environments e ON e.id=r.environment_id WHERE r.id=?`
	case "deployments":
		query = `SELECT e.project_id FROM deployments r JOIN applications a ON a.id=r.application_id JOIN environments e ON e.id=a.environment_id WHERE r.id=?`
	case "tasks":
		query = `SELECT e.project_id FROM scheduled_tasks r JOIN applications a ON a.id=r.application_id JOIN environments e ON e.id=a.environment_id WHERE r.id=?`
	case "backups":
		query = `SELECT e.project_id FROM backup_executions r JOIN databases d ON d.id=r.database_id JOIN environments e ON e.id=d.environment_id WHERE r.id=?`
	default:
		return "", denied()
	}
	var project string
	err := s.db.QueryRowContext(ctx, query, id).Scan(&project)
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
