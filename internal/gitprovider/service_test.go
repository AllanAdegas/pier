package gitprovider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
)

const webhookSecret = "ssssssssssssssssssssssssssssssss"

var testKey = func() string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}()

// fixture is a configured service over a temporary database where
// application "app" and stack "stack" exist.
func fixture(t *testing.T) *Service {
	t.Helper()
	return fixtureWithClient(t, nil)
}

func fixtureWithClient(t *testing.T, client *http.Client) *Service {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`PRAGMA foreign_keys=OFF`,
		`INSERT INTO applications(id,environment_id,server_id,name,config,webhook_secret,created_at,updated_at)
			VALUES('app','env','srv','app','{}','x','now','now')`,
		`INSERT INTO stacks(id,environment_id,server_id,name,compose,created_at,updated_at)
			VALUES('stack','env','srv','stack','','now','now')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	box := testbox.Box(t)
	s := NewWithHTTPClient(db, box, client)
	if err := s.Configure(context.Background(), Config{AppID: 42, Slug: "pier-test", PrivateKey: testKey, WebhookSecret: webhookSecret}); err != nil {
		t.Fatal(err)
	}
	return s
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// mock answers every request of the service with handler, in process.
func mock(s *Service, handler http.HandlerFunc) {
	s.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Result(), nil
	})
}

// githubFake routes requests by "METHOD path" (query excluded) and fails
// the test on anything unexpected.
func githubFake(t *testing.T, s *Service, routes map[string]http.HandlerFunc) {
	t.Helper()
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "api.github.com" && r.URL.Host != "api.bitbucket.org" {
			t.Errorf("unexpected host %s", r.URL.Host)
		}
		handler, found := routes[r.Method+" "+r.URL.Path]
		if !found {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusTeapot)
			return
		}
		handler(w, r)
	})
}

func tokenHandler(token string, lifetime time.Duration, calls *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*calls++
		json.NewEncoder(w).Encode(map[string]any{"token": token, "expires_at": time.Now().Add(lifetime)})
	}
}

func TestConfigurationIsSealedAndValidated(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	var raw string
	s.db.QueryRow(`SELECT encrypted FROM git_provider_config`).Scan(&raw)
	if strings.Contains(raw, "PRIVATE KEY") || strings.Contains(raw, webhookSecret) {
		t.Fatal("plaintext secrets at rest")
	}
	jwt, err := s.jwt(ctx)
	if err != nil || len(strings.Split(jwt, ".")) != 3 {
		t.Fatal(jwt, err)
	}
	status := s.Status(ctx)
	if !status.Configured || status.AppID != 42 || status.InstallURL != "https://github.com/apps/pier-test/installations/new" {
		t.Fatal(status)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "PRIVATE") || strings.Contains(string(encoded), webhookSecret) {
		t.Fatal("status exposes secrets")
	}
	for _, bad := range []Config{
		{AppID: 42},
		{AppID: 42, PrivateKey: testKey, WebhookSecret: "short"},
		{AppID: 0, PrivateKey: testKey, WebhookSecret: webhookSecret},
		{AppID: 42, Slug: "Bad Slug", PrivateKey: testKey, WebhookSecret: webhookSecret},
	} {
		if err := s.Configure(ctx, bad); err == nil {
			t.Fatalf("accepted %+v", bad.Slug)
		}
	}
	if validRepo("x/../y") || validRepo("x/y?token=secret") || validRepo("x/y/z") || !validRepo("acme/app.web") {
		t.Fatal("repository validation")
	}
}

func TestManifestFlowStoresGeneratedCredentialsOnce(t *testing.T) {
	s := fixture(t)
	start, err := s.StartManifest("https://pier.example.com/", "acme")
	if err != nil {
		t.Fatal(err)
	}
	action, _ := url.Parse(start.ActionURL)
	state := action.Query().Get("state")
	if action.Host != "github.com" || action.Path != "/organizations/acme/settings/apps/new" || len(state) < 32 {
		t.Fatal(start.ActionURL)
	}
	var manifest struct {
		Hook struct {
			URL string `json:"url"`
		} `json:"hook_attributes"`
		Redirect    string            `json:"redirect_url"`
		Permissions map[string]string `json:"default_permissions"`
		Events      []string          `json:"default_events"`
	}
	if err := json.Unmarshal([]byte(start.Manifest), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Hook.URL != "https://pier.example.com/webhooks/git-provider/github" ||
		manifest.Redirect != "https://pier.example.com/settings/git-provider" ||
		manifest.Permissions["contents"] != "read" || manifest.Permissions["administration"] != "" ||
		strings.Join(manifest.Events, ",") != "push,pull_request" {
		t.Fatal(start.Manifest)
	}
	conversions := 0
	githubFake(t, s, map[string]http.HandlerFunc{
		"POST /app-manifests/abc_123/conversions": func(w http.ResponseWriter, r *http.Request) {
			conversions++
			if r.Header.Get("Authorization") != "" {
				t.Error("conversion must not carry credentials")
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id": 77, "slug": "pier-example", "pem": testKey, "webhook_secret": "generated-secret-0123456789",
			})
		},
	})
	status, err := s.CompleteManifest(t.Context(), "abc_123", state)
	if err != nil || status.AppID != 77 || status.Slug != "pier-example" {
		t.Fatal(status, err)
	}
	if _, err := s.CompleteManifest(t.Context(), "abc_123", state); err == nil || conversions != 1 {
		t.Fatal("manifest state reused")
	}
	if _, err := s.CompleteManifest(t.Context(), "abc_123", "forged"); err == nil {
		t.Fatal("unknown state accepted")
	}
	config, _ := s.config(t.Context())
	if config.WebhookSecret != "generated-secret-0123456789" {
		t.Fatal("generated secret not stored")
	}
	if _, err := s.StartManifest("javascript:alert(1)", ""); err == nil {
		t.Fatal("invalid public URL accepted")
	}
	if _, err := s.StartManifest("https://pier.example.com", "../evil"); err == nil {
		t.Fatal("invalid organization accepted")
	}
}

func TestManifestStateExpires(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	start, err := s.StartManifest("https://pier.example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	action, _ := url.Parse(start.ActionURL)
	now = now.Add(manifestLifetime + time.Second)
	mock(s, func(w http.ResponseWriter, r *http.Request) { t.Fatal("expired state reached GitHub") })
	if _, err := s.CompleteManifest(t.Context(), "code", action.Query().Get("state")); err == nil {
		t.Fatal("expired state accepted")
	}
}

func TestInstallationsAreMappedAndPaginated(t *testing.T) {
	s := fixture(t)
	pages := 0
	githubFake(t, s, map[string]http.HandlerFunc{
		"GET /app/installations": func(w http.ResponseWriter, r *http.Request) {
			pages++
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ey") {
				t.Error("installations need the App JWT")
			}
			rows := []map[string]any{}
			if pages == 1 {
				for i := range 100 {
					rows = append(rows, map[string]any{"id": i + 1, "account": map[string]string{"login": "acme", "type": "Organization"}})
				}
			}
			json.NewEncoder(w).Encode(rows)
		},
	})
	rows, err := s.Installations(t.Context())
	if err != nil || len(rows) != 100 || pages != 2 || rows[0].Account != "acme" {
		t.Fatal(len(rows), pages, err)
	}
}

func TestRepositoryPaginationAndRateLimitCooldown(t *testing.T) {
	s := fixture(t)
	mints, pages := 0, 0
	githubFake(t, s, map[string]http.HandlerFunc{
		"POST /app/installations/12/access_tokens": tokenHandler("list-token", time.Hour, &mints),
		"GET /installation/repositories": func(w http.ResponseWriter, r *http.Request) {
			pages++
			rows := []Repository{}
			if pages == 1 {
				for i := range 100 {
					rows = append(rows, Repository{ID: int64(i), FullName: "owner/repo", Private: true})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"repositories": rows})
		},
	})
	rows, err := s.Repositories(t.Context(), 12)
	if err != nil || len(rows) != 100 || pages != 2 || mints != 1 {
		t.Fatal(len(rows), pages, mints, err)
	}
	mock(s, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	s.forgetInstallation(12)
	if _, err := s.Repositories(t.Context(), 12); err == nil {
		t.Fatal("rate limit ignored")
	}
	mock(s, func(w http.ResponseWriter, r *http.Request) { t.Fatal("request during cooldown") })
	if _, err := s.Repositories(t.Context(), 12); err == nil {
		t.Fatal("cooldown ignored")
	}
}

func TestUpstreamBodiesAreNeverEchoed(t *testing.T) {
	s := fixture(t)
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "sensitive-upstream-body")
	})
	_, err := s.Installations(t.Context())
	if err == nil || strings.Contains(err.Error(), "sensitive-upstream-body") || !strings.Contains(err.Error(), "500") {
		t.Fatal(err)
	}
}
