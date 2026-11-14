package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pier/internal/identity"
	"pier/internal/logdrain"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
	"pier/internal/provision"
	"pier/internal/server"
)

type operationsFakeConnector struct{ ex *executor.Fake }

func (c operationsFakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return c.ex, server.Server{ID: "ops", Name: "QA", Kind: server.KindSSH}, nil
}
func (c operationsFakeConnector) Forget(string) {}

type extrasClient struct {
	t       *testing.T
	handler http.Handler
}

func (c extrasClient) do(method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	return w
}

// extrasAPI wires the server-extras services over a temporary database and
// returns an owner's read and write tokens.
func extrasAPI(t *testing.T) (extrasClient, *API, string, string) {
	t.Helper()
	a := platformAPI(t)
	ctx := context.Background()
	db := a.Audit.DB
	for _, query := range []string{
		`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('ops','QA','ssh','/qa','` + sqlite.FormatTime(time.Now()) + `')`,
		`INSERT INTO projects(id,name,created_at) VALUES('p','Loja','2026-01-01')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	ex := executor.NewFake()
	connector := operationsFakeConnector{ex}
	box := testbox.Box(t)
	a.ServerOperations = server.NewOperations(db, connector)
	a.DockerInstaller = server.NewDockerInstaller(connector, nil)
	a.Servers = server.NewService(server.NewStore(db), nil, nil)
	a.Provisioning = provision.NewService(db, box, nil, nil, nil)
	a.LogDrains = logdrain.NewService(db, box, connector, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	user, err := a.Identity.CreateUser(ctx, "Ops", "ops@example.com", "long-testing-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Access.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	_, read, err := a.Identity.CreateToken(ctx, user.ID, "read", []identity.Ability{identity.AbilityRead})
	if err != nil {
		t.Fatal(err)
	}
	_, write, err := a.Identity.CreateToken(ctx, user.ID, "write", []identity.Ability{identity.AbilityRead, identity.AbilityWrite})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.registerServerOperationsRoutes(mux)
	a.registerProvisionRoutes(mux)
	a.registerLogDrainRoutes(mux)
	return extrasClient{t: t, handler: a.auditRequest(mux)}, a, read, write
}

type routeCase struct {
	method, path, body, token string
	status                    int
}

func checkRoutes(t *testing.T, client extrasClient, cases []routeCase) {
	t.Helper()
	for _, tc := range cases {
		w := client.do(tc.method, tc.path, tc.body, tc.token)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
}

func TestServerOperationsAbilitiesAndConfirmation(t *testing.T) {
	client, a, read, write := extrasAPI(t)
	base := "/api/v1/servers/ops"
	policy := `{"maintenance":true,"cleanup_cron":"0 4 * * 0","min_age_hours":168}`
	checkRoutes(t, client, []routeCase{
		{"GET", base + "/operations-policy", "", "", 401},
		{"GET", base + "/operations-policy", "", read, 200},
		{"PUT", base + "/operations-policy", policy, read, 403},
		{"PUT", base + "/operations-policy", policy, write, 200},
		{"PUT", base + "/operations-policy", `{"min_age_hours":1}`, write, 422},
		{"POST", base + "/safe-cleanup", `{}`, read, 403},
		{"POST", base + "/safe-cleanup", `{}`, write, 200},
		{"POST", base + "/safe-cleanup", `{"dry_run":false}`, write, 422},
		{"POST", base + "/safe-cleanup", `{"dry_run":false,"confirmation":"QA"}`, write, 200},
		{"GET", base + "/safe-cleanup", "", read, 200},
		{"GET", "/api/v1/servers/missing/operations-policy", "", write, 404},
		{"GET", base + "/docker-install", "", read, 200},
		{"POST", base + "/docker-install", `{"method":"convenience","confirmation":"QA"}`, read, 403},
		{"POST", base + "/docker-install", `{"method":"convenience","confirmation":"qa"}`, write, 422},
		{"POST", base + "/docker-install", `{"method":"script","confirmation":"QA"}`, write, 422},
	})
	if err := server.CheckDeployment(context.Background(), a.Audit.DB, "ops"); err == nil {
		t.Fatal("policy not persisted")
	}
}

func TestCloudRoutesKeepCredentialsAndDestructionToSessions(t *testing.T) {
	client, _, read, write := extrasAPI(t)
	checkRoutes(t, client, []routeCase{
		{"GET", "/api/v1/cloud/credentials", "", read, 200},
		{"GET", "/api/v1/cloud/machines", "", read, 200},
		{"POST", "/api/v1/cloud/credentials", `{"provider":"hetzner","name":"x","token":"0123456789abcdefghij"}`, write, 403},
		{"DELETE", "/api/v1/cloud/credentials/x", "", write, 403},
		{"DELETE", "/api/v1/cloud/machines/x?confirmation=x", "", write, 403},
		{"POST", "/api/v1/cloud/machines", `{"credential_id":"x","name":"web-1","region":"fsn1","size":"cx22","image":"ubuntu-24.04"}`, read, 403},
		{"POST", "/api/v1/cloud/machines", `{"credential_id":"missing","name":"web-1","region":"fsn1","size":"cx22","image":"ubuntu-24.04"}`, write, 404},
		{"GET", "/api/v1/cloud/machines/missing", "", read, 404},
	})
}

func TestLogDrainRoutesNeverReturnTheToken(t *testing.T) {
	client, _, read, write := extrasAPI(t)
	const secret = "sink-token-0123456789"
	body := `{"name":"central","kind":"http","url":"https://logs.example.com/in","token":"` + secret + `","labels":{"env":"prod"},"scope_type":"project","scope_id":"p","enabled":false}`
	checkRoutes(t, client, []routeCase{
		{"POST", "/api/v1/log-drains", body, read, 403},
		{"POST", "/api/v1/log-drains", strings.Replace(body, "https:", "http:", 1), write, 422},
	})
	created := client.do("POST", "/api/v1/log-drains", body, write)
	if created.Code != 201 || strings.Contains(created.Body.String(), secret) || !strings.Contains(created.Body.String(), `"has_token":true`) {
		t.Fatalf("create %d %s", created.Code, created.Body)
	}
	listed := client.do("GET", "/api/v1/log-drains", "", read)
	if listed.Code != 200 || strings.Contains(listed.Body.String(), secret) || !strings.Contains(listed.Body.String(), `"dropped":0`) {
		t.Fatalf("list %d %s", listed.Code, listed.Body)
	}
	id := strings.Split(strings.Split(created.Body.String(), `"id":"`)[1], `"`)[0]
	checkRoutes(t, client, []routeCase{
		{"DELETE", "/api/v1/log-drains/" + id + "?confirmation=central", "", read, 403},
		{"DELETE", "/api/v1/log-drains/" + id + "?confirmation=other", "", write, 422},
		{"POST", "/api/v1/log-drains/" + id + "/test", "", read, 403},
		{"DELETE", "/api/v1/log-drains/" + id + "?confirmation=central", "", write, 204},
	})
}
