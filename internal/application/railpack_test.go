package application

import (
	"context"
	"strings"
	"testing"

	"pier/internal/envvar"
	"pier/internal/platform/executor"
)

// installingExecutor reports the Railpack binary missing until the install
// container ran, like a fresh server.
type installingExecutor struct {
	*executor.Fake
	installed bool
}

func (e *installingExecutor) Run(ctx context.Context, command string) (string, error) {
	if strings.HasPrefix(command, "sha256sum") {
		e.Fake.Commands = append(e.Fake.Commands, command)
		if !e.installed {
			return "", &executor.ExitError{Command: command, Code: 1}
		}
		return railpackAssets["x86_64"].binarySHA256 + "  /data/pier/tools/railpack/v0.43.0/railpack\n", nil
	}
	return e.Fake.Run(ctx, command)
}

func (e *installingExecutor) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	if strings.Contains(command, "PIER_RAILPACK_URL") {
		e.installed = true
	}
	return e.Fake.Stream(ctx, command, sink)
}

func railpackJob(t *testing.T) (*job, *deployFixture) {
	t.Helper()
	config := gitConfig()
	config.Build.Pack = PackRailpack
	fixture := newDeployFixture(t, config)
	fixture.ex.On("uname -m", "x86_64\n", 0)
	j := sourceTestJob(fixture)
	j.app.Config.Build.InstallCommand = "pnpm install --frozen-lockfile"
	j.app.Config.Build.BuildCommand = "pnpm build"
	j.app.Config.Build.StartCommand = "node dist/server.js"
	j.env = []envvar.Variable{
		{Key: "PUBLIC_URL", Value: "https://public-value.example", BuildTime: true},
		{Key: "NPM_TOKEN", Value: "npm-secret-value", SecretBuild: true},
		{Key: "RUNTIME_ONLY", Value: "runtime-secret-value"},
	}
	return j, fixture
}

func TestRailpackInstallsPinnedReleaseAndBuildsWithFrontend(t *testing.T) {
	j, fixture := railpackJob(t)
	installer := &installingExecutor{Fake: fixture.ex}
	j.ex = installer
	if _, err := (railpackBuilder{}).build(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(fixture.ex.Commands, "\n")
	install := fixture.ex.Commands[indexOf(fixture.ex.Commands, "PIER_RAILPACK_URL")]
	for _, expected := range []string{
		"PIER_RAILPACK_URL=https://github.com/railwayapp/railpack/releases/download/v0.43.0/railpack-v0.43.0-x86_64-unknown-linux-musl.tar.gz",
		"PIER_RAILPACK_SHA256=248a8f60f36854aabed6124113631078a802de3461660855fc4de9c26065c1fb",
		"sha256sum -c -", "/data/pier/tools/railpack/v0.43.0:/out",
	} {
		if !strings.Contains(install, expected) {
			t.Errorf("install command lacks %q:\n%s", expected, install)
		}
	}
	prepare := fixture.ex.Commands[indexOf(fixture.ex.Commands, " prepare /app")]
	for _, expected := range []string{
		"--security-opt no-new-privileges", "/data/pier/tools/railpack/v0.43.0/railpack:/usr/local/bin/railpack:ro",
		"/build-env-test/src:/app:ro", "--plan-out /out/railpack-plan.json", "--info-out /out/railpack-info.json",
		"--build-cmd 'pnpm build'", "--start-cmd 'node dist/server.js'", "--env NPM_TOKEN", "--env PUBLIC_URL",
		"--env RAILPACK_INSTALL_CMD", "-e RAILPACK_INSTALL_CMD", ". /data/pier/builds/build-env-test/build.env.sh && docker run",
	} {
		if !strings.Contains(prepare, expected) {
			t.Errorf("prepare command lacks %q:\n%s", expected, prepare)
		}
	}
	if strings.Contains(prepare, "RUNTIME_ONLY") {
		t.Error("runtime-only variable reached the build")
	}
	build := fixture.ex.Commands[indexOf(fixture.ex.Commands, "docker buildx build")]
	for _, expected := range []string{
		"BUILDKIT_SYNTAX=" + railpackFrontend, "cache-key=" + j.app.ID, "secrets-hash=",
		"--file /data/pier/builds/build-env-test/railpack/railpack-plan.json",
		"--secret id=NPM_TOKEN,env=NPM_TOKEN", "--secret id=PUBLIC_URL,env=PUBLIC_URL",
		"--secret id=RAILPACK_INSTALL_CMD,env=RAILPACK_INSTALL_CMD",
	} {
		if !strings.Contains(build, expected) {
			t.Errorf("build command lacks %q:\n%s", expected, build)
		}
	}
	if strings.Contains(build, "--build-arg PUBLIC_URL") || strings.Contains(build, "--target") {
		t.Errorf("railpack build must pass variables only as secrets:\n%s", build)
	}
	for _, value := range []string{"npm-secret-value", "https://public-value.example", "runtime-secret-value", "pnpm install"} {
		if strings.Contains(commands, value) {
			t.Errorf("value %q leaked into argv", value)
		}
	}
	if !strings.Contains(railpackFrontend, "@sha256:") || !strings.HasSuffix(strings.Split(railpackFrontend, "@")[0], ":v"+RailpackVersion) {
		t.Errorf("frontend must be pinned to the same release by digest: %s", railpackFrontend)
	}
}

func TestRailpackSkipsInstallWhenVerifiedBinaryExists(t *testing.T) {
	j, fixture := railpackJob(t)
	j.ex = &installingExecutor{Fake: fixture.ex, installed: true}
	if _, err := (railpackBuilder{}).build(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if fixture.ex.Ran("PIER_RAILPACK_URL") {
		t.Fatal("verified binary must not be downloaded again")
	}
}

func TestRailpackRefusesBinaryWithWrongChecksum(t *testing.T) {
	j, fixture := railpackJob(t)
	fixture.ex.On("sha256sum", "0000  railpack", 0)
	_, err := (railpackBuilder{}).build(context.Background(), j)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered binary must stop the deploy, got %v", err)
	}
	if fixture.ex.Ran(" prepare /app") {
		t.Fatal("an unverified binary was executed")
	}
}

func TestRailpackRefusesUnknownArchitecture(t *testing.T) {
	j, fixture := railpackJob(t)
	fixture.ex.On("uname -m", "riscv64\n", 0)
	if _, err := (railpackBuilder{}).build(context.Background(), j); err == nil || !strings.Contains(err.Error(), "riscv64") {
		t.Fatalf("unknown architecture must fail clearly, got %v", err)
	}
}

func TestRailpackArm64UsesItsOwnAsset(t *testing.T) {
	j, fixture := railpackJob(t)
	fixture.ex.On("uname -m", "aarch64\n", 0)
	asset, err := j.railpackAsset(context.Background())
	if err != nil || asset.archiveSHA256 != "732ece14eb4f4676a43ebba19431f954edf7b130acb76159ef0f2ff86ac42ac9" {
		t.Fatalf("arm64 asset = %+v, %v", asset, err)
	}
}

func TestRailpackPackIsAcceptedByConfigAndPierYAML(t *testing.T) {
	config := gitConfig()
	config.Build.Pack = PackRailpack
	if err := config.Validate(); err != nil {
		t.Fatalf("railpack config refused: %v", err)
	}
	merged, _, err := overlayRepoConfig([]byte("build:\n  pack: railpack\n"), gitConfig())
	if err != nil || merged.Build.Pack != PackRailpack {
		t.Fatalf("pier.yaml railpack: %v %v", merged.Build.Pack, err)
	}
	spec := releaseSpec{App: Application{ID: "app", Config: config}, Image: "img", ContainerName: "app-1", DeploymentID: "d"}
	spec.App.Config.Build.StartCommand = "node server.js"
	file, err := composeFor(spec)
	if err != nil {
		t.Fatal(err)
	}
	if file.Services["app-1"].Command != nil {
		t.Fatal("railpack bakes the start command into the image; compose must not override it")
	}
}
