package identity

import (
	"context"
	"database/sql"
	"time"

	"pier/internal/platform/sqlite"
)

// sessionRow is a session as stored; the token itself is only kept hashed.
type sessionRow struct {
	tokenHash string
	id        string
	userID    string
	expiresAt time.Time
	createdAt time.Time
	client    Client
}

// sessionTouchInterval bounds the writes "last seen" costs: at most one per
// session per minute, not one per request.
const sessionTouchInterval = time.Minute

func (s *Store) insertSession(ctx context.Context, row sessionRow) error {
	created := sqlite.FormatTime(row.createdAt)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, id, user_id, expires_at, created_at, last_seen_at, ip, user_agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		row.tokenHash, row.id, row.userID, sqlite.FormatTime(row.expiresAt), created, created,
		row.client.IP, row.client.UserAgent)
	return err
}

// activeSession returns the session id and user of a valid session token.
func (s *Store) activeSession(ctx context.Context, tokenHash string, now time.Time) (id, userID string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT id, user_id FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, sqlite.FormatTime(now)).Scan(&id, &userID)
	return id, userID, sqlite.NotFoundIfNoRows(err)
}

func (s *Store) touchSession(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ? WHERE token_hash = ? AND last_seen_at < ?`,
		sqlite.FormatTime(now), tokenHash, sqlite.FormatTime(now.Add(-sessionTouchInterval)))
	return err
}

func (s *Store) listSessions(ctx context.Context, userID string, now time.Time) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, created_at, last_seen_at, ip, user_agent FROM sessions
		 WHERE user_id = ? AND expires_at > ? ORDER BY last_seen_at DESC`, userID, sqlite.FormatTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []Session{}
	for rows.Next() {
		var session Session
		var createdAt, lastSeenAt string
		if err := rows.Scan(&session.ID, &createdAt, &lastSeenAt, &session.IP, &session.UserAgent); err != nil {
			return nil, err
		}
		session.CreatedAt, session.LastSeenAt = sqlite.ParseTime(createdAt), sqlite.ParseTime(lastSeenAt)
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) deleteSessionByID(ctx context.Context, userID, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND id = ?`, userID, id))
}

func (s *Store) deleteOtherSessions(ctx context.Context, userID, keepID string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID, keepID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// totpRecord is the stored second factor of a user.
type totpRecord struct {
	sealedSecret string
	enabled      bool
	lastStep     int64
}

func (s *Store) totpByUser(ctx context.Context, userID string) (totpRecord, error) {
	var record totpRecord
	err := s.db.QueryRowContext(ctx,
		`SELECT secret, enabled, last_step FROM user_totp WHERE user_id = ?`, userID).
		Scan(&record.sealedSecret, &record.enabled, &record.lastStep)
	return record, sqlite.NotFoundIfNoRows(err)
}

// savePendingTOTP stores a secret awaiting confirmation. An enabled factor
// is never overwritten: it must be disabled first (ErrNotFound otherwise).
func (s *Store) savePendingTOTP(ctx context.Context, userID, sealedSecret string, now time.Time) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx,
		`INSERT INTO user_totp (user_id, secret, enabled, last_step, created_at) VALUES (?, ?, 0, 0, ?)
		 ON CONFLICT(user_id) DO UPDATE SET secret = excluded.secret, last_step = 0, created_at = excluded.created_at
		 WHERE user_totp.enabled = 0`,
		userID, sealedSecret, sqlite.FormatTime(now)))
}

// enableTOTP turns the pending factor on and stores its recovery codes, in
// one transaction. step is the step of the confirming code, now spent.
func (s *Store) enableTOTP(ctx context.Context, userID string, step int64, now time.Time, codeHashes []string) error {
	return s.inTransaction(ctx, func(tx *sql.Tx) error {
		err := sqlite.RequireAffected(tx.ExecContext(ctx,
			`UPDATE user_totp SET enabled = 1, last_step = ?, enabled_at = ?
			 WHERE user_id = ? AND enabled = 0 AND last_step < ?`, step, sqlite.FormatTime(now), userID, step))
		if err != nil {
			return err
		}
		return replaceCodes(ctx, tx, userID, codeHashes)
	})
}

// advanceTOTPStep records an accepted code. Only a later step moves it, so
// two requests racing with the same code cannot both succeed.
func (s *Store) advanceTOTPStep(ctx context.Context, userID string, step int64) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx,
		`UPDATE user_totp SET last_step = ? WHERE user_id = ? AND enabled = 1 AND last_step < ?`, step, userID, step))
}

func (s *Store) deleteTOTP(ctx context.Context, userID string) error {
	return s.inTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM user_totp WHERE user_id = ?`, userID)
		return err
	})
}

func (s *Store) replaceRecoveryCodes(ctx context.Context, userID string, codeHashes []string) error {
	return s.inTransaction(ctx, func(tx *sql.Tx) error { return replaceCodes(ctx, tx, userID, codeHashes) })
}

func replaceCodes(ctx context.Context, tx *sql.Tx, userID string, codeHashes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, hash := range codeHashes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, hash); err != nil {
			return err
		}
	}
	return nil
}

// consumeRecoveryCode deletes the code; ErrNotFound means it was not valid.
func (s *Store) consumeRecoveryCode(ctx context.Context, userID, codeHash string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx,
		`DELETE FROM recovery_codes WHERE user_id = ? AND code_hash = ?`, userID, codeHash))
}

func (s *Store) countRecoveryCodes(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE user_id = ?`, userID).Scan(&count)
	return count, err
}

// insertReset keeps a single pending reset per user: a new request voids
// the previous link.
func (s *Store) insertReset(ctx context.Context, tokenHash, userID string, now, expiresAt time.Time) error {
	return s.inTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = ? OR expires_at <= ?`,
			userID, sqlite.FormatTime(now)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO password_resets (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
			tokenHash, userID, sqlite.FormatTime(expiresAt), sqlite.FormatTime(now))
		return err
	})
}

// consumeReset deletes the token in the same statement that reads it, so a
// link works once even under concurrent use. ErrNotFound when unknown.
func (s *Store) consumeReset(ctx context.Context, tokenHash string) (userID string, expiresAt time.Time, err error) {
	var expires string
	err = s.db.QueryRowContext(ctx,
		`DELETE FROM password_resets WHERE token_hash = ? RETURNING user_id, expires_at`, tokenHash).
		Scan(&userID, &expires)
	return userID, sqlite.ParseTime(expires), sqlite.NotFoundIfNoRows(err)
}

func (s *Store) deleteUserResets(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = ?`, userID)
	return err
}

func (s *Store) inTransaction(ctx context.Context, work func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := work(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
