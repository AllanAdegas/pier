package application

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/notify"
	"pier/internal/platform/executor"
	"pier/internal/platform/pubsub"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

// fakeConnector hands the same scripted executor to every server lookup.
type fakeConnector struct{ ex *executor.Fake }

func (f fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return f.ex, server.Server{ID: "srv1", Name: "test", DataDir: "/data/pier"}, nil
}

type recordingPublisher struct {
	mu     sync.Mutex
	events []notify.Event
}

func (r *recordingPublisher) Publish(event notify.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingPublisher) types() []notify.EventType {
	r.mu.Lock()
	defer r.mu.Unlock()
	types := make([]notify.EventType, len(r.events))
	for i, event := range r.events {
		types[i] = event.Type
	}
	return types
}

type deployFixture struct {
	store     *Store
	env       *envvar.Store
	deployer  *Deployer
	ex        *executor.Fake
	publisher *recordingPublisher
	app       Application
}

func newDeployFixture(t *testing.T, config Config) *deployFixture {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	seedPlacement(t, db)
	box, err := secretbox.New("test-passphrase-123456")
	if err != nil {
		t.Fatalf("secretbox: %v", err)
	}
	fixture := &deployFixture{
		store: NewStore(db, box), env: envvar.NewStore(db, box),
		ex: executor.NewFake(), publisher: &recordingPublisher{},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fixture.deployer = NewDeployer(fixture.store, fixture.env, fakeConnector{fixture.ex}, nil,
		fixture.publisher, pubsub.NewHub[LogEntry](), logger)
	// The dispatcher is only needed to accept the enqueue; nothing consumes it here.
	service := NewService(fixture.store, fixture.env, fakeConnector{fixture.ex},
		NewDispatcher(fixture.store, fixture.deployer, logger))
	fixture.app, err = service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "demo", Config: config})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	return fixture
}

func seedPlacement(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO servers (id, name, kind, data_dir, created_at) VALUES ('srv1', 'test', 'local', '/data/pier', '2026-01-01T00:00:00Z')`,
		`INSERT INTO projects (id, name, created_at) VALUES ('prj1', 'p', '2026-01-01T00:00:00Z')`,
		`INSERT INTO environments (id, project_id, name, created_at) VALUES ('env1', 'prj1', 'production', '2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func (f *deployFixture) deploy(t *testing.T) Deployment {
	t.Helper()
	ctx := context.Background()
	deployment := Deployment{ID: "dep" + strings.Repeat("x", 3) + string(rune('a'+len(f.ex.Commands)%26)), ApplicationID: f.app.ID,
		Status: DeploymentQueued, Trigger: TriggerManual}
	if err := f.store.insertDeployment(ctx, deployment); err != nil {
		t.Fatalf("insert deployment: %v", err)
	}
	if claimed, err := f.store.claimDeployment(ctx, deployment.ID); err != nil || !claimed {
		t.Fatalf("claim deployment: %v %v", claimed, err)
	}
	f.deployer.Run(ctx, deployment.ID)
	finished, err := f.store.GetDeployment(ctx, deployment.ID)
	if err != nil {
		t.Fatalf("reload deployment: %v", err)
	}
	return finished
}

func (f *deployFixture) logText(t *testing.T, deploymentID string) string {
	t.Helper()
	entries, err := f.store.Logs(context.Background(), deploymentID, 0)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	var text strings.Builder
	for _, entry := range entries {
		text.WriteString(entry.Line + "\n")
	}
	return text.String()
}

// indexOf returns the position of the first executed command containing the fragment.
func indexOf(commands []string, fragment string) int {
	for i, command := range commands {
		if strings.Contains(command, fragment) {
			return i
		}
	}
	return -1
}

func imageConfig() Config {
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	config.Network.Port = 80
	config.Network.Domains = []string{"https://demo.example.com"}
	config.Health.Enabled = true // healthy containers are ready immediately, keeping the test fast
	return config
}

const previousContainer = "oldid\tapp-old\tnginx:1.26\trunning\tUp 2 hours\tapplication\tappid\tolddep"

func TestRollingUpdateRetiresTheOldVersionOnlyAfterTheNewOneIsHealthy(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	fixture.ex.On("docker ps", previousContainer, 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	deployment := fixture.deploy(t)

	if deployment.Status != DeploymentFinished {
		t.Fatalf("status = %s (%s)\n%s", deployment.Status, deployment.Error, fixture.logText(t, deployment.ID))
	}
	commands := fixture.ex.Commands
	up, stopOld, removeOld := indexOf(commands, "compose"), indexOf(commands, "docker stop"), indexOf(commands, "docker rm -f oldid")
	if up == -1 || stopOld == -1 || removeOld == -1 {
		t.Fatalf("expected up, stop and rm of the old container; ran:\n%s", strings.Join(commands, "\n"))
	}
	if !(up < stopOld && stopOld < removeOld) {
		t.Errorf("old version must be retired after the new one is up: up=%d stop=%d rm=%d", up, stopOld, removeOld)
	}
	app, _ := fixture.store.Get(context.Background(), fixture.app.ID)
	if app.CurrentImage != "nginx:1.27" || app.Status != "running" {
		t.Errorf("application not updated: image=%q status=%q", app.CurrentImage, app.Status)
	}
	compose := string(fixture.ex.Files["/data/pier/applications/"+fixture.app.ID+"/docker-compose.yaml"])
	if !strings.Contains(compose, "image: nginx:1.27") || !strings.Contains(compose, "demo.example.com") {
		t.Errorf("compose file does not describe the release:\n%s", compose)
	}
	if got := fixture.publisher.types(); len(got) != 1 || got[0] != notify.DeploymentSucceeded {
		t.Errorf("events = %v", got)
	}
}

func TestFailedHealthcheckKeepsThePreviousVersionRunning(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	fixture.ex.On("docker ps", previousContainer, 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|unhealthy|0", 0)

	deployment := fixture.deploy(t)

	if deployment.Status != DeploymentFailed || deployment.Error == "" {
		t.Fatalf("status = %s, error = %q", deployment.Status, deployment.Error)
	}
	if fixture.ex.Ran("docker stop") || fixture.ex.Ran("docker rm -f oldid") {
		t.Error("the previous version must stay untouched when the new one is unhealthy")
	}
	newContainer := docker.ReadableName(fixture.app.Name, fixture.app.ID) + "-" + docker.ShortID(deployment.ID)
	if !fixture.ex.Ran("docker rm -f " + newContainer) {
		t.Error("the unhealthy new container must be discarded")
	}
	app, _ := fixture.store.Get(context.Background(), fixture.app.ID)
	if app.CurrentImage != "" {
		t.Errorf("current image must not change on failure, got %q", app.CurrentImage)
	}
	if got := fixture.publisher.types(); len(got) != 1 || got[0] != notify.DeploymentFailed {
		t.Errorf("events = %v", got)
	}
}

func TestHostPortMappingReplacesInsteadOfRolling(t *testing.T) {
	config := imageConfig()
	config.Network.PortMappings = []string{"8080:80"}
	fixture := newDeployFixture(t, config)
	fixture.ex.On("docker ps", previousContainer, 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	deployment := fixture.deploy(t)

	if deployment.Status != DeploymentFinished {
		t.Fatalf("status = %s (%s)", deployment.Status, deployment.Error)
	}
	// Two containers cannot bind the same host port, so the old one goes first.
	if removeOld, up := indexOf(fixture.ex.Commands, "docker rm -f oldid"), indexOf(fixture.ex.Commands, "compose"); removeOld == -1 || removeOld > up {
		t.Errorf("old container must be removed before the new one starts: rm=%d up=%d", removeOld, up)
	}
}

func TestRegistryFailureFallsBackToTheLocalImageOrFails(t *testing.T) {
	t.Run("image present locally", func(t *testing.T) {
		fixture := newDeployFixture(t, imageConfig())
		fixture.ex.On("docker pull", "toomanyrequests: rate limit", 1)
		fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
		if deployment := fixture.deploy(t); deployment.Status != DeploymentFinished {
			t.Fatalf("a cached image should still deploy: %s (%s)", deployment.Status, deployment.Error)
		}
	})
	t.Run("image absent", func(t *testing.T) {
		fixture := newDeployFixture(t, imageConfig())
		fixture.ex.On("docker pull", "manifest unknown", 1)
		fixture.ex.On("docker image inspect", "No such image", 1)
		deployment := fixture.deploy(t)
		if deployment.Status != DeploymentFailed || !strings.Contains(deployment.Error, "docker pull") {
			t.Fatalf("status = %s, error = %q", deployment.Status, deployment.Error)
		}
		if fixture.ex.Ran("compose") {
			t.Error("nothing must be started when the image is unavailable")
		}
	})
}

func TestSecretsAreRedactedFromDeploymentLogs(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	owner := envvar.Owner{Type: envvar.OwnerApplication, ID: fixture.app.ID}
	if err := fixture.env.Set(context.Background(), owner, "API_KEY", "sk-live-9f8e7d6c5b4a", false); err != nil {
		t.Fatalf("set variable: %v", err)
	}
	// A tool echoing the environment must not leak the value into the log.
	fixture.ex.On("docker pull", "debug: API_KEY=sk-live-9f8e7d6c5b4a", 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	deployment := fixture.deploy(t)

	log := fixture.logText(t, deployment.ID)
	if strings.Contains(log, "sk-live-9f8e7d6c5b4a") {
		t.Errorf("secret leaked into the deployment log:\n%s", log)
	}
	if !strings.Contains(log, "API_KEY=••••••") {
		t.Errorf("expected the redacted line in the log:\n%s", log)
	}
	// The container itself still receives the real value.
	compose := string(fixture.ex.Files["/data/pier/applications/"+fixture.app.ID+"/docker-compose.yaml"])
	if !strings.Contains(compose, "sk-live-9f8e7d6c5b4a") {
		t.Error("the runtime environment must contain the real secret")
	}
}

func TestGitSourceIsClonedWithValuesPassedAsDataNotCode(t *testing.T) {
	config := gitConfig()
	config.Source.Branch = "main"
	config.Health.Enabled = true
	fixture := newDeployFixture(t, config)
	fixture.ex.On("alpine/git", "Cloning into '/work/src'...\nPIER_COMMIT 0123456789abcdef fix: something", 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	deployment := fixture.deploy(t)

	if deployment.Status != DeploymentFinished {
		t.Fatalf("status = %s (%s)\n%s", deployment.Status, deployment.Error, fixture.logText(t, deployment.ID))
	}
	if deployment.CommitSHA != "0123456789abcdef" || deployment.CommitMessage != "fix: something" {
		t.Errorf("commit not recorded: %q %q", deployment.CommitSHA, deployment.CommitMessage)
	}
	clone := fixture.ex.Commands[indexOf(fixture.ex.Commands, "alpine/git")]
	// Repository and branch travel as environment values; the script only
	// ever expands them inside double quotes.
	if !strings.Contains(clone, "PIER_REPOSITORY=https://github.com/acme/app") || !strings.Contains(clone, `-- "$PIER_REPOSITORY"`) {
		t.Errorf("unexpected clone command:\n%s", clone)
	}
	build := fixture.ex.Commands[indexOf(fixture.ex.Commands, "docker build")]
	wantImage := "pier/" + fixture.app.ID + ":" + deployment.ID
	if !strings.Contains(build, wantImage) || deployment.Image != wantImage {
		t.Errorf("image tag mismatch: build=%q deployment=%q", build, deployment.Image)
	}
	// Cleanup runs through a container (clone output may be root-owned):
	// docker run ... --entrypoint rm <image> -rf /builds/<deployment>.
	if !fixture.ex.Ran("--entrypoint rm") || !fixture.ex.Ran("-rf /builds/"+deployment.ID) {
		t.Errorf("the build directory must be cleaned up after the deployment; ran:\n%s", strings.Join(fixture.ex.Commands, "\n"))
	}
}
