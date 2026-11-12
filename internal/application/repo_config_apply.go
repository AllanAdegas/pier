package application

import (
	"context"
	"io"
	"path"
	"slices"
	"strings"

	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// overlay applies the fields present in the file and names each of them.
type overlay struct {
	fields []string
}

func setIfPresent[T any](o *overlay, name string, value *T, target *T) {
	if value != nil {
		*target = *value
		o.fields = append(o.fields, name)
	}
}

// apply returns config with the file's fields over it. Slices are copied:
// the stored config of the application is never modified.
func (file repoConfig) apply(config Config) (Config, []string) {
	o := &overlay{}
	config.Network.Domains = slices.Clone(config.Network.Domains)
	config.Network.PortMappings = slices.Clone(config.Network.PortMappings)
	config.Storages = slices.Clone(config.Storages)
	setIfPresent(o, "runtime.restart_policy", file.Runtime.RestartPolicy, &config.Runtime.RestartPolicy)
	setIfPresent(o, "runtime.stop_grace_seconds", file.Runtime.StopGraceSeconds, &config.Runtime.StopGraceSeconds)
	setIfPresent(o, "build.target", file.Build.Target, &config.Build.Target)
	setIfPresent(o, "build.pack", file.Build.Pack, &config.Build.Pack)
	setIfPresent(o, "build.dockerfile", file.Build.Dockerfile, &config.Build.DockerfilePath)
	setIfPresent(o, "build.publish_directory", file.Build.PublishDirectory, &config.Build.PublishDirectory)
	setIfPresent(o, "build.spa", file.Build.SPA, &config.Build.SPA)
	setIfPresent(o, "build.install_command", file.Build.InstallCommand, &config.Build.InstallCommand)
	setIfPresent(o, "build.build_command", file.Build.BuildCommand, &config.Build.BuildCommand)
	setIfPresent(o, "build.start_command", file.Build.StartCommand, &config.Build.StartCommand)
	setIfPresent(o, "port", file.Port, &config.Network.Port)
	setIfPresent(o, "health.enabled", file.Health.Enabled, &config.Health.Enabled)
	setIfPresent(o, "health.path", file.Health.Path, &config.Health.Path)
	setIfPresent(o, "health.command", file.Health.Command, &config.Health.Command)
	setIfPresent(o, "health.interval_seconds", file.Health.IntervalSeconds, &config.Health.IntervalSeconds)
	setIfPresent(o, "health.timeout_seconds", file.Health.TimeoutSeconds, &config.Health.TimeoutSeconds)
	setIfPresent(o, "health.retries", file.Health.Retries, &config.Health.Retries)
	setIfPresent(o, "health.start_period_seconds", file.Health.StartPeriodSeconds, &config.Health.StartPeriodSeconds)
	setIfPresent(o, "release.command", file.Release.Command, &config.Release.Command)
	if file.Limits != nil {
		config.Limits = Limits{CPUs: file.Limits.CPUs, Memory: file.Limits.Memory}
		o.fields = append(o.fields, "limits")
	}
	if file.Processes != nil {
		config.Processes = make([]Process, 0, len(*file.Processes))
		for _, process := range *file.Processes {
			config.Processes = append(config.Processes, Process{
				Name: process.Name, Command: process.Command, Replicas: process.Replicas,
				Limits: Limits{CPUs: process.Limits.CPUs, Memory: process.Limits.Memory},
			})
		}
		o.fields = append(o.fields, "processes")
	} else {
		config.Processes = slices.Clone(config.Processes)
	}
	if file.WatchPaths != nil {
		config.Source.WatchPaths = slices.Clone(*file.WatchPaths)
		o.fields = append(o.fields, "watch_paths")
	} else {
		config.Source.WatchPaths = slices.Clone(config.Source.WatchPaths)
	}
	return config, o.fields
}

// findRepoConfigScript prints the first configuration file present.
const findRepoConfigScript = `for name in "$@"; do
  if [ -f "$name" ]; then echo "$name"; exit 0; fi
done`

// applyRepoConfig overlays pier.yaml (or .pier.yaml) from the base directory
// on the configuration of this deployment only. An invalid file fails it.
func (j *job) applyRepoConfig(ctx context.Context) error {
	dir := j.contextDir()
	args := []string{"sh", "-c", "cd " + shell.Quote(dir) + " && " + findRepoConfigScript, "sh"}
	output, err := j.ex.Run(ctx, shell.Join(append(args, repoConfigNames...)...))
	name := strings.TrimSpace(output)
	if err != nil || name == "" {
		return nil
	}
	content, err := j.readRepoConfig(ctx, path.Join(dir, name))
	if err != nil {
		return err
	}
	merged, fields, err := overlayRepoConfig(content, j.app.Config)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		j.log.Info("%s encontrado, sem campos para aplicar.", name)
		return nil
	}
	j.app.Config = merged
	j.log.Info("Configuração de %s aplicada a este deploy: %s.", name, strings.Join(fields, ", "))
	// The file may point the build at another Dockerfile: check it again.
	return j.ensureInsideSource(ctx)
}

func (j *job) readRepoConfig(ctx context.Context, file string) ([]byte, error) {
	reader, err := j.ex.ReadFile(ctx, file)
	if err != nil {
		return nil, i18n.Errorf("não foi possível ler %s: %w", path.Base(file), err)
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, maxRepoConfigBytes+1))
	if err != nil {
		return nil, i18n.Errorf("não foi possível ler %s: %w", path.Base(file), err)
	}
	return content, nil
}
