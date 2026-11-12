package gitprovider

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"pier/internal/platform/apperr"
	"strings"
	"time"
)

func invalid() error { return apperr.Invalid("Configuração do GitHub App inválida.") }
func (s *Service) request(ctx context.Context, method, path, token string, input, output any) error {
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return apperr.Invalid("GitHub indisponível. Tente novamente.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 || resp.StatusCode == 403 && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		s.blockedUntil = time.Now().Add(time.Minute)
		return apperr.Invalid("Limite do GitHub atingido. Aguarde um minuto.")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apperr.Invalid("O GitHub recusou a operação (HTTP %d).", resp.StatusCode)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(output)
}

// Token never belongs in a URL or persisted configuration. Callers must keep it in memory.
func (s *Service) token(ctx context.Context, id int64) (string, error) {
	if id <= 0 {
		return "", invalid()
	}
	var suspended int
	err := s.db.QueryRowContext(ctx, `SELECT suspended FROM git_provider_installations WHERE id=?`, id).Scan(&suspended)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err == nil && suspended != 0 {
		return "", invalid()
	}
	if t := s.tokens[id]; t.expires.After(time.Now().Add(time.Minute)) {
		return t.value, nil
	}
	jwt, err := s.jwt(ctx)
	if err != nil {
		return "", err
	}
	var result struct {
		Token   string    `json:"token"`
		Expires time.Time `json:"expires_at"`
	}
	if err = s.request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", id), jwt, map[string]any{"permissions": map[string]string{"contents": "read", "statuses": "write", "pull_requests": "write"}}, &result); err != nil {
		return "", err
	}
	if result.Token == "" || !result.Expires.After(time.Now().Add(time.Minute)) {
		return "", invalid()
	}
	s.tokens[id] = cachedToken{result.Token, result.Expires}
	return result.Token, nil
}
func (s *Service) locked(ctx context.Context, fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Now().Before(s.blockedUntil) {
		return apperr.Invalid("Limite do GitHub atingido. Aguarde um minuto.")
	}
	return fn()
}
func (s *Service) Installations(ctx context.Context) ([]json.RawMessage, error) {
	result := []json.RawMessage{}
	err := s.locked(ctx, func() error {
		jwt, err := s.jwt(ctx)
		if err != nil {
			return err
		}
		for page := 1; page <= 100; page++ {
			var rows []json.RawMessage
			if err = s.request(ctx, "GET", fmt.Sprintf("/app/installations?per_page=100&page=%d", page), jwt, nil, &rows); err != nil {
				return err
			}
			result = append(result, rows...)
			if len(rows) < 100 {
				return nil
			}
		}
		return invalid()
	})
	return result, err
}

type Repository struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

func (s *Service) repositories(ctx context.Context, id int64) ([]Repository, error) {
	token, err := s.token(ctx, id)
	if err != nil {
		return nil, err
	}
	result := []Repository{}
	for page := 1; page <= 100; page++ {
		var data struct {
			Repositories []Repository `json:"repositories"`
		}
		if err = s.request(ctx, "GET", fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page), token, nil, &data); err != nil {
			return nil, err
		}
		result = append(result, data.Repositories...)
		if len(data.Repositories) < 100 {
			return result, nil
		}
	}
	return nil, invalid()
}
func (s *Service) Repositories(ctx context.Context, id int64) ([]Repository, error) {
	var rows []Repository
	err := s.locked(ctx, func() error { var err error; rows, err = s.repositories(ctx, id); return err })
	return rows, err
}
func validRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return false
		}
		for _, c := range p {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return false
			}
		}
	}
	return true
}
func repositoryPath(repo string) string {
	parts := strings.Split(repo, "/")
	return "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
}
