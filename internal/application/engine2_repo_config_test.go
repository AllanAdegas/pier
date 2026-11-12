package application

import (
	"strings"
	"testing"
)

func TestPierYAMLOverlaysOnlyTheFieldsItSets(t *testing.T) {
	stored := gitConfig()
	stored.Network.Domains = []string{"https://app.example.com"}
	stored.Health.Path = "/stored"
	content := []byte(`
build:
  pack: dockerfile
  dockerfile: docker/Dockerfile.prod
port: 8080
health:
  enabled: true
  path: /healthz
release:
  command: ./bin/migrate
limits:
  cpus: "0.5"
  memory: 512m
watch_paths: ["apps/api/**"]
`)
	merged, fields, err := overlayRepoConfig(content, stored)
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}
	if merged.Network.Port != 8080 || merged.Build.DockerfilePath != "docker/Dockerfile.prod" || !merged.Health.Enabled ||
		merged.Health.Path != "/healthz" || merged.Release.Command != "./bin/migrate" || merged.Limits.Memory != "512m" ||
		merged.Source.WatchPaths[0] != "apps/api/**" {
		t.Errorf("merged = %+v", merged)
	}
	if merged.Network.Domains[0] != "https://app.example.com" || merged.Source.Repository != stored.Source.Repository {
		t.Error("fields absent from the file must keep the stored values")
	}
	if stored.Health.Path != "/stored" || stored.Network.Port != 3000 {
		t.Error("the stored config must not change")
	}
	want := "build.pack,build.dockerfile,port,health.enabled,health.path,release.command,limits,watch_paths"
	if strings.Join(fields, ",") != want {
		t.Errorf("fields = %v", fields)
	}
}

func TestPierYAMLErrorsNameTheLine(t *testing.T) {
	cases := map[string]struct{ content, want string }{
		"panel only":      {"port: 80\ndomains: [x.example.com]\n", "linha 2: \"domains\" só pode ser configurado no painel"},
		"source":          {"source:\n  repository: https://evil.example\n", "linha 1: \"source\" só pode"},
		"unknown":         {"build:\n  pack: dockerfile\n  dockerfil: x\n", "linha 3: campo desconhecido \"dockerfil\""},
		"wrong type":      {"port: abc\n", "linha 1: tipo de valor inválido"},
		"syntax":          {"build:\n  pack: [\n", "linha"},
		"not a map":       {"- port: 80\n", "linha 1: o arquivo deve ser um mapa"},
		"image pack":      {"\nbuild:\n  pack: image\n", "linha 3: build.pack aceita"},
		"invalid value":   {"port: 3000\nhealth:\n  path: no-slash\n", "linha 3 (health.path)"},
		"nested unknown":  {"processes:\n  - name: web\n    port: 80\n", "linha 3: campo desconhecido \"port\""},
		"server override": {"server: other\n", "\"server\" só pode"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := overlayRepoConfig([]byte(c.content), gitConfig())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want %q", err, c.want)
			}
		})
	}
	if _, fields, err := overlayRepoConfig([]byte("# only comments\n"), gitConfig()); err != nil || len(fields) != 0 {
		t.Errorf("an empty file must be accepted: %v %v", fields, err)
	}
	if _, _, err := overlayRepoConfig([]byte(strings.Repeat("#", maxRepoConfigBytes+1)), gitConfig()); err == nil {
		t.Error("oversized file accepted")
	}
}

func TestADeploymentUsesPierYAMLFromTheBaseDirectory(t *testing.T) {
	config := gitConfig()
	config.Build.BaseDirectory = "/services/api"
	f := newDeployFixture(t, config)
	f.ex.On("git clone --depth", "PIER_COMMIT 0123456789abcdef msg", 0)
	f.ex.On("for name in", "pier.yaml", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	deployment := Deployment{ID: "depyaml", ApplicationID: f.app.ID, Status: DeploymentQueued, Trigger: TriggerManual}
	file := "/data/pier/builds/" + deployment.ID + "/src/services/api/pier.yaml"
	f.ex.Files[file] = []byte("port: 9090\nrelease:\n  command: ./migrate\n")
	finished := runDeployment(t, f, deployment)

	if finished.Status != DeploymentFinished {
		t.Fatalf("%s %s\n%s", finished.Status, finished.Error, f.logText(t, finished.ID))
	}
	if !strings.Contains(f.logText(t, finished.ID), "pier.yaml aplicada a este deploy: port, release.command") {
		t.Errorf("log:\n%s", f.logText(t, finished.ID))
	}
	compose := string(f.ex.Files["/data/pier/applications/"+f.app.ID+"/docker-compose.yaml"])
	if !strings.Contains(compose, "PORT: \"9090\"") || !f.ex.Ran("./migrate") {
		t.Errorf("the file was not applied:\n%s", compose)
	}
	stored, _ := f.store.Get(t.Context(), f.app.ID)
	if stored.Config.Network.Port != 3000 || stored.Config.Release.Command != "" {
		t.Error("pier.yaml must not change the stored configuration")
	}

	f.ex.Files[strings.Replace(file, "depyaml", "depyaml2", 1)] = []byte("domains: [evil.example.com]\n")
	failed := runDeployment(t, f, Deployment{ID: "depyaml2", ApplicationID: f.app.ID, Status: DeploymentQueued, Trigger: TriggerManual})
	if failed.Status != DeploymentFailed || !strings.Contains(failed.Error, "só pode ser configurado no painel") {
		t.Errorf("an invalid file must fail the deployment: %s %s", failed.Status, failed.Error)
	}
}

func runDeployment(t *testing.T, f *deployFixture, deployment Deployment) Deployment {
	t.Helper()
	ctx := t.Context()
	if err := f.store.insertDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	if claimed, err := f.store.claimDeployment(ctx, deployment.ID); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	f.deployer.Run(ctx, deployment.ID)
	finished, err := f.store.GetDeployment(ctx, deployment.ID)
	if err != nil {
		t.Fatal(err)
	}
	return finished
}
