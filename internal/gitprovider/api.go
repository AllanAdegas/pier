package gitprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"pier/internal/platform/apperr"
)

const (
	maxResponseBytes  = 4 << 20
	rateLimitCooldown = time.Minute
	githubAPI         = "https://api.github.com"
)

// apiRequest is one JSON call to a provider API. The credential travels in
// a header only, never in the URL, and no response body is ever echoed.
type apiRequest struct {
	provider string
	method   string
	url      string
	header   http.Header
	input    any
	output   any
	// received, when set, gets the response headers of a 2xx answer
	// (pagination and token scopes travel there).
	received *http.Header
}

// upstreamError is a provider answer outside 2xx. Its text names only the
// provider and the status, so it is safe to log and to return.
type upstreamError struct {
	provider string
	status   int
}

func (e *upstreamError) Error() string {
	return apperr.Unavailable("%s recusou a operação (HTTP %d).", e.provider, e.status).Error()
}

// Unwrap lets the HTTP layer render the error like any other apperr.
func (e *upstreamError) Unwrap() error {
	return apperr.Unavailable("%s recusou a operação (HTTP %d).", e.provider, e.status)
}

func statusOf(err error) int {
	var upstream *upstreamError
	if errors.As(err, &upstream) {
		return upstream.status
	}
	return 0
}

func (s *Service) call(ctx context.Context, r apiRequest) error {
	var body io.Reader = http.NoBody
	if r.input != nil {
		raw, err := json.Marshal(r.input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, r.url, body)
	if err != nil {
		return apperr.Invalid("Endereço da API do %s inválido.", r.provider)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Pier")
	if r.input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range r.header {
		req.Header[key] = values
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return apperr.Unavailable("%s indisponível. Tente novamente.", r.provider)
	}
	defer resp.Body.Close()
	if r.provider == "GitHub" && rateLimited(resp) {
		s.block()
		return apperr.Unavailable("Limite do GitHub atingido. Aguarde um minuto.")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &upstreamError{provider: r.provider, status: resp.StatusCode}
	}
	if r.received != nil {
		*r.received = resp.Header.Clone()
	}
	if r.output == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(r.output); err != nil {
		return apperr.Unavailable("Resposta inesperada do %s.", r.provider)
	}
	return nil
}

func rateLimited(resp *http.Response) bool {
	return resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0"
}

// github calls the REST API of the configured GitHub (github.com or a
// GitHub Enterprise Server), honouring the rate limit cooldown.
func (s *Service) github(ctx context.Context, method, path, bearer string, input, output any) error {
	return s.githubAt(ctx, s.githubAPIBase(ctx), method, path, bearer, input, output)
}

// githubAt calls the GitHub REST API at base.
func (s *Service) githubAt(ctx context.Context, base, method, path, bearer string, input, output any) error {
	if s.blocked() {
		return apperr.Unavailable("Limite do GitHub atingido. Aguarde um minuto.")
	}
	header := http.Header{}
	header.Set("Accept", "application/vnd.github+json")
	header.Set("X-GitHub-Api-Version", "2022-11-28")
	if bearer != "" {
		header.Set("Authorization", "Bearer "+bearer)
	}
	return s.call(ctx, apiRequest{provider: "GitHub", method: method, url: base + path, header: header, input: input, output: output})
}

func (s *Service) blocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Before(s.blockedUntil)
}

func (s *Service) block() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockedUntil = s.now().Add(rateLimitCooldown)
}
