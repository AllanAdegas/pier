package application

import (
	"context"
	"database/sql"
	"errors"
	"pier/internal/platform/i18n"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
	"pier/internal/registry"
	"pier/internal/server"
	"pier/internal/swarm"
)

// A target cannot be moved while a release may still exist on the old target.
func (s *MultiServer) saveSwarmTarget(ctx context.Context, tx *sql.Tx, app Application, p Placement) error {
	var previous string
	err := tx.QueryRowContext(ctx, "SELECT cluster_id FROM application_swarm WHERE application_id = ?", app.ID).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previous != p.SwarmClusterID && app.CurrentImage != "" {
		return apperr.Conflict("Remova a versão implantada antes de mudar o alvo Swarm.")
	}
	if p.SwarmClusterID == "" {
		_, err = tx.ExecContext(ctx, "DELETE FROM application_swarm WHERE application_id = ?", app.ID)
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM swarm_clusters WHERE id = ?", p.SwarmClusterID).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return apperr.NotFound("Swarm")
	}
	if len(p.Destinations) > 1 {
		return apperr.Conflict("Escolha Swarm ou destinos individuais.")
	}
	if err = validateSwarmConfig(app.Config); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO application_swarm(application_id,cluster_id,registry_id,repository,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(application_id) DO UPDATE SET cluster_id=excluded.cluster_id,registry_id=excluded.registry_id,repository=excluded.repository,updated_at=excluded.updated_at`, app.ID, p.SwarmClusterID, nullable(p.RegistryID), p.Repository, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func validateSwarmConfig(c Config) error {
	empty := DockerOptions{}
	empty.normalize()
	options := c.DockerOptions
	options.normalize()
	if len(c.Storages) > 0 || len(c.Network.PortMappings) > 0 || c.Hooks.PreDeploy != "" || c.Hooks.PostDeploy != "" || c.Release.Command != "" || !reflect.DeepEqual(options, empty) {
		return apperr.Conflict("Swarm não aceita volumes, portas publicadas, hooks, release ou opções Docker extras neste alvo.")
	}
	// Domains require an independently configured Traefik Swarm provider.
	return nil
}

func (d *Deployer) swarmHook(ctx context.Context, deployment Deployment, app Application, log *deployLog) (bool, error) {
	p, err := d.multi.settings(ctx, app.ID)
	if err != nil {
		return true, err
	}
	if p.SwarmClusterID == "" {
		return false, nil
	}
	if err = validateSwarmConfig(app.Config); err != nil {
		return true, err
	}
	manager, err := swarm.NewService(d.multi.db, d.connector).Manager(ctx, p.SwarmClusterID)
	if err != nil {
		return true, err
	}
	build := p.BuildServerID
	if build == "" {
		build = app.ServerID
	}
	if err = server.CheckConnectorDeployment(ctx, d.connector, build); err != nil {
		return true, err
	}
	r := &rollout{d: d, app: app, deployment: deployment, log: log, plan: placementPlan{Placement: p, buildServer: build, servers: []string{"swarm"}}}
	if err = r.produce(ctx); err != nil {
		return true, err
	}
	if err = validateSwarmConfig(r.app.Config); err != nil {
		return true, err
	}
	j, err := r.job(ctx, build)
	if err != nil {
		return true, err
	}
	runtimeEnv, err := d.env.ListForDeploy(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: app.ID}, app.EnvironmentID, "swarm:"+p.SwarmClusterID)
	if err != nil {
		return true, err
	}
	for _, v := range runtimeEnv {
		log.redact(v.Value)
	}
	j.env = runtimeEnv
	data, names, err := swarmCompose(releaseSpec{App: r.app, Image: r.image, ContainerName: "app", DeploymentID: deployment.ID, Env: environmentOf(j.env), Entry: j.server.Proxy.Entry(), Server: j.server})
	if err != nil {
		return true, err
	}
	name := "pier-" + app.ID
	dir, err := manager.Run(ctx, "mktemp -d /tmp/pier-swarm-XXXXXXXXXX")
	if err != nil {
		return true, err
	}
	dir = strings.TrimSpace(dir)
	if !strings.HasPrefix(dir, "/tmp/pier-swarm-") || strings.ContainsAny(dir, "\n\r ") {
		return true, i18n.Errorf("Diretório temporário Swarm inválido.")
	}
	path := dir + "/stack.yaml"

	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		_, _ = manager.Run(cleanup, shell.Join("rm", "-rf", dir))
	}()
	if err = manager.WriteFile(ctx, path, data, 0600); err != nil {
		return true, err
	}

	old, err := docker.ListSwarmServices(ctx, manager, app.ID)
	if err != nil {
		return true, err
	}
	if len(old) > 0 {
		expected := map[string]bool{}
		for _, n := range names {
			expected[n] = true
		}
		if len(old) != len(names) {
			return true, apperr.Conflict("Mantenha os mesmos processos e réplicas no alvo Swarm.")
		}
		for _, v := range old {
			if !expected[v.Name] {
				return true, apperr.Conflict("Mantenha os mesmos processos e réplicas no alvo Swarm.")
			}
		}
	}

	if err = docker.EnsureSwarmNetwork(ctx, manager); err != nil {
		return true, err
	}
	if err = d.store.setDeploymentImage(ctx, deployment.ID, r.image); err != nil {
		return true, err
	}

	return true, d.deploySwarmStack(ctx, manager, j, r.image, path, name, names, old)
}

func swarmCompose(spec releaseSpec) ([]byte, []string, error) {
	f, err := composeFor(spec)
	if err != nil {
		return nil, nil, err
	}
	raw, err := f.Marshal()
	if err != nil {
		return nil, nil, err
	}
	var doc map[string]any
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, err
	}
	services := doc["services"].(map[string]any)
	names := []string{}
	for key, value := range services {
		s := value.(map[string]any)
		labels := s["labels"].(map[string]any)
		labels["traefik.docker.network"] = docker.SwarmNetwork
		labels["traefik.swarm.network"] = docker.SwarmNetwork
		s["deploy"] = map[string]any{"labels": labels, "replicas": 1, "update_config": map[string]any{"parallelism": 1, "order": "start-first", "failure_action": "pause"}}
		delete(s, "container_name")
		delete(s, "restart")

		limits := map[string]any{}
		if cpu, ok := s["cpus"]; ok {
			limits["cpus"] = cpu
			delete(s, "cpus")
		}
		if mem, ok := s["mem_limit"]; ok {
			limits["memory"] = mem
			delete(s, "mem_limit")
		}
		if len(limits) > 0 {
			s["deploy"].(map[string]any)["resources"] = map[string]any{"limits": limits}
		}

		s["networks"] = map[string]any{docker.SwarmNetwork: map[string]any{}}
		names = append(names, "pier-"+spec.App.ID+"_"+key)
	}
	doc["networks"] = map[string]any{docker.SwarmNetwork: map[string]any{"external": true}}
	doc["version"] = "3.8"
	data, err := yaml.Marshal(doc)
	return data, names, err
}

func swarmReady(ctx context.Context, ex executor.Executor, names []string, image ...string) error {
	stable := time.Now()
	for {
		ready := true
		for _, name := range names {
			state, err := docker.SwarmUpdateState(ctx, ex, name)
			if err != nil {
				return err
			}
			if strings.Contains(state, "paused") {
				return i18n.Errorf("Rollout Swarm interrompido: %s", state)
			}
			tasks, err := docker.ListSwarmTasks(ctx, ex, name)
			if err != nil {
				return err
			}
			running := false
			for _, task := range tasks {
				if task.DesiredState == "running" {
					if task.Error != "" {
						return i18n.Errorf("Tarefa Swarm falhou: %s", task.Error)
					}
					running = running || (task.Running() && (len(image) == 0 || task.Image == image[0]))
				}
			}
			ready = ready && running && (state == "" || state == "completed" || state == "rollback_completed")
		}
		if ready && time.Since(stable) >= stabilizationPeriod {
			return nil
		}
		if !ready {
			stable = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readinessPollInterval):
		}
	}
}

func (d *Deployer) deploySwarmStack(ctx context.Context, ex executor.Executor, j *job, image, path, name string, names []string, old []docker.SwarmService) error {
	args, err := docker.StackDeployArgs(path, name)
	if err != nil {
		return err
	}

	versions := map[string]string{}
	for _, v := range old {
		version, e := ex.Run(ctx, shell.Join("docker", "service", "inspect", "--format", "{{.Version.Index}}", v.Name))
		if e != nil {
			return e
		}
		version = strings.TrimSpace(version)
		if version == "" {
			return i18n.Errorf("Não foi possível identificar a versão do serviço Swarm.")
		}
		versions[v.Name] = version
	}
	err = d.images.WithImageAuth(ctx, ex, image, func(command string, mask func(registry.Output) registry.Output) error {
		return ex.Stream(ctx, command+" "+args, mask(j.log.Output))
	})
	if err == nil {
		ready, cancel := context.WithTimeout(ctx, 3*time.Minute)
		err = swarmReady(ready, ex, names, image)
		if err == nil {
			err = d.swarmContainerReadiness(ready, ex, j, names)
		}
		cancel()
	}
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		var recovery error
		if len(old) == 0 {
			recovery = docker.RemoveSwarmStack(cleanup, ex, name)
		} else {
			for _, service := range old {
				version, e := ex.Run(cleanup, shell.Join("docker", "service", "inspect", "--format", "{{.Version.Index}}", service.Name))
				if e != nil {
					recovery = errors.Join(recovery, e)
					continue
				}
				if strings.TrimSpace(version) != versions[service.Name] {
					recovery = errors.Join(recovery, docker.RollbackSwarmService(cleanup, ex, service.Name))
				}
			}
			if recovery == nil {
				recovery = swarmReady(cleanup, ex, names)
			}
		}
		return errors.Join(err, recovery)
	}
	return d.store.setCurrentImage(ctx, j.app.ID, image)
}

// The scheduler's Running state alone is insufficient when health is starting.
// Inspect containers through the registered worker, never the build host.
func (d *Deployer) swarmContainerReadiness(ctx context.Context, manager executor.Executor, j *job, names []string) error {
	startedAt := time.Now()
	for {
		ready := len(names) > 0
		for _, name := range names {
			tasks, err := docker.ListSwarmTasks(ctx, manager, name)
			if err != nil {
				return err
			}
			inspected := false
			for _, task := range tasks {
				if task.DesiredState != "running" {
					continue
				}
				inspected = true
				location, err := manager.Run(ctx, shell.Join("docker", "inspect", "--type", "task", "--format", "{{.NodeID}}|{{.Status.ContainerStatus.ContainerID}}", task.ID))
				if err != nil {
					return err
				}
				parts := strings.Split(strings.TrimSpace(location), "|")
				if len(parts) != 2 || parts[1] == "" {
					return i18n.Errorf("Não foi possível localizar o container da tarefa Swarm.")
				}
				var serverID string
				if err = d.store.db.QueryRowContext(ctx, "SELECT server_id FROM swarm_nodes WHERE node_id = ?", parts[0]).Scan(&serverID); err != nil {
					return err
				}
				worker, _, err := d.connector.Executor(ctx, serverID)
				if err != nil {
					return err
				}
				status, err := worker.Run(ctx, shell.Join("docker", "inspect", "--format", `{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}|{{.RestartCount}}`, parts[1]))
				if err != nil {
					return err
				}
				healthy, err := evaluateReadiness(status, time.Since(startedAt))
				if err != nil {
					return err
				}
				ready = ready && healthy
			}
			ready = ready && inspected
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readinessPollInterval):
		}
	}
}
