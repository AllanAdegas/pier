package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"pier/internal/envvar"
	"pier/internal/platform/executor"
	"pier/internal/platform/pubsub"
	"pier/internal/server"
)

func sourceTestJob(fixture *deployFixture) *job {
	return &job{app: fixture.app, deployment: Deployment{ID: "build-env-test"}, server: server.Server{ID: "srv1", DataDir: "/data/pier"}, ex: fixture.ex,
		log: &deployLog{store: fixture.store, hub: pubsub.NewHub[LogEntry](), deploymentID: "build-env-test"}}
}

func TestBuildEnvSecretsNeverBecomeBuildArgsOrCommandValues(t *testing.T) {
	fixture := newDeployFixture(t, gitConfig())
	j := sourceTestJob(fixture)
	j.env = []envvar.Variable{
		{Key: "PLAIN", Value: "public-value-for-test", BuildTime: true},
		{Key: "TOKEN", Value: "private-value-for-test\nsecond-line", BuildTime: true, SecretBuild: true},
		{Key: "RUNTIME", Value: "runtime-value-for-test"},
	}
	fixture.ex.On("df -Pk", "10485760", 0)
	strategy, err := builderFor(PackDockerfile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.build(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(fixture.ex.Commands, "\n")
	for _, value := range []string{"public-value-for-test", "private-value-for-test", "runtime-value-for-test"} {
		if strings.Contains(commands, value) {
			t.Errorf("value %s appeared in argv", value)
		}
	}
	if !strings.Contains(commands, "--build-arg PLAIN") || !strings.Contains(commands, "--secret id=TOKEN,env=TOKEN") || strings.Contains(commands, "--build-arg TOKEN") || strings.Contains(commands, "RUNTIME") {
		t.Fatalf("incorrect build arguments: %s", commands)
	}
	env := string(fixture.ex.Files[path.Join(j.server.BuildDir(j.deployment.ID), "build.env.sh")])
	if !strings.Contains(env, "public-value-for-test") || !strings.Contains(env, "private-value-for-test\nsecond-line") || strings.Contains(env, "runtime-value-for-test") {
		t.Fatalf("incorrect build environment: %s", env)
	}
	if !strings.Contains(commands, "cpu-quota=100000") || !strings.Contains(commands, "memory=2g") || !strings.Contains(commands, "--keep-storage 10GB -f") {
		t.Fatalf("missing build guard rails: %s", commands)
	}
}

func TestBuildEnvReservedClientVariablesAndFailedCleanupAreRejected(t *testing.T) {
	fixture := newDeployFixture(t, gitConfig())
	j := sourceTestJob(fixture)
	for _, key := range []string{"DOCKER_HOST", "PATH", "LD_PRELOAD", "BUILDX_BUILDER"} {
		j.env = []envvar.Variable{{Key: key, Value: "x", BuildTime: true}}
		if err := j.streamBuildEnvironment(context.Background(), []string{"docker", "buildx", "build"}, j.log.Output); err == nil {
			t.Errorf("accepted reserved key %s", key)
		}
	}
	j.env = []envvar.Variable{{Key: "TOKEN", Value: "safe", SecretBuild: true}}
	fixture.ex.On("rm -f", "permission denied", 1)
	if err := j.streamBuildEnvironment(context.Background(), []string{"docker", "buildx", "build"}, j.log.Output); err == nil {
		t.Fatal("ignored credential cleanup failure")
	}
}

func TestBuildEnvDiskGuardFailsBeforeBuilderStarts(t *testing.T) {
	for i, output := range []string{"", "not-a-number", "1048576"} {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			fixture := newDeployFixture(t, gitConfig())
			j := sourceTestJob(fixture)
			fixture.ex.On("df -Pk", output, 0)
			strategy, _ := builderFor(PackDockerfile)
			if _, err := strategy.build(context.Background(), j); err == nil {
				t.Fatal("build accepted insufficient/unreadable free disk")
			}
			if fixture.ex.Ran("docker buildx") {
				t.Fatal("builder started before disk guard")
			}
		})
	}
	fixture := newDeployFixture(t, gitConfig())
	j := sourceTestJob(fixture)
	fixture.ex.On("df -Pk", "2097152", 0)
	if err := j.checkBuildDisk(context.Background()); err != nil {
		t.Fatalf("exact minimum should be allowed: %v", err)
	}
}

func TestBuildEnvQueueSerializesPerServerAndCancelsWaiters(t *testing.T) {
	ctx := context.Background()
	first, err := acquireBuild(ctx, "queue-test-one", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { first() }()
	other, err := acquireBuild(ctx, "queue-test-two", nil)
	if err != nil {
		t.Fatal(err)
	}
	other()
	expired, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := acquireBuild(expired, "queue-test-one", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued build ignored cancellation: %v", err)
	}
	acquired := make(chan func(), 1)
	go func() {
		release, err := acquireBuild(ctx, "queue-test-one", nil)
		if err == nil {
			acquired <- release
		}
	}()
	select {
	case release := <-acquired:
		release()
		t.Fatal("second build ran concurrently")
	case <-time.After(20 * time.Millisecond):
	}
	first()
	// Prevent the deferred release from releasing the token twice.
	first = func() {}
	select {
	case release := <-acquired:
		release()
	case <-time.After(time.Second):
		t.Fatal("queued build did not start after release")
	}
}

func TestBuildEnvNixpacksUsesOnlyBuildKitSecretMounts(t *testing.T) {
	fixture := newDeployFixture(t, gitConfig())
	j := sourceTestJob(fixture)
	j.app.Config.Build.Pack = PackNixpacks
	j.env = []envvar.Variable{{Key: "PUBLIC_URL", Value: "https://public.example", BuildTime: true}, {Key: "NPM_TOKEN", Value: "npm-test-secret", SecretBuild: true}, {Key: "RUNTIME", Value: "runtime-test-secret"}}
	fixture.ex.On("nixpacks plan", `{"variables":{"NODE_ENV":"production","PUBLIC_URL":"https://public.example"}}`, 0)
	dockerfile := path.Join(j.contextDir(), ".nixpacks", "Dockerfile")
	fixture.ex.Files[dockerfile] = []byte("FROM node\nARG NODE_ENV PUBLIC_URL NPM_TOKEN\nENV NODE_ENV=$NODE_ENV PUBLIC_URL=$PUBLIC_URL NPM_TOKEN=$NPM_TOKEN\nRUN npm ci\nRUN npm run build\nCMD npm start\n")
	if _, err := (nixpacksBuilder{}).build(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(fixture.ex.Commands, "\n")
	generated := string(fixture.ex.Files[dockerfile])
	if strings.Contains(generated, "ARG NODE_ENV PUBLIC_URL NPM_TOKEN") || strings.Contains(generated, "NPM_TOKEN=$") || strings.Count(generated, "--mount=type=secret,id=NPM_TOKEN,env=NPM_TOKEN") != 2 {
		t.Fatalf("secret not confined to RUN mounts:\n%s", generated)
	}
	for _, value := range []string{"npm-test-secret", "runtime-test-secret", "https://public.example"} {
		if strings.Contains(commands, value) || strings.Contains(generated, value) {
			t.Errorf("%s leaked in generated content or argv", value)
		}
	}
	for _, command := range fixture.ex.Commands {
		if strings.Contains(command, "nixpacks") && strings.Contains(command, "--env NPM_TOKEN") {
			t.Fatal("secret sent to Nixpacks as a plain variable")
		}
	}
	if !strings.Contains(commands, "--secret id=NPM_TOKEN,env=NPM_TOKEN") || !strings.Contains(commands, "--build-arg NODE_ENV") || strings.Contains(commands, "--build-arg NPM_TOKEN") {
		t.Fatalf("incorrect final builder command: %s", commands)
	}
}

func TestBuildEnvStaticPackExcludesGitBeforeAnyLayer(t *testing.T) {
	dockerfile, err := staticDockerfile(Build{PublishDirectory: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dockerfile, "COPY --exclude=.git --exclude=**/.git") || strings.Contains(dockerfile, "RUN rm -rf") {
		t.Fatalf("git must be excluded before COPY, not deleted from a later layer:\n%s", dockerfile)
	}
}

type buildReferenceFunc func(context.Context, string, string, string, string) (string, error)

func (f buildReferenceFunc) Resolve(ctx context.Context, environment, server, database, field string) (string, error) {
	return f(ctx, environment, server, database, field)
}

func TestBuildEnvDeploymentResolvesAndMasksReferencesAtRuntime(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	owner := envvar.Owner{Type: envvar.OwnerApplication, ID: fixture.app.ID}
	if err := fixture.env.Set(context.Background(), owner, "DATABASE_URL", "${{db.url}}", false); err != nil {
		t.Fatal(err)
	}
	fixture.env.SetReferenceResolver(buildReferenceFunc(func(_ context.Context, environment, server, database, field string) (string, error) {
		if environment != fixture.app.EnvironmentID || server != fixture.app.ServerID || database != "db" || field != "url" {
			t.Fatalf("wrong reference scope: %s %s %s %s", environment, server, database, field)
		}
		return "postgres://rotated-secret@db/app", nil
	}))
	fixture.ex.On("docker pull", "postgres://rotated-secret@db/app", 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	deployment := fixture.deploy(t)
	if deployment.Status != DeploymentFinished {
		t.Fatalf("deploy failed: %s", deployment.Error)
	}
	if strings.Contains(fixture.logText(t, deployment.ID), "rotated-secret") {
		t.Error("resolved reference leaked to deploy log")
	}
	compose := string(fixture.ex.Files["/data/pier/applications/"+fixture.app.ID+"/docker-compose.yaml"])
	if !strings.Contains(compose, "postgres://rotated-secret@db/app") {
		t.Fatalf("runtime did not receive resolved value: %s", compose)
	}
	variables, err := fixture.env.List(context.Background(), owner)
	if err != nil || variables[0].Value != "${{db.url}}" {
		t.Fatalf("API/store reference changed: %+v %v", variables, err)
	}
}

type buildPermissionExecutor struct {
	executor.Executor
	filename string
}

func (e buildPermissionExecutor) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	if strings.HasPrefix(command, ". ") {
		info, err := os.Stat(e.filename)
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0o600 {
			return fmt.Errorf("build env permissions: %o", info.Mode().Perm())
		}
	}
	return e.Executor.Stream(ctx, command, sink)
}

func TestBuildEnvShellFilePreservesLiteralMultilineValuesAndIsRemoved(t *testing.T) {
	fixture := newDeployFixture(t, gitConfig())
	j := sourceTestJob(fixture)
	j.server.DataDir = t.TempDir()
	j.ex = buildPermissionExecutor{Executor: executor.NewLocal(), filename: path.Join(j.server.BuildDir(j.deployment.ID), "build.env.sh")}
	value := "literal ' quote\n$HOME $(false) `false` end"
	j.env = []envvar.Variable{{Key: "TOKEN", Value: value, SecretBuild: true}}
	var output []string
	err := j.streamBuildEnvironment(context.Background(), []string{"sh", "-c", `printf '%s\n' "$TOKEN"`}, func(line string) { output = append(output, line) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(output, "\n") != value {
		t.Fatalf("value was interpreted or changed: %q", strings.Join(output, "\n"))
	}
	if _, err := os.Stat(path.Join(j.server.BuildDir(j.deployment.ID), "build.env.sh")); !os.IsNotExist(err) {
		t.Fatalf("build environment file was not removed: %v", err)
	}
}
