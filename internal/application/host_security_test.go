package application

import (
	"strings"
	"testing"
)

func TestHostBindsRequireDangerousPolicy(t *testing.T) {
	for _, source := range []string{"/etc", "/proc", "/data/pier", "/tmp/alias", "../host", "/run/docker.sock"} {
		c := imageConfig()
		c.Storages = []Storage{{Kind: StorageBind, Source: source, Target: "/mnt"}}
		if dockerOptionsAllowed(c, DockerOptionsPolicy{}) == nil {
			t.Fatalf("accepted %s", source)
		}
		if err := dockerOptionsAllowed(c, DockerOptionsPolicy{AllowDangerous: true}); err != nil {
			t.Fatal(err)
		}
	}
	c := imageConfig()
	c.Storages = []Storage{{Kind: StorageVolume, Source: "data", Target: "/data"}}
	if err := dockerOptionsAllowed(c, DockerOptionsPolicy{}); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewRevokedHostPolicyBeforeBuild(t *testing.T) {
	for _, fork := range []bool{false, true} {
		t.Run(map[bool]string{false: "own", true: "fork"}[fork], func(t *testing.T) {
			f, s, p := previewFixture(t)
			ctx := t.Context()
			_, err := p.UpdateSettings(ctx, f.app.ID, PreviewSettings{Enabled: true, AllowForks: true, DomainTemplate: "pr-{number}.preview.example.com"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.SetDockerOptionsPolicy(ctx, DockerOptionsPolicy{AllowDangerous: true})
			if err != nil {
				t.Fatal(err)
			}
			config := f.app.Config
			config.DockerOptions.Privileged = true
			app, err := s.Update(ctx, f.app.ID, UpdateInput{Name: f.app.Name, Config: config})
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.SetDockerOptionsPolicy(ctx, DockerOptionsPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.HandlePullRequest(ctx, app, ProviderGitHub, previewPayload("opened", fork))
			if err != nil {
				t.Fatal(err)
			}
			if claimed, err := f.store.claimDeployment(ctx, result.DeploymentID); err != nil || !claimed {
				t.Fatal(claimed, err)
			}
			f.deployer.Run(ctx, result.DeploymentID)
			dep, _ := f.store.GetDeployment(ctx, result.DeploymentID)
			if dep.Status != DeploymentFailed || !strings.Contains(dep.Error, "política") {
				t.Fatalf("%+v", dep)
			}
			if f.ex.Ran("git clone") || f.ex.Ran("docker build") || f.ex.Ran("docker compose") {
				t.Fatal("host execution before policy gate")
			}
			previews, err := p.List(ctx, app.ID)
			if err != nil || len(previews) != 1 {
				t.Fatal(previews, err)
			}
			manual, err := p.Redeploy(ctx, app.ID, previews[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if claimed, err := f.store.claimDeployment(ctx, manual.ID); err != nil || !claimed {
				t.Fatal(claimed, err)
			}
			f.deployer.Run(ctx, manual.ID)
			dep, _ = f.store.GetDeployment(ctx, manual.ID)
			if dep.Status != DeploymentFailed || !strings.Contains(dep.Error, "política") {
				t.Fatalf("manual: %+v", dep)
			}
			if f.ex.Ran("git clone") || f.ex.Ran("docker build") {
				t.Fatal("manual build before gate")
			}

		})
	}
}

func TestPreviewHostOptionsNeverSurviveIsolationOrOverlay(t *testing.T) {
	c := gitConfig()
	c.DockerOptions = DockerOptions{Privileged: true, PidMode: "host", NetworkMode: "host", Devices: []string{"/dev/alias"}, CapAdd: []string{"SYS_ADMIN", "NET_ADMIN"}}
	c.Storages = []Storage{{Kind: StorageBind, Source: "/tmp/alias", Target: "/host"}, {Kind: StorageVolume, Source: "data", Target: "/data"}}
	for _, fork := range []bool{false, true} {
		view := previewApplication(Application{Config: c}, Preview{Fork: fork})
		merged, _, err := overlayRepoConfig([]byte("port: 9090\n"), view.Config)
		if err != nil {
			t.Fatal(err)
		}
		isolatePreview(&merged, Preview{Fork: fork})
		o := merged.DockerOptions
		if o.Privileged || o.PidMode != "" || o.NetworkMode != "" || len(o.Devices)+len(o.CapAdd) > 0 || len(merged.Storages) != 1 {
			t.Fatalf("%+v", merged)
		}
	}
	for _, field := range []string{"docker_options: {privileged: true}", "storages: [{kind: bind, source: /etc, target: /host}]"} {
		if _, _, err := overlayRepoConfig([]byte(field), gitConfig()); err == nil {
			t.Fatal("host overlay accepted")
		}
	}
}

func TestAuthorizedProductionHostOptionsAreRemovedFromFullPreview(t *testing.T) {
	f, s, p := previewFixture(t)
	ctx := t.Context()
	_, err := s.SetDockerOptionsPolicy(ctx, DockerOptionsPolicy{AllowDangerous: true, AllowDevices: true})
	if err != nil {
		t.Fatal(err)
	}
	config := f.app.Config
	config.DockerOptions = DockerOptions{Privileged: true, PidMode: "host", Devices: []string{"/dev/alias"}, CapAdd: []string{"SYS_ADMIN"}}
	config.Storages = []Storage{{Kind: StorageBind, Source: "/tmp/alias", Target: "/host"}}
	app, err := s.Update(ctx, f.app.ID, UpdateInput{Name: f.app.Name, Config: config})
	if err != nil {
		t.Fatal(err)
	}
	f.ex.On("git clone", commitMarker+strings.Repeat("a", 40)+" test", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	f.ex.On("for name in", "pier.yaml", 0)
	result, err := p.HandlePullRequest(ctx, app, ProviderGitHub, previewPayload("opened", false))
	if err != nil {
		t.Fatal(err)
	}
	f.ex.Files["/data/pier/builds/"+result.DeploymentID+"/src/pier.yaml"] = []byte("port: 9090\n")
	if claimed, err := f.store.claimDeployment(ctx, result.DeploymentID); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	f.deployer.Run(ctx, result.DeploymentID)
	dep, _ := f.store.GetDeployment(ctx, result.DeploymentID)
	if dep.Status != DeploymentFinished {
		t.Fatal(dep.Error, f.logText(t, dep.ID))
	}
	previews, err := p.List(ctx, app.ID)
	if err != nil || len(previews) != 1 {
		t.Fatal(previews, err)
	}
	content := string(f.ex.Files["/data/pier/applications/"+previews[0].ID+"/docker-compose.yaml"])
	if content == "" {
		t.Fatal("missing preview compose")
	}
	for _, host := range []string{"privileged: true", "pid: host", "/dev/alias", "SYS_ADMIN", "/tmp/alias"} {
		if strings.Contains(content, host) {
			t.Fatalf("host option %s in compose: %s", host, content)
		}
	}
}
