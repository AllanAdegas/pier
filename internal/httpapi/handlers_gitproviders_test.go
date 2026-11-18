package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rbac "pier/internal/access"
	"pier/internal/application"
	"pier/internal/envvar"
	"pier/internal/gitprovider"
	"pier/internal/identity"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
	"pier/internal/settings"
)

const (
	gitAppWebhookSecret = "a-webhook-secret-of-thirty-two-chars"
	gitAppFeedbackToken = "glpat-feedback-secret-token"
)

type gitBindingTransport func(*http.Request) (*http.Response, error)

func (f gitBindingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type gitProviderFixture struct {
	remoteCalls    *int
	api            *API
	db             *sql.DB
	mux            *http.ServeMux
	app            string
	reader, writer string
}

func newGitProviderFixture(t *testing.T) gitProviderFixture {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box := testbox.Box(t)
	for _, statement := range []string{
		`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES ('srv','test','local','/data','2026-01-01T00:00:00Z')`,
		`INSERT INTO projects(id,name,created_at) VALUES ('p','p','2026-01-01T00:00:00Z')`,
		`INSERT INTO environments(id,project_id,name,created_at) VALUES ('e','p','e','2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	env := envvar.NewStore(db, box)
	appStore := application.NewStore(db, box)
	dispatcher := application.NewDispatcher(appStore, nil, slog.Default(), time.Minute)
	apps := application.NewService(appStore, env, engineHTTPConnector{executor.NewFake()}, dispatcher)
	config := application.DefaultConfig(application.PackDockerfile)
	config.Source.Repository = "https://github.com/acme/app.git"
	app, err := apps.Create(ctx, application.CreateInput{EnvironmentID: "e", ServerID: "srv", Name: "app", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	remoteCalls := new(int)
	providers := gitprovider.NewWithHTTPClient(db, box, &http.Client{Transport: gitBindingTransport(func(r *http.Request) (*http.Response, error) {
		*remoteCalls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"path_with_namespace":"acme/foreign","http_url_to_repo":"https://forge.example/acme/foreign.git"}`))}, nil
	})})
	sealed, err := box.Seal("valid-instance-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO git_connections(id,provider,name,base_url,encrypted_token,created_at,updated_at) VALUES('valid-instance-connection','gitlab','test','https://forge.example',?,'now','now')`, sealed); err != nil {
		t.Fatal(err)
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err := providers.Configure(ctx, gitprovider.Config{AppID: 9, PrivateKey: pemKey, WebhookSecret: gitAppWebhookSecret}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO git_provider_selections VALUES('application',?,55,'acme/app')`, app.ID); err != nil {
		t.Fatal(err)
	}
	auth := identity.NewService(identity.NewStore(db), time.Hour)
	a := New(Dependencies{
		Access: testAccess(t, db), Identity: auth, Env: env, Applications: apps, GitProviders: providers, GitConnections: gitprovider.NewConnections(providers),
		GitFeedback: gitprovider.NewQueue(providers, nil, gitprovider.DefaultRetryPolicy(), slog.Default()),
		Settings:    settings.NewStore(db), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	user, err := auth.CreateUser(ctx, "Owner", "gitapp@example.invalid", "testing-password-long")
	if err != nil {
		t.Fatal(err)
	}
	grantRole(t, a, user.ID, rbac.Owner)
	_, reader, _ := auth.CreateToken(ctx, user.ID, "reader", []identity.Ability{identity.AbilityRead})
	_, writer, _ := auth.CreateToken(ctx, user.ID, "writer", []identity.Ability{identity.AbilityWrite})
	mux := http.NewServeMux()
	a.registerGitProviderRoutes(mux)
	a.registerGitFeedbackRoutes(mux)
	a.registerGitConnectionRoutes(mux)
	return gitProviderFixture{remoteCalls: remoteCalls, db: db, api: a, mux: mux, app: app.ID, reader: reader, writer: writer}
}

func (f gitProviderFixture) call(method, path, token, body string, header http.Header) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for key, values := range header {
		request.Header[key] = values
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	f.mux.ServeHTTP(response, request)
	return response
}

func TestGitProviderRoutesAbilitiesAndNoSecretsInResponses(t *testing.T) {
	f := newGitProviderFixture(t)
	feedback := "/api/v1/applications/" + f.app + "/git-provider/feedback"
	for _, tc := range []struct {
		method, path, token, body string
		want                      int
	}{
		{"GET", "/api/v1/git-connections", f.reader, "", 200},
		{"POST", "/api/v1/git-connections", f.reader, `{}`, 403},
		{"PUT", "/api/v1/git-connections/missing", f.reader, `{}`, 403},
		{"DELETE", "/api/v1/git-connections/missing", f.reader, "", 403},
		{"GET", "/api/v1/git-connections/missing/repositories", f.reader, "", 403},
		{"GET", "/api/v1/git-connections", "", "", 401},
		{"PUT", "/api/v1/applications/" + f.app + "/git-connection", f.reader, `{}`, 403},
		{"GET", "/api/v1/applications/" + f.app + "/git-connection", f.reader, "", 200},
		{"GET", "/api/v1/git-provider", f.reader, "", 200},
		{"PUT", "/api/v1/git-provider", f.reader, `{}`, 403},
		{"POST", "/api/v1/git-provider/manifest", f.reader, `{}`, 403},
		{"GET", "/api/v1/applications/" + f.app + "/git-provider", f.reader, "", 200},
		{"DELETE", "/api/v1/applications/" + f.app + "/git-provider", f.reader, "", 403},
		{"GET", feedback, f.reader, "", 200},
		{"PUT", feedback, f.reader, `{"provider":"none"}`, 403},
		{"PUT", feedback, f.writer, `{"provider":"bitbucket","token":"x"}`, 422},
		{"PUT", feedback, f.writer, `{"provider":"gitlab","commit_status":true,"token":"` + gitAppFeedbackToken + `"}`, 200},
		{"PUT", feedback, f.writer, `{"provider":"github","commit_status":true,"pull_request_comments":true}`, 200},
		{"GET", "/api/v1/git-provider", "", "", 401},
	} {
		response := f.call(tc.method, tc.path, tc.token, tc.body, nil)
		if response.Code != tc.want {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, response.Code, response.Body.String())
		}
		for _, secret := range []string{gitAppWebhookSecret, "PRIVATE KEY", gitAppFeedbackToken} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("%s %s leaked a secret", tc.method, tc.path)
			}
		}
	}
	status := f.call("GET", "/api/v1/git-provider", f.reader, "", nil).Body.String()
	if !strings.Contains(status, `"configured":true`) || !strings.Contains(status, "/webhooks/git-provider/github") {
		t.Fatal(status)
	}
	link := f.call("GET", "/api/v1/applications/"+f.app+"/git-provider", f.reader, "", nil).Body.String()
	if !strings.Contains(link, `"selected":true`) || !strings.Contains(link, "acme/app") {
		t.Fatal(link)
	}
}

func TestGitProviderWebhookVerifiesTheAppSignatureAndDispatches(t *testing.T) {
	f := newGitProviderFixture(t)
	body := `{"ref":"refs/heads/main","installation":{"id":55},"repository":{"full_name":"acme/app"}}`
	header := http.Header{}
	header.Set("X-GitHub-Event", "push")
	header.Set("X-GitHub-Delivery", "d-1")
	header.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64))
	if response := f.call("POST", "/webhooks/git-provider/github", "", body, header); response.Code != http.StatusUnauthorized {
		t.Fatal("bad signature:", response.Code, response.Body.String())
	}
	mac := hmac.New(sha256.New, []byte(gitAppWebhookSecret))
	mac.Write([]byte(body))
	header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response := f.call("POST", "/webhooks/git-provider/github", "", body, header)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"resource_id":"`+f.app+`"`) ||
		!strings.Contains(response.Body.String(), "Deploy automático desativado") {
		t.Fatal(response.Code, response.Body.String())
	}
	again := f.call("POST", "/webhooks/git-provider/github", "", body, header)
	if !strings.Contains(again.Body.String(), `"duplicate":true`) || strings.Contains(again.Body.String(), f.app) {
		t.Fatal("redelivery dispatched again", again.Body.String())
	}
}

func TestGitFeedbackRoutesRequireAbilitiesAndScope(t *testing.T) {
	f := newGitProviderFixture(t)
	base := "/api/v1/applications/" + f.app + "/git-provider/reports"
	for _, tc := range []struct {
		method, path, token string
		want                int
	}{
		{"GET", base, "", 401}, {"GET", base, f.reader, 200},
		{"POST", base + "/1/resend", f.reader, 403},
		{"POST", base + "/1/resend", f.writer, 404},
		{"GET", "/api/v1/applications/missing/git-provider/reports", f.reader, 404},
	} {
		if got := f.call(tc.method, tc.path, tc.token, "", nil); got.Code != tc.want {
			t.Errorf("%s %s: %d: %s", tc.method, tc.path, got.Code, got.Body.String())
		}
	}
}

func TestGitProviderBindingRequiresInstanceWrite(t *testing.T) {
	f := newGitProviderFixture(t)
	user, token := testIdentity(t, f.api, "project-writer")
	if _, err := f.db.Exec(`INSERT INTO access_memberships(project_id,user_id,role) VALUES(?,?,?)`, "p", user.ID, rbac.Admin); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/applications/" + f.app + "/git-provider",
		"/api/v1/applications/" + f.app + "/git-connection",
	} {
		response := f.call("PUT", path, token, `{"installation_id":55,"repository":"acme/foreign","connection_id":"valid-instance-connection"}`, nil)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
	}
	if *f.remoteCalls != 0 {
		t.Fatalf("denied binding made %d remote calls", *f.remoteCalls)
	}
}

func TestGitProviderApprovedBindingAllowsProjectDeployer(t *testing.T) {
	f := newGitProviderFixture(t)
	user, token := testIdentity(t, f.api, "project-deployer", identity.AbilityDeploy)
	if _, err := f.db.Exec(`INSERT INTO access_memberships(project_id,user_id,role) VALUES('p',?,'developer')`, user.ID); err != nil {
		t.Fatal(err)
	}
	f.api.route(f.mux, "POST /api/v1/applications/{id}/deploy", canDeploy, f.api.deployApplication)
	response := f.call("POST", "/api/v1/applications/"+f.app+"/deploy", token, "{}", nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("approved deployment: %d %s", response.Code, response.Body.String())
	}
	if *f.remoteCalls != 0 {
		t.Fatal("queuing deployment contacted provider")
	}
}
