package application

import (
	"context"
	"fmt"
	"pier/internal/docker"
	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
	"regexp"
	"strings"

	"pier/internal/platform/apperr"
)

// Runtime controls how Docker restarts and gracefully stops each process.
// Empty values preserve the existing unless-stopped/30-second Pier defaults.
type Runtime struct {
	RestartPolicy    string `json:"restart_policy"`
	StopGraceSeconds int    `json:"stop_grace_seconds"`
}

var buildTargetPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func (c *Config) normalizeEngineExtras() {
	c.Build.Target = strings.TrimSpace(c.Build.Target)
	c.Runtime.RestartPolicy = strings.TrimSpace(c.Runtime.RestartPolicy)
}

func (c Config) validateEngineExtras(fields apperr.Fields) {
	if c.Build.Target != "" && (c.Build.Pack != PackDockerfile || !buildTargetPattern.MatchString(c.Build.Target)) {
		fields.Add("build.target", "Informe um estágio válido de Dockerfile (até 128 letras, números, '.', '_' ou '-').")
	}
	switch c.Runtime.RestartPolicy {
	case "", "no", "always", "unless-stopped", "on-failure":
	default:
		fields.Add("runtime.restart_policy", "Política inválida: use no, always, unless-stopped ou on-failure.")
	}
	if c.Runtime.StopGraceSeconds < 0 || c.Runtime.StopGraceSeconds > 300 {
		fields.Add("runtime.stop_grace_seconds", "Use de 0 a 300 segundos para encerrar os processos.")
	}
}

func stopGracePeriod(runtime Runtime) string {
	if runtime.StopGraceSeconds == 0 {
		return "30s"
	}
	return fmt.Sprintf("%ds", runtime.StopGraceSeconds)
}

// Do not override --time: each container remembers the grace period of its
// own release, including when the new version changes its runtime settings.
func stopApplicationContainers(ctx context.Context, ex executor.Executor, containers []docker.Container) error {
	return configuredContainerOperation(ctx, ex, containers, "stop")
}
func restartApplicationContainers(ctx context.Context, ex executor.Executor, containers []docker.Container) error {
	return configuredContainerOperation(ctx, ex, containers, "restart")
}
func configuredContainerOperation(ctx context.Context, ex executor.Executor, containers []docker.Container, operation string) error {
	if len(containers) == 0 {
		return nil
	}
	args := []string{"docker", operation}
	for _, container := range containers {
		args = append(args, container.ID)
	}
	_, err := ex.Run(ctx, shell.Join(args...))
	return err
}
