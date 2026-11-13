package application

import (
	"context"
	"pier/internal/docker"
	"pier/internal/platform/executor"
	"strings"
	"testing"
)

func TestEngineExtrasValidation(t *testing.T) {
	for _, policy := range []string{"", "no", "always", "unless-stopped", "on-failure"} {
		c := gitConfig()
		c.Runtime = Runtime{RestartPolicy: policy, StopGraceSeconds: 300}
		c.Build.Target = "production-v2"
		if err := c.Validate(); err != nil {
			t.Fatalf("valid config: %v", err)
		}
	}
	for _, tc := range []struct {
		field  string
		mutate func(*Config)
	}{
		{"build.target", func(c *Config) { c.Build.Target = "--network=host" }},
		{"build.target", func(c *Config) { c.Build.Target = "x; echo unsafe" }},
		{"build.target", func(c *Config) { c.Build.Pack = PackStatic; c.Build.Target = "stage" }},
		{"runtime.restart_policy", func(c *Config) { c.Runtime.RestartPolicy = "on-failure:999999999" }},
		{"runtime.stop_grace_seconds", func(c *Config) { c.Runtime.StopGraceSeconds = -1 }},
		{"runtime.stop_grace_seconds", func(c *Config) { c.Runtime.StopGraceSeconds = 301 }},
	} {
		c := gitConfig()
		tc.mutate(&c)
		if fieldsOf(t, c.Validate())[tc.field] == "" {
			t.Fatalf("expected %s validation", tc.field)
		}
	}
}

func TestEngineExtrasComposeAppliesToAllProcesses(t *testing.T) {
	c := imageConfig()
	c.Runtime = Runtime{RestartPolicy: "on-failure", StopGraceSeconds: 45}
	c.Processes = []Process{{Name: "web", Replicas: 2}, {Name: "worker", Command: "queue", Replicas: 1}}
	f, err := composeFor(releaseSpec{App: Application{ID: "app", Config: c}, Image: "nginx:1.27", ContainerName: "app-deploy"})
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range f.Services {
		if service.Restart != "on-failure" || service.StopGracePeriod != "45s" {
			t.Fatalf("lifecycle not applied: %+v", service)
		}
	}
	encoded, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "stop_grace_period: 45s") {
		t.Fatalf("missing compose field: %s", encoded)
	}
	c.Runtime = Runtime{}
	f, err = composeFor(releaseSpec{App: Application{ID: "app", Config: c}, Image: "nginx:1.27", ContainerName: "app-deploy"})
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range f.Services {
		if service.Restart != "unless-stopped" || service.StopGracePeriod != "30s" {
			t.Fatal("old defaults changed")
		}
	}
}

func TestEngineExtrasDockerBuildUsesNamedStage(t *testing.T) {
	c := gitConfig()
	c.Build.Target = "production"
	fixture := newDeployFixture(t, c)
	j := sourceTestJob(fixture)
	strategy, _ := builderFor(PackDockerfile)
	if _, err := strategy.build(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if !fixture.ex.Ran("--target production") {
		t.Fatalf("missing build target: %v", fixture.ex.Commands)
	}
}

func TestEngineExtrasRepositoryOverlayAndValidation(t *testing.T) {
	c := gitConfig()
	updated, _, err := overlayRepoConfig([]byte("build:\n  target: production\nruntime:\n  restart_policy: on-failure\n  stop_grace_seconds: 35\n"), c)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Build.Target != "production" || updated.Runtime.RestartPolicy != "on-failure" || updated.Runtime.StopGraceSeconds != 35 {
		t.Fatalf("overlay lost settings %+v", updated)
	}
	if c.Build.Target != "" || c.Runtime.RestartPolicy != "" {
		t.Fatal("overlay mutated stored config")
	}
	if _, _, err := overlayRepoConfig([]byte("runtime:\n  stop_grace_seconds: 301\n"), c); err == nil {
		t.Fatal("invalid repository settings accepted")
	}
}

func TestEngineExtrasStopsUsingTheContainersOwnGracePeriod(t *testing.T) {
	ex := &executor.Fake{}
	containers := []docker.Container{{ID: "oldrelease"}}
	if err := stopApplicationContainers(context.Background(), ex, containers); err != nil {
		t.Fatal(err)
	}
	if err := restartApplicationContainers(context.Background(), ex, containers); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(ex.Commands, "\n")
	if strings.Contains(commands, "--time") || !strings.Contains(commands, "docker stop oldrelease") || !strings.Contains(commands, "docker restart oldrelease") {
		t.Fatalf("override container timeout: %s", commands)
	}
}
