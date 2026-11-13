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

	"pier/internal/application"
	"pier/internal/envvar"
	"pier/internal/identity"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/registry"
)

func TestRegistryAndPlacementRoutesNeverReturnSecretsAndCheckAbilities(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := secretbox.New("multiserver-http-testing-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	auth := identity.NewService(identity.NewStore(db), time.Hour)
	user, err := auth.CreateUser(ctx, "Test", "multiserver@example.invalid", "testing-password-long")
	if err != nil {
		t.Fatal(err)
	}
	_, writer, _ := auth.CreateToken(ctx, user.ID, "writer", []identity.Ability{identity.AbilityWrite})
	_, reader, _ := auth.CreateToken(ctx, user.ID, "reader", []identity.Ability{identity.AbilityRead})
	for _, statement := range []string{
		`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES ('srv','one','local','/data','2026-01-01T00:00:00Z')`,
		`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES ('srv2','two','local','/data','2026-01-01T00:00:00Z')`,
		`INSERT INTO projects(id,name,created_at) VALUES ('p','p','2026-01-01T00:00:00Z')`,
		`INSERT INTO environments(id,project_id,name,created_at) VALUES ('e','p','e','2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store := application.NewStore(db, box)
	apps := application.NewService(store, envvar.NewStore(db, box), engineHTTPConnector{executor.NewFake()}, nil)
	config := application.DefaultConfig(application.PackImage)
	config.Source.Image = "nginx"
	app, err := apps.Create(ctx, application.CreateInput{EnvironmentID: "e", ServerID: "srv", Name: "app", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	images := registry.New(db, box)
	a := New(Dependencies{Access: testAccess(t, db), Identity: auth, Applications: apps, Registries: images,
		MultiServer: application.NewMultiServer(db, store, nil, images), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	mux := http.NewServeMux()
	a.registerMultiServerRoutes(mux)
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}
	secret := `{"name":"GHCR","host":"ghcr.io","username":"bot","password":"ghp-http-secret-value"}`
	if response := call("POST", "/registries", secret, reader); response.Code != http.StatusForbidden {
		t.Fatalf("reader created a registry: %d", response.Code)
	}
	response := call("POST", "/registries", secret, writer)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "ghp-http-secret-value") {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var created registry.Registry
	_ = json.Unmarshal(response.Body.Bytes(), &created)
	if listed := call("GET", "/registries", "", reader); listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "ghp-http") {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
	placement := `{"registry_id":"` + created.ID + `","repository":"acme/app","destinations":["srv2"]}`
	if response := call("PUT", "/applications/"+app.ID+"/placement", placement, reader); response.Code != http.StatusForbidden {
		t.Fatalf("reader changed the placement: %d", response.Code)
	}
	saved := call("PUT", "/applications/"+app.ID+"/placement", placement, writer)
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `"destinations":["srv","srv2"]`) {
		t.Fatalf("save: %d %s", saved.Code, saved.Body.String())
	}
	servers := call("GET", "/applications/"+app.ID+"/placement/servers", "", reader)
	if servers.Code != http.StatusOK || !strings.Contains(servers.Body.String(), `"role":"primary"`) {
		t.Fatalf("servers: %d %s", servers.Code, servers.Body.String())
	}
	if response := call("DELETE", "/registries/"+created.ID, "", writer); response.Code != http.StatusConflict {
		t.Fatalf("deleted a registry in use: %d", response.Code)
	}
}
