package identity

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"pier/internal/platform/sqlite"
)

// Store persists users, sessions and API tokens.
type Store struct {
	db *sql.DB
}

// NewStore creates the store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type userRecord struct {
	User
	PasswordHash string
}

func (s *Store) countUsers(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *Store) insertUser(ctx context.Context, user User, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, name, password_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, user.Email, user.Name, passwordHash, sqlite.FormatTime(user.CreatedAt))
	return err
}

func (s *Store) userByEmail(ctx context.Context, email string) (userRecord, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, email, name, password_hash, created_at FROM users WHERE email = ?`, email))
}

func (s *Store) userByID(ctx context.Context, id string) (userRecord, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, email, name, password_hash, created_at FROM users WHERE id = ?`, id))
}

func (s *Store) scanUser(row *sql.Row) (userRecord, error) {
	var record userRecord
	var createdAt string
	err := row.Scan(&record.ID, &record.Email, &record.Name, &record.PasswordHash, &createdAt)
	if err != nil {
		return userRecord{}, sqlite.NotFoundIfNoRows(err)
	}
	record.CreatedAt = sqlite.ParseTime(createdAt)
	return record, nil
}

func (s *Store) listUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, name, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var user User
		var createdAt string
		if err := rows.Scan(&user.ID, &user.Email, &user.Name, &createdAt); err != nil {
			return nil, err
		}
		user.CreatedAt = sqlite.ParseTime(createdAt)
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) updateProfile(ctx context.Context, id, name, email string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx,
		`UPDATE users SET name = ?, email = ? WHERE id = ?`, name, email, id))
}

func (s *Store) updatePassword(ctx context.Context, id, passwordHash string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id))
}

func (s *Store) deleteUser(ctx context.Context, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id))
}

func (s *Store) insertSession(ctx context.Context, tokenHash, userID string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, sqlite.FormatTime(expiresAt), sqlite.FormatTime(time.Now()))
	return err
}

func (s *Store) sessionUserID(ctx context.Context, tokenHash string, now time.Time) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, sqlite.FormatTime(now)).Scan(&userID)
	return userID, sqlite.NotFoundIfNoRows(err)
}

func (s *Store) deleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) deleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) deleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, sqlite.FormatTime(now))
	return err
}

func (s *Store) insertToken(ctx context.Context, token APIToken, tokenHash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (id, user_id, name, token_hash, abilities, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		token.ID, token.UserID, token.Name, tokenHash, joinAbilities(token.Abilities),
		sqlite.FormatTime(token.CreatedAt))
	return err
}

func (s *Store) listTokens(ctx context.Context) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, abilities, last_used_at, created_at FROM api_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := []APIToken{}
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (s *Store) tokenByHash(ctx context.Context, tokenHash string) (APIToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, abilities, last_used_at, created_at FROM api_tokens WHERE token_hash = ?`,
		tokenHash)
	token, err := scanToken(row)
	return token, sqlite.NotFoundIfNoRows(err)
}

func (s *Store) touchToken(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
		sqlite.FormatTime(now), id)
	return err
}

func (s *Store) deleteToken(ctx context.Context, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ?`, id))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanToken(row rowScanner) (APIToken, error) {
	var token APIToken
	var abilities, createdAt string
	var lastUsedAt sql.NullString
	if err := row.Scan(&token.ID, &token.UserID, &token.Name, &abilities, &lastUsedAt, &createdAt); err != nil {
		return APIToken{}, err
	}
	token.Abilities = splitAbilities(abilities)
	token.LastUsedAt = sqlite.ParseOptionalTime(lastUsedAt)
	token.CreatedAt = sqlite.ParseTime(createdAt)
	return token, nil
}

func joinAbilities(abilities []Ability) string {
	parts := make([]string, len(abilities))
	for i, ability := range abilities {
		parts[i] = string(ability)
	}
	return strings.Join(parts, ",")
}

func splitAbilities(raw string) []Ability {
	abilities := []Ability{}
	for _, part := range strings.Split(raw, ",") {
		if part != "" {
			abilities = append(abilities, Ability(part))
		}
	}
	return abilities
}
