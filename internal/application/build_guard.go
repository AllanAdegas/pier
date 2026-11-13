package application

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"pier/internal/config"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

var buildLimits atomic.Pointer[config.BuildLimits]

func init() { ConfigureBuild(config.BuildLimits{CPUs: 1, Memory: "2g", MinFreeGB: 2, CacheGB: 10}) }

// ConfigureBuild installs process-wide limits at startup.
func ConfigureBuild(limits config.BuildLimits) { buildLimits.Store(&limits) }

type buildQueue struct {
	token chan struct{}
	users int
}

var serverBuilds = struct {
	sync.Mutex
	queues map[string]*buildQueue
}{queues: make(map[string]*buildQueue)}

// buildNotifier is told when a build waits for the one before it.
type buildNotifier interface {
	Info(format string, args ...any)
}

// acquireBuild serializes builds by server, with cancellation while queued.
func acquireBuild(ctx context.Context, serverID string, log buildNotifier) (func(), error) {
	serverBuilds.Lock()
	queue := serverBuilds.queues[serverID]
	if queue == nil {
		queue = &buildQueue{token: make(chan struct{}, 1)}
		serverBuilds.queues[serverID] = queue
	}
	queue.users++
	serverBuilds.Unlock()
	drop := func() {
		serverBuilds.Lock()
		defer serverBuilds.Unlock()
		queue.users--
		if queue.users == 0 {
			delete(serverBuilds.queues, serverID)
		}
	}
	select {
	case queue.token <- struct{}{}:
	default:
		if log != nil {
			log.Info("Aguardando o build anterior neste servidor terminar.")
		}
		select {
		case queue.token <- struct{}{}:
		case <-ctx.Done():
			drop()
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		<-queue.token
		drop()
		return nil, err
	}
	return func() { <-queue.token; drop() }, nil
}

// AcquireBuild waits for the build slot of a server (one build at a time) and
// returns the function that gives it back. note is told when it has to wait.
func AcquireBuild(ctx context.Context, serverID string, note func(string)) (func(), error) {
	return acquireBuild(ctx, serverID, buildWaitNote(note))
}

type buildWaitNote func(string)

func (n buildWaitNote) Info(format string, args ...any) {
	if n != nil {
		n(i18n.Instance().Sprintf(format, args...))
	}
}

type guardedBuilder struct{ builder }

func (b guardedBuilder) build(ctx context.Context, j *job) (string, error) {
	release, err := acquireBuild(ctx, j.app.ServerID, j.log)
	if err != nil {
		return "", err
	}
	defer release()
	if err := j.checkBuildDisk(ctx); err != nil {
		return "", err
	}
	return b.builder.build(ctx, j)
}

// checkBuildDisk asks Docker for its actual root, including custom data-root.
func (j *job) checkBuildDisk(ctx context.Context) error { return CheckBuildDisk(ctx, j.ex) }

// CheckBuildDisk refuses a build when the Docker data directory of the server
// is below the configured free space. Other resources that build on the
// server (stacks with build sections) apply the same limit.
func CheckBuildDisk(ctx context.Context, ex executor.Executor) error {
	limits := buildLimits.Load()
	if limits.MinFreeGB == 0 {
		return nil
	}
	// The daemon root may not be mounted inside the Pier container. Probe the
	// daemon's filesystem through a read-only bind, which also works over SSH.
	command := `root=$(docker info --format '{{.DockerRootDir}}') && test -n "$root" && docker run --rm --volume "$root:/docker-root:ro" --entrypoint sh ` + gitImage + ` -c 'df -Pk /docker-root' | awk 'NR==2 {print $4}'`
	output, err := ex.Run(ctx, command)
	if err != nil {
		return i18n.Errorf("não foi possível verificar o espaço livre no diretório do Docker: %w", err)
	}
	freeKB, err := strconv.ParseUint(strings.TrimSpace(output), 10, 64)
	if err != nil {
		return i18n.Errorf("o servidor não informou o espaço livre no diretório do Docker")
	}
	if freeKB/1024/1024 < uint64(limits.MinFreeGB) {
		return i18n.Errorf("espaço insuficiente para o build: são necessários pelo menos %d GB livres no diretório do Docker. Execute a limpeza em Servidores antes de tentar novamente", limits.MinFreeGB)
	}
	return nil
}

// BuildKit ignores the legacy docker build --memory/--cpu-quota options. A
// dedicated docker-container driver enforces them on the actual build daemon.
func (j *job) createBuildKit(ctx context.Context) (string, error) {
	limits := buildLimits.Load()
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%g:%s", j.app.ServerID, limits.CPUs, limits.Memory)))
	name := fmt.Sprintf("pier-build-%x", digest[:8])
	if output, err := j.ex.Run(ctx, shell.Join("docker", "buildx", "inspect", name)); err == nil && strings.TrimSpace(output) != "" {
		return name, nil
	}
	args := []string{"docker", "buildx", "create", "--name", name, "--driver", "docker-container",
		"--driver-opt", "cpu-period=100000", "--driver-opt", fmt.Sprintf("cpu-quota=%d", int64(limits.CPUs*100000)),
		"--driver-opt", "memory=" + limits.Memory}
	if _, err := j.ex.Run(ctx, shell.Join(args...)); err != nil {
		return "", i18n.Errorf("não foi possível iniciar o builder com limites de CPU e memória; verifique se o Docker Buildx está instalado: %w", err)
	}
	return name, nil
}
