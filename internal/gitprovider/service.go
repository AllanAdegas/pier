// Package gitprovider integrates Pier with Git hosting providers: a
// self-hosted GitHub App (installation tokens, repository selection and
// webhooks) and the feedback Pier gives on commits and pull requests of
// GitHub, GitLab, Gitea/Forgejo and Bitbucket Cloud.
package gitprovider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/secretbox"
)

const (
	requestTimeout       = 15 * time.Second
	manualSecretLength   = 32
	generatedSecretLimit = 16
	maxSecretLength      = 1024
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)

// Config is the GitHub App of the instance. It is stored sealed and never
// returned by the API: Status is what callers see.
type Config struct {
	AppID         int64  `json:"app_id"`
	Slug          string `json:"slug,omitempty"`
	PrivateKey    string `json:"private_key,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// Status describes the configured GitHub App without any secret.
type Status struct {
	Configured bool   `json:"configured"`
	AppID      int64  `json:"app_id,omitempty"`
	Slug       string `json:"slug,omitempty"`
	InstallURL string `json:"install_url,omitempty"`
}

// Service is the Git provider integration. Tokens live only in memory.
type Service struct {
	db     *sql.DB
	box    *secretbox.Box
	client *http.Client
	now    func() time.Time

	mu           sync.Mutex
	tokens       map[tokenKey]cachedToken
	blockedUntil time.Time
	manifests    map[string]time.Time
	// checksRefused holds installations whose App cannot write check runs,
	// until when commit statuses are used instead.
	checksRefused map[int64]time.Time
}

// New creates the service with the production HTTP client.
func New(db *sql.DB, box *secretbox.Box) *Service {
	return NewWithHTTPClient(db, box, nil)
}

// NewWithHTTPClient lets tests replace the transport. The timeout and the
// refusal to follow redirects (which could forward a token) always apply.
func NewWithHTTPClient(db *sql.DB, box *secretbox.Box, client *http.Client) *Service {
	configured := http.Client{}
	if client != nil {
		configured = *client
	}
	configured.Timeout = requestTimeout
	configured.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Service{
		db: db, box: box, client: &configured, now: time.Now,
		tokens: map[tokenKey]cachedToken{}, manifests: map[string]time.Time{},
		checksRefused: map[int64]time.Time{},
	}
}

func invalid() error { return apperr.Invalid("Configuração do GitHub App inválida.") }

// Configure stores credentials typed by the operator (manual setup).
func (s *Service) Configure(ctx context.Context, c Config) error {
	if len(c.WebhookSecret) < manualSecretLength {
		return invalid()
	}
	return s.store(ctx, c)
}

func (s *Service) store(ctx context.Context, c Config) error {
	if c.AppID <= 0 || len(c.WebhookSecret) < generatedSecretLimit || len(c.WebhookSecret) > maxSecretLength {
		return invalid()
	}
	if c.Slug != "" && !slugPattern.MatchString(c.Slug) {
		return invalid()
	}
	if _, err := parseKey(c.PrivateKey); err != nil {
		return invalid()
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	sealed, err := s.box.Seal(string(raw))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO git_provider_config(id,encrypted) VALUES(1,?)
		ON CONFLICT(id) DO UPDATE SET encrypted=excluded.encrypted`, sealed)
	if err == nil {
		s.forgetTokens(func(tokenKey) bool { return true })
	}
	return err
}

func (s *Service) config(ctx context.Context) (Config, error) {
	var sealed string
	var c Config
	err := s.db.QueryRowContext(ctx, `SELECT encrypted FROM git_provider_config WHERE id=1`).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return c, apperr.Invalid("Nenhum GitHub App configurado nesta instância.")
	}
	if err != nil {
		return c, err
	}
	raw, err := s.box.Open(sealed)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(raw), &c)
	return c, err
}

// Configured reports whether a GitHub App is set up.
func (s *Service) Configured(ctx context.Context) bool {
	_, err := s.config(ctx)
	return err == nil
}

// Status returns the public facts of the configured GitHub App.
func (s *Service) Status(ctx context.Context) Status {
	c, err := s.config(ctx)
	if err != nil {
		return Status{}
	}
	status := Status{Configured: true, AppID: c.AppID, Slug: c.Slug}
	if c.Slug != "" {
		status.InstallURL = "https://github.com/apps/" + c.Slug + "/installations/new"
	}
	return status
}

func parseKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("invalid PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		if key.N.BitLen() < 2048 {
			return nil, errors.New("RSA key too small")
		}
		return key, nil
	}
	raw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := raw.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() < 2048 {
		return nil, errors.New("invalid RSA key")
	}
	return key, nil
}

// jwt signs the short-lived App token that mints installation tokens.
func (s *Service) jwt(ctx context.Context) (string, error) {
	c, err := s.config(ctx)
	if err != nil {
		return "", err
	}
	key, err := parseKey(c.PrivateKey)
	if err != nil {
		return "", err
	}
	now := s.now()
	payload, err := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(8 * time.Minute).Unix(), "iss": fmt.Sprint(c.AppID),
	})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	hash := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
