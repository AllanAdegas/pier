package server

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/oplog"
	"pier/internal/platform/shell"
)

// Docker install assist: on a Linux server reached over SSH, install Docker
// Engine with the official convenience script (get.docker.com) or with the
// distribution's own packages, then verify the versions Pier requires.

const (
	DockerConvenience = "convenience"
	DockerPackages    = "packages"

	dockerInstallTimeout = 20 * time.Minute
	convenienceScriptURL = "https://get.docker.com"
)

// Distributions each method supports, by os-release ID. The convenience
// script documents exactly these; the package lists below exist only for these.
var (
	convenienceSystems = []string{"ubuntu", "debian", "raspbian", "fedora", "centos", "rhel"}
	packageCommands    = map[string][]string{
		"ubuntu":   aptInstall("docker.io", "docker-compose-v2", "docker-buildx"),
		"debian":   aptInstall("docker.io", "docker-compose", "docker-buildx"),
		"raspbian": aptInstall("docker.io", "docker-compose", "docker-buildx"),
		"fedora":   {"{sudo}dnf install -y moby-engine docker-compose docker-buildx"},
	}
)

func aptInstall(packages ...string) []string {
	return []string{
		"{sudo}env DEBIAN_FRONTEND=noninteractive apt-get update -q",
		"{sudo}env DEBIAN_FRONTEND=noninteractive apt-get install -y -q " + strings.Join(packages, " "),
	}
}

// DockerInstallRequest is what the operator confirms.
type DockerInstallRequest struct {
	Method       string `json:"method"`
	Confirmation string `json:"confirmation"`
}

// DockerInstallResult is what the verification found.
type DockerInstallResult struct {
	System         string `json:"system"`
	DockerVersion  string `json:"docker_version"`
	ComposeVersion string `json:"compose_version"`
}

type hostSystem struct{ Kernel, ID, Pretty string }

type dockerConnector interface {
	Executor(context.Context, string) (executor.Executor, Server, error)
	Forget(string)
}

// DockerInstaller runs the install assist, one at a time per server.
type DockerInstaller struct {
	connector  dockerConnector
	validate   func(context.Context, string) (Server, error)
	operations *oplog.Registry
}

// NewDockerInstaller creates the installer. validate, when set, refreshes the
// server checks after a successful install.
func NewDockerInstaller(connector dockerConnector, validate func(context.Context, string) (Server, error)) *DockerInstaller {
	return &DockerInstaller{connector: connector, validate: validate, operations: oplog.NewRegistry()}
}

// Start checks the request and installs in the background; Status follows it.
func (d *DockerInstaller) Start(ctx context.Context, id string, request DockerInstallRequest) (oplog.Snapshot, error) {
	_, target, err := d.connector.Executor(ctx, id)
	if err != nil {
		return oplog.Snapshot{}, err
	}
	if err := checkInstallRequest(target, request); err != nil {
		return oplog.Snapshot{}, err
	}
	operation, started := d.operations.Begin(id, i18n.Instance().Text("Instalando o Docker"))
	if !started {
		return oplog.Snapshot{}, apperr.Conflict("Já há uma instalação do Docker em andamento neste servidor.")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), dockerInstallTimeout)
		defer cancel()
		result, err := d.Install(ctx, id, request.Method, operation.Append)
		if err == nil {
			operation.Say("Docker %s e Compose %s prontos em %s.", result.DockerVersion, result.ComposeVersion, result.System)
		}
		operation.Finish(localized(err))
	}()
	return d.Status(id), nil
}

// Status returns the latest install operation of a server.
func (d *DockerInstaller) Status(id string) oplog.Snapshot {
	snapshot, _ := d.operations.Get(id)
	return snapshot
}

func checkInstallRequest(target Server, request DockerInstallRequest) error {
	if target.Kind != KindSSH {
		return apperr.Invalid("A instalação assistida só vale para servidores SSH.")
	}
	if request.Method != DockerConvenience && request.Method != DockerPackages {
		return apperr.Invalid("Escolha o método: script oficial ou pacotes da distribuição.")
	}
	if strings.TrimSpace(request.Confirmation) != target.Name {
		return apperr.Invalid("Digite o nome do servidor para confirmar a instalação.")
	}
	return nil
}

// Install installs and verifies Docker synchronously, writing the output to
// sink. It refuses unsupported systems before running anything privileged.
func (d *DockerInstaller) Install(ctx context.Context, id, method string, sink executor.LineSink) (DockerInstallResult, error) {
	ex, target, err := d.connector.Executor(ctx, id)
	if err != nil {
		return DockerInstallResult{}, err
	}
	system, err := detectSystem(ctx, ex)
	if err != nil {
		return DockerInstallResult{}, err
	}
	sink(i18n.Instance().Sprintf("Sistema detectado: %s.", system.Pretty))
	commands, err := installCommands(method, system)
	if err != nil {
		return DockerInstallResult{}, err
	}
	sudo, err := privilegePrefix(ctx, ex, target.User)
	if err != nil {
		return DockerInstallResult{}, err
	}
	if _, err := verifyDocker(ctx, ex); err == nil {
		sink(i18n.Instance().Text("O Docker já está instalado nas versões exigidas. Nada foi alterado."))
	} else if err := runInstall(ctx, ex, commands, sudo, sink); err != nil {
		return DockerInstallResult{}, err
	}
	if sudo != "" {
		if err := runInstall(ctx, ex, []string{"{sudo}usermod -aG docker " + shell.Quote(target.User)}, sudo, sink); err != nil {
			return DockerInstallResult{}, err
		}
	}
	// A new session picks up the docker group of a non-root user.
	d.connector.Forget(id)
	return d.verifyAfterInstall(ctx, id, system)
}

func (d *DockerInstaller) verifyAfterInstall(ctx context.Context, id string, system hostSystem) (DockerInstallResult, error) {
	ex, _, err := d.connector.Executor(ctx, id)
	if err != nil {
		return DockerInstallResult{}, err
	}
	result, err := verifyDocker(ctx, ex)
	if err != nil {
		return DockerInstallResult{}, err
	}
	result.System = system.Pretty
	if d.validate != nil {
		if _, err := d.validate(ctx, id); err != nil {
			return DockerInstallResult{}, err
		}
	}
	return result, nil
}

func detectSystem(ctx context.Context, ex executor.Executor) (hostSystem, error) {
	kernel, err := ex.Run(ctx, "uname -s")
	if err != nil {
		return hostSystem{}, apperr.Unavailable("Não foi possível identificar o sistema do servidor: %s", describeFailure(err))
	}
	if strings.TrimSpace(kernel) != "Linux" {
		return hostSystem{}, apperr.Invalid("Sistema não suportado: %s. A instalação assistida exige Linux.", strings.TrimSpace(kernel))
	}
	release, err := ex.Run(ctx, "cat /etc/os-release")
	if err != nil {
		return hostSystem{}, apperr.Invalid("O servidor não tem /etc/os-release; instale o Docker manualmente (docs.docker.com/engine/install).")
	}
	values := parseOSRelease(release)
	system := hostSystem{Kernel: "Linux", ID: strings.ToLower(values["ID"]), Pretty: values["PRETTY_NAME"]}
	if system.Pretty == "" {
		system.Pretty = system.ID
	}
	return system, nil
}

func parseOSRelease(text string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && key != "" && !strings.HasPrefix(key, "#") {
			values[key] = strings.Trim(value, `"'`)
		}
	}
	return values
}

func installCommands(method string, system hostSystem) ([]string, error) {
	switch method {
	case DockerConvenience:
		if !slices.Contains(convenienceSystems, system.ID) {
			return nil, apperr.Invalid("O script oficial não suporta %s. Instale o Docker manualmente (docs.docker.com/engine/install).", system.Pretty)
		}
		download := `script=$(mktemp) && { curl -fsSL ` + convenienceScriptURL + ` -o "$script" || wget -qO "$script" ` +
			convenienceScriptURL + `; } && {sudo}sh "$script"; status=$?; rm -f "$script"; exit $status`
		return []string{download, enableDocker}, nil
	case DockerPackages:
		commands, supported := packageCommands[system.ID]
		if !supported {
			return nil, apperr.Invalid("Não há pacotes do Docker conhecidos para %s. Use o script oficial ou instale manualmente.", system.Pretty)
		}
		return append(slices.Clone(commands), enableDocker), nil
	}
	return nil, apperr.Invalid("Escolha o método: script oficial ou pacotes da distribuição.")
}

const enableDocker = "if command -v systemctl >/dev/null 2>&1; then {sudo}systemctl enable --now docker; fi"

// privilegePrefix is empty for root and "sudo -n " for a user with
// passwordless sudo; anything else cannot install packages unattended.
func privilegePrefix(ctx context.Context, ex executor.Executor, user string) (string, error) {
	uid, err := ex.Run(ctx, "id -u")
	if err != nil {
		return "", apperr.Unavailable("Não foi possível consultar o usuário no servidor: %s", describeFailure(err))
	}
	if strings.TrimSpace(uid) == "0" {
		return "", nil
	}
	if _, err := ex.Run(ctx, "sudo -n true"); err != nil {
		return "", apperr.Invalid("O usuário %s não é root e não tem sudo sem senha. Instale o Docker como root ou libere o sudo.", user)
	}
	return "sudo -n ", nil
}

func runInstall(ctx context.Context, ex executor.Executor, commands []string, sudo string, sink executor.LineSink) error {
	for _, template := range commands {
		command := strings.ReplaceAll(template, "{sudo}", sudo)
		sink("$ " + command)
		if err := ex.Stream(ctx, command, sink); err != nil {
			return apperr.Unavailable("A instalação falhou: %s", describeFailure(err))
		}
	}
	return nil
}

func verifyDocker(ctx context.Context, ex executor.Executor) (DockerInstallResult, error) {
	version, err := ex.Run(ctx, "docker version --format '{{.Server.Version}}'")
	if err != nil || !versionAtLeast(version, 24, 0) {
		return DockerInstallResult{}, apperr.Unavailable("O Docker não respondeu na versão 24 ou superior depois da instalação.")
	}
	compose, err := ex.Run(ctx, "docker compose version --short")
	if err != nil || !versionAtLeast(compose, 2, 20) {
		return DockerInstallResult{}, apperr.Unavailable("O Docker Compose 2.20 ou superior não está disponível. Use o script oficial, que instala o plugin compose.")
	}
	return DockerInstallResult{DockerVersion: lastLine(version), ComposeVersion: lastLine(compose)}, nil
}

// localized renders an apperr in the instance language for operation logs.
func localized(err error) error {
	if err == nil {
		return nil
	}
	if appErr, ok := apperr.As(err); ok {
		return errors.New(appErr.Localize(i18n.Instance()))
	}
	return err
}
