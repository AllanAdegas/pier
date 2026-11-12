package application

import (
	"context"
	"strings"
	"testing"

	"pier/internal/docker"
	"pier/internal/platform/executor"
)

func processConfig() Config {
	config := imageConfig()
	config.Limits = Limits{CPUs: "1", Memory: "1g"}
	config.Processes = []Process{
		{Name: "web", Replicas: 2},
		{Name: "worker", Command: "bundle exec sidekiq", Limits: Limits{Memory: "2g"}},
	}
	return config
}

func TestProcessesShareTheImageButOnlyWebIsPublished(t *testing.T) {
	config := processConfig()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	file, err := composeFor(releaseSpec{App: Application{ID: "app1", Config: config}, Image: "img", ContainerName: "app-dep", DeploymentID: "dep"})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for name := range file.Services {
		names = append(names, name)
	}
	if len(file.Services) != 3 || file.Services["app-dep"] == nil || file.Services["app-dep-web-2"] == nil || file.Services["app-dep-worker"] == nil {
		t.Fatalf("services = %v", names)
	}
	web, second, worker := file.Services["app-dep"], file.Services["app-dep-web-2"], file.Services["app-dep-worker"]
	for key, value := range web.Labels {
		if strings.HasPrefix(key, "traefik.") && second.Labels[key] != value {
			t.Errorf("web replicas must share router and service labels: %s", key)
		}
	}
	if web.Labels["traefik.enable"] != "true" || web.Healthcheck == nil || web.Networks["pier"].Aliases[0] != "app1" {
		t.Errorf("web replica not published: %+v", web)
	}
	if _, published := worker.Labels["traefik.enable"]; published || worker.Healthcheck != nil || len(worker.Ports) != 0 ||
		len(worker.Networks["pier"].Aliases) != 0 {
		t.Errorf("workers get no routes, ports, health check or app alias: %+v", worker)
	}
	if worker.Image != "img" || strings.Join(worker.Command, " ") != "sh -c bundle exec sidekiq" || worker.MemLimit != "2g" || worker.CPUs != "1" {
		t.Errorf("worker = %+v", worker)
	}
	if worker.Labels[LabelProcess] != "worker" || worker.Environment["PIER_PROCESS"] != "worker" || web.Labels[LabelProcess] != "web" {
		t.Error("each container must say its process")
	}
}

func TestApplicationsWithoutProcessesKeepASingleContainer(t *testing.T) {
	config := imageConfig()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	file, err := composeFor(releaseSpec{App: Application{ID: "app1", Config: config}, Image: "img", ContainerName: "app-dep", DeploymentID: "dep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Services) != 1 || file.Services["app-dep"] == nil || file.Services["app-dep"].Labels["traefik.enable"] != "true" {
		t.Errorf("services = %+v", file.Services)
	}
}

func TestProcessesAreValidated(t *testing.T) {
	cases := map[string][]Process{
		"bad name":       {{Name: "Web!"}},
		"duplicate":      {{Name: "web"}, {Name: "web"}},
		"no command":     {{Name: "web"}, {Name: "worker"}},
		"replicas":       {{Name: "web", Replicas: 11}},
		"limits":         {{Name: "web", Limits: Limits{Memory: "lots"}}},
		"no web+domains": {{Name: "worker", Command: "run"}},
	}
	for name, processes := range cases {
		t.Run(name, func(t *testing.T) {
			config := imageConfig()
			config.Processes = processes
			if fieldsOf(t, config.Validate())["processes"] == "" {
				t.Error("expected a processes error")
			}
		})
	}
	config := imageConfig()
	config.Network.PortMappings = []string{"8080:80"}
	config.Processes = []Process{{Name: "web", Replicas: 2}}
	if fieldsOf(t, config.Validate())["processes"] == "" {
		t.Error("host ports with several web replicas must be refused")
	}
	config = imageConfig()
	config.Network.Domains = nil
	config.Processes = []Process{{Name: "worker", Command: "run"}}
	if err := config.Validate(); err != nil || config.Processes[0].Replicas != 1 {
		t.Errorf("a worker-only application is valid: %v %+v", err, config.Processes)
	}
}

func TestEveryReplicaIsReadyBeforeTheOldVersionRetires(t *testing.T) {
	fixture := newDeployFixture(t, processConfig())
	fixture.ex.On("docker ps", previousContainer, 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	deployment := fixture.deploy(t)

	if deployment.Status != DeploymentFinished {
		t.Fatalf("%s %s\n%s", deployment.Status, deployment.Error, fixture.logText(t, deployment.ID))
	}
	commands := fixture.ex.Commands
	stop := indexOf(commands, "docker stop")
	for _, suffix := range []string{"", "-web-2", "-worker"} {
		probe := -1
		for index, command := range commands {
			if strings.Contains(command, "docker inspect --format '{{.State.Status}}") && strings.HasSuffix(strings.TrimSpace(command), "-"+docker.ShortID(deployment.ID)+suffix) {
				probe = index
			}
		}
		if probe == -1 || stop == -1 || probe > stop {
			t.Errorf("replica %q must be ready before the old version stops (probe=%d stop=%d)", suffix, probe, stop)
		}
	}
}

func TestAFailingWorkerDiscardsTheWholeNewVersion(t *testing.T) {
	fixture := newDeployFixture(t, processConfig())
	fixture.ex.On("docker ps", previousContainer, 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	fixture.ex.On("-worker", "exited|none|0", 0)

	deployment := fixture.deploy(t)

	if deployment.Status != DeploymentFailed {
		t.Fatalf("status = %s", deployment.Status)
	}
	if fixture.ex.Ran("docker stop") || fixture.ex.Ran("docker rm -f oldid") {
		t.Error("the previous version must keep running")
	}
	discard := commandsWith(fixture.ex, "docker rm -f")
	if len(discard) == 0 || !strings.Contains(discard[len(discard)-1], "-web-2") || !strings.Contains(discard[len(discard)-1], "-worker") {
		t.Errorf("all new replicas must be discarded: %v", discard)
	}
}

func commandsWith(ex *executor.Fake, fragment string) []string {
	found := []string{}
	for _, command := range ex.Commands {
		if strings.Contains(command, fragment) {
			found = append(found, command)
		}
	}
	return found
}

func TestContainersNameTheirProcess(t *testing.T) {
	ex := executor.NewFake()
	ex.On(`{{.Label "pier.process"}}`, "aaa\tworker\nbbb\nccc\tweb", 0)
	processes := containerProcesses(context.Background(), ex, "app1")
	listed := withProcesses([]docker.Container{{ID: "aaa"}, {ID: "bbb"}, {ID: "zzz"}}, processes)
	if listed[0].Process != "worker" || listed[1].Process != "web" || listed[2].Process != "web" {
		t.Errorf("listed = %+v", listed)
	}
	running := []docker.Container{{ID: "aaa", State: "running"}, {ID: "ccc", State: "running"}}
	if got := runningWebContainer(running, processes); got != "ccc" {
		t.Errorf("hooks must run in the web container, got %q", got)
	}
}
