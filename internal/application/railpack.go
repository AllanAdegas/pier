package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path"

	"pier/internal/envvar"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// railpackInstallVariable carries the install override: Railpack has
// --build-cmd and --start-cmd flags but reads the install command from it.
const railpackInstallVariable = "RAILPACK_INSTALL_CMD"

// railpackBuilder detects the stack with Railpack (the Nixpacks successor):
// `railpack prepare` writes a build plan and BuildKit builds it through the
// Railpack frontend, pinned to the same release.
type railpackBuilder struct{}

func (railpackBuilder) build(ctx context.Context, j *job) (string, error) {
	binary, err := j.ensureRailpack(ctx)
	if err != nil {
		return "", err
	}
	planDir := path.Join(j.server.BuildDir(j.deployment.ID), "railpack")
	if _, err := j.ex.Run(ctx, shell.Join("mkdir", "-p", "--", planDir)); err != nil {
		return "", err
	}
	railpackJob := j.withRailpackOverrides()
	j.log.Command("railpack prepare " + j.contextDir())
	args := railpackPrepareArgs(binary, j.contextDir(), planDir, j.app.Config.Build, railpackJob.buildTimeVariables())
	if err := railpackJob.streamBuildEnvironment(ctx, args, j.log.Output); err != nil {
		return "", i18n.Errorf("railpack prepare falhou: %w", err)
	}
	return railpackJob.dockerBuild(ctx, path.Join(planDir, "railpack-plan.json"))
}

// withRailpackOverrides adds the install override as a build variable, so its
// value travels in the private environment file like every other value.
func (j *job) withRailpackOverrides() *job {
	copied := *j
	copied.env = []envvar.Variable{}
	install := j.app.Config.Build.InstallCommand
	for _, variable := range j.env {
		if install == "" || variable.Key != railpackInstallVariable {
			copied.env = append(copied.env, variable)
		}
	}
	if install != "" {
		copied.env = append(copied.env, envvar.Variable{Key: railpackInstallVariable, Value: install, BuildTime: true})
	}
	return &copied
}

// railpackPrepareArgs runs `railpack prepare` in a disposable container: the
// repository is mounted read-only and only the plan directory is writable,
// so analysing untrusted code never runs on the server itself. Variables are
// passed by name (-e KEY / --env KEY); their values come from the environment
// file sourced by streamBuildEnvironment and never reach the argv.
func railpackPrepareArgs(binary, contextDir, planDir string, build Build, variables []envvar.Variable) []string {
	args := []string{"docker", "run", "--rm", "--security-opt", "no-new-privileges",
		"-v", binary + ":/usr/local/bin/railpack:ro", "-v", contextDir + ":/app:ro", "-v", planDir + ":/out"}
	for _, variable := range variables {
		args = append(args, "-e", variable.Key)
	}
	args = append(args, "--entrypoint", "/usr/local/bin/railpack", gitImage,
		"prepare", "/app", "--plan-out", "/out/railpack-plan.json", "--info-out", "/out/railpack-info.json")
	if build.BuildCommand != "" {
		args = append(args, "--build-cmd", build.BuildCommand)
	}
	if build.StartCommand != "" {
		args = append(args, "--start-cmd", build.StartCommand)
	}
	for _, variable := range variables {
		args = append(args, "--env", variable.Key)
	}
	return args
}

// railpackBuildArgs replaces the generic build arguments for the Railpack
// pack. Railpack declares every variable given to prepare as a plan secret,
// so all build-time values are BuildKit secrets (never --build-arg values).
// cache-key isolates the mount caches of each application on shared
// builders; secrets-hash invalidates them when a value changes.
func (j *job) railpackBuildArgs() []string {
	variables := j.buildTimeVariables()
	args := []string{"--build-arg", "BUILDKIT_SYNTAX=" + railpackFrontend, "--build-arg", "cache-key=" + j.app.ID}
	if len(variables) > 0 {
		args = append(args, "--build-arg", "secrets-hash="+railpackSecretsHash(j.app.ID, variables))
	}
	for _, variable := range variables {
		args = append(args, "--secret", "id="+variable.Key+",env="+variable.Key)
	}
	return args
}

// railpackSecretsHash changes whenever a build value changes. It is a SHA-256
// over the application id and the sorted KEY=value pairs, NUL separated.
func railpackSecretsHash(applicationID string, variables []envvar.Variable) string {
	hash := sha256.New()
	hash.Write([]byte(applicationID))
	for _, variable := range variables {
		hash.Write([]byte{0})
		hash.Write([]byte(variable.Key + "=" + variable.Value))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
