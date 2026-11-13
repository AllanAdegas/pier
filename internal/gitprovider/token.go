package gitprovider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pier/internal/platform/apperr"
)

// tokenRenewalMargin renews a cached token before it can expire mid-clone.
const tokenRenewalMargin = 5 * time.Minute

// tokenPurpose narrows what an installation token may do.
type tokenPurpose int

const (
	// purposeList lists the repositories of an installation.
	purposeList tokenPurpose = iota
	// purposeClone reads the contents of one repository.
	purposeClone
	// purposeReport writes commit statuses and pull request comments of one repository.
	purposeReport
)

func (p tokenPurpose) permissions() map[string]string {
	switch p {
	case purposeClone:
		return map[string]string{"contents": "read"}
	case purposeReport:
		return map[string]string{"statuses": "write", "pull_requests": "write"}
	}
	return map[string]string{"metadata": "read"}
}

type tokenKey struct {
	installation int64
	repository   string
	purpose      tokenPurpose
}

type cachedToken struct {
	value   string
	expires time.Time
}

// installationToken returns a short-lived token for key, minting one when
// the cached token is missing or close to its expiry. Clone and report
// tokens are narrowed to one repository. Never persist or log the result.
func (s *Service) installationToken(ctx context.Context, key tokenKey) (string, error) {
	if key.installation <= 0 || key.purpose != purposeList && !validRepo(key.repository) {
		return "", invalid()
	}
	key.repository = strings.ToLower(key.repository)
	if err := s.checkInstallation(ctx, key.installation); err != nil {
		return "", err
	}
	if token, found := s.cachedToken(key); found {
		return token, nil
	}
	jwt, err := s.jwt(ctx)
	if err != nil {
		return "", err
	}
	input := map[string]any{"permissions": key.purpose.permissions()}
	if key.purpose != purposeList {
		input["repositories"] = []string{strings.SplitN(key.repository, "/", 2)[1]}
	}
	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", key.installation)
	if err := s.github(ctx, http.MethodPost, path, jwt, input, &result); err != nil {
		return "", mintError(err)
	}
	if result.Token == "" || !result.ExpiresAt.After(s.now().Add(tokenRenewalMargin)) {
		return "", apperr.Unavailable("O GitHub devolveu um token de instalação inválido.")
	}
	s.mu.Lock()
	s.tokens[key] = cachedToken{value: result.Token, expires: result.ExpiresAt}
	s.mu.Unlock()
	return result.Token, nil
}

// mintError explains why GitHub refused an installation token.
func mintError(err error) error {
	switch statusOf(err) {
	case http.StatusUnauthorized:
		return apperr.Unavailable("O GitHub recusou as credenciais do app. Confira o App ID e a chave privada em Configurações.")
	case http.StatusForbidden, http.StatusNotFound:
		return apperr.Unavailable("A instalação do GitHub App foi removida, suspensa ou não tem as permissões necessárias. " +
			"Reinstale o app no GitHub ou vincule outro repositório.")
	case http.StatusUnprocessableEntity:
		return apperr.Unavailable("O repositório não está mais disponível para a instalação do GitHub App. " +
			"Inclua-o na instalação ou vincule outro repositório.")
	}
	return err
}

func (s *Service) checkInstallation(ctx context.Context, id int64) error {
	var suspended int
	err := s.db.QueryRowContext(ctx, `SELECT suspended FROM git_provider_installations WHERE id=?`, id).Scan(&suspended)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if suspended != 0 {
		return apperr.Unavailable("A instalação do GitHub App foi removida ou suspensa. Reinstale o app no GitHub.")
	}
	return nil
}

func (s *Service) cachedToken(key tokenKey) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cached, found := s.tokens[key]
	if !found || !cached.expires.After(s.now().Add(tokenRenewalMargin)) {
		delete(s.tokens, key)
		return "", false
	}
	return cached.value, true
}

// forgetTokens drops the cached tokens selected by match.
func (s *Service) forgetTokens(match func(tokenKey) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.tokens {
		if match(key) {
			delete(s.tokens, key)
		}
	}
}

func (s *Service) forgetInstallation(id int64) {
	s.forgetTokens(func(key tokenKey) bool { return key.installation == id })
}

// withInstallationToken runs call with a token for key. A 401 means the
// token was revoked before it expired: it is dropped so the next call mints
// a fresh one (or learns that the installation is gone).
func (s *Service) withInstallationToken(ctx context.Context, key tokenKey, call func(token string) error) error {
	token, err := s.installationToken(ctx, key)
	if err != nil {
		return err
	}
	err = call(token)
	if statusOf(err) == http.StatusUnauthorized {
		s.forgetInstallation(key.installation)
	}
	return err
}
