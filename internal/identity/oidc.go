package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"pier/internal/access"
	"pier/internal/platform/apperr"
	"pier/internal/platform/secretbox"
)

// OIDCConfig is sealed in full. Issuers and callback are exact deployment
// allowlists, never derived from Host/X-Forwarded-Host or login input.
type OIDCConfig struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Callback     string `json:"callback"`
	// RoleClaim, RoleMappings and DefaultRole map the ID token to the
	// instance role of the linked account (see oidc_roles.go).
	RoleClaim    string            `json:"role_claim,omitempty"`
	RoleMappings map[string]string `json:"role_mappings,omitempty"`
	DefaultRole  string            `json:"default_role,omitempty"`
}
type OIDCService struct {
	db       *sql.DB
	box      *secretbox.Box
	allowed  map[string]bool
	callback string
	client   *http.Client
}

func NewOIDC(db *sql.DB, box *secretbox.Box, issuers []string, callback string) *OIDCService {
	allowed := map[string]bool{}
	for _, issuer := range issuers {
		if secureOIDCURL(issuer) {
			allowed[issuer] = true
		}
	}
	// No redirects when retrieving discovery, keys or exchanging credentials.
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &OIDCService{db: db, box: box, allowed: allowed, callback: callback, client: client}
}
func secureOIDCURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && u.RawQuery == ""
}
func oidcDenied() error { return apperr.Forbidden("SSO não autorizado.") }
func oidcRandom() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func oidcHash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func (s *OIDCService) owner(ctx context.Context, actor string) error {
	var role string
	if err := s.db.QueryRowContext(ctx, `SELECT role FROM access_instance_roles WHERE user_id=?`, actor).Scan(&role); err != nil {
		return oidcDenied()
	}
	if role != "owner" {
		return oidcDenied()
	}
	return nil
}
func (s *OIDCService) Configure(ctx context.Context, actor string, c OIDCConfig) error {
	if err := s.owner(ctx, actor); err != nil {
		return err
	}
	if s.box == nil || !s.allowed[c.Issuer] || !secureOIDCURL(c.Callback) || c.Callback != s.callback || strings.TrimSpace(c.ClientID) == "" {
		return oidcDenied()
	}
	if err := validateRoleMapping(c); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	sealed, err := s.box.Seal(string(raw))
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO access_oidc_config(id,sealed) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET sealed=excluded.sealed`, sealed); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM access_oidc_states`); err != nil {
		return err
	}
	return tx.Commit()
}

// Link authorizes an exact subject for an existing identity. Email claims are
// deliberately ignored, verified or otherwise. No public registration exists.
func (s *OIDCService) Link(ctx context.Context, actor, issuer, subject, user string) error {
	if err := s.owner(ctx, actor); err != nil {
		return err
	}
	if !s.allowed[issuer] || subject == "" || len(subject) > 255 {
		return oidcDenied()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO access_oidc_links(issuer,subject,user_id) VALUES(?,?,?)`, issuer, subject, user)
	return err
}
func (s *OIDCService) Unlink(ctx context.Context, actor, issuer, subject string) error {
	if err := s.owner(ctx, actor); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM access_oidc_links WHERE issuer=? AND subject=?`, issuer, subject)
	return err
}
func (s *OIDCService) config(ctx context.Context) (OIDCConfig, error) {
	var c OIDCConfig
	var sealed string
	if s.box == nil {
		return c, oidcDenied()
	}
	if err := s.db.QueryRowContext(ctx, `SELECT sealed FROM access_oidc_config WHERE id=1`).Scan(&sealed); err != nil {
		return c, oidcDenied()
	}
	raw, err := s.box.Open(sealed)
	if err != nil {
		return c, oidcDenied()
	}
	if err = json.Unmarshal([]byte(raw), &c); err != nil {
		return c, oidcDenied()
	}
	if !s.allowed[c.Issuer] || c.Callback != s.callback {
		return c, oidcDenied()
	}
	return c, nil
}
func (s *OIDCService) provider(ctx context.Context, c OIDCConfig) (*oidc.Provider, *oauth2.Config, error) {
	ctx = oidc.ClientContext(ctx, s.client)
	p, err := oidc.NewProvider(ctx, c.Issuer)
	if err != nil {
		return nil, nil, oidcDenied()
	}
	var discovery struct {
		JWKS string `json:"jwks_uri"`
	}
	if p.Claims(&discovery) != nil || !secureOIDCURL(discovery.JWKS) {
		return nil, nil, oidcDenied()
	}
	ep := p.Endpoint()
	if !secureOIDCURL(ep.AuthURL) || !secureOIDCURL(ep.TokenURL) {
		return nil, nil, oidcDenied()
	}
	return p, &oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: c.Callback, Endpoint: ep, Scopes: []string{oidc.ScopeOpenID}}, nil
}

type oidcPending struct {
	Verifier string
	Nonce    string
}

// Begin returns the provider redirect and a browser-binding secret. The HTTP
// adapter must set that secret in a Secure/HttpOnly/SameSite=Lax cookie.
func (s *OIDCService) Begin(ctx context.Context) (string, string, error) {
	c, err := s.config(ctx)
	if err != nil {
		return "", "", err
	}
	_, oauth, err := s.provider(ctx, c)
	if err != nil {
		return "", "", err
	}
	state, err := oidcRandom()
	if err != nil {
		return "", "", err
	}
	browser, err := oidcRandom()
	if err != nil {
		return "", "", err
	}
	nonce, err := oidcRandom()
	if err != nil {
		return "", "", err
	}
	verifier := oauth2.GenerateVerifier()
	raw, err := json.Marshal(oidcPending{verifier, nonce})
	if err != nil {
		return "", "", err
	}
	sealed, err := s.box.Seal(string(raw))
	if err != nil {
		return "", "", err
	}
	// Bound pending states without retaining stale credentials indefinitely.
	_, err = s.db.ExecContext(ctx, `DELETE FROM access_oidc_states WHERE expires_at<=?`, time.Now().Unix())
	if err != nil {
		return "", "", err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO access_oidc_states(state_hash,browser_hash,sealed,expires_at) SELECT ?,?,?,? WHERE (SELECT COUNT(*) FROM access_oidc_states)<1024`, oidcHash(state), oidcHash(browser), sealed, time.Now().Add(5*time.Minute).Unix())
	if err != nil {
		return "", "", err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return "", "", oidcDenied()
	}
	return oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)), browser, nil
}

// OIDCLogin is a verified SSO login of a linked account.
type OIDCLogin struct {
	UserID string
	// Role is the instance role the configuration maps the token to ("" =
	// none); it applies only when RoleManaged.
	Role        access.Role
	RoleManaged bool
}

// Complete consumes state before network I/O. Signature, issuer, audience,
// expiry, nonce and PKCE are verified; only an administratively linked user
// is returned, with the instance role the configuration maps the token to.
func (s *OIDCService) Complete(ctx context.Context, state, browser, code string) (OIDCLogin, error) {
	user, claims, config, err := s.exchange(ctx, state, browser, code)
	if err != nil {
		return OIDCLogin{}, err
	}
	role, managed := mappedRole(config, claims)
	return OIDCLogin{UserID: user, Role: role, RoleManaged: managed}, nil
}

// exchange redeems the code and returns the linked account and the claims.
func (s *OIDCService) exchange(ctx context.Context, state, browser, code string) (string, map[string]any, OIDCConfig, error) {
	if state == "" || browser == "" || code == "" {
		return "", nil, OIDCConfig{}, oidcDenied()
	}
	var sealed string
	err := s.db.QueryRowContext(ctx, `DELETE FROM access_oidc_states WHERE state_hash=? AND browser_hash=? AND expires_at>? RETURNING sealed`, oidcHash(state), oidcHash(browser), time.Now().Unix()).Scan(&sealed)
	if err != nil {
		return "", nil, OIDCConfig{}, oidcDenied()
	}
	raw, err := s.box.Open(sealed)
	if err != nil {
		return "", nil, OIDCConfig{}, oidcDenied()
	}
	var pending oidcPending
	if json.Unmarshal([]byte(raw), &pending) != nil {
		return "", nil, OIDCConfig{}, oidcDenied()
	}
	c, err := s.config(ctx)
	if err != nil {
		return "", nil, OIDCConfig{}, err
	}
	p, oauth, err := s.provider(ctx, c)
	if err != nil {
		return "", nil, OIDCConfig{}, err
	}
	token, err := oauth.Exchange(oidc.ClientContext(ctx, s.client), code, oauth2.VerifierOption(pending.Verifier))
	if err != nil {
		return "", nil, OIDCConfig{}, oidcDenied()
	}
	idToken, ok := token.Extra("id_token").(string)
	if !ok {
		return "", nil, OIDCConfig{}, oidcDenied()
	}
	verified, err := p.Verifier(&oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{"RS256", "ES256"}}).Verify(oidc.ClientContext(ctx, s.client), idToken)
	if err != nil || subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(pending.Nonce)) != 1 {
		return "", nil, OIDCConfig{}, oidcDenied()
	}
	var user string
	err = s.db.QueryRowContext(ctx, `SELECT user_id FROM access_oidc_links WHERE issuer=? AND subject=?`, c.Issuer, verified.Subject).Scan(&user)
	if err != nil {
		return "", nil, OIDCConfig{}, oidcDenied()
	}
	claims := map[string]any{}
	if err := verified.Claims(&claims); err != nil {
		return "", nil, OIDCConfig{}, oidcDenied()
	}
	return user, claims, c, nil
}
// Disable removes credentials and invalidates pending logins atomically.
func (s *OIDCService) Disable(ctx context.Context, actor string) error {
	if err := s.owner(ctx, actor); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM access_oidc_config`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM access_oidc_states`); err != nil {
		return err
	}
	return tx.Commit()
}
