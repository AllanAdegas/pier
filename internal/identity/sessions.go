package identity

import (
	"context"
	"strings"
	"unicode/utf8"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
)

const (
	maxUserAgentLength = 256
	maxClientIPLength  = 64
)

// OpenSession creates a session for an already-verified user.
func (s *Service) OpenSession(ctx context.Context, userID string) (string, error) {
	return s.StartSession(ctx, userID, Client{})
}

// StartSession creates a session and remembers who opened it, so its owner
// can recognize it in the session list.
func (s *Service) StartSession(ctx context.Context, userID string, client Client) (string, error) {
	now := s.now()
	_ = s.store.deleteExpiredSessions(ctx, now)
	token := idgen.Token(32)
	row := sessionRow{
		tokenHash: hashToken(token), id: idgen.New(), userID: userID,
		createdAt: now, expiresAt: now.Add(s.sessionTTL),
		client: Client{IP: clip(client.IP, maxClientIPLength), UserAgent: clip(client.UserAgent, maxUserAgentLength)},
	}
	if err := s.store.insertSession(ctx, row); err != nil {
		return "", err
	}
	return token, nil
}

// Logout closes a session.
func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	return s.store.deleteSession(ctx, hashToken(sessionToken))
}

// AuthenticateSession resolves a browser session cookie.
func (s *Service) AuthenticateSession(ctx context.Context, sessionToken string) (Principal, error) {
	tokenHash := hashToken(sessionToken)
	now := s.now()
	sessionID, userID, err := s.store.activeSession(ctx, tokenHash, now)
	if err != nil {
		return Principal{}, unauthorizedIfMissing(err)
	}
	record, err := s.store.userByID(ctx, userID)
	if err != nil {
		return Principal{}, unauthorizedIfMissing(err)
	}
	_ = s.store.touchSession(ctx, tokenHash, now)
	return Principal{User: record.User, Abilities: AllAbilities, SessionID: sessionID}, nil
}

// ListSessions returns the user's open sessions, marking the caller's.
func (s *Service) ListSessions(ctx context.Context, userID, currentSessionID string) ([]Session, error) {
	sessions, err := s.store.listSessions(ctx, userID, s.now())
	if err != nil {
		return nil, err
	}
	for index := range sessions {
		sessions[index].Current = sessions[index].ID == currentSessionID
	}
	return sessions, nil
}

// RevokeSession closes one of the user's own sessions.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	return notFoundAs(s.store.deleteSessionByID(ctx, userID, sessionID), "Sessão")
}

// RevokeOtherSessions closes every session of the user but the current one.
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, currentSessionID string) (int64, error) {
	if currentSessionID == "" {
		return 0, apperr.Forbidden("Esta operação exige login no painel.")
	}
	return s.store.deleteOtherSessions(ctx, userID, currentSessionID)
}

// clip bounds client-supplied text without splitting a UTF-8 sequence.
func clip(value string, limit int) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
