package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pier/internal/identity"
	"pier/internal/platform/executor"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

type operationsFakeConnector struct{ ex *executor.Fake }

func (c operationsFakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return c.ex, server.Server{ID: "ops", Name: "QA"}, nil
}
func TestServerOperationsAbilitiesAndConfirmation(t *testing.T) {
	a := platformAPI(t)
	ctx := context.Background()
	db := a.Audit.DB
	if _, err := db.Exec(`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('ops','QA','local','/qa',?)`, sqlite.FormatTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	ex := executor.NewFake()
	a.ServerOperations = server.NewOperations(db, operationsFakeConnector{ex})
	a.Servers = server.NewService(server.NewStore(db), nil, nil)
	user, err := a.Identity.CreateUser(ctx, "Ops", "ops@example.com", "long-testing-password")
	if err != nil {
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
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.auditRequest(mux).ServeHTTP(w, r)
		return w
	}
	base := "/api/v1/servers/ops"
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"GET", base + "/operations-policy", "", "", 401},
		{"GET", base + "/operations-policy", "", read, 200},
		{"PUT", base + "/operations-policy", `{"maintenance":true,"cleanup_cron":"0 4 * * 0","min_age_hours":168}`, read, 403},
		{"PUT", base + "/operations-policy", `{"maintenance":true,"cleanup_cron":"0 4 * * 0","min_age_hours":168}`, write, 200},
		{"PUT", base + "/operations-policy", `{"min_age_hours":1}`, write, 422},
		{"POST", base + "/safe-cleanup", `{}`, read, 403},
		{"POST", base + "/safe-cleanup", `{}`, write, 200},
		{"POST", base + "/safe-cleanup", `{"dry_run":false}`, write, 422},
		{"POST", base + "/safe-cleanup", `{"dry_run":false,"confirmation":"QA"}`, write, 200},
		{"GET", "/api/v1/servers/missing/operations-policy", "", write, 404},
	} {
		w := request(tc.method, tc.path, tc.body, tc.token)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	if err := server.CheckDeployment(ctx, db, "ops"); err == nil {
		t.Fatal("policy not persisted")
	}
	if ex.Ran(" rm ") || ex.Ran("prune") {
		t.Fatal("empty inventory mutated")
	}
}
