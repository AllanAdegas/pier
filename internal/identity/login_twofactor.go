package identity

import (
	"context"
	"crypto/subtle"
)

// ChallengeUser returns whose login a valid challenge belongs to, so the
// caller can rate limit the codes per account before checking one.
func (s *Service) ChallengeUser(challenge string) (string, error) {
	verified, err := s.challenges.verify(challenge, s.now())
	return verified.userID, err
}

// CompleteLogin is the second step of a login with two factors: the
// challenge proves the password, the code proves the device (or it is a
// recovery code). It opens the session.
func (s *Service) CompleteLogin(ctx context.Context, challenge, code string, client Client) (LoginOutcome, error) {
	verified, err := s.challenges.verify(challenge, s.now())
	if err != nil {
		return LoginOutcome{}, err
	}
	if err := s.challenges.attempt(verified.nonce, s.now()); err != nil {
		return LoginOutcome{}, err
	}
	record, err := s.store.userByID(ctx, verified.userID)
	if err != nil {
		return LoginOutcome{}, unauthorizedIfMissing(err)
	}
	current := passwordFingerprint(record.PasswordHash)
	if subtle.ConstantTimeCompare([]byte(current), []byte(verified.fingerprint)) != 1 {
		return LoginOutcome{}, challengeExpired()
	}
	accepted, err := s.checkSecondFactor(ctx, record.ID, code)
	if err != nil {
		return LoginOutcome{}, err
	}
	if !accepted {
		return LoginOutcome{}, wrongCode()
	}
	if !s.challenges.spend(verified.nonce) {
		return LoginOutcome{}, challengeExpired()
	}
	return s.openVerifiedSession(ctx, record, client)
}
