package application

import (
	"context"
	"encoding/json"
	"path"
	"strings"
	"time"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// runRelease never publishes ports, routes or app DNS aliases. It uses the new
// image and runtime environment even on the first deployment.
func (j *job) runRelease(ctx context.Context, file *compose.File, serviceName string) error {
	command := j.app.Config.Release.Command
	if command == "" {
		return nil
	}
	service := *file.Services[serviceName]
	service.Labels = map[string]string{}
	service.Ports = nil
	service.Healthcheck = nil
	service.Restart = "no"
	service.Networks = map[string]*compose.NetworkAttachment{docker.Network: {}}
	releaseName := serviceName + "-release"
	service.ContainerName = releaseName
	releaseFile := *file
	releaseFile.Services = map[string]*compose.Service{"release": &service}
	yaml, err := releaseFile.Marshal()
	if err != nil {
		return err
	}
	project := compose.Project{Name: j.app.ID + "-release", Dir: path.Join(j.server.ResourceDir(docker.TypeApplication, j.app.ID), "release-"+j.deployment.ID)}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = j.ex.Run(cleanup, shell.Join("docker", "rm", "-f", releaseName))
		_ = project.RemoveDir(cleanup, j.ex)
	}()
	if err := project.Write(ctx, j.ex, yaml, nil); err != nil {
		return err
	}
	j.log.Info("Executando comando de release na nova imagem.")
	// --entrypoint overrides image entrypoints as well as the usual start command.
	if err := j.ex.Stream(ctx, project.Command("run", "--rm", "--no-deps", "--name", releaseName, "--entrypoint", "sh", "release", "-c", command), j.log.Output); err != nil {
		return i18n.Errorf("o comando de release falhou; a versão anterior foi preservada: %w", err)
	}
	return nil
}

const retiredLogLines = 1000

func (j *job) captureRetiredLogs(ctx context.Context, containers []docker.Container) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, container := range containers {
		values, inspectErr := docker.Inspect(ctx, j.ex, container.ID, "{{json .Config.Env}}")
		var environment []string
		if inspectErr != nil || json.Unmarshal([]byte(values), &environment) != nil {
			j.log.Info("Não foi possível verificar os segredos do container anterior; seus logs não foram preservados.")
			continue
		}
		for _, entry := range environment {
			if _, value, ok := strings.Cut(entry, "="); ok {
				j.log.redact(value)
			}
		}
		lines := make([]string, 0, retiredLogLines)
		err := j.ex.Stream(ctx, docker.LogsCommand(container.ID, retiredLogLines, false), func(line string) {
			line = j.log.mask(line)
			if len(line) > maxLogLineLength {
				line = line[:maxLogLineLength]
			}
			if len(lines) == retiredLogLines {
				copy(lines, lines[1:])
				lines = lines[:retiredLogLines-1]
			}
			lines = append(lines, line)
		})
		if err != nil {
			j.log.Info("Não foi possível preservar todos os logs do container anterior: %s", err)
		}
		j.deployment.RetiredLogs = append(j.deployment.RetiredLogs, RetiredLog{Container: container.Name, Output: strings.Join(lines, "\n")})
	}
	if err := j.log.store.setDeploymentDetails(context.WithoutCancel(ctx), j.deployment); err != nil {
		j.log.Info("Não foi possível salvar os logs anteriores: %s", err)
	}
}
