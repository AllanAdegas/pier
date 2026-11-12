package access

import (
	"context"
	"database/sql"

	"pier/internal/platform/apperr"
)

type Member struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   Role   `json:"role"`
}

// Members lists the explicit memberships of a project.
func (s *Service) Members(ctx context.Context, actor, project string) ([]Member, error) {
	if err := s.Check(ctx, actor, project, "read"); err != nil {
		return nil, err
	}
	return listMembers(ctx, s.db, `SELECT m.user_id,u.email,m.role FROM access_memberships m
		JOIN users u ON u.id=m.user_id WHERE project_id=? ORDER BY u.email`, project)
}

func listMembers(ctx context.Context, db *sql.DB, query string, args ...any) ([]Member, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMember grants, changes or (empty role) removes a project membership. The
// authorization, the escalation rules and the last-owner check run in the
// transaction of the mutation. Self changes are refused to prevent lockout.
func (s *Service) SetMember(ctx context.Context, actor, project, user string, role Role) error {
	if actor == user || (role != "" && !role.Valid()) {
		return denied()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	caller, err := effectiveRole(ctx, tx, actor, project)
	if err != nil {
		return err
	}
	previous, err := memberRole(ctx, tx, user, project)
	if err != nil {
		return err
	}
	if err := allowGrant(caller, previous, role); err != nil {
		return err
	}
	if err := keepProjectOwner(ctx, tx, project, previous, role); err != nil {
		return err
	}
	if err := writeMembership(ctx, tx, project, user, role); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.changes.notify(user)
	return nil
}

// allowGrant: only admins and owners manage members, and nobody grants, or
// touches a member holding, more than they hold themselves.
func allowGrant(caller, previous, next Role) error {
	if !caller.AtLeast(Admin) {
		return denied()
	}
	if (next != "" && !caller.AtLeast(next)) || (previous != "" && !caller.AtLeast(previous)) {
		return denied()
	}
	return nil
}

func keepProjectOwner(ctx context.Context, tx *sql.Tx, project string, previous, next Role) error {
	if previous != Owner || next == Owner {
		return nil
	}
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_memberships WHERE project_id=? AND role='owner'`,
		project).Scan(&count)
	if err != nil {
		return err
	}
	if count <= 1 {
		return apperr.Conflict("Mantenha pelo menos um proprietário.")
	}
	return nil
}

func writeMembership(ctx context.Context, tx *sql.Tx, project, user string, role Role) error {
	if role == "" {
		_, err := tx.ExecContext(ctx, `DELETE FROM access_memberships WHERE project_id=? AND user_id=?`, project, user)
		return err
	}
	if err := requireUser(ctx, tx, user); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO access_memberships(project_id,user_id,role) VALUES(?,?,?)
		ON CONFLICT(project_id,user_id) DO UPDATE SET role=excluded.role`, project, user, role)
	return err
}

func requireUser(ctx context.Context, q querier, user string) error {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id=?`, user).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return apperr.NotFound("Usuário")
	}
	return nil
}
