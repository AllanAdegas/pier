package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
)

// OIDCSettings is what the owner sees of the SSO setup: the stored
// configuration without its client secret, the values the server allows
// (from PIER_OIDC_ISSUERS and PIER_OIDC_CALLBACK) and the authorized identities.
type OIDCSettings struct {
	Configured       bool              `json:"configured"`
	Issuer           string            `json:"issuer"`
	ClientID         string            `json:"client_id"`
	ClientSecretSet  bool              `json:"client_secret_set"`
	Callback         string            `json:"callback"`
	RoleClaim        string            `json:"role_claim"`
	RoleMappings     map[string]string `json:"role_mappings"`
	DefaultRole      string            `json:"default_role"`
	AllowedIssuers   []string          `json:"allowed_issuers"`
	ExpectedCallback string            `json:"expected_callback"`
	Links            []OIDCLink        `json:"links"`
}

// OIDCLink is one provider identity authorized to sign in as a Pier user.
type OIDCLink struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
}

// Settings returns the SSO setup to an instance owner. The client secret is
// never returned; ClientSecretSet tells whether one is stored.
func (s *OIDCService) Settings(ctx context.Context, actor string) (OIDCSettings, error) {
	if err := s.owner(ctx, actor); err != nil {
		return OIDCSettings{}, err
	}
	out := OIDCSettings{RoleClaim: "groups", RoleMappings: map[string]string{}, AllowedIssuers: s.allowedIssuers(), ExpectedCallback: s.callback}
	stored, found, err := s.storedConfig(ctx)
	if err != nil {
		return OIDCSettings{}, err
	}
	if found {
		out.Configured = true
		out.Issuer, out.ClientID, out.Callback = stored.Issuer, stored.ClientID, stored.Callback
		out.ClientSecretSet = stored.ClientSecret != ""
		out.DefaultRole = stored.DefaultRole
		if stored.RoleClaim != "" {
			out.RoleClaim = stored.RoleClaim
		}
		if stored.RoleMappings != nil {
			out.RoleMappings = stored.RoleMappings
		}
	}
	out.Links, err = s.links(ctx)
	return out, err
}

func (s *OIDCService) allowedIssuers() []string {
	issuers := make([]string, 0, len(s.allowed))
	for issuer := range s.allowed {
		issuers = append(issuers, issuer)
	}
	sort.Strings(issuers)
	return issuers
}

// storedConfig reads the sealed configuration as saved, without the
// allowlist checks that config applies before a sign-in.
func (s *OIDCService) storedConfig(ctx context.Context) (OIDCConfig, bool, error) {
	var c OIDCConfig
	if s.box == nil {
		return c, false, nil
	}
	var sealed string
	err := s.db.QueryRowContext(ctx, `SELECT sealed FROM access_oidc_config WHERE id=1`).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	raw, err := s.box.Open(sealed)
	if err != nil {
		return c, false, err
	}
	err = json.Unmarshal([]byte(raw), &c)
	return c, err == nil, err
}

func (s *OIDCService) links(ctx context.Context) ([]OIDCLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.issuer, l.subject, l.user_id, u.email FROM access_oidc_links l
		JOIN users u ON u.id = l.user_id ORDER BY u.email, l.issuer, l.subject`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := []OIDCLink{}
	for rows.Next() {
		var link OIDCLink
		if err := rows.Scan(&link.Issuer, &link.Subject, &link.UserID, &link.Email); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

// keepStoredSecret fills an empty client secret with the stored one, so the
// owner can change other fields without typing the secret again. The secret
// belongs to one client of one issuer: changing either requires a new one.
func (s *OIDCService) keepStoredSecret(ctx context.Context, c *OIDCConfig) error {
	if c.ClientSecret != "" {
		return nil
	}
	stored, found, err := s.storedConfig(ctx)
	if err != nil || !found || stored.Issuer != c.Issuer || stored.ClientID != c.ClientID {
		return err
	}
	c.ClientSecret = stored.ClientSecret
	return nil
}
