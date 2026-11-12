package application

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/executor"

	"gopkg.in/yaml.v3"
	"pier/internal/compose"
)

func TestEngineReleaseCommandRunsOnFirstDeployWithNewImageAndEnvironment(t *testing.T) {
	config := imageConfig()
	config.Release.Command = "npm run migrate"
	f := newDeployFixture(t, config)
	setVariable(t, f, "DATABASE_URL", "dummy-runtime-secret", false)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	f.ex.On("--entrypoint sh release -c", "migrated dummy-runtime-secret", 0)
	dep := f.deploy(t)
	if dep.Status != DeploymentFinished {
		t.Fatalf("%s %s", dep.Status, dep.Error)
	}
	release, up := indexOf(f.ex.Commands, "run --rm --no-deps"), indexOf(f.ex.Commands, "up --detach")
	if release < 0 || up < release {
		t.Fatalf("release %d,up %d", release, up)
	}
	if strings.Contains(f.logText(t, dep.ID), "dummy-runtime-secret") {
		t.Fatal("release output leaked")
	}
	var releaseFile compose.File
	filePath := "/data/pier/applications/" + f.app.ID + "/release-" + dep.ID + "/docker-compose.yaml"
	if err := yaml.Unmarshal(f.ex.Files[filePath], &releaseFile); err != nil {
		t.Fatal(err)
	}
	service := releaseFile.Services["release"]
	if service == nil || service.Image != config.Source.Image || service.Environment["DATABASE_URL"] != "dummy-runtime-secret" || service.Environment["PORT"] != "80" {
		t.Fatalf("release service %+v", service)
	}
	if len(service.Ports) != 0 || len(service.Labels) != 0 || len(service.Networks["pier"].Aliases) != 0 {
		t.Fatalf("release publishes routes or aliases: %+v", service)
	}
	if !f.ex.Ran("docker rm -f " + service.ContainerName) {
		t.Fatal("disposable container cleanup missing")
	}
}

func TestEngineFailedReleasePreservesServingVersionIncludingHostPorts(t *testing.T) {
	for _, hostPorts := range []bool{false, true} {
		t.Run(fmt.Sprint(hostPorts), func(t *testing.T) {
			config := imageConfig()
			config.Release.Command = "migrate"
			if hostPorts {
				config.Network.PortMappings = []string{"8000:80"}
			}
			f := newDeployFixture(t, config)
			f.ex.On("docker ps", previousContainer, 0)
			f.ex.On("run --rm --no-deps", "migration failed", 1)
			dep := f.deploy(t)
			if dep.Status != DeploymentFailed {
				t.Fatal(dep.Status)
			}
			if f.ex.Ran("up --detach") || f.ex.Ran("docker stop") || f.ex.Ran("docker rm -f oldid") {
				t.Fatal("serving version touched before release passed")
			}
			app, _ := f.store.Get(context.Background(), f.app.ID)
			if app.CurrentImage != "" {
				t.Fatal("current image changed")
			}
		})
	}
}

func TestEngineRetiredLogsAreBoundedMaskedAndBelongToReplacement(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	setVariable(t, f, "TOKEN", "new-dummy-secret", false)
	f.ex.On("docker ps", previousContainer, 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	f.ex.On("{{json .Config.Env}}", `["TOKEN=old-dummy-secret"]`, 0)
	lines := []string{}
	for i := range 1005 {
		lines = append(lines, fmt.Sprintf("line %d old-dummy-secret new-dummy-secret", i))
	}
	f.ex.On("docker logs --timestamps --tail 1000", strings.Join(lines, "\n"), 0)
	dep := f.deploy(t)
	if dep.Status != DeploymentFinished || len(dep.RetiredLogs) != 1 {
		t.Fatalf("%s: %s logs=%v", dep.Status, dep.Error, dep.RetiredLogs)
	}
	summaries, err := f.store.ListDeployments(context.Background(), f.app.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || len(summaries[0].RetiredLogs) != 0 {
		t.Fatal("history list loaded full retired log tails")
	}
	log := dep.RetiredLogs[0]
	if log.Container != "app-old" || len(strings.Split(log.Output, "\n")) != 1000 || !strings.HasPrefix(log.Output, "line 5 ") {
		t.Fatal("wrong retained tail")
	}
	if strings.Contains(log.Output, "dummy-secret") {
		t.Fatal("retired logs leaked a rotated secret")
	}
	capture, remove := indexOf(f.ex.Commands, "docker logs --timestamps --tail 1000"), indexOf(f.ex.Commands, "docker rm -f oldid")
	if capture < 0 || remove < capture {
		t.Fatalf("capture %d remove %d", capture, remove)
	}
}

func TestEngineFailureHintsAndListenerPortPersist(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|unhealthy|0", 0)
	// /proc/net/tcp LISTEN on 0.0.0.0:8080.
	f.ex.On("cat /proc/net/tcp /proc/net/tcp6", "0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1", 0)
	f.ex.On("docker logs --timestamps --tail 40", "out of memory", 0)
	dep := f.deploy(t)
	codes := map[string]bool{}
	for _, hint := range dep.Hints {
		codes[hint.Code] = true
	}
	if dep.SuggestedPort != 8080 || !codes["listener_port_mismatch"] || !codes["out_of_memory"] {
		t.Fatalf("port %d hints %+v", dep.SuggestedPort, dep.Hints)
	}
}

func TestEngineSuccessfulDeployStillDiagnosesLoopback(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	f.ex.On("cat /proc/net/tcp /proc/net/tcp6", "0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1", 0)
	dep := f.deploy(t)
	if dep.Status != DeploymentFinished || len(dep.Hints) != 1 || dep.Hints[0].Code != "listener_loopback_only" {
		t.Fatalf("%s %+v", dep.Status, dep.Hints)
	}
}

func TestEngineImagePortSuggestionsUseExposedPorts(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	f.ex.On("docker image inspect nginx", `[{"Config":{"ExposedPorts":{"8080/tcp":{},"53/udp":{}}}}]`, 0)
	service := NewService(f.store, f.env, fakeConnector{f.ex}, nil)
	ports, err := service.ImagePorts(context.Background(), "srv1", "nginx")
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 || ports[0] != 53 || ports[1] != 8080 {
		t.Fatal(ports)
	}
	if _, err := service.ImagePorts(context.Background(), "srv1", "nginx; touch /tmp/x"); err == nil {
		t.Fatal("bad image accepted")
	}
}

func TestEngineRuntimeExit137ExplainsOOMWithoutLogText(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	f.ex.On("docker inspect --format '{{.State.Status}}", "exited|none|0", 0)
	f.ex.On("{{.State.ExitCode}}", "137", 0)
	dep := f.deploy(t)
	found := false
	for _, hint := range dep.Hints {
		if hint.Code == "out_of_memory" {
			found = true
		}
	}
	if !found {
		t.Fatalf("runtime OOM not explained: %+v", dep.Hints)
	}
}

func TestEngineRetiredLogsFailClosedWhenOldEnvironmentCannotBeRead(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	f.ex.On("docker ps", previousContainer, 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	f.ex.On("{{json .Config.Env}}", "unavailable", 1)
	f.ex.On("docker logs --timestamps --tail 1000", "old-unregistered-secret", 0)
	dep := f.deploy(t)
	if dep.Status != DeploymentFinished || len(dep.RetiredLogs) != 0 || f.ex.Ran("docker logs --timestamps --tail 1000") {
		t.Fatalf("unsafe log capture: %s %+v", dep.Status, dep.RetiredLogs)
	}
}

type imagePullExecutor struct{ *executor.Fake }

func (e imagePullExecutor) Run(ctx context.Context, command string) (string, error) {
	if strings.Contains(command, "docker pull new-image") {
		e.On("docker image inspect new-image", `[{"Config":{"ExposedPorts":{"8080/tcp":{}}}}]`, 0)
	}
	return e.Fake.Run(ctx, command)
}
func TestEngineImagePortSuggestionsPullMissingImage(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	f.ex.On("docker image inspect new-image", "missing", 1)
	service := NewService(f.store, f.env, engineConnector{imagePullExecutor{f.ex}}, nil)
	ports, err := service.ImagePorts(context.Background(), "srv1", "new-image")
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 || ports[0] != 8080 || !f.ex.Ran("docker pull new-image") {
		t.Fatal("missing image not pulled/inspected", ports)
	}
}

type releaseTimeoutExecutor struct{ *executor.Fake }

func (e releaseTimeoutExecutor) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	if strings.Contains(command, "run --rm --no-deps") {
		<-ctx.Done()
		return ctx.Err()
	}
	return e.Fake.Stream(ctx, command, sink)
}
func TestEngineReleaseCommandHonorsDeploymentTimeout(t *testing.T) {
	config := imageConfig()
	config.Release.Command = "wait-forever"
	f := newDeployFixture(t, config)
	f.deployer.connector = engineConnector{releaseTimeoutExecutor{f.ex}}
	dep := Deployment{ID: "timeout-release", ApplicationID: f.app.ID, Status: DeploymentQueued, Trigger: TriggerManual}
	if err := f.store.insertDeployment(context.Background(), dep); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	f.deployer.Run(ctx, dep.ID)
	result, err := f.store.GetDeployment(context.Background(), dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DeploymentFailed || !strings.Contains(result.Error, "PIER_DEPLOY_TIMEOUT") || f.ex.Ran("up --detach") {
		t.Fatalf("%s: %s", result.Status, result.Error)
	}
	if !f.ex.Ran("docker rm -f ") {
		t.Fatal("timed out release container not cleaned")
	}
}
