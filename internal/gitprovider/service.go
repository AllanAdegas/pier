// Package gitprovider implements a self-hosted GitHub App, independently of login.
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
	"pier/internal/platform/secretbox"
	"sync"
	"time"
)

type Config struct {
	AppID         int64  `json:"app_id"`
	PrivateKey    string `json:"private_key,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}
type cachedToken struct {
	value   string
	expires time.Time
}
type Service struct {
	db           *sql.DB
	box          *secretbox.Box
	client       *http.Client
	mu           sync.Mutex
	tokens       map[int64]cachedToken
	scopedTokens map[string]cachedToken
	blockedUntil time.Time
}

func New(db *sql.DB, box *secretbox.Box) *Service {
	return &Service{db: db, box: box, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, tokens: map[int64]cachedToken{}}
}

// NewWithHTTPClient supports embedded integrations with a custom transport.
// The fixed API host, timeout and no-redirect policy still apply.
func NewWithHTTPClient(db *sql.DB, box *secretbox.Box, client *http.Client) *Service {
	s := New(db, box)
	if client != nil {
		configured := *client
		configured.Timeout = 15 * time.Second
		configured.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		s.client = &configured
	}
	return s
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
func (s *Service) Configure(ctx context.Context, c Config) error {
	if c.AppID <= 0 || len(c.WebhookSecret) < 32 || len(c.WebhookSecret) > 1024 {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.ExecContext(ctx, `INSERT INTO git_provider_config(id,encrypted) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET encrypted=excluded.encrypted`, sealed)
	if err == nil {
		s.tokens = map[int64]cachedToken{}
		s.scopedTokens = map[string]cachedToken{}
	}
	return err
}
func (s *Service) config(ctx context.Context) (Config, error) {
	var sealed string
	var c Config
	if err := s.db.QueryRowContext(ctx, `SELECT encrypted FROM git_provider_config WHERE id=1`).Scan(&sealed); err != nil {
		return c, err
	}
	raw, err := s.box.Open(sealed)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(raw), &c)
	return c, err
}
func (s *Service) Configured(ctx context.Context) bool { _, err := s.config(ctx); return err == nil }
func (s *Service) jwt(ctx context.Context) (string, error) {
	c, err := s.config(ctx)
	if err != nil {
		return "", err
	}
	key, err := parseKey(c.PrivateKey)
	if err != nil {
		return "", err
	}
	now := time.Now()
	payload, _ := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(8 * time.Minute).Unix(), "iss": fmt.Sprint(c.AppID)})
	unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(payload)
	hash := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
