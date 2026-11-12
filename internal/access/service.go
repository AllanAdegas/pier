// Package access enforces instance and project membership independently of credentials.
package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/mail"
	"strings"
	"time"

	"pier/internal/platform/apperr"
)

type Role string

const (
	Owner     Role = "owner"
	Admin     Role = "admin"
	Developer Role = "developer"
	Viewer    Role = "viewer"
)

func (r Role) Valid() bool { return r == Owner || r == Admin || r == Developer || r == Viewer }
func (r Role) Allows(ability string) bool {
	switch ability {
	case "read":
		return r.Valid()
	case "deploy":
		return r == Owner || r == Admin || r == Developer
	case "write":
		return r == Owner || r == Admin
	}
	return false
}

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Service { return &Service{db: db, now: time.Now} }
func denied() error           { return apperr.Forbidden("Acesso não autorizado.") }

// Bootstrap is safe after first-account setup: it selects the oldest account,
// never the caller. An existing instance role prevents further bootstrap.
func (s *Service) Bootstrap(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO access_instance_roles SELECT id,'owner' FROM users WHERE NOT EXISTS (SELECT 1 FROM access_instance_roles) ORDER BY created_at,id LIMIT 1`)
	return err
}
func (s *Service) Role(ctx context.Context, user, project string) (Role, error) {
	var role Role
	err := s.db.QueryRowContext(ctx, `SELECT role FROM access_instance_roles WHERE user_id=?`, user).Scan(&role)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if role == Owner {
		return role, nil
	}
	if project == "" {
		return "", denied()
	}
	err = s.db.QueryRowContext(ctx, `SELECT role FROM access_memberships WHERE user_id=? AND project_id=?`, user, project).Scan(&role)
	if err == sql.ErrNoRows {
		return "", denied()
	}
	return role, err
}
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
func (s *Service) manage(ctx context.Context, user, project string) (Role, error) {
	role, err := s.Role(ctx, user, project)
	if err != nil {
		return "", err
	}
	if role != Owner && role != Admin {
		return "", denied()
	}
	return role, nil
}

type Member struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   Role   `json:"role"`
}

func (s *Service) Members(ctx context.Context, actor, project string) ([]Member, error) {
	if err := s.Check(ctx, actor, project, "read"); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.user_id,u.email,m.role FROM access_memberships m JOIN users u ON u.id=m.user_id WHERE project_id=? ORDER BY u.email`, project)
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

// SetMember serializes authorization and the last-owner check with the mutation.
// Empty role removes membership. Self changes are rejected to prevent lockout.
func (s *Service) SetMember(ctx context.Context, actor, project, user string, role Role) error {
	if actor == user || (role != "" && !role.Valid()) {
		return denied()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var instance, caller, previous Role
	err = tx.QueryRowContext(ctx, `SELECT role FROM access_instance_roles WHERE user_id=?`, actor).Scan(&instance)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT role FROM access_memberships WHERE project_id=? AND user_id=?`, project, actor).Scan(&caller)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if instance != Owner && caller != Owner && caller != Admin {
		return denied()
	}
	err = tx.QueryRowContext(ctx, `SELECT role FROM access_memberships WHERE project_id=? AND user_id=?`, project, user).Scan(&previous)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if instance != Owner && caller != Owner && (previous == Owner || role == Owner) {
		return denied()
	}
	if previous == Owner && role != Owner {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_memberships WHERE project_id=? AND role='owner'`, project).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return apperr.Conflict("Mantenha pelo menos um proprietário.")
		}
	}
	if role == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM access_memberships WHERE project_id=? AND user_id=?`, project, user)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO access_memberships(project_id,user_id,role) VALUES(?,?,?) ON CONFLICT(project_id,user_id) DO UPDATE SET role=excluded.role`, project, user, role)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

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
func (s *Service) Invite(ctx context.Context, actor, project, email string, role Role) (Invite, string, error) {
	if _, err := s.manage(ctx, actor, project); err != nil {
		return Invite{}, "", err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !role.Valid() || role == Owner {
		return Invite{}, "", apperr.Invalid("Convite inválido.")
	}
	token, err := random()
	if err != nil {
		return Invite{}, "", err
	}
	id, err := random()
	if err != nil {
		return Invite{}, "", err
	}
	item := Invite{ID: id, ProjectID: project, Email: email, Role: role, ExpiresAt: s.now().Add(24 * time.Hour).Unix()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO access_invites(id,project_id,email,role,token_hash,expires_at) VALUES(?,?,?,?,?,?)`, id, project, email, role, hash(token), item.ExpiresAt)
	return item, token, err
}
func (s *Service) Invites(ctx context.Context, actor, project string) ([]Invite, error) {
	if _, err := s.manage(ctx, actor, project); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,email,role,expires_at,consumed_at,revoked_at FROM access_invites WHERE project_id=? ORDER BY expires_at DESC`, project)
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
	_, err := s.db.ExecContext(ctx, `UPDATE access_invites SET revoked_at=? WHERE id=? AND project_id=? AND consumed_at IS NULL`, s.now().Unix(), id, project)
	return err
}

// Accept requires an authenticated existing account; invitation possession never
// creates an identity. Consumption and membership insertion are one transaction.
func (s *Service) Accept(ctx context.Context, user, token string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var project string
	var role Role
	err = tx.QueryRowContext(ctx, `SELECT i.project_id,i.role FROM access_invites i JOIN users u ON lower(u.email)=i.email WHERE u.id=? AND i.token_hash=? AND i.expires_at>? AND i.consumed_at IS NULL AND i.revoked_at IS NULL`, user, hash(token), s.now().Unix()).Scan(&project, &role)
	if err == sql.ErrNoRows {
		return denied()
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO access_memberships(project_id,user_id,role) VALUES(?,?,?) ON CONFLICT(project_id,user_id) DO NOTHING`, project, user, role)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE access_invites SET consumed_at=? WHERE token_hash=?`, s.now().Unix(), hash(token))
	if err != nil {
		return err
	}
	return tx.Commit()
}
