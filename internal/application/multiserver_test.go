package application

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox"
	"pier/internal/registry"
	"pier/internal/server"
)

const registryPassword = "ghp-registry-secret-value"

type placementGuard struct{}

func (placementGuard) CheckDeploy(context.Context, string) error { return nil }

// serverConnector gives each server its own scripted executor.
type serverConnector map[string]*executor.Fake

func (c serverConnector) Executor(_ context.Context, id string) (executor.Executor, server.Server, error) {
	ex, ok := c[id]
	if !ok {
		return nil, server.Server{}, errors.New("unknown server")
	}
	return ex, server.Server{ID: id, Name: "name-" + id, DataDir: "/data/pier", ProxyToken: "route-token-" + id}, nil
}

// freshServer reports registry images as missing until a pull ran on it.
type freshServer struct {
	*executor.Fake
	pulled bool
}

func (s *freshServer) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	if strings.Contains(command, " pull ") {
		s.pulled = true
	}
	return s.Fake.Stream(ctx, command, sink)
}

func (s *freshServer) Run(ctx context.Context, command string) (string, error) {
	output, err := s.Fake.Run(ctx, command)
	if strings.Contains(command, "'{{.Id}}' ghcr.io/") && !s.pulled {
		return "", errors.New("no such image")
	}
	return output, err
}

// freshConnector serves some servers through a freshServer.
type freshConnector struct {
	serverConnector
	fresh map[string]*freshServer
}

func (c freshConnector) Executor(ctx context.Context, id string) (executor.Executor, server.Server, error) {
	ex, target, err := c.serverConnector.Executor(ctx, id)
	if fresh, ok := c.fresh[id]; ok {
		return fresh, target, err
	}
	return ex, target, err
}

// offlineRegistry answers the registry API without any network.
type offlineRegistry struct{ manifest int }

func (o offlineRegistry) RoundTrip(request *http.Request) (*http.Response, error) {
	status := http.StatusOK
	if request.Method == http.MethodHead {
		status = o.manifest
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: request}, nil
}

type placementFixture struct {
	*deployFixture
	servers serverConnector
	multi   *MultiServer
	images  *registry.Service
}

func digestRef(repository, hex string) string {
	return "ghcr.io/" + repository + "@sha256:" + strings.Repeat(hex, 64)
}

func newPlacementFixture(t *testing.T, config Config, servers ...string) *placementFixture {
	t.Helper()
	config.Health.Enabled = true
	f := newDeployFixture(t, config)
	db := f.store.db
	connector := serverConnector{"srv1": f.ex}
	for _, id := range servers {
		if _, err := db.Exec(`INSERT INTO servers (id, name, kind, data_dir, created_at) VALUES (?, ?, 'local', '/data/pier', '2026-01-01T00:00:00Z')`, id, id); err != nil {
			t.Fatal(err)
		}
		connector[id] = executor.NewFake().
			On("{{json .Config.Env}}", "[]", 0).
			On("docker image inspect --format '{{json .Config.Labels}}'", "{}", 0)
	}
	for _, ex := range connector {
		ex.On(".State.Status", "running|healthy|0", 0).On("mktemp -d", "/tmp/pier-registry-abc123", 0)
	}
	box, err := secretbox.New("test-passphrase-123456")
	if err != nil {
		t.Fatal(err)
	}
	images := registry.New(db, box)
	images.SetHTTPClient(&http.Client{Transport: offlineRegistry{manifest: http.StatusOK}})
	multi := NewMultiServer(db, f.store, placementGuard{}, images)
	f.deployer.connector = connector
	f.deployer.SetMultiServer(multi)
	return &placementFixture{deployFixture: f, servers: connector, multi: multi, images: images}
}

func (p *placementFixture) registry(t *testing.T) string {
	t.Helper()
	created, err := p.images.Create(context.Background(), registry.Input{Name: "GHCR", Host: "ghcr.io", Username: "bot", Password: registryPassword})
	if err != nil {
		t.Fatal(err)
	}
	return created.ID
}

func (p *placementFixture) deploy(t *testing.T, deployment Deployment) error {
	t.Helper()
	deployment.ID = newDeploymentID()
	deployment.ApplicationID = p.app.ID
	if err := p.store.insertDeployment(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	log := &deployLog{store: p.store, hub: p.deployer.hub, deploymentID: deployment.ID}
	return p.deployer.execute(context.Background(), deployment, log)
}

func (p *placementFixture) statuses(t *testing.T) map[string]DestinationStatus {
	t.Helper()
	rows, err := p.multi.Status(context.Background(), p.app.ID)
	if err != nil {
		t.Fatal(err)
	}
	byServer := map[string]DestinationStatus{}
	for _, row := range rows {
		byServer[row.ServerID] = row
	}
	return byServer
}

func lastCompose(ex *executor.Fake) string {
	return string(ex.Files["/data/pier/applications/"+currentAppDir(ex)+"/docker-compose.yaml"])
}

func currentAppDir(ex *executor.Fake) string {
	for path := range ex.Files {
		if match := regexp.MustCompile(`^/data/pier/applications/([a-z0-9]+)/docker-compose.yaml$`).FindStringSubmatch(path); match != nil {
			return match[1]
		}
	}
	return ""
}

func TestBuildServerPushesOnceAndEveryServerRunsTheSameDigest(t *testing.T) {
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	config.Network.Domains = []string{"https://api.example.com"}
	p := newPlacementFixture(t, config, "srv2", "build")
	registryID := p.registry(t)
	if _, err := p.multi.Save(context.Background(), p.app.ID, Placement{BuildServerID: "build", RegistryID: registryID,
		Repository: "acme/api", Destinations: []string{"srv2"}}); err != nil {
		t.Fatal(err)
	}
	pushed := digestRef("acme/api", "a")
	build := p.servers["build"].On("RepoDigests", `[]`, 0).On("RepoDigests}}' ghcr.io/acme/api:pier-", `["`+pushed+`"]`, 0)
	for _, id := range []string{"srv1", "srv2"} {
		p.servers[id].On("{{.Id}}' "+pushed, "", 1)
	}
	if err := p.deploy(t, Deployment{Trigger: TriggerRollback, RollbackImage: "pier/" + p.app.ID + ":old"}); err != nil {
		t.Fatal(err)
	}
	if !build.Ran("push ghcr.io/acme/api:pier-") {
		t.Fatalf("build server did not push: %v", build.Commands)
	}
	for _, id := range []string{"srv1", "srv2"} {
		ex := p.servers[id]
		if !ex.Ran("docker --config /tmp/pier-registry-abc123 pull " + pushed) {
			t.Fatalf("%s did not pull the digest with the credential: %v", id, ex.Commands)
		}
		if compose := lastCompose(ex); !strings.Contains(compose, pushed) || !strings.Contains(compose, "Host(`api.example.com`)") {
			t.Fatalf("%s compose does not run the digest behind its proxy:\n%s", id, compose)
		}
		if !strings.Contains(lastCompose(ex), "route-token-"+id) {
			t.Fatalf("%s proxy labels not rendered for that server", id)
		}
		if ex.Ran("push") {
			t.Fatalf("%s pushed", id)
		}
	}
	for _, ex := range p.servers {
		for _, command := range ex.Commands {
			if strings.Contains(command, registryPassword) {
				t.Fatalf("registry password in a command: %s", command)
			}
		}
	}
	statuses := p.statuses(t)
	if statuses["srv1"].Status != PlacementHealthy || statuses["srv2"].Image != pushed {
		t.Fatalf("statuses: %+v", statuses)
	}
	app, _ := p.store.Get(context.Background(), p.app.ID)
	if app.CurrentImage != pushed {
		t.Fatalf("current image %q", app.CurrentImage)
	}
}

func TestImageApplicationsPinTheUpstreamDigestWithoutARegistry(t *testing.T) {
	config := DefaultConfig(PackImage)
	config.Source.Image = "ghcr.io/acme/web:1.0"
	p := newPlacementFixture(t, config, "srv2")
	if _, err := p.multi.Save(context.Background(), p.app.ID, Placement{Destinations: []string{"srv2"}}); err != nil {
		t.Fatal(err)
	}
	upstream := digestRef("acme/web", "c")
	p.servers["srv1"].On("RepoDigests", `["`+upstream+`"]`, 0)
	p.servers["srv2"].On("{{.Id}}' "+upstream, "", 1)
	if err := p.deploy(t, Deployment{Trigger: TriggerManual}); err != nil {
		t.Fatal(err)
	}
	if !p.servers["srv2"].Ran("docker pull " + upstream) {
		t.Fatalf("destination did not pull the upstream digest: %v", p.servers["srv2"].Commands)
	}
	if !strings.Contains(lastCompose(p.servers["srv2"]), upstream) {
		t.Fatal("destination does not run the digest")
	}
}

func TestFailureOnALaterServerRollsBackTheServersAlreadyUpdated(t *testing.T) {
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	config.Release.Command = "migrate"
	p := newPlacementFixture(t, config, "srv2")
	registryID := p.registry(t)
	if _, err := p.multi.Save(context.Background(), p.app.ID, Placement{RegistryID: registryID, Repository: "acme/api",
		Destinations: []string{"srv2"}}); err != nil {
		t.Fatal(err)
	}
	old, next := digestRef("acme/api", "b"), digestRef("acme/api", "a")
	for id, ex := range p.servers {
		ex.On("docker ps", "old-"+id+"\tdemo-old\t"+old+"\trunning\tUp\tapplication\t"+p.app.ID+"\tolddeployment", 0)
	}
	p.servers["srv2"].On(".State.Status", "running|unhealthy|0", 0)
	err := p.deploy(t, Deployment{Trigger: TriggerRollback, RollbackImage: next})
	if err == nil || !strings.Contains(err.Error(), "voltaram para a versão anterior") {
		t.Fatalf("expected a rolled back failure, got %v", err)
	}
	if !strings.Contains(lastCompose(p.servers["srv1"]), old) || strings.Contains(lastCompose(p.servers["srv1"]), next) {
		t.Fatalf("srv1 not back on the previous digest:\n%s", lastCompose(p.servers["srv1"]))
	}
	if p.servers["srv2"].Ran("docker stop old-srv2") {
		t.Fatal("srv2 retired its previous version although the new one never got ready")
	}
	if p.servers["srv2"].Ran("migrate") {
		t.Fatal("release command ran on more than one server")
	}
	statuses := p.statuses(t)
	if statuses["srv1"].Status != PlacementRolledBack || statuses["srv1"].Image != old {
		t.Fatalf("srv1: %+v", statuses["srv1"])
	}
	if statuses["srv2"].Status != PlacementFailed || statuses["srv2"].Image != old || statuses["srv2"].Message == "" {
		t.Fatalf("srv2: %+v", statuses["srv2"])
	}
	app, _ := p.store.Get(context.Background(), p.app.ID)
	if app.CurrentImage == next {
		t.Fatal("failed rollout recorded as current")
	}
}

func TestAFailedPullChangesNoServer(t *testing.T) {
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	p := newPlacementFixture(t, config, "srv2")
	registryID := p.registry(t)
	if _, err := p.multi.Save(context.Background(), p.app.ID, Placement{RegistryID: registryID, Repository: "acme/api",
		Destinations: []string{"srv2"}}); err != nil {
		t.Fatal(err)
	}
	next := digestRef("acme/api", "a")
	p.servers["srv2"].On("{{.Id}}' "+next, "", 1).On("pull "+next, "denied", 1)
	if err := p.deploy(t, Deployment{Trigger: TriggerRollback, RollbackImage: next}); err == nil {
		t.Fatal("pull failure ignored")
	}
	for id, ex := range p.servers {
		if ex.Ran("docker compose") {
			t.Fatalf("%s changed although the image never reached every server", id)
		}
	}
}

func TestSingleServerDeploymentsRunTheSameCommandsAsBefore(t *testing.T) {
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	config.Health.Enabled = true
	f := newDeployFixture(t, config)
	f.ex.On(".State.Status", "running|healthy|0", 0)
	run := func() []string {
		f.ex.Commands = nil
		deployment := Deployment{ID: newDeploymentID(), ApplicationID: f.app.ID, Trigger: TriggerManual}
		if err := f.store.insertDeployment(context.Background(), deployment); err != nil {
			t.Fatal(err)
		}
		log := &deployLog{store: f.store, hub: f.deployer.hub, deploymentID: deployment.ID}
		if err := f.deployer.execute(context.Background(), deployment, log); err != nil {
			t.Fatal(err)
		}
		commands := strings.Join(f.ex.Commands, "\n")
		commands = strings.ReplaceAll(commands, deployment.ID, "DEPLOYMENT")
		return strings.Split(strings.ReplaceAll(commands, deployment.ID[:6], "DEPLOY"), "\n")
	}
	before := run()
	box, _ := secretbox.New("test-passphrase-123456")
	images := registry.New(f.store.db, box)
	if _, err := images.Create(context.Background(), registry.Input{Name: "Outro", Host: "ghcr.io", Username: "x", Password: "y"}); err != nil {
		t.Fatal(err)
	}
	multi := NewMultiServer(f.store.db, f.store, placementGuard{}, images)
	f.deployer.SetMultiServer(multi)
	withoutPlacement := run()
	if _, err := multi.Save(context.Background(), f.app.ID, Placement{Destinations: []string{"srv1"}}); err != nil {
		t.Fatal(err)
	}
	ownServerOnly := run()
	for name, after := range map[string][]string{"no placement": withoutPlacement, "own server only": ownServerOnly} {
		if strings.Join(after, "\n") != strings.Join(before, "\n") {
			t.Fatalf("%s changed the single-server commands:\nbefore:\n%s\nafter:\n%s", name, strings.Join(before, "\n"), strings.Join(after, "\n"))
		}
	}
}

func TestPrivateImagesArePulledWithTheStoredCredential(t *testing.T) {
	config := DefaultConfig(PackImage)
	config.Source.Image = "ghcr.io/acme/private:2.0"
	p := newPlacementFixture(t, config)
	p.registry(t)
	if err := p.deploy(t, Deployment{Trigger: TriggerManual}); err != nil {
		t.Fatal(err)
	}
	if !p.ex.Ran("docker --config /tmp/pier-registry-abc123 pull ghcr.io/acme/private:2.0") || !p.ex.Ran("rm -rf -- /tmp/pier-registry-abc123") {
		t.Fatalf("private pull: %v", p.ex.Commands)
	}
}

func TestPromotionAcrossServersReusesThePushedDigest(t *testing.T) {
	ctx := context.Background()
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	p := newPlacementFixture(t, config, "srv2")
	p.registry(t)
	pushed := digestRef("acme/api", "a")
	if err := p.store.setCurrentImage(ctx, p.app.ID, pushed); err != nil {
		t.Fatal(err)
	}
	service := NewService(p.store, p.env, p.servers, NewDispatcher(p.store, p.deployer, p.deployer.logger, 0))
	service.SetMultiServer(p.multi)
	target, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv2", Name: "staging", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := service.Promote(ctx, p.app.ID, target.ID)
	if err != nil || queued.RollbackImage != pushed || queued.Trigger != TriggerPromotion {
		t.Fatalf("promotion: %+v %v", queued, err)
	}
	p.deployer.connector = freshConnector{p.servers, map[string]*freshServer{"srv2": {Fake: p.servers["srv2"]}}}
	log := &deployLog{store: p.store, hub: p.deployer.hub, deploymentID: queued.ID}
	queued.Status = DeploymentInProgress
	if err := p.deployer.execute(ctx, queued, log); err != nil {
		t.Fatal(err)
	}
	srv2 := p.servers["srv2"]
	if !srv2.Ran("--config /tmp/pier-registry-abc123 pull "+pushed) || !srv2.Ran("docker tag "+pushed+" pier/"+target.ID) {
		t.Fatalf("target server did not pull and tag the digest: %v", srv2.Commands)
	}
	p.images.SetHTTPClient(&http.Client{Transport: offlineRegistry{manifest: http.StatusNotFound}})
	_, err = service.Promote(ctx, p.app.ID, target.ID)
	if appErr, ok := apperr.As(err); !ok || appErr.Kind != apperr.KindConflict {
		t.Fatalf("promotion of a digest the registry no longer has: %v", err)
	}
}

func TestSaveValidatesThePlacement(t *testing.T) {
	ctx := context.Background()
	config := DefaultConfig(PackDockerfile)
	config.Source.Repository = "https://example.com/app.git"
	p := newPlacementFixture(t, config, "srv2")
	_, err := p.multi.Save(ctx, p.app.ID, Placement{Destinations: []string{"srv2"}})
	if appErr, ok := apperr.As(err); !ok || appErr.Fields["registry_id"] == "" {
		t.Fatalf("built image without registry accepted: %v", err)
	}
	_, err = p.multi.Save(ctx, p.app.ID, Placement{Destinations: []string{"missing"}})
	if appErr, ok := apperr.As(err); !ok || appErr.Fields["destinations"] == "" {
		t.Fatalf("unknown server accepted: %v", err)
	}
	registryID := p.registry(t)
	saved, err := p.multi.Save(ctx, p.app.ID, Placement{BuildServerID: "srv1", RegistryID: registryID, Repository: "acme/api",
		Destinations: []string{"srv2", "srv1"}})
	if err != nil || strings.Join(saved.Destinations, ",") != "srv1,srv2" || saved.BuildServerID != "" {
		t.Fatalf("saved: %+v %v", saved, err)
	}
	servers, err := p.multi.Servers(ctx, p.app.ID)
	if err != nil || len(servers) != 2 || servers[0].Role != RolePrimary {
		t.Fatalf("servers: %+v %v", servers, err)
	}
}

func TestLifecycleOperationsReachEveryServerAndServersLeavingAreCleaned(t *testing.T) {
	ctx := context.Background()
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	p := newPlacementFixture(t, config, "srv2")
	if _, err := p.multi.Save(ctx, p.app.ID, Placement{Destinations: []string{"srv2"}}); err != nil {
		t.Fatal(err)
	}
	for id, ex := range p.servers {
		ex.On("docker ps", "cid-"+id+"\tdemo\timage\trunning\tUp\tapplication\t"+p.app.ID+"\tdep", 0)
	}
	service := NewService(p.store, p.env, p.servers, NewDispatcher(p.store, p.deployer, p.deployer.logger, 0))
	service.SetMultiServer(p.multi)
	if err := service.Stop(ctx, p.app.ID); err != nil {
		t.Fatal(err)
	}
	for id, ex := range p.servers {
		if !ex.Ran("docker stop cid-" + id) {
			t.Fatalf("%s not stopped", id)
		}
	}
	if err := p.multi.record(ctx, p.app.ID, DestinationStatus{ServerID: "srv2", Status: PlacementHealthy}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.multi.Save(ctx, p.app.ID, Placement{}); err != nil {
		t.Fatal(err)
	}
	if err := p.deploy(t, Deployment{Trigger: TriggerManual}); err != nil {
		t.Fatal(err)
	}
	if !p.servers["srv2"].Ran("docker rm -f cid-srv2") {
		t.Fatalf("server taken out still runs the application: %v", p.servers["srv2"].Commands)
	}
	if len(p.statuses(t)) != 0 {
		t.Fatalf("placement state kept after going back to one server: %+v", p.statuses(t))
	}
}

func TestRecoverMarksInterruptedRollouts(t *testing.T) {
	ctx := context.Background()
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	p := newPlacementFixture(t, config)
	if err := p.multi.record(ctx, p.app.ID, DestinationStatus{ServerID: "srv1", Status: PlacementDeploying}); err != nil {
		t.Fatal(err)
	}
	if err := p.multi.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if status := p.statuses(t)["srv1"].Status; status != PlacementInterrupted {
		t.Fatalf("status %q", status)
	}
}
