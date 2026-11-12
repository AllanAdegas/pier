package access

import (
	"context"
	"database/sql"

	"pier/internal/platform/apperr"
)

// InstanceMembers lists the accounts and their instance role ("" = none,
// project memberships only). Only instance owners manage the instance.
func (s *Service) InstanceMembers(ctx context.Context, actor string) ([]Member, error) {
	if err := s.CheckRole(ctx, actor, "", Owner); err != nil {
		return nil, err
	}
	return listMembers(ctx, s.db, `SELECT u.id,u.email,COALESCE(r.role,'') FROM users u
		LEFT JOIN access_instance_roles r ON r.user_id=u.id ORDER BY u.email`)
}

// SetInstanceRole grants, changes or (empty role) removes the instance role
// of another account. The instance always keeps one owner.
func (s *Service) SetInstanceRole(ctx context.Context, actor, user string, role Role) error {
	if actor == user || (role != "" && !role.Valid()) {
		return denied()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	caller, err := instanceRole(ctx, tx, actor)
	if err != nil {
		return err
	}
	if caller != Owner {
		return denied()
	}
	if err := requireUser(ctx, tx, user); err != nil {
		return err
	}
	if err := writeInstanceRole(ctx, tx, user, role); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.changes.notify(user)
	return nil
}

// ApplySSORole sets the instance role the SSO configuration mapped for a
// login. Owners are managed only inside Pier: SSO never grants, changes or
// removes the owner role, so a compromised identity provider cannot take the
// instance over or lock its owners out.
func (s *Service) ApplySSORole(ctx context.Context, user string, role Role) error {
	if role == Owner || (role != "" && !role.Valid()) {
		return denied()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := instanceRole(ctx, tx, user)
	if err != nil {
		return err
	}
	if current == Owner || current == role {
		return nil
	}
	if err := writeInstanceRole(ctx, tx, user, role); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.changes.notify(user)
	return nil
}

func writeInstanceRole(ctx context.Context, tx *sql.Tx, user string, role Role) error {
	if err := keepInstanceOwner(ctx, tx, user, role); err != nil {
		return err
	}
	if role == "" {
		_, err := tx.ExecContext(ctx, `DELETE FROM access_instance_roles WHERE user_id=?`, user)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO access_instance_roles(user_id,role) VALUES(?,?)
		ON CONFLICT(user_id) DO UPDATE SET role=excluded.role`, user, role)
	return err
}

// keepInstanceOwner gives a clear error; the database triggers of migration
// 0019 enforce the same invariant for every other path (account deletion).
func keepInstanceOwner(ctx context.Context, q querier, user string, next Role) error {
	previous, err := instanceRole(ctx, q, user)
	if err != nil || previous != Owner || next == Owner {
		return err
	}
	var owners int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_instance_roles WHERE role='owner'`).Scan(&owners); err != nil {
		return err
	}
	if owners <= 1 {
		return apperr.Conflict("Mantenha pelo menos um proprietário.")
	}
	return nil
}

// GuardAccountRemoval refuses to delete the last instance owner, with the
// same message as a demotion, before the account deletion reaches the trigger.
func (s *Service) GuardAccountRemoval(ctx context.Context, user string) error {
	return keepInstanceOwner(ctx, s.db, user, "")
}
