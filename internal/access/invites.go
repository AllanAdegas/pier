package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/mail"
	"strings"
	"time"

	"pier/internal/platform/apperr"
)

const inviteTTL = 24 * time.Hour

type Invite struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	Email      string `json:"email"`
	Role       Role   `json:"role"`
	ExpiresAt  int64  `json:"expires_at"`
	ConsumedAt *int64 `json:"consumed_at"`
	RevokedAt  *int64 `json:"revoked_at"`
}

func random() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}

func hash(token string) string { h := sha256.Sum256([]byte(token)); return hex.EncodeToString(h[:]) }

// manage demands admin or owner on the project and returns the caller's role.
func (s *Service) manage(ctx context.Context, actor, project string) (Role, error) {
	role, err := s.Role(ctx, actor, project)
	if err != nil {
		return "", err
	}
	if !role.AtLeast(Admin) {
		return "", denied()
	}
	return role, nil
}

// Invite never grants more than the inviter holds, and never owner: owners
// are promoted explicitly by another owner.
func (s *Service) Invite(ctx context.Context, actor, project, email string, role Role) (Invite, string, error) {
	caller, err := s.manage(ctx, actor, project)
	if err != nil {
		return Invite{}, "", err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !role.Valid() || role == Owner {
		return Invite{}, "", apperr.Invalid("Convite inválido.")
	}
	if !caller.AtLeast(role) {
		return Invite{}, "", denied()
	}
	token, err := random()
	if err != nil {
		return Invite{}, "", err
	}
	id, err := random()
	if err != nil {
		return Invite{}, "", err
	}
	item := Invite{ID: id, ProjectID: project, Email: email, Role: role, ExpiresAt: s.now().Add(inviteTTL).Unix()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO access_invites(id,project_id,email,role,token_hash,invited_by,expires_at)
		VALUES(?,?,?,?,?,?,?)`, id, project, email, role, hash(token), actor, item.ExpiresAt)
	return item, token, err
}

func (s *Service) Invites(ctx context.Context, actor, project string) ([]Invite, error) {
	if _, err := s.manage(ctx, actor, project); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,email,role,expires_at,consumed_at,revoked_at
		FROM access_invites WHERE project_id=? ORDER BY expires_at DESC`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var i Invite
		if err := rows.Scan(&i.ID, &i.ProjectID, &i.Email, &i.Role, &i.ExpiresAt, &i.ConsumedAt, &i.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Service) Revoke(ctx context.Context, actor, project, id string) error {
	if _, err := s.manage(ctx, actor, project); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE access_invites SET revoked_at=? WHERE id=? AND project_id=? AND consumed_at IS NULL`,
		s.now().Unix(), id, project)
	return err
}

// Accept requires an authenticated existing account whose e-mail matches;
// possessing the token never creates an identity. The inviter must still hold
// the invited role when it is accepted: a demoted inviter's pending invites
// stop working. Consumption and membership insertion are one transaction.
func (s *Service) Accept(ctx context.Context, user, token string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var project, inviter string
	var role Role
	err = tx.QueryRowContext(ctx, `SELECT i.project_id,i.role,i.invited_by FROM access_invites i
		JOIN users u ON lower(u.email)=i.email WHERE u.id=? AND i.token_hash=? AND i.expires_at>?
		AND i.consumed_at IS NULL AND i.revoked_at IS NULL`, user, hash(token), s.now().Unix()).Scan(&project, &role, &inviter)
	if errors.Is(err, sql.ErrNoRows) {
		return denied()
	}
	if err != nil {
		return err
	}
	if err := inviterStillHolds(ctx, tx, inviter, project, role); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO access_memberships(project_id,user_id,role) VALUES(?,?,?)
		ON CONFLICT(project_id,user_id) DO NOTHING`, project, user, role)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE access_invites SET consumed_at=? WHERE token_hash=?`, s.now().Unix(), hash(token)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.changes.notify(user)
	return nil
}

func inviterStillHolds(ctx context.Context, tx *sql.Tx, inviter, project string, role Role) error {
	current, err := effectiveRole(ctx, tx, inviter, project)
	if err != nil {
		return err
	}
	if !current.AtLeast(Admin) || !current.AtLeast(role) {
		return denied()
	}
	return nil
}
