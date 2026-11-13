package identity

import (
	"context"

	"golang.org/x/crypto/bcrypt"

	"pier/internal/platform/apperr"
)

// These operations back `pier admin`: they run with the panel stopped, by
// whoever controls the server's data directory, so they ask for no proof
// besides that access.

// SetPasswordByEmail replaces the password of an account and closes every
// session and pending reset link it has.
func (s *Service) SetPasswordByEmail(ctx context.Context, email, password string) error {
	fields := apperr.Fields{}
	validatePassword(fields, password)
	if err := fields.Err(); err != nil {
		return err
	}
	record, err := s.store.userByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return notFoundAs(err, "Usuário")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.replacePassword(ctx, record.ID, string(hash))
}

// RemoveTwoFactorByEmail turns the second factor off, for someone who lost
// both the device and the recovery codes.
func (s *Service) RemoveTwoFactorByEmail(ctx context.Context, email string) error {
	record, err := s.store.userByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return notFoundAs(err, "Usuário")
	}
	if err := s.store.deleteTOTP(ctx, record.ID); err != nil {
		return err
	}
	return s.store.deleteUserSessions(ctx, record.ID)
}
