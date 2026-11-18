package identity

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
)

const (
	settingsIssuer   = "https://id.example.com"
	settingsCallback = "https://pier.example/api/v1/access/oidc/callback"
)

func newSettingsService(t *testing.T) *OIDCService {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, user := range []string{"owner", "member"} {
		if _, err = db.Exec(`INSERT INTO users(id,email,name,password_hash,created_at) VALUES(?,?,?,'unused','2026-01-01T00:00:00Z')`,
			user, user+"@example.com", user); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO access_instance_roles VALUES('owner','owner')`); err != nil {
		t.Fatal(err)
	}
	return NewOIDC(db, testbox.Box(t), []string{settingsIssuer, "http://insecure.example"}, settingsCallback)
}

func TestOIDCSettingsBeforeConfigurationShowWhatTheServerAllows(t *testing.T) {
	s := newSettingsService(t)
	got, err := s.Settings(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	if got.Configured || got.RoleClaim != "groups" || got.ExpectedCallback != settingsCallback {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if strings.Join(got.AllowedIssuers, ",") != settingsIssuer {
		t.Fatalf("allowed issuers must list only the secure allowlist: %v", got.AllowedIssuers)
	}
	if got.Links == nil || got.RoleMappings == nil {
		t.Fatal("empty collections must encode as [] and {}")
	}
}

func TestOIDCSettingsNeverReturnTheSecretAndKeepItOnSave(t *testing.T) {
	ctx := context.Background()
	s := newSettingsService(t)
	config := OIDCConfig{Issuer: settingsIssuer, ClientID: "pier", ClientSecret: "first-secret", Callback: settingsCallback,
		RoleClaim: "teams", RoleMappings: map[string]string{"ops": "admin"}, DefaultRole: "viewer"}
	if err := s.Configure(ctx, "owner", config); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "owner", settingsIssuer, "subject-1", "member"); err != nil {
		t.Fatal(err)
	}
	config.ClientSecret, config.DefaultRole = "", "developer"
	if err := s.Configure(ctx, "owner", config); err != nil {
		t.Fatal(err)
	}
	stored, found, err := s.storedConfig(ctx)
	if err != nil || !found || stored.ClientSecret != "first-secret" || stored.DefaultRole != "developer" {
		t.Fatalf("an empty secret must keep the stored one: %+v %v", stored, err)
	}
	got, err := s.Settings(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Configured || !got.ClientSecretSet || got.ClientID != "pier" || got.RoleClaim != "teams" || got.RoleMappings["ops"] != "admin" {
		t.Fatalf("stored settings not returned: %+v", got)
	}
	if len(got.Links) != 1 || got.Links[0].Email != "member@example.com" || got.Links[0].Subject != "subject-1" {
		t.Fatalf("links: %+v", got.Links)
	}
}

func TestOIDCChangingTheClientRequiresANewSecret(t *testing.T) {
	ctx := context.Background()
	s := newSettingsService(t)
	config := OIDCConfig{Issuer: settingsIssuer, ClientID: "pier", ClientSecret: "first-secret", Callback: settingsCallback}
	if err := s.Configure(ctx, "owner", config); err != nil {
		t.Fatal(err)
	}
	config.ClientID, config.ClientSecret = "other-client", ""
	if err := s.Configure(ctx, "owner", config); err != nil {
		t.Fatal(err)
	}
	stored, _, err := s.storedConfig(ctx)
	if err != nil || stored.ClientSecret != "" {
		t.Fatalf("a new client must not inherit the old secret: %+v %v", stored, err)
	}
}

func TestOIDCSettingsAreOwnerOnly(t *testing.T) {
	s := newSettingsService(t)
	if _, err := s.Settings(context.Background(), "member"); err == nil {
		t.Fatal("a non-owner read the SSO settings")
	}
}
