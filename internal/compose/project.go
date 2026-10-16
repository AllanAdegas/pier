package compose

import (
	"context"
	"fmt"
	"path"
	"strings"

	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

const (
	composeFileName = "docker-compose.yaml"
	envFileName     = ".env"
	// failureContextLines is how much of the output is kept to explain a failure.
	failureContextLines = 3
)

// Project is a compose project living in a directory of a server.
type Project struct {
	Name string
	Dir  string
	// BaseFile, when set, is a compose file kept somewhere else (a folder the
	// user chose). The file in Dir is then applied on top of it as an
	// override, and relative paths resolve against the folder of BaseFile.
	BaseFile string
	// EnvFiles are read before the env file of Dir, whose values win.
	EnvFiles []string
}

// FilePath is where the compose file is written.
func (p Project) FilePath() string { return path.Join(p.Dir, composeFileName) }

func (p Project) envPath() string { return path.Join(p.Dir, envFileName) }

// Write stores the compose file and its env file. Both may hold secrets, so
// they are only readable by the owner.
func (p Project) Write(ctx context.Context, ex executor.Executor, composeYAML, dotenv []byte) error {
	if err := ex.WriteFile(ctx, p.FilePath(), composeYAML, 0o600); err != nil {
		return err
	}
	return p.WriteEnv(ctx, ex, dotenv)
}

// WriteEnv stores only the env file.
func (p Project) WriteEnv(ctx context.Context, ex executor.Executor, dotenv []byte) error {
	return ex.WriteFile(ctx, p.envPath(), dotenv, 0o600)
}

// Command builds a `docker compose` invocation scoped to this project.
func (p Project) Command(args ...string) string {
	return shell.Join(append(p.invocation(true), args...)...)
}

// invocation is the part of the command that selects the project: its name,
// compose files and env files.
func (p Project) invocation(withOwnFile bool) []string {
	base := []string{"docker", "compose", "--project-name", p.Name}
	if p.BaseFile != "" {
		base = append(base, "--project-directory", path.Dir(p.BaseFile), "--file", p.BaseFile)
	}
	if withOwnFile {
		base = append(base, "--file", p.FilePath())
	}
	for _, file := range p.EnvFiles {
		base = append(base, "--env-file", file)
	}
	return append(base, "--env-file", p.envPath(), "--progress", "plain")
}

// BaseServices lists the services of BaseFile as docker compose resolves it
// (includes and extends followed, variables interpolated). It doubles as a
// validation of the file by docker itself. The env file of Dir must exist.
func (p Project) BaseServices(ctx context.Context, ex executor.Executor) ([]string, error) {
	command := shell.Join(append(p.invocation(false), "config", "--services")...)
	output, err := ex.Run(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("docker compose config: %w", err)
	}
	services := []string{}
	for _, line := range strings.Split(output, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			services = append(services, name)
		}
	}
	return services, nil
}

// Up creates and starts the services. The output goes to the sink as it is
// produced; on failure its last lines are also attached to the error, since
// that is where compose says why (a port already in use, a missing image).
func (p Project) Up(ctx context.Context, ex executor.Executor, sink executor.LineSink, extraArgs ...string) error {
	args := append([]string{"up", "--detach"}, extraArgs...)
	recent := []string{}
	remember := func(line string) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			recent = append(recent, trimmed)
			if len(recent) > failureContextLines {
				recent = recent[1:]
			}
		}
		sink(line)
	}
	if err := ex.Stream(ctx, p.Command(args...), remember); err != nil {
		if len(recent) == 0 || ctx.Err() != nil {
			return fmt.Errorf("docker compose up: %w", err)
		}
		return fmt.Errorf("docker compose up: %w: %s", err, strings.Join(recent, " | "))
	}
	return nil
}

// Pull downloads the images of the project.
func (p Project) Pull(ctx context.Context, ex executor.Executor, sink executor.LineSink) error {
	if err := ex.Stream(ctx, p.Command("pull", "--ignore-buildable"), sink); err != nil {
		return fmt.Errorf("docker compose pull: %w", err)
	}
	return nil
}

// Stop stops the services keeping containers and data.
func (p Project) Stop(ctx context.Context, ex executor.Executor) error {
	if _, err := ex.Run(ctx, p.Command("stop")); err != nil {
		return fmt.Errorf("docker compose stop: %w", err)
	}
	return nil
}

// Restart restarts the services.
func (p Project) Restart(ctx context.Context, ex executor.Executor) error {
	if _, err := ex.Run(ctx, p.Command("restart")); err != nil {
		return fmt.Errorf("docker compose restart: %w", err)
	}
	return nil
}

// Down removes containers and networks; volumes only when asked.
func (p Project) Down(ctx context.Context, ex executor.Executor, removeVolumes bool) error {
	args := []string{"down", "--remove-orphans"}
	if removeVolumes {
		args = append(args, "--volumes")
	}
	if _, err := ex.Run(ctx, p.Command(args...)); err != nil {
		return fmt.Errorf("docker compose down: %w", err)
	}
	return nil
}

// RemoveFiles deletes what Pier wrote (compose and env files) and then the
// directory, but only if nothing else is left in it: data that containers
// stored there through relative bind mounts is kept.
func (p Project) RemoveFiles(ctx context.Context, ex executor.Executor) error {
	if p.Dir == "" || p.Dir == "/" {
		return fmt.Errorf("refusing to remove files of directory %q", p.Dir)
	}
	if _, err := ex.Run(ctx, shell.Join("rm", "-f", p.FilePath(), p.envPath())); err != nil {
		return err
	}
	// rmdir refuses a directory that is not empty, which is the point.
	_, _ = ex.Run(ctx, shell.Join("rmdir", p.Dir)+" 2>/dev/null || true")
	return nil
}

// RemoveDir deletes the project directory from the server.
func (p Project) RemoveDir(ctx context.Context, ex executor.Executor) error {
	if p.Dir == "" || p.Dir == "/" {
		return fmt.Errorf("refusing to remove directory %q", p.Dir)
	}
	_, err := ex.Run(ctx, shell.Join("rm", "-rf", p.Dir))
	return err
}
