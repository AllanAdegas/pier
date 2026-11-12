package application

import (
	"context"
	"fmt"
	"path"
	"strings"
	"testing"

	"pier/internal/envvar"
	"pier/internal/platform/pubsub"
)

func setVariable(t *testing.T, fixture *deployFixture, key, value string, buildTime bool) {
	t.Helper()
	owner := envvar.Owner{Type: envvar.OwnerApplication, ID: fixture.app.ID}
	if err := fixture.env.Set(context.Background(), owner, key, value, buildTime); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func TestNixpacksBuildsOnlySeeBuildTimeVariables(t *testing.T) {
	config := DefaultConfig(PackNixpacks)
	config.Source.Repository = "https://github.com/acme/app"
	config.Health.Enabled = true
	fixture := newDeployFixture(t, config)
	setVariable(t, fixture, "NPM_TOKEN", "build-only-value", true)
	setVariable(t, fixture, "DATABASE_URL", "runtime-only-value", false)
	fixture.ex.On("nixpacks plan", `{"variables":{}}`, 0)
	// Fake generated output at the known fixture deployment's context path.
	fixture.ex.Files[path.Join("/data/pier/builds/depxxxa/src/.nixpacks/Dockerfile")] = []byte("FROM node:22\nRUN npm ci\n")
	fixture.ex.On("git clone --depth", "PIER_COMMIT 0123456789abcdef msg", 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	fixture.deploy(t)

	build := fixture.ex.Commands[indexOf(fixture.ex.Commands, "nixpacks build")]
	if !strings.Contains(build, "--env NPM_TOKEN") || strings.Contains(build, "build-only-value") {
		t.Errorf("the build-time variable is missing from the build:\n%s", build)
	}
	privateEnv := string(fixture.ex.Files["/data/pier/builds/"+"depxxxa"+"/build.env.sh"])
	if !strings.Contains(privateEnv, "build-only-value") || strings.Contains(privateEnv, "runtime-only-value") {
		t.Fatalf("invalid private build environment: %q", privateEnv)
	}
	if strings.Contains(build, "runtime-only-value") {
		t.Errorf("a runtime-only variable reached the build:\n%s", build)
	}
}

func TestMultilineSecretsAndFailureReasonsAreMasked(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	key := "-----BEGIN KEY-----\nline-one-secret\nline-two-secret\n-----END KEY-----"
	setVariable(t, fixture, "PRIVATE_KEY", key, false)
	setVariable(t, fixture, "TOKEN", "plain-secret-value", false)
	// The tool prints one line of the key, then fails quoting the token.
	fixture.ex.On("docker pull", "line-two-secret", 0)
	fixture.ex.On("docker compose", "error: auth plain-secret-value rejected", 1)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	deployment := fixture.deploy(t)

	everything := fixture.logText(t, deployment.ID) + "\n" + deployment.Error
	for _, event := range fixture.publisher.events {
		everything += "\n" + event.Message
	}
	for _, secret := range []string{"line-two-secret", "plain-secret-value"} {
		if strings.Contains(everything, secret) {
			t.Errorf("%q leaked into the log, the failure or a notification:\n%s", secret, everything)
		}
	}
}

func TestOneDeploymentKeepsABoundedAmountOfOutput(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	log := &deployLog{store: fixture.store, hub: pubsub.NewHub[LogEntry](), deploymentID: "flood"}
	if err := fixture.store.insertDeployment(context.Background(),
		Deployment{ID: "flood", ApplicationID: fixture.app.ID, Status: DeploymentQueued, Trigger: TriggerManual}); err != nil {
		t.Fatal(err)
	}
	for line := range maxOutputLines + 500 {
		log.Output(fmt.Sprintf("y %d", line))
	}
	log.Info("still recorded")
	entries, err := fixture.store.Logs(context.Background(), "flood", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxOutputLines+2 {
		t.Errorf("%d entries kept, want %d output lines, one notice and the final message", len(entries), maxOutputLines+2)
	}
	if last := entries[len(entries)-1].Line; last != "still recorded" {
		t.Errorf("messages after the cap must still be written, last = %q", last)
	}
}

func TestTheDeployKeyIsRemovedRightAfterTheClone(t *testing.T) {
	config := gitConfig()
	config.Source.Repository = "git@github.com:acme/app.git"
	config.Source.PrivateKeyID = "key1"
	config.Health.Enabled = true
	fixture := newDeployFixture(t, config)
	fixture.deployer.keys = staticKeys{"key1": "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----"}
	fixture.ex.On("git clone --depth", "PIER_COMMIT 0123456789abcdef msg", 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	fixture.deploy(t)

	removed := indexOf(fixture.ex.Commands, "rm -f /data/pier/builds/")
	build := indexOf(fixture.ex.Commands, "docker buildx build")
	if removed < 0 || build < 0 || removed > build {
		t.Fatalf("the key must be removed before the build (rm at %d, build at %d):\n%s",
			removed, build, strings.Join(fixture.ex.Commands, "\n"))
	}
	if !strings.Contains(fixture.ex.Commands[removed], "deploy_key") {
		t.Errorf("unexpected removal: %s", fixture.ex.Commands[removed])
	}
}

func TestABuildDirectoryLinkedOutOfTheRepositoryIsRefused(t *testing.T) {
	config := gitConfig()
	config.Build.BaseDirectory = "site"
	config.Health.Enabled = true
	fixture := newDeployFixture(t, config)
	fixture.ex.On("git clone --depth", "PIER_COMMIT 0123456789abcdef msg", 0)
	fixture.ex.On("readlink -f", "outside", 0)

	deployment := fixture.deploy(t)

	if deployment.Status != DeploymentFailed || !strings.Contains(deployment.Error, "link simbólico") {
		t.Fatalf("status = %s, error = %q", deployment.Status, deployment.Error)
	}
	if fixture.ex.Ran("docker build") {
		t.Error("nothing may be built from outside the repository")
	}
}

func TestClonesShowRepositoriesWithoutCredentials(t *testing.T) {
	config := gitConfig()
	config.Source.Repository = "https://user:ghp_secrettoken@github.com/acme/app"
	config.Health.Enabled = true
	fixture := newDeployFixture(t, config)
	fixture.ex.On("git clone --depth", "PIER_COMMIT 0123456789abcdef msg", 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	deployment := fixture.deploy(t)

	if log := fixture.logText(t, deployment.ID); strings.Contains(log, "ghp_secrettoken") {
		t.Errorf("the repository token leaked into the log:\n%s", log)
	}
}

func TestPinnedCommitsUpdateTheirSubmodules(t *testing.T) {
	if !strings.Contains(cloneScript, "git submodule update --init --recursive") ||
		strings.Index(cloneScript, "git submodule update") < strings.Index(cloneScript, "git checkout --quiet FETCH_HEAD") {
		t.Errorf("submodules must follow the pinned commit, after its checkout:\n%s", cloneScript)
	}
}

type staticKeys map[string]string

func (k staticKeys) Material(_ context.Context, id string) (string, error) { return k[id], nil }

func TestADeploymentPastItsTimeLimitFails(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	deployment := Deployment{ID: "slow", ApplicationID: fixture.app.ID, Status: DeploymentQueued, Trigger: TriggerManual}
	ctx := context.Background()
	if err := fixture.store.insertDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.claimDeployment(ctx, deployment.ID); err != nil {
		t.Fatal(err)
	}
	expired, cancel := context.WithTimeout(ctx, 0)
	defer cancel()

	fixture.deployer.Run(expired, deployment.ID)

	finished, err := fixture.store.GetDeployment(ctx, deployment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != DeploymentFailed || !strings.Contains(finished.Error, "PIER_DEPLOY_TIMEOUT") {
		t.Errorf("status = %s, error = %q", finished.Status, finished.Error)
	}
}
