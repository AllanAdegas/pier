package gitprovider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// repositoryToken is always called with mu held. Clone tokens have contents
// read only and are narrowed to the selected repository, never the installation.
func (s *Service) repositoryToken(ctx context.Context, id int64, repository string, reporting bool) (string, error) {
	if id <= 0 || !validRepo(repository) {
		return "", invalid()
	}
	var suspended int
	err := s.db.QueryRowContext(ctx, `SELECT suspended FROM git_provider_installations WHERE id=?`, id).Scan(&suspended)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if suspended != 0 {
		return "", invalid()
	}
	key := fmt.Sprintf("%d/%s/%t", id, repository, reporting)
	if cached := s.scopedTokens[key]; cached.expires.After(time.Now().Add(time.Minute)) {
		return cached.value, nil
	}
	jwt, err := s.jwt(ctx)
	if err != nil {
		return "", err
	}
	permissions := map[string]string{"contents": "read"}
	if reporting {
		permissions = map[string]string{"statuses": "write", "pull_requests": "write"}
	}
	var result struct {
		Token   string    `json:"token"`
		Expires time.Time `json:"expires_at"`
	}
	input := map[string]any{"repositories": []string{strings.Split(repository, "/")[1]}, "permissions": permissions}
	if err = s.request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", id), jwt, input, &result); err != nil {
		return "", err
	}
	if result.Token == "" || !result.Expires.After(time.Now().Add(time.Minute)) {
		return "", invalid()
	}
	if s.scopedTokens == nil {
		s.scopedTokens = map[string]cachedToken{}
	}
	s.scopedTokens[key] = cachedToken{result.Token, result.Expires}
	return result.Token, nil
}
