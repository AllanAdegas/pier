package application

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
	"pier/internal/server"
)

const (
	readinessPollInterval = 2 * time.Second
	// A container without healthcheck is considered ready after staying up this long.
	stabilizationPeriod = 6 * time.Second
	readinessMargin     = 30 * time.Second
	failureLogTail      = 40
)

// releaseSpec is everything needed to describe the container of a deployment.
type releaseSpec struct {
	App           Application
	Image         string
	ContainerName string
	DeploymentID  string
	CommitSHA     string
	Env           map[string]string
	// Entry is how browsers reach the proxy of the server.
	Entry  compose.ProxyEntry
	Server server.Server
	// PendingCertificates are HTTPS hosts not pointing to the server yet:
	// they are published over HTTP until DNS is fixed.
	PendingCertificates map[string]bool
}

// composeFor renders the compose file that runs one release of an application:
// one service per replica of each process. Services are named after their
// containers so consecutive releases coexist inside the same compose project
// during a rolling update.
func composeFor(spec releaseSpec) (*compose.File, error) {
	config := spec.App.Config
	routes, err := parseRoutes(config.Network.Domains)
	if err != nil {
		return nil, err
	}
	file := &compose.File{
		Services: map[string]*compose.Service{},
		Networks: map[string]*compose.Network{docker.Network: {Name: docker.Network, External: true}},
		Volumes:  map[string]*compose.Volume{},
	}
	mounts := []string{}
	for _, storage := range config.Storages {
		source := storage.Source
		if storage.Kind == StorageVolume {
			source = VolumeName(spec.App.ID, storage.Source)
			file.Volumes[source] = &compose.Volume{Name: source}
		}
		mounts = append(mounts, source+":"+storage.Target)
	}
	for _, replica := range replicasOf(config, spec.ContainerName) {
		service := serviceFor(spec, replica, routes)
		service.Volumes = slices.Clone(mounts)
		applyDockerOptions(service, config.DockerOptions)
		file.Services[replica.Name] = service
	}
	return file, nil
}

// serviceFor describes one replica. Only web replicas are published (routes,
// host ports, the application's DNS alias) and health checked: identical
// labels make Traefik balance across them.
func serviceFor(spec releaseSpec, replica replica, routes []compose.Route) *compose.Service {
	config := spec.App.Config
	labels := docker.Labels(docker.TypeApplication, spec.App.ID)
	labels[docker.LabelDeployment] = spec.DeploymentID
	labels[LabelProcess] = replica.Process.Name
	service := &compose.Service{
		Image:         spec.Image,
		ContainerName: replica.Name,
		Restart:       orDefault(config.Runtime.RestartPolicy, "unless-stopped"),
		StopGracePeriod: stopGracePeriod(config.Runtime),
		Environment:   runtimeEnvironment(spec, routes, replica.Process.Name),
		Labels:        labels,
		Networks:      map[string]*compose.NetworkAttachment{docker.Network: {}},
		CPUs:          orDefault(replica.Process.Limits.CPUs, config.Limits.CPUs),
		MemLimit:      orDefault(replica.Process.Limits.Memory, config.Limits.Memory),
	}
	if command := replica.Process.Command; command != "" {
		service.Command = []string{"sh", "-c", command}
	}
	if replica.Process.Name != WebProcess {
		return service
	}
	if service.Command == nil && config.Build.StartCommand != "" && config.Build.Pack != PackNixpacks && config.Build.Pack != PackStatic && config.Build.Pack != PackRailpack {
		service.Command = []string{"sh", "-c", config.Build.StartCommand}
	}
	publication := compose.Publication{
		Name: spec.App.ID, Network: docker.Network, Routes: withoutPendingCertificates(routes, spec.PendingCertificates),
		Port: config.Network.Port, Entry: spec.Entry,
	}
	for key, value := range publication.Labels() {
		labels[key] = value
	}
	if len(routes) > 0 {
		for key, value := range server.ProxyRouteLabels(spec.Server) {
			labels[key] = value
		}
	}
	service.Ports = config.Network.PortMappings
	service.Networks[docker.Network].Aliases = []string{spec.App.ID}
	service.Healthcheck = healthcheckFor(config)
	return service
}

// VolumeName is the docker volume backing a named storage of an application.
func VolumeName(applicationID, storageName string) string { return applicationID + "_" + storageName }

func parseRoutes(domains []string) ([]compose.Route, error) {
	routes := make([]compose.Route, 0, len(domains))
	for _, domain := range domains {
		route, err := compose.ParseRoute(domain)
		if err != nil {
			return nil, i18n.Errorf("domínio %q: %w", domain, err)
		}
		routes = append(routes, route)
	}
	return routes, nil
}

// runtimeEnvironment merges the user variables with the ones Pier injects.
// User-defined values always win.
func runtimeEnvironment(spec releaseSpec, routes []compose.Route, process string) map[string]string {
	environment := map[string]string{
		"PIER_APPLICATION_ID": spec.App.ID,
		"PIER_PROCESS":        process,
		"PIER_DEPLOYMENT_ID":  spec.DeploymentID,
		"PIER_BRANCH":         spec.App.Config.Source.Branch,
		"SOURCE_COMMIT":       spec.CommitSHA,
	}
	if spec.App.Config.Build.Pack != PackStatic {
		environment["PORT"] = strconv.Itoa(spec.App.Config.Network.Port)
	}
	if len(routes) > 0 {
		environment["PIER_URL"] = routes[0].String()
	}
	for key, value := range spec.Env {
		environment[key] = value
	}
	return environment
}

func healthcheckFor(config Config) *compose.Healthcheck {
	if !config.Health.Enabled {
		return nil
	}
	test := config.Health.Command
	if test == "" {
		url := fmt.Sprintf("http://127.0.0.1:%d%s", config.Network.Port, config.Health.Path)
		test = fmt.Sprintf("curl -fsS -o /dev/null %[1]s || wget -q -O /dev/null %[1]s || exit 1", shell.Quote(url))
	}
	return &compose.Healthcheck{
		Test:        []string{"CMD-SHELL", test},
		Interval:    fmt.Sprintf("%ds", config.Health.IntervalSeconds),
		Timeout:     fmt.Sprintf("%ds", config.Health.TimeoutSeconds),
		Retries:     config.Health.Retries,
		StartPeriod: fmt.Sprintf("%ds", config.Health.StartPeriodSeconds),
	}
}

// release starts the new containers and retires the previous ones. With no
// host port mapping it is a rolling update: the old version keeps serving
// until every new replica is ready.
func (j *job) release(ctx context.Context, image string, env map[string]string) error {
	// Readable in `docker ps`; unique per deployment so consecutive releases
	// coexist during the rolling update.
	containerName := docker.ReadableName(j.app.Name, j.app.ID) + "-" + docker.ShortID(j.deployment.ID)
	file, err := composeFor(releaseSpec{
		App: j.app, Image: image, ContainerName: containerName, DeploymentID: j.deployment.ID,
		CommitSHA: j.deployment.CommitSHA, Env: env, Entry: j.server.Proxy.Entry(), Server: j.server,
		PendingCertificates: j.pendingCertificates(ctx),
	})
	if err != nil {
		return err
	}
	replicas := replicasOf(j.app.Config, containerName)
	primary := primaryReplica(replicas)
	if err := j.runRelease(ctx, file, primary.Name); err != nil {
		return err
	}
	yaml, err := file.Marshal()
	if err != nil {
		return err
	}
	project := compose.Project{Name: j.app.ID, Dir: j.server.ResourceDir(docker.TypeApplication, j.app.ID)}
	if err := project.Write(ctx, j.ex, yaml, nil); err != nil {
		return err
	}
	previous, err := docker.ListByResource(ctx, j.ex, j.app.ID)
	if err != nil {
		return err
	}
	if err := j.runHook(ctx, "pré-deploy", j.app.Config.Hooks.PreDeploy, j.previousHookTarget(ctx, previous)); err != nil {
		return err
	}
	rolling := len(j.app.Config.Network.PortMappings) == 0
	if !rolling && len(previous) > 0 {
		j.captureRetiredLogs(ctx, previous)
		j.log.Info("Portas mapeadas no host impedem o rolling update: parando a versão atual antes de subir a nova.")
		if err := docker.Remove(ctx, j.ex, previous); err != nil {
			return err
		}
		previous = nil
	}
	if err := j.startReplicas(ctx, project, replicas); err != nil {
		return err
	}
	if primary.Process.Name == WebProcess {
		j.diagnoseListeners(ctx, primary.Name)
	}
	if err := j.retire(ctx, previous); err != nil {
		return err
	}
	if len(j.app.Config.Network.Domains) > 0 && !docker.ProxyRunning(ctx, j.ex) {
		j.log.Info(docker.ProxyStoppedWarning)
	}
	server.ReportEdgeSync(ctx, j.ex, j.server, func(message string) { j.log.Info("%s", message) })
	return j.runHook(ctx, "pós-deploy", j.app.Config.Hooks.PostDeploy, primary.Name)
}

// startReplicas brings every replica up and waits until all are ready. If
// any fails, all new replicas are removed and the previous version stays.
func (j *job) startReplicas(ctx context.Context, project compose.Project, replicas []replica) error {
	names := make([]string, len(replicas))
	for index, replica := range replicas {
		names[index] = replica.Name
	}
	j.log.Command("docker compose up --detach (" + strings.Join(names, ", ") + ")")
	if err := project.Up(ctx, j.ex, j.log.Output); err != nil {
		j.discard(names...)
		return err
	}
	if len(replicas) > 1 {
		j.log.Info("Aguardando %d contêineres da nova versão ficarem prontos.", len(replicas))
	}
	for _, replica := range replicas {
		if err := j.waitUntilReady(ctx, replica.Name); err != nil {
			err = j.explainNotReady(ctx, replica, err)
			j.discard(names...)
			return err
		}
	}
	return nil
}

// explainNotReady logs what is known about a replica that did not get ready.
func (j *job) explainNotReady(ctx context.Context, replica replica, err error) error {
	if replica.Process.Name == WebProcess {
		j.diagnoseListeners(ctx, replica.Name)
	}
	j.dumpContainerLogs(replica.Name)
	// Container exit status explains runtime OOM even when its log is empty.
	inspectCtx, stopInspect := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	state, _ := docker.Inspect(inspectCtx, j.ex, replica.Name, "{{.State.ExitCode}}")
	stopInspect()
	if code, parseErr := strconv.Atoi(strings.TrimSpace(state)); parseErr == nil && code > 0 {
		err = fmt.Errorf("%w: %w", err, &executor.ExitError{Command: replica.Name, Code: code})
	}
	return err
}

// previousHookTarget is the running web container of the current version.
func (j *job) previousHookTarget(ctx context.Context, previous []docker.Container) string {
	if j.app.Config.Hooks.PreDeploy == "" {
		return ""
	}
	return runningWebContainer(previous, containerProcesses(ctx, j.ex, j.app.ID))
}

// retire takes the previous version out of service: a graceful stop lets
// in-flight requests finish before the containers are removed.
func (j *job) retire(ctx context.Context, previous []docker.Container) error {
	if len(previous) == 0 {
		return nil
	}
	j.log.Info("Nova versão pronta. Removendo %d container(s) da versão anterior.", len(previous))
	if err := stopApplicationContainers(ctx, j.ex, previous); err != nil {
		return err
	}
	j.captureRetiredLogs(ctx, previous)
	return docker.Remove(ctx, j.ex, previous)
}

func (j *job) runHook(ctx context.Context, label, script, container string) error {
	if script == "" {
		return nil
	}
	label = i18n.Instance().Text(label)
	if container == "" {
		j.log.Info("Comando de %s ignorado: não há container em execução.", label)
		return nil
	}
	j.log.Command(fmt.Sprintf("[%s] %s", label, script))
	if err := j.ex.Stream(ctx, docker.Exec(container, script), j.log.Output); err != nil {
		return i18n.Errorf("comando de %s falhou: %w", label, err)
	}
	return nil
}

// waitUntilReady blocks until the container is healthy (or stable, when it
// has no healthcheck) and fails as soon as it is known to be broken.
func (j *job) waitUntilReady(ctx context.Context, containerName string) error {
	health := j.app.Config.Health
	deadline := time.Now().Add(stabilizationPeriod + readinessMargin)
	if health.Enabled {
		budget := health.StartPeriodSeconds + health.Retries*(health.IntervalSeconds+health.TimeoutSeconds)
		deadline = time.Now().Add(time.Duration(budget)*time.Second + readinessMargin)
		j.log.Info("Aguardando o healthcheck do novo container (até %d tentativas).", health.Retries)
	} else {
		j.log.Info("Healthcheck desativado: aguardando o container estabilizar.")
	}
	startedAt := time.Now()
	const format = `{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}|{{.RestartCount}}`
	for {
		output, err := docker.Inspect(ctx, j.ex, containerName, format)
		if err != nil {
			return i18n.Errorf("inspecionar o novo container: %w", err)
		}
		ready, err := evaluateReadiness(output, time.Since(startedAt))
		if err != nil || ready {
			return err
		}
		if time.Now().After(deadline) {
			return i18n.Errorf("o container não ficou pronto dentro do tempo limite (estado: %s)", output)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readinessPollInterval):
		}
	}
}

// evaluateReadiness interprets "status|health|restarts" from docker inspect.
func evaluateReadiness(inspection string, uptime time.Duration) (bool, error) {
	fields := strings.Split(strings.TrimSpace(inspection), "|")
	if len(fields) != 3 {
		return false, i18n.Errorf("resposta inesperada do docker inspect: %q", inspection)
	}
	status, health, restarts := fields[0], fields[1], fields[2]
	switch {
	case status == "exited" || status == "dead":
		return false, i18n.Errorf("o container encerrou logo após iniciar")
	case restarts != "0":
		return false, i18n.Errorf("o container está reiniciando em loop")
	case health == "healthy":
		return true, nil
	case health == "unhealthy":
		return false, i18n.Errorf("o healthcheck do novo container falhou")
	case health == "none":
		return status == "running" && uptime >= stabilizationPeriod, nil
	}
	return false, nil
}

func (j *job) dumpContainerLogs(containerName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	j.log.Info("Últimas linhas do container que falhou:")
	_ = j.ex.Stream(ctx, docker.LogsCommand(containerName, failureLogTail, false), j.log.Output)
}

// discard removes containers that did not make it, leaving the previous
// version untouched.
func (j *job) discard(containerNames ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = j.ex.Run(ctx, shell.Join(append([]string{"docker", "rm", "-f"}, containerNames...)...))
}
