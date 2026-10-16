package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/idgen"
	"pier/internal/platform/memo"
	"pier/internal/platform/shell"
	"pier/internal/platform/sqlite"
)

const (
	defaultRemoteDataDir = "/data/pier"
	validationTimeout    = 30 * time.Second
)

var (
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.:-]{0,253}[A-Za-z0-9])?$`)
	userPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
)

// Service implements the server use cases.
type Service struct {
	store     *Store
	keys      *KeyService
	connector *Connector
	// metrics shares collections between callers: one costs four processes
	// on the server, `docker stats` alone about two seconds. Without it a
	// burst of requests (many tabs, or a client looping) piles up processes
	// until the machine runs out of memory.
	metrics *memo.Group[Metrics]
}

const (
	metricsTTL     = 5 * time.Second
	metricsTimeout = 30 * time.Second
)

// NewService creates the service.
func NewService(store *Store, keys *KeyService, connector *Connector) *Service {
	return &Service{store: store, keys: keys, connector: connector, metrics: memo.New[Metrics](metricsTTL, metricsTimeout)}
}

// EnsureLocal registers the machine running Pier as a server on first boot,
// and keeps its data directory where Pier's own data directory is. The local
// server's directory is not a choice of the user: it follows PIER_DATA_DIR,
// so when that moves (a new path, a renamed folder) the record must follow,
// or deployments would write to a directory that no longer exists.
func (s *Service) EnsureLocal(ctx context.Context, dataDir string) error {
	servers, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	for _, existing := range servers {
		if existing.Kind == KindLocal && existing.DataDir != dataDir {
			slog.Warn("local server data directory moved", "from", existing.DataDir, "to", dataDir)
			existing.DataDir = dataDir
			if err := s.store.update(ctx, existing); err != nil {
				return err
			}
		}
	}
	if len(servers) > 0 {
		return nil
	}
	return s.store.insert(ctx, Server{
		ID:          idgen.New(),
		Name:        "localhost",
		Description: "Máquina onde o Pier está instalado.",
		Kind:        KindLocal,
		Port:        22,
		DataDir:     dataDir,
		Proxy:       DefaultProxySettings(),
		Status:      StatusUnknown,
		CreatedAt:   time.Now(),
	})
}

func (s *Service) List(ctx context.Context) ([]Server, error) { return s.store.List(ctx) }

// Get returns one server.
func (s *Service) Get(ctx context.Context, id string) (Server, error) {
	server, err := s.store.Get(ctx, id)
	return server, serverNotFound(err)
}

// Create registers a remote server reached over SSH.
func (s *Service) Create(ctx context.Context, input Input) (Server, error) {
	server := Server{ID: idgen.New(), Kind: KindSSH, Status: StatusUnknown, CreatedAt: time.Now()}
	if err := applyInput(&server, input); err != nil {
		return Server{}, err
	}
	if err := s.store.insert(ctx, server); err != nil {
		return Server{}, err
	}
	return server, nil
}

// Update changes the settings of a server.
func (s *Service) Update(ctx context.Context, id string, input Input) (Server, error) {
	server, err := s.Get(ctx, id)
	if err != nil {
		return Server{}, err
	}
	previousEndpoint := server.Host + ":" + strconv.Itoa(server.Port)
	if err := applyInput(&server, input); err != nil {
		return Server{}, err
	}
	if previousEndpoint != server.Host+":"+strconv.Itoa(server.Port) {
		// A different machine legitimately has a different host key.
		server.HostKey = ""
	}
	if err := s.store.update(ctx, server); err != nil {
		return Server{}, err
	}
	s.connector.Forget(id)
	return server, nil
}

// Delete removes a server that no longer hosts resources.
func (s *Service) Delete(ctx context.Context, id string) error {
	server, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if server.Kind == KindLocal {
		return apperr.Conflict("O servidor local não pode ser removido.")
	}
	resources, err := s.store.countResources(ctx, id)
	if err != nil {
		return err
	}
	if resources > 0 {
		return apperr.Conflict("O servidor ainda hospeda %d recurso(s). Remova-os antes.", resources)
	}
	s.connector.Forget(id)
	return s.store.delete(ctx, id)
}

// ResetHostKey forgets the pinned SSH host key (e.g. after reinstalling the machine).
func (s *Service) ResetHostKey(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	s.connector.Forget(id)
	return s.store.setHostKey(ctx, id, "")
}

// Validate checks connectivity and Docker, prepares the server and records the result.
func (s *Service) Validate(ctx context.Context, id string) (Server, error) {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()
	dockerVersion, err := s.prepare(ctx, id)
	if err != nil {
		s.connector.Forget(id)
		_ = s.store.setStatus(context.WithoutCancel(ctx), id, StatusUnreachable, describeFailure(err), "")
	} else {
		_ = s.store.setStatus(context.WithoutCancel(ctx), id, StatusReachable, "", dockerVersion)
	}
	return s.Get(context.WithoutCancel(ctx), id)
}

func (s *Service) prepare(ctx context.Context, id string) (string, error) {
	ex, server, err := s.connector.Executor(ctx, id)
	if err != nil {
		return "", err
	}
	dockerVersion, err := ex.Run(ctx, "docker version --format '{{.Server.Version}}'")
	if err != nil {
		return "", fmt.Errorf("Docker não respondeu: %w", err)
	}
	if _, err := ex.Run(ctx, "docker compose version --short"); err != nil {
		return "", fmt.Errorf("plugin docker compose ausente: %w", err)
	}
	if _, err := ex.Run(ctx, shell.Join("mkdir", "-p", server.DataDir)); err != nil {
		return "", fmt.Errorf("não foi possível criar %s: %w", server.DataDir, err)
	}
	if err := docker.EnsureNetwork(ctx, ex); err != nil {
		return "", err
	}
	return dockerVersion, nil
}

// Ping is a cheap reachability probe used by the status monitor.
func (s *Service) Ping(ctx context.Context, id string) error {
	ex, _, err := s.connector.Executor(ctx, id)
	if err == nil {
		_, err = ex.Run(ctx, "docker version --format '{{.Server.Version}}'")
	}
	if err != nil {
		s.connector.Forget(id)
		return s.store.setStatus(ctx, id, StatusUnreachable, describeFailure(err), "")
	}
	return s.store.setStatus(ctx, id, StatusReachable, "", "")
}

// Metrics returns capacity and per-container usage, collected at most once
// every few seconds per server whatever the number of callers.
func (s *Service) Metrics(ctx context.Context, id string) (Metrics, error) {
	return s.metrics.Do(ctx, id, func(ctx context.Context) (Metrics, error) { return s.collectMetrics(ctx, id) })
}

func (s *Service) collectMetrics(ctx context.Context, id string) (Metrics, error) {
	ex, server, err := s.connector.Executor(ctx, id)
	if err != nil {
		return Metrics{}, err
	}
	metrics, err := readDockerInfo(ctx, ex)
	if err != nil {
		return Metrics{}, apperr.Unavailable("Não foi possível consultar o servidor: %s", describeFailure(err))
	}
	metrics.DiskTotalBytes, metrics.DiskUsedBytes = readDiskUsage(ctx, ex, server.DataDir)
	metrics.Containers, err = readContainerUsage(ctx, ex)
	return metrics, err
}

func readDockerInfo(ctx context.Context, ex executor.Executor) (Metrics, error) {
	format := docker.FormatColumns("{{.OperatingSystem}}", "{{.Architecture}}", "{{.NCPU}}", "{{.MemTotal}}",
		"{{.ContainersRunning}}", "{{.Images}}")
	output, err := ex.Run(ctx, shell.Join("docker", "info", "--format", format))
	if err != nil {
		return Metrics{}, err
	}
	fields := strings.Split(lastLine(output), "\t")
	if len(fields) < 6 {
		return Metrics{}, fmt.Errorf("unexpected docker info output: %q", output)
	}
	metrics := Metrics{OperatingSystem: fields[0], Architecture: fields[1]}
	metrics.CPUs, _ = strconv.Atoi(fields[2])
	metrics.MemoryTotalBytes, _ = strconv.ParseInt(fields[3], 10, 64)
	metrics.ContainersRunning, _ = strconv.Atoi(fields[4])
	metrics.Images, _ = strconv.Atoi(fields[5])
	return metrics, nil
}

func readDiskUsage(ctx context.Context, ex executor.Executor, dir string) (total, used int64) {
	output, err := ex.Run(ctx, "df -Pk "+shell.Quote(dir)+" | tail -n 1")
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(output)
	if len(fields) < 4 {
		return 0, 0
	}
	totalKB, _ := strconv.ParseInt(fields[1], 10, 64)
	usedKB, _ := strconv.ParseInt(fields[2], 10, 64)
	return totalKB * 1024, usedKB * 1024
}

func readContainerUsage(ctx context.Context, ex executor.Executor) ([]ContainerUsage, error) {
	managed, err := docker.ListManaged(ctx, ex)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]docker.Container, len(managed))
	for _, container := range managed {
		byName[container.Name] = container
	}
	format := docker.FormatColumns("{{.Name}}", "{{.CPUPerc}}", "{{.MemUsage}}", "{{.MemPerc}}")
	output, err := ex.Run(ctx, shell.Join("docker", "stats", "--no-stream", "--format", format))
	if err != nil {
		return nil, fmt.Errorf("docker stats: %w", err)
	}
	usage := []ContainerUsage{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			continue
		}
		container, isManaged := byName[fields[0]]
		if !isManaged {
			continue
		}
		usage = append(usage, ContainerUsage{
			Name: fields[0], ResourceType: container.Type, ResourceID: container.ResourceID,
			CPUPercent: fields[1], MemoryUsage: fields[2], MemoryPercent: fields[3],
		})
	}
	return usage, nil
}

// Cleanup frees disk space without touching anything Pier did not create:
// stopped managed containers, dangling images and the build cache.
func (s *Service) Cleanup(ctx context.Context, id string) (string, error) {
	ex, server, err := s.connector.Executor(ctx, id)
	if err != nil {
		return "", err
	}
	commands := []string{
		"docker container prune --force --filter label=" + docker.LabelManaged + "=true",
		"docker image prune --force",
		"docker builder prune --force",
		shell.Join("rm", "-rf", path.Join(server.DataDir, "builds")),
	}
	var report strings.Builder
	for _, command := range commands {
		output, err := ex.Run(ctx, command)
		if err != nil {
			return report.String(), apperr.Unavailable("Falha na limpeza: %s", describeFailure(err))
		}
		report.WriteString("$ " + command + "\n" + output + "\n\n")
	}
	return strings.TrimSpace(report.String()), nil
}

func applyInput(server *Server, input Input) error {
	fields := apperr.Fields{}
	server.Name = strings.TrimSpace(input.Name)
	server.Description = strings.TrimSpace(input.Description)
	if server.Name == "" {
		fields.Add("name", "Informe o nome do servidor.")
	}
	if server.Kind == KindSSH {
		applyConnection(server, input, fields)
	}
	if dataDir := strings.TrimSpace(input.DataDir); dataDir != "" {
		server.DataDir = path.Clean(dataDir)
	}
	if !path.IsAbs(server.DataDir) || server.DataDir == "/" {
		fields.Add("data_dir", "Use um caminho absoluto diferente de /.")
	}
	applyProxy(server, input.Proxy, fields)
	return fields.Err()
}

func applyConnection(server *Server, input Input, fields apperr.Fields) {
	server.Host = strings.TrimSpace(input.Host)
	server.User = strings.TrimSpace(input.User)
	server.Port = input.Port
	server.PrivateKeyID = input.PrivateKeyID
	if server.Port == 0 {
		server.Port = 22
	}
	if server.DataDir == "" && strings.TrimSpace(input.DataDir) == "" {
		server.DataDir = defaultRemoteDataDir
	}
	if !hostPattern.MatchString(server.Host) {
		fields.Add("host", "Informe um IP ou hostname válido.")
	}
	if !userPattern.MatchString(server.User) {
		fields.Add("user", "Usuário SSH inválido.")
	}
	if !validPort(server.Port) {
		fields.Add("port", "Porta fora do intervalo 1–65535.")
	}
	if server.PrivateKeyID == "" {
		fields.Add("private_key_id", "Selecione a chave privada.")
	}
}

func applyProxy(server *Server, proxy ProxySettings, fields apperr.Fields) {
	if proxy.HTTPPort == 0 && proxy.HTTPSPort == 0 {
		proxy.HTTPPort, proxy.HTTPSPort = DefaultProxySettings().HTTPPort, DefaultProxySettings().HTTPSPort
	}
	proxy.ACMEEmail = strings.TrimSpace(proxy.ACMEEmail)
	if !validPort(proxy.HTTPPort) {
		fields.Add("proxy.http_port", "Porta fora do intervalo 1–65535.")
	}
	if !validPort(proxy.HTTPSPort) || proxy.HTTPSPort == proxy.HTTPPort {
		fields.Add("proxy.https_port", "Porta inválida ou igual à porta HTTP.")
	}
	if proxy.ACMEEmail != "" && !strings.Contains(proxy.ACMEEmail, "@") {
		fields.Add("proxy.acme_email", "E-mail inválido.")
	}
	proxy.Edge = normalizeEdge(proxy, fields)
	server.Proxy = proxy
}

var (
	containerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	absolutePathPattern  = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)+$`)
)

func normalizeEdge(proxy ProxySettings, fields apperr.Fields) EdgeProxy {
	edge := EdgeProxy{Container: strings.TrimSpace(proxy.Edge.Container), Caddyfile: strings.TrimSpace(proxy.Edge.Caddyfile)}
	if !edge.Enabled() {
		return EdgeProxy{}
	}
	if edge.Caddyfile == "" {
		edge.Caddyfile = DefaultCaddyfile
	}
	if !containerNamePattern.MatchString(edge.Container) {
		fields.Add("proxy.edge.container", "Nome de container inválido.")
	}
	if !absolutePathPattern.MatchString(edge.Caddyfile) {
		fields.Add("proxy.edge.caddyfile", "Informe o caminho absoluto do Caddyfile dentro do container.")
	}
	if proxy.HTTPPort == 80 || proxy.HTTPSPort == 443 {
		fields.Add("proxy.http_port", "Com um Caddy na frente, as portas 80 e 443 são dele: use outras (ex.: 8880 e 8843).")
	}
	return edge
}

func validPort(port int) bool { return port >= 1 && port <= 65535 }

func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return lines[len(lines)-1]
}

func describeFailure(err error) string {
	if errors.Is(err, executor.ErrHostKeyMismatch) {
		return "A chave de host SSH mudou desde o primeiro acesso. Se a máquina foi reinstalada, " +
			"redefina a chave de host do servidor."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Tempo esgotado ao contatar o servidor."
	}
	return err.Error()
}

func serverNotFound(err error) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound("Servidor")
	}
	return err
}
