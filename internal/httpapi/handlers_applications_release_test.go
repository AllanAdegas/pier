package httpapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"pier/internal/platform/pubsub"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

type releaseHTTPFixture struct {
	mux               *http.ServeMux
	deployToken, read string
	apps              *application.Service
	ex                *executor.Fake
	appID             string
}

func newReleaseHTTPFixture(t *testing.T) releaseHTTPFixture {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := secretbox.New("engine-http-testing-passphrase")
	if err != nil {
		t.Fatal(err)
	}
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
	ex := executor.NewFake()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := application.NewStore(db, box)
	deployer := application.NewDeployer(store, env, engineHTTPConnector{ex}, nil, nil, pubsub.NewHub[application.LogEntry](), logger)
	deployer.SetArchiveDir(t.TempDir())
	apps := application.NewService(store, env, engineHTTPConnector{ex}, application.NewDispatcher(store, deployer, logger, time.Hour))
	config := application.DefaultConfig(application.PackDockerfile)
	config.Source.Repository = "https://example.invalid/acme/app"
	app, err := apps.Create(ctx, application.CreateInput{EnvironmentID: "e", ServerID: "srv", Name: "app", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	auth := identity.NewService(identity.NewStore(db), time.Hour)
	user, err := auth.CreateUser(ctx, "Test", "release@example.invalid", "testing-password-long")
	if err != nil {
		t.Fatal(err)
	}
	_, deployToken, err := auth.CreateToken(ctx, user.ID, "deployer", []identity.Ability{identity.AbilityDeploy})
	if err != nil {
		t.Fatal(err)
	}
	_, readToken, err := auth.CreateToken(ctx, user.ID, "reader", []identity.Ability{identity.AbilityRead})
	if err != nil {
		t.Fatal(err)
	}
	a := New(Dependencies{Access: testAccess(t, db), Identity: auth, Env: env, Applications: apps, Logger: logger})
	mux := http.NewServeMux()
	a.registerApplicationRoutes(mux)
	return releaseHTTPFixture{mux: mux, deployToken: deployToken, read: readToken, apps: apps, ex: ex, appID: app.ID}
}

func (f releaseHTTPFixture) post(path, token, contentType string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest("POST", path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	f.mux.ServeHTTP(response, request)
	return response
}

func sourceTarGz(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipped := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipped)
	_ = writer.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: 0o644, Size: 12, Typeflag: tar.TypeReg})
	_, _ = writer.Write([]byte("FROM scratch"))
	_ = writer.Close()
	_ = zipped.Close()
	return buffer.Bytes()
}

func TestDeployArchiveReturnsTheQueuedDeployment(t *testing.T) {
	f := newReleaseHTTPFixture(t)
	path := "/api/v1/applications/" + f.appID + "/deploy-archive"
	if response := f.post(path, f.read, "application/gzip", sourceTarGz(t)); response.Code != http.StatusForbidden {
		t.Errorf("read token: %d", response.Code)
	}
	if response := f.post(path, f.deployToken, "application/json", sourceTarGz(t)); response.Code != http.StatusUnprocessableEntity {
		t.Errorf("wrong content type: %d", response.Code)
	}
	if response := f.post(path, f.deployToken, "application/gzip", []byte("not a tarball")); response.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid archive: %d %s", response.Code, response.Body.String())
	}
	response := f.post(path, f.deployToken, "application/gzip", sourceTarGz(t))
	if response.Code != http.StatusCreated {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var deployment application.Deployment
	if err := json.Unmarshal(response.Body.Bytes(), &deployment); err != nil {
		t.Fatal(err)
	}
	if deployment.Trigger != application.TriggerArchive || deployment.Status != application.DeploymentQueued || deployment.Source == "" {
		t.Errorf("deployment = %+v", deployment)
	}
}

func TestDeployAcceptsAnOptionalTag(t *testing.T) {
	f := newReleaseHTTPFixture(t)
	path := "/api/v1/applications/" + f.appID + "/deploy"
	plain := f.post(path, f.deployToken, "", nil)
	tagged := f.post(path, f.deployToken, "application/json", []byte(`{"ref":"v1.2.3"}`))
	if plain.Code != http.StatusAccepted || tagged.Code != http.StatusAccepted {
		t.Fatalf("%d %s / %d %s", plain.Code, plain.Body.String(), tagged.Code, tagged.Body.String())
	}
	var first, second application.Deployment
	_ = json.Unmarshal(plain.Body.Bytes(), &first)
	_ = json.Unmarshal(tagged.Body.Bytes(), &second)
	if first.Ref != "" || second.Ref != "v1.2.3" || first.ID == second.ID {
		t.Errorf("plain=%+v tagged=%+v", first, second)
	}
	if response := f.post(path, f.deployToken, "application/json", []byte(`{"ref":"v1..2"}`)); response.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid ref: %d", response.Code)
	}
}

func TestPromoteRequiresADeployedSource(t *testing.T) {
	f := newReleaseHTTPFixture(t)
	other, err := f.apps.Create(context.Background(), application.CreateInput{EnvironmentID: "e", ServerID: "srv", Name: "prod", Config: func() application.Config {
		config := application.DefaultConfig(application.PackDockerfile)
		config.Source.Repository = "https://example.invalid/acme/app"
		return config
	}()})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/applications/" + f.appID + "/promote"
	body := []byte(`{"target_application_id":"` + other.ID + `"}`)
	if response := f.post(path, f.read, "application/json", body); response.Code != http.StatusForbidden {
		t.Errorf("read token: %d", response.Code)
	}
	response := f.post(path, f.deployToken, "application/json", body)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "origem") {
		t.Errorf("%d %s", response.Code, response.Body.String())
	}
}
