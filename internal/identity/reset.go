package identity

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
)

const (
	resetTTL = 30 * time.Minute
	// maxPendingResets bounds the e-mails being sent at once; requests over
	// it are dropped, which the generic answer hides anyway.
	maxPendingResets = 4
	resetSendTimeout = 30 * time.Second
)

// resetDelivery sends reset e-mails off the request path, so the answer
// takes the same time whether or not the e-mail belongs to an account.
type resetDelivery struct {
	mailer ResetMailer
	logger *slog.Logger
	slots  chan struct{}
	work   sync.WaitGroup
}

// SetResetMailer enables "forgot my password" through the given mailer.
func (s *Service) SetResetMailer(mailer ResetMailer, logger *slog.Logger) {
	s.resets.mailer, s.resets.logger = mailer, logger
}

// PasswordResetAvailable reports whether a reset link can be e-mailed: it
// needs a mailer (an e-mail channel) and the public URL the link points to.
func (s *Service) PasswordResetAvailable(ctx context.Context, publicURL string) bool {
	return s.resets.mailer != nil && publicURL != "" && s.resets.mailer.Available(ctx)
}

// RequestPasswordReset e-mails a single-use link if the address belongs to
// an account. It answers the same either way: the caller never learns
// whether the e-mail exists.
func (s *Service) RequestPasswordReset(ctx context.Context, email, publicURL string) error {
	if !s.PasswordResetAvailable(ctx, publicURL) {
		return apperr.Unavailable("A recuperação por e-mail não está configurada. Peça a um administrador para redefinir a senha.")
	}
	email = normalizeEmail(email)
	if email == "" || len(email) > 320 {
		fields := apperr.Fields{}
		fields.Add("email", "Informe um e-mail válido.")
		return fields.Err()
	}
	select {
	case s.resets.slots <- struct{}{}:
	default:
		return nil
	}
	s.resets.work.Add(1)
	go func() {
		defer func() { <-s.resets.slots; s.resets.work.Done() }()
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resetSendTimeout)
		defer cancel()
		if err := s.sendResetLink(sendCtx, email, publicURL); err != nil {
			s.resets.logger.Warn("password reset e-mail failed", "error", err)
		}
	}()
	return nil
}

func (s *Service) sendResetLink(ctx context.Context, email, publicURL string) error {
	record, err := s.store.userByEmail(ctx, email)
	if err != nil {
		return ignoreNotFound(err)
	}
	token := idgen.Token(32)
	now := s.now()
	if err := s.store.insertReset(ctx, hashToken(token), record.ID, now, now.Add(resetTTL)); err != nil {
		return err
	}
	// The token goes in the fragment: browsers do not send it to the server
	// nor put it in the Referer header.
	link := strings.TrimRight(publicURL, "/") + "/#reset=" + token
	return s.resets.mailer.SendPasswordReset(ctx, record.Email, link)
}

// ResetPassword sets a new password with a reset token. The token works
// once; every session of the account is closed. The second factor, if on,
// stays on.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	fields := apperr.Fields{}
	validatePassword(fields, password)
	if err := fields.Err(); err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 128 {
		return invalidResetLink()
	}
	userID, expiresAt, err := s.store.consumeReset(ctx, hashToken(token))
	if err != nil {
		return notFoundAsInvalidLink(err)
	}
	if !s.now().Before(expiresAt) {
		return invalidResetLink()
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.replacePassword(ctx, userID, string(hash))
}

func invalidResetLink() error {
	return apperr.Invalid("O link de redefinição é inválido ou expirou. Peça um novo.")
}

func notFoundAsInvalidLink(err error) error {
	if ignoreNotFound(err) == nil {
		return invalidResetLink()
	}
	return err
}
