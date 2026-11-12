package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/application"
	"pier/internal/envvar"
	"pier/internal/identity"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

type engineHTTPConnector struct{ ex *executor.Fake }

func (c engineHTTPConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return c.ex, server.Server{ID: "srv", DataDir: "/data", ProxyToken: "dummy-route-token"}, nil
}

func engineHTTPFixture(t *testing.T) (*API, *executor.Fake, *http.ServeMux, string, string) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := secretbox.New("engine-http-testing-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	env := envvar.NewStore(db, box)
	auth := identity.NewService(identity.NewStore(db), time.Hour)
	user, err := auth.CreateUser(context.Background(), "Test", "engine@example.invalid", "testing-password-long")
	if err != nil {
		t.Fatal(err)
	}
	_, writeToken, err := auth.CreateToken(context.Background(), user.ID, "writer", []identity.Ability{identity.AbilityWrite})
	if err != nil {
		t.Fatal(err)
	}
	ex := executor.NewFake()
	apps := application.NewService(application.NewStore(db, box), env, engineHTTPConnector{ex}, nil)
	for _, statement := range []string{
		`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES ('srv','test','local','/data','2026-01-01T00:00:00Z')`,
		`INSERT INTO projects(id,name,created_at) VALUES ('p','p','2026-01-01T00:00:00Z')`,
		`INSERT INTO environments(id,project_id,name,created_at) VALUES ('e','p','e','2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	config := application.DefaultConfig(application.PackImage)
	config.Source.Image = "nginx"
	app, err := apps.Create(context.Background(), application.CreateInput{EnvironmentID: "e", ServerID: "srv", Name: "app", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	a := New(Dependencies{Access: testAccess(t,db), Identity: auth, Env: env, Applications: apps, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	mux := http.NewServeMux()
	a.registerApplicationRoutes(mux)
	return a, ex, mux, writeToken, app.ID
}

func TestEngineEnvPostAndDotenvKeepSecretBuildFlag(t *testing.T) {
	a, _, mux, token, id := engineHTTPFixture(t)
	request := httptest.NewRequest("POST", "/api/v1/applications/"+id+"/envs", strings.NewReader(`{"key":"NPM_TOKEN","value":"dummy-build-token","build_time":true,"secret_build":true}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	owner := envvar.Owner{Type: envvar.OwnerApplication, ID: id}
	stored, err := a.Env.List(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || !stored[0].SecretBuild || !stored[0].BuildTime {
		t.Fatal(stored)
	}
	variables, err := a.parseDotenv(httptest.NewRequest("PUT", "/", nil), owner, "NPM_TOKEN=rotated-token\nNEW_VAR=hello")
	if err != nil {
		t.Fatal(err)
	}
	for _, variable := range variables {
		if variable.Key == "NPM_TOKEN" && (!variable.SecretBuild || !variable.BuildTime) {
			t.Fatal(variable)
		}
		if variable.Key == "NEW_VAR" && (variable.SecretBuild || variable.BuildTime) {
			t.Fatal(variable)
		}
	}
}

func TestEngineGitHostKeyResetRouteIsRegisteredAndRequiresWrite(t *testing.T) {
	a, ex, mux, token, id := engineHTTPFixture(t)
	path := "/api/v1/applications/" + id + "/git-host-key/reset"
	request := httptest.NewRequest("POST", path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !ex.Ran("/git/known_hosts") {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	principal, err := a.Identity.AuthenticateToken(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	_, readToken, err := a.Identity.CreateToken(context.Background(), principal.User.ID, "reader", []identity.Ability{identity.AbilityRead})
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest("POST", path, nil)
	request.Header.Set("Authorization", "Bearer "+readToken)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("read reset: %d", response.Code)
	}
}

func TestEngineRawTaskHistoryAndImageInspectRequireWriteBeforeAccess(t *testing.T) {
	a := &API{}
	for _, ability := range []identity.Ability{identity.AbilityRead, identity.AbilityDeploy} {
		request := requestAs(ability)
		request.URL.RawQuery = "raw=1"
		err := a.taskExecutions(httptest.NewRecorder(), request)
		appErr, ok := apperr.As(err)
		if !ok || appErr.Kind != apperr.KindForbidden {
			t.Fatalf("%s raw output: %v", ability, err)
		}
		err = a.dockerInspect(httptest.NewRecorder(), request)
		appErr, ok = apperr.As(err)
		if !ok || appErr.Kind != apperr.KindForbidden {
			t.Fatalf("%s inspect token access: %v", ability, err)
		}
	}
}

func TestEngineMalformedRepositoryURLsAreMaskedForReadAndDeployCallers(t *testing.T) {
	for _, ability := range []identity.Ability{identity.AbilityRead, identity.AbilityDeploy} {
		for _, password := range []string{"dummy^token", "dummy%zztoken", "dummy#token"} {
			app := application.Application{Config: application.Config{Source: application.Source{Repository: "https://dummy-user:" + password + "@git.invalid/app"}}}
			visible := forCaller(requestAs(ability), app)
			if visible.Config.Source.Repository != "https://***@git.invalid/app" {
				t.Fatal(visible.Config.Source.Repository)
			}
		}
	}
}
