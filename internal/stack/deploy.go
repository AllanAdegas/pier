package stack

import (
	"context"
	"strings"
	"time"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/oplog"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

const operationTimeout = 30 * time.Minute

// Deploy brings the stack up in the background; the progress is available
// through Operation.
func (s *Service) Deploy(ctx context.Context, id string) error {
 target, err := s.Get(ctx,id)
 if err != nil { return err }
 if err := server.CheckConnectorDeployment(ctx,s.connector,target.ServerID); err != nil { return err }
	if s.guard != nil {
		stack, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := s.guard.CheckDeploy(ctx, stack.EnvironmentID); err != nil {
			return err
		}
	}
	return s.runInBackground(ctx, id, "Deploy da stack", func(ctx context.Context, run *stackRun) error {
		if err := server.CheckConnectorDeployment(ctx, s.connector, run.stack.ServerID); err != nil { return err }
		if run.stack.FromGit() {
			return s.deployGit(ctx, run)
		}
		if run.stack.FromDirectory() {
			return s.deployDirectory(ctx, run)
		}
		return s.deployInline(ctx, run)
	})
}

// deployInline runs the compose file Pier keeps.
func (s *Service) deployInline(ctx context.Context, run *stackRun) error {
	s.withPendingCertificates(ctx, run)
	doc, err := parseCompose(run.stack.Compose)
	if err != nil {
		return err
	}
	rendered, files, err := render(run.stack)
	if err != nil {
		return err
	}
	dotenv, err := s.environment(ctx, run, run.stack.Compose, doc)
	if err != nil {
		return err
	}
	if err := docker.EnsureNetwork(ctx, run.ex); err != nil {
		return err
	}
	for _, file := range files {
		if err := writeHostFile(ctx, run, file); err != nil {
			return err
		}
	}
	if err := run.project.Write(ctx, run.ex, rendered, dotenv); err != nil {
		return err
	}
	return bringUp(ctx, run)
}

// deployDirectory runs the compose file of a folder of the server. The file
// is read again on every deployment, so editing it and deploying is enough.
func (s *Service) deployDirectory(ctx context.Context, run *stackRun) error {
	run.operation.Say("Pasta: %s (%s)", run.stack.Directory, run.stack.ComposeFile)
	s.withPendingCertificates(ctx, run)
	raw, err := readSource(ctx, run.ex, run.project.BaseFile)
	if err != nil {
		return apperr.Unavailable("Não foi possível ler %s: %s", run.project.BaseFile, lastLine(err))
	}
	doc, err := parseCompose(raw)
	if err != nil {
		return err
	}
	return s.runFolder(ctx, run, raw, doc)
}

// runFolder brings up a stack whose compose file is on the server, given the
// file as read: Pier's env file, the override and then the project.
func (s *Service) runFolder(ctx context.Context, run *stackRun, raw string, doc document) error {
	if len(run.project.EnvFiles) > 0 {
		run.operation.Say("Usando o .env da pasta.")
	}
	dotenv, err := s.environment(ctx, run, raw, doc)
	if err != nil {
		return err
	}
	if err := docker.EnsureNetwork(ctx, run.ex); err != nil {
		return err
	}
	if err := run.project.WriteEnv(ctx, run.ex, dotenv); err != nil {
		return err
	}
	run.operation.Append("$ docker compose config --services")
	services, err := run.project.BaseServices(ctx, run.ex)
	if err != nil {
		return err
	}
	override, err := renderOverride(run.stack, doc, services)
	if err != nil {
		return err
	}
	if err := run.ex.WriteFile(ctx, run.project.FilePath(), override, 0o600); err != nil {
		return err
	}
	// The folder may hold the source of the images: build what changed.
	return bringUp(ctx, run, "--build")
}

// environment prepares the env file of a deployment: stored variables,
// generated values and the addresses of the published services.
func (s *Service) environment(ctx context.Context, run *stackRun, raw string, doc document) ([]byte, error) {
	if err := s.generate(ctx, run.stack.ID, raw); err != nil {
		return nil, err
	}
	variables, err := s.env.Map(ctx, owner(run.stack.ID))
	if err != nil {
		return nil, err
	}
	addresses := routeVariables(run.stack.Routes)
	for key, value := range addresses {
		variables[key] = value
	}
	if !run.stack.FromDirectory() {
		variables[directoryVariable] = run.project.Dir
	}
	for _, service := range unpublished(raw, addresses) {
		run.operation.Say("AVISO: o compose usa o endereço público de %s (PIER_URL_…/PIER_HOST_…), "+
			"mas esse serviço não tem domínio; defina um em Compose e domínios.", service)
	}
	return envvar.FormatDotenv(variables), nil
}

// writeHostFile creates a file the compose file mounts, unless it is already
// there: what the user changed on the server is not overwritten.
func writeHostFile(ctx context.Context, run *stackRun, file hostFile) error {
	run.operation.Say("Criando %s", file.Path)
	return createHostFile(ctx, run.ex, run.project.Dir, file)
}

func bringUp(ctx context.Context, run *stackRun, extraArgs ...string) error {
	run.operation.Append("$ docker compose pull")
	// Refreshing images is best effort: a registry outage or rate limit must
	// not block a deploy whose images are already on the server. If an image
	// is really missing, "up" fails right after with a clear message.
	if err := run.project.Pull(ctx, run.ex, run.operation.Append); err != nil {
		run.operation.Say("AVISO: não foi possível atualizar as imagens (%s); usando as locais.", err)
	}
	if run.stack.FromDirectory() {
		// The images of a folder are built here, so they can be inspected
		// before anything starts; "up --build" afterwards reuses the cache.
		if err := buildImages(ctx, run); err != nil {
			return err
		}
	}
	if err := verifyRoutedServices(ctx, run); err != nil {
		return err
	}
	args := append([]string{"--remove-orphans"}, extraArgs...)
	run.operation.Append("$ docker compose up --detach " + strings.Join(args, " "))
	if err := run.project.Up(ctx, run.ex, run.operation.Append, args...); err != nil {
		return docker.Explain(err)
	}
	docker.ConnectProxy(ctx, run.ex, IngressNetwork(run.stack.ID))
	if len(run.stack.Routes) > 0 && !docker.ProxyRunning(ctx, run.ex) {
		run.operation.Say(docker.ProxyStoppedWarning)
	}
	server.ReportEdgeSync(ctx, run.ex, run.target, run.operation.Append)
	return nil
}

// Stop stops every container of the stack keeping its data.
func (s *Service) Stop(ctx context.Context, id string) error {
	return s.runInBackground(ctx, id, "Parando a stack", func(ctx context.Context, run *stackRun) error {
		run.operation.Append("$ docker compose stop")
		return run.project.Stop(ctx, run.ex)
	})
}

// Restart restarts every container of the stack.
func (s *Service) Restart(ctx context.Context, id string) error {
	return s.runInBackground(ctx, id, "Reiniciando a stack", func(ctx context.Context, run *stackRun) error {
		run.operation.Append("$ docker compose restart")
		return run.project.Restart(ctx, run.ex)
	})
}

// Operation returns the progress of the latest background operation.
func (s *Service) Operation(id string) oplog.Snapshot {
	snapshot, _ := s.operations.Get(id)
	return snapshot
}

type stackRun struct {
	// lockBuild, when set, is taken while images are built: Git stacks build
	// under the same queue and disk limits as applications.
	lockBuild func(ctx context.Context) (release func(), err error)
	stack     Stack
	ex        executor.Executor
	target    server.Server
	project   compose.Project
	operation *oplog.Operation
}

func (s *Service) runInBackground(ctx context.Context, id, title string,
	action func(ctx context.Context, run *stackRun) error) error {
	operation, started := s.operations.Begin(id, i18n.Instance().Text(title))
	if !started {
		return apperr.Conflict("Já existe uma operação em andamento nesta stack.")
	}
	run, err := s.reach(ctx, id)
	if err != nil {
		operation.Finish(err)
		return err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
		defer cancel()
		run.operation = operation
		err := action(ctx, run)
		if err != nil {
			operation.Say("ERRO: %s", err)
		} else {
			operation.Say("Concluído.")
		}
		operation.Finish(err)
	}()
	return nil
}

// Delete brings the stack down and erases it. deleteVolumes destroys its
// data. The folder of a directory stack is never touched.
func (s *Service) Delete(ctx context.Context, id string, deleteVolumes, force bool) error {
	operation, started := s.operations.Begin(id, "")
	if !started {
		return apperr.Conflict("Já existe uma operação em andamento nesta stack.")
	}
	defer operation.Finish(nil)
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.cleanup(ctx, id, deleteVolumes); err != nil && !force {
		return apperr.Unavailable("Não foi possível limpar o servidor (%s). Use a remoção forçada para "+
			"apagar apenas o registro.", err)
	}
	if err := s.env.DeleteAll(ctx, owner(id)); err != nil {
		return err
	}
	if err := sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM stacks WHERE id = ?`, id)); err != nil {
		return err
	}
	s.operations.Forget(id)
	return nil
}

func (s *Service) cleanup(ctx context.Context, id string, deleteVolumes bool) error {
	run, err := s.reach(ctx, id)
	if err != nil {
		return err
	}
	stack, ex, project := run.stack, run.ex, run.project
	containers, err := docker.ListByResource(ctx, ex, id)
	if err != nil {
		return err
	}
	docker.DisconnectProxy(ctx, ex, IngressNetwork(id))
	if len(containers) > 0 {
		if err := project.Down(ctx, ex, deleteVolumes); err != nil {
			// The compose file may be gone or broken; fall back to removing containers.
			if err := docker.Remove(ctx, ex, containers); err != nil {
				return err
			}
		}
		server.LogEdgeSync(ctx, ex, run.target)
	}
	// The directory of the stack may hold data written through relative bind
	// mounts (./data:/var/lib/app); it follows the same choice as the volumes.
	// For a directory stack it only holds what Pier generated.
	if stack.FromGit() {
		if err := removeCheckout(ctx, run); err != nil {
			return err
		}
	}
	if deleteVolumes || stack.FromDirectory() {
		return project.RemoveDir(ctx, ex)
	}
	return project.RemoveFiles(ctx, ex)
}

// Containers lists the containers of the stack on its server.
func (s *Service) Containers(ctx context.Context, id string) ([]docker.Container, error) {
	run, err := s.reach(ctx, id)
	if err != nil {
		return nil, err
	}
	containers, err := docker.ListByResource(ctx, run.ex, id)
	if err != nil {
		return nil, apperr.Unavailable("Não foi possível consultar o servidor: %s", err)
	}
	return containers, nil
}

// reach prepares a run of the stack on its server, without an operation yet.
func (s *Service) reach(ctx context.Context, id string) (*stackRun, error) {
	stack, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	ex, target, err := s.connector.Executor(ctx, stack.ServerID)
	if err != nil {
		return nil, err
	}
	stack.proxyEntry = target.Proxy.Entry()
	stack.proxyLabels = server.ProxyRouteLabels(target)
	return &stackRun{stack: stack, ex: ex, target: target, project: projectFor(ctx, ex, target, stack)}, nil
}

// buildImages builds what the compose file builds, apart from "up" so the
// images can be inspected before anything starts.
func buildImages(ctx context.Context, run *stackRun) error {
	if run.lockBuild != nil {
		release, err := run.lockBuild(ctx)
		if err != nil {
			return err
		}
		defer release()
	}
	run.operation.Append("$ docker compose build")
	if err := run.ex.Stream(ctx, run.project.Command("build"), run.operation.Append); err != nil {
		return docker.Explain(err)
	}
	return nil
}
