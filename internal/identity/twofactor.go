package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"pier/internal/platform/apperr"
	"pier/internal/platform/sqlite"
)

const (
	recoveryCodeCount = 10
	// recoveryCodeLength base32 characters carry 80 bits: hashing them with
	// SHA-256, like API tokens, is enough (no need for a slow hash).
	recoveryCodeLength = 16
)

// TwoFactorStatus reports whether the user has the second factor on.
func (s *Service) TwoFactorStatus(ctx context.Context, userID string) (TwoFactorStatus, error) {
	enabled, err := s.twoFactorEnabled(ctx, userID)
	if err != nil || !enabled {
		return TwoFactorStatus{}, err
	}
	left, err := s.store.countRecoveryCodes(ctx, userID)
	return TwoFactorStatus{Enabled: true, RecoveryCodesLeft: left}, err
}

func (s *Service) twoFactorEnabled(ctx context.Context, userID string) (bool, error) {
	record, err := s.store.totpByUser(ctx, userID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return false, nil
	}
	return record.enabled, err
}

// BeginTwoFactor creates a secret for the user's authenticator app. It only
// takes effect after ConfirmTwoFactor receives a code generated from it.
func (s *Service) BeginTwoFactor(ctx context.Context, userID string) (Enrollment, error) {
	if s.box == nil {
		return Enrollment{}, apperr.Unavailable("A verificação em duas etapas não está disponível nesta instância.")
	}
	record, err := s.store.userByID(ctx, userID)
	if err != nil {
		return Enrollment{}, notFoundAs(err, "Usuário")
	}
	secret := newTOTPSecret()
	sealed, err := s.box.Seal(secret)
	if err != nil {
		return Enrollment{}, err
	}
	if err := s.store.savePendingTOTP(ctx, userID, sealed, s.now()); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return Enrollment{}, apperr.Conflict("A verificação em duas etapas já está ativa.")
		}
		return Enrollment{}, err
	}
	uri := otpauthURI(record.Email, secret)
	image, err := qrSVG(uri)
	if err != nil {
		return Enrollment{}, err
	}
	return Enrollment{Secret: secret, URI: uri, QRSVG: image}, nil
}

// ConfirmTwoFactor turns the second factor on once the app proves it holds
// the secret, and returns the recovery codes, shown only this once.
func (s *Service) ConfirmTwoFactor(ctx context.Context, userID, code string) ([]string, error) {
	record, err := s.store.totpByUser(ctx, userID)
	if errors.Is(err, sqlite.ErrNotFound) || (err == nil && record.enabled) {
		return nil, apperr.Conflict("Nenhuma ativação em andamento. Comece de novo.")
	}
	if err != nil {
		return nil, err
	}
	key, err := s.openTOTPKey(record)
	if err != nil {
		return nil, err
	}
	step, matched := matchTOTP(key, code, s.now(), record.lastStep)
	if !matched {
		return nil, wrongCode()
	}
	codes, hashes := newRecoveryCodes()
	if err := s.store.enableTOTP(ctx, userID, step, s.now(), hashes); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return nil, wrongCode()
		}
		return nil, err
	}
	return codes, nil
}

// DisableTwoFactor turns the second factor off. It asks for both factors:
// a stolen session alone must not be able to weaken the account.
func (s *Service) DisableTwoFactor(ctx context.Context, userID, password, code string) error {
	record, err := s.store.userByID(ctx, userID)
	if err != nil {
		return notFoundAs(err, "Usuário")
	}
	if bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(password)) != nil {
		fields := apperr.Fields{}
		fields.Add("password", "A senha está incorreta.")
		return fields.Err()
	}
	if err := s.requireSecondFactor(ctx, userID, code); err != nil {
		return err
	}
	return s.store.deleteTOTP(ctx, userID)
}

// RegenerateRecoveryCodes replaces every recovery code with a new set.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID, code string) ([]string, error) {
	if err := s.requireSecondFactor(ctx, userID, code); err != nil {
		return nil, err
	}
	codes, hashes := newRecoveryCodes()
	if err := s.store.replaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// requireSecondFactor checks a code on an account form (field error).
func (s *Service) requireSecondFactor(ctx context.Context, userID, code string) error {
	accepted, err := s.checkSecondFactor(ctx, userID, code)
	if err != nil {
		return err
	}
	if !accepted {
		return wrongCode()
	}
	return nil
}

// checkSecondFactor accepts a TOTP code for a step not used yet, or an
// unused recovery code, and spends what it accepted.
func (s *Service) checkSecondFactor(ctx context.Context, userID, code string) (bool, error) {
	record, err := s.store.totpByUser(ctx, userID)
	if errors.Is(err, sqlite.ErrNotFound) || (err == nil && !record.enabled) {
		return false, apperr.Conflict("A verificação em duas etapas não está ativa.")
	}
	if err != nil {
		return false, err
	}
	if !isNumericOTP(code) {
		err := s.store.consumeRecoveryCode(ctx, userID, hashToken(normalizeRecoveryCode(code)))
		return err == nil, ignoreNotFound(err)
	}
	key, err := s.openTOTPKey(record)
	if err != nil {
		return false, err
	}
	step, matched := matchTOTP(key, code, s.now(), record.lastStep)
	if !matched {
		return false, nil
	}
	// A concurrent request may have spent this step first.
	err = s.store.advanceTOTPStep(ctx, userID, step)
	return err == nil, ignoreNotFound(err)
}

func (s *Service) openTOTPKey(record totpRecord) ([]byte, error) {
	if s.box == nil {
		return nil, apperr.Unavailable("A verificação em duas etapas não está disponível nesta instância.")
	}
	secret, err := s.box.Open(record.sealedSecret)
	if err != nil {
		return nil, err
	}
	return decodeTOTPSecret(secret)
}

func wrongCode() error {
	fields := apperr.Fields{}
	fields.Add("code", "Código incorreto ou já usado. Confira o horário do celular e tente de novo.")
	return fields.Err()
}

// newRecoveryCodes returns the codes to show and the hashes to store.
func newRecoveryCodes() (codes, hashes []string) {
	for range recoveryCodeCount {
		raw := rand.Text()[:recoveryCodeLength]
		codes = append(codes, formatRecoveryCode(raw))
		hashes = append(hashes, hashToken(raw))
	}
	return codes, hashes
}

// formatRecoveryCode groups the code in fours, lower case, easy to copy.
func formatRecoveryCode(raw string) string {
	lower := strings.ToLower(raw)
	return lower[0:4] + "-" + lower[4:8] + "-" + lower[8:12] + "-" + lower[12:16]
}

func normalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}

func ignoreNotFound(err error) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return nil
	}
	return err
}
