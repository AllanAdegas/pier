// Package access enforces instance and project roles independently of
// credentials. A request is allowed when the credential holds the ability AND
// the caller's role, read fresh from the database, grants it.
package access

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"pier/internal/platform/apperr"
)

// Service reads and changes roles. Roles are never cached: a removed
// membership takes effect on the next request.
type Service struct {
	db      *sql.DB
	now     func() time.Time
	changes *changes
}

func New(db *sql.DB) *Service { return &Service{db: db, now: time.Now, changes: newChanges()} }

func denied() error { return apperr.Forbidden("Acesso não autorizado.") }

// querier is satisfied by *sql.DB and *sql.Tx, so checks run inside the
// transaction of the mutation they guard.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Bootstrap makes the oldest account instance owner when no instance role
// exists yet (fresh install, right after the first account). It selects the
// oldest account, never the caller.
func (s *Service) Bootstrap(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO access_instance_roles SELECT id,'owner' FROM users
		WHERE NOT EXISTS (SELECT 1 FROM access_instance_roles) ORDER BY created_at,id LIMIT 1`)
	return err
}

// Role is the effective role of user on project: the stronger of the instance
// role and the project membership. An empty project asks for the instance role.
func (s *Service) Role(ctx context.Context, user, project string) (Role, error) {
	role, err := effectiveRole(ctx, s.db, user, project)
	if err != nil {
		return "", err
	}
	if !role.Valid() {
		return "", denied()
	}
	return role, nil
}

// Check implements identity.AccessAuthorizer.
func (s *Service) Check(ctx context.Context, user, project, ability string) error {
	role, err := s.Role(ctx, user, project)
	if err != nil {
		return err
	}
	if !role.Allows(ability) {
		return denied()
	}
	return nil
}

// CheckRole demands at least minimum on project ("" = the instance).
func (s *Service) CheckRole(ctx context.Context, user, project string, minimum Role) error {
	role, err := s.Role(ctx, user, project)
	if err != nil {
		return err
	}
	if !role.AtLeast(minimum) {
		return denied()
	}
	return nil
}

func effectiveRole(ctx context.Context, q querier, user, project string) (Role, error) {
	instance, err := instanceRole(ctx, q, user)
	if err != nil || project == "" {
		return instance, err
	}
	member, err := memberRole(ctx, q, user, project)
	return higher(instance, member), err
}

func instanceRole(ctx context.Context, q querier, user string) (Role, error) {
	return optionalRole(q.QueryRowContext(ctx, `SELECT role FROM access_instance_roles WHERE user_id=?`, user))
}

func memberRole(ctx context.Context, q querier, user, project string) (Role, error) {
	return optionalRole(q.QueryRowContext(ctx,
		`SELECT role FROM access_memberships WHERE user_id=? AND project_id=?`, user, project))
}

func optionalRole(row *sql.Row) (Role, error) {
	var role Role
	err := row.Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// ProjectRole is one membership of the caller.
type ProjectRole struct {
	ProjectID string `json:"project_id"`
	Role      Role   `json:"role"`
}

// Summary is what the caller holds, for the UI to hide what it cannot do.
type Summary struct {
	InstanceRole Role          `json:"instance_role"`
	Projects     []ProjectRole `json:"projects"`
}

func (s *Service) Summary(ctx context.Context, user string) (Summary, error) {
	instance, err := instanceRole(ctx, s.db, user)
	if err != nil {
		return Summary{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT project_id,role FROM access_memberships WHERE user_id=? ORDER BY project_id`, user)
	if err != nil {
		return Summary{}, err
	}
	defer rows.Close()
	summary := Summary{InstanceRole: instance, Projects: []ProjectRole{}}
	for rows.Next() {
		var item ProjectRole
		if err := rows.Scan(&item.ProjectID, &item.Role); err != nil {
			return Summary{}, err
		}
		summary.Projects = append(summary.Projects, item)
	}
	return summary, rows.Err()
}

// Subscribe signals every change of user's roles until cancel is called.
func (s *Service) Subscribe(user string) (<-chan struct{}, func()) { return s.changes.subscribe(user) }
