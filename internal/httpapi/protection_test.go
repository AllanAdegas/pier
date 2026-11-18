package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"pier/internal/application"
	"pier/internal/identity"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
	"pier/internal/project"
)

// protectionFixture is an API with one application in environment "e" whose
// protection flags the test controls through the returned project service.
func protectionFixture(t *testing.T) (*API, *http.ServeMux, *project.Service, string) {
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
		`INSERT INTO environments(id,project_id,name,created_at) VALUES ('e','p','production','2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	projects := project.NewService(db)
	apps := application.NewService(application.NewStore(db, box), nil, engineHTTPConnector{executor.NewFake()}, nil)
	apps.SetDeployGuard(projects)
	config := application.DefaultConfig(application.PackImage)
	config.Source.Image = "nginx"
	app, err := apps.Create(ctx, application.CreateInput{EnvironmentID: "e", ServerID: "srv", Name: "app", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	a := New(Dependencies{Projects: projects, Applications: apps})
	mux := http.NewServeMux()
	mux.Handle("DELETE /api/v1/applications/{id}", a.adapt(a.deleteApplication))
	return a, mux, projects, app.ID
}

func TestAProtectedEnvironmentRequiresItsNameToRemoveAResource(t *testing.T) {
	a, mux, projects, appID := protectionFixture(t)
	if _, err := projects.SetProtection(context.Background(), "e", true, false); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+appID+"?force=1", nil)
	request = request.WithContext(context.WithValue(request.Context(), contextKey{}, identity.Principal{Abilities: identity.AllAbilities}))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("removal without confirmation: status %d, want 409", response.Code)
	}

	confirmed := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+appID+"?confirmation=production", nil)
	if err := a.checkDestroy(confirmed, "e"); err != nil {
		t.Fatalf("removal with the environment name was refused: %v", err)
	}
}

func TestAFrozenEnvironmentRefusesDeployments(t *testing.T) {
	a, _, projects, appID := protectionFixture(t)
	if _, err := projects.SetProtection(context.Background(), "e", false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Applications.Deploy(context.Background(), appID, application.TriggerManual); err == nil {
		t.Fatal("a frozen environment accepted a deployment")
	}
}
