package httpapi

import (
	"context"
	"encoding/json"
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
	"pier/internal/identity"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
	"pier/internal/project"
)

type engineWorld struct {
	handler            http.Handler
	owner, admin, view string
	appID              string
}

// newEngineWorld: project p (env e) with an application, project q (env f).
// The owner is the first account; admin and view are members of p only.
func newEngineWorld(t *testing.T) engineWorld {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box := testbox.Box(t)
	env := envvar.NewStore(db, box)
	auth := identity.NewService(identity.NewStore(db), time.Hour)
	token := func(name string) (string, string) {
		user, err := auth.CreateUser(ctx, name, name+"@example.invalid", "testing-password-long")
		if err != nil {
			t.Fatal(err)
		}
		_, value, err := auth.CreateToken(ctx, user.ID, name, []identity.Ability{identity.AbilityWrite})
		if err != nil {
			t.Fatal(err)
		}
		return user.ID, value
	}
	_, ownerToken := token("owner")
	adminID, adminToken := token("admin")
	viewID, viewToken := token("viewer")
	for _, statement := range []string{
		`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES ('srv','test','local','/data','2026-01-01T00:00:00Z')`,
		`INSERT INTO projects(id,name,created_at) VALUES ('p','p','2026-01-01T00:00:00Z'), ('q','q','2026-01-01T00:00:00Z')`,
		`INSERT INTO environments(id,project_id,name,created_at) VALUES ('e','p','e','2026-01-01T00:00:00Z'), ('f','q','f','2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	apps := application.NewService(application.NewStore(db, box), env, engineHTTPConnector{executor.NewFake()}, nil)
	config := application.DefaultConfig(application.PackImage)
	config.Source.Image = "nginx"
	app, err := apps.Create(ctx, application.CreateInput{EnvironmentID: "e", ServerID: "srv", Name: "app", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	access := testAccess(t, db)
	for user, role := range map[string]rbac.Role{adminID: rbac.Admin, viewID: rbac.Viewer} {
		if _, err := db.Exec(`INSERT INTO access_memberships(project_id,user_id,role) VALUES('p',?,?)`, user, role); err != nil {
			t.Fatal(err)
		}
	}
	a := New(Dependencies{Access: access, Identity: auth, Env: env, Applications: apps, Projects: project.NewService(db),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	mux := http.NewServeMux()
	a.registerEngineRoutes(mux)
	return engineWorld{handler: mux, owner: ownerToken, admin: adminToken, view: viewToken, appID: app.ID}
}

func (w engineWorld) call(t *testing.T, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	w.handler.ServeHTTP(recorder, r)
	return recorder
}

func TestEngineDangerousDockerOptionsOnlyOwnerCanEnable(t *testing.T) {
	world := newEngineWorld(t)
	body := `{"allow_dangerous":true,"allow_devices":true}`
	for _, token := range []string{world.admin, world.view} {
		if got := world.call(t, token, "PATCH", "/api/v1/settings/docker-options", body).Code; got != http.StatusForbidden {
			t.Errorf("non-owner enabled dangerous options: %d", got)
		}
	}
	response := world.call(t, world.owner, "PATCH", "/api/v1/settings/docker-options", body)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"allow_dangerous":true`) {
		t.Fatalf("owner: %d %s", response.Code, response.Body.String())
	}
}

func TestEngineSharedVariableAPIMasksSecrets(t *testing.T) {
	world := newEngineWorld(t)
	created := world.call(t, world.admin, "POST", "/api/v1/projects/p/shared-variables", `{"key":"TOKEN","value":"shared-api-secret","secret":true}`)
	if created.Code != http.StatusOK || strings.Contains(created.Body.String(), "shared-api-secret") {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	world.call(t, world.admin, "POST", "/api/v1/environments/e/shared-variables", `{"key":"REGION","value":"sa-east-1"}`)
	for _, token := range []string{world.owner, world.admin, world.view} {
		listed := world.call(t, token, "GET", "/api/v1/projects/p/shared-variables", "")
		var variables []envvar.SharedVariable
		if err := json.Unmarshal(listed.Body.Bytes(), &variables); err != nil || len(variables) != 1 || variables[0].Value != "" ||
			strings.Contains(listed.Body.String(), "shared-api-secret") {
			t.Fatalf("list leaked or failed: %d %s", listed.Code, listed.Body.String())
		}
	}
	if listed := world.call(t, world.view, "GET", "/api/v1/environments/e/shared-variables", ""); !strings.Contains(listed.Body.String(), "sa-east-1") {
		t.Errorf("non-secret values are visible to readers: %s", listed.Body.String())
	}
	if got := world.call(t, world.view, "POST", "/api/v1/projects/p/shared-variables", `{"key":"X","value":"y"}`).Code; got != http.StatusForbidden {
		t.Errorf("viewer wrote a shared variable: %d", got)
	}
	if got := world.call(t, world.admin, "GET", "/api/v1/projects/q/shared-variables", "").Code; got != http.StatusForbidden {
		t.Errorf("member of p read q's shared variables: %d", got)
	}
}

func TestEngineCloneNeedsWriteOnTheTargetProject(t *testing.T) {
	world := newEngineWorld(t)
	path := "/api/v1/applications/" + world.appID + "/clone"
	if got := world.call(t, world.admin, "POST", path, `{"target_environment_id":"f"}`).Code; got != http.StatusForbidden {
		t.Errorf("admin of p cloned into q: %d", got)
	}
	if got := world.call(t, world.view, "POST", path, `{"target_environment_id":"e"}`).Code; got != http.StatusForbidden {
		t.Errorf("viewer cloned: %d", got)
	}
	if got := world.call(t, world.admin, "POST", path, `{"target_environment_id":"e"}`).Code; got != http.StatusCreated {
		t.Errorf("admin clone inside p: %d", got)
	}
	if got := world.call(t, world.owner, "POST", path, `{"target_environment_id":"f"}`).Code; got != http.StatusCreated {
		t.Errorf("owner clone into q: %d", got)
	}
}
