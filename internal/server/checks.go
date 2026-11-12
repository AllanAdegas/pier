package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// Check is one actionable result of validating a server.
type Check struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
	cause   error
}

func versionAtLeast(value string, major, minor int) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	a, errA := strconv.Atoi(parts[0])
	b, errB := strconv.Atoi(parts[1])
	return errA == nil && errB == nil && (a > major || a == major && b >= minor)
}

func collectChecks(ctx context.Context, ex executor.Executor, settings ProxySettings, now func() time.Time) ([]Check, string) {
	checks := []Check{}
	language := i18n.Instance()
	add := func(code, status, message, fix string) {
		checks = append(checks, Check{Code: code, Status: status, Message: message, Fix: fix})
	}
	version, err := ex.Run(ctx, "docker version --format '{{.Server.Version}}'")
	status := "pass"
	if err != nil || !versionAtLeast(version, 24, 0) {
		status = "error"
	}
	add("docker", status, language.Text("Docker 24 ou superior"), language.Text("Instale ou atualize o Docker Engine para a versão 24 ou superior."))
	checks[len(checks)-1].cause = err
	composeVersion, err := ex.Run(ctx, "docker compose version --short")
	status = "pass"
	if err != nil || !versionAtLeast(composeVersion, 2, 20) {
		status = "error"
	}
	add("compose", status, language.Text("Docker Compose 2.20 ou superior"), language.Text("Instale ou atualize o plugin Docker Compose para a versão 2.20 ou superior."))
	checks[len(checks)-1].cause = err
	root, err := ex.Run(ctx, "docker info --format '{{.DockerRootDir}}'")
	var metrics Metrics
	diskErr := err
	if diskErr == nil && strings.HasPrefix(root, "/") {
		diskErr = readDiskUsage(ctx, ex, root, &metrics)
	}
	status = "pass"
	if diskErr != nil || metrics.DiskAvailableBytes == nil || *metrics.DiskAvailableBytes < 10_000_000_000 {
		status = "warning"
	}
	add("disk", status, language.Text("Pelo menos 10 GB livres no disco do Docker"), language.Text("Libere espaço no diretório raiz do Docker, removendo imagens e cache sem uso, ou amplie o disco."))
	dockerPS, psErr := ex.Run(ctx, `docker ps --format '{{.Names}}\t{{.Image}}\t{{.Ports}}'`)
	sockets, socketErr := ex.Run(ctx, "ss -Hltnp 2>/dev/null || ss -Hltn 2>/dev/null")
	status = "pass"
	// An explicitly configured edge may own the public ports.
	if settings.Edge.Enabled() && psErr == nil {
		dockerPS = withoutContainer(dockerPS, settings.Edge.Container)
	}
	if psErr != nil || socketErr != nil || len(portHolders(dockerPS, sockets, []int{80, 443})) > 0 {
		status = "warning"
	}
	add("ports", status, language.Text("Portas 80 e 443 livres ou usadas pelo proxy configurado"), language.Text("Libere as portas 80 e 443 ou configure o Caddy existente como proxy na frente. Confira também as regras do firewall do provedor."))
	daemon, daemonErr := ex.Run(ctx, "if test -f /etc/docker/daemon.json; then head -c 1048577 /etc/docker/daemon.json; else printf '{}'; fi")
	var config struct {
		Driver  string            `json:"log-driver"`
		Options map[string]string `json:"log-opts"`
	}
	status = "info"
	if daemonErr == nil && len(daemon) <= maxCaddyfileBytes && json.Unmarshal([]byte(daemon), &config) == nil &&
		(config.Driver == "local" || config.Driver == "json-file" && config.Options["max-size"] != "" && config.Options["max-size"] != "-1") {
		status = "pass"
	}
	add("logs", status, language.Text("Rotação de logs do Docker"), language.Text("O Pier já configura rotação por container. Para os demais, configure log-driver local ou max-size e max-file em /etc/docker/daemon.json."))
	before := now()
	clock, clockErr := ex.Run(ctx, "date +%s")
	after := now()
	timestamp, parseErr := strconv.ParseInt(strings.TrimSpace(clock), 10, 64)
	skew := time.Unix(timestamp, 0).Sub(before.Add(after.Sub(before) / 2))
	status = "pass"
	if clockErr != nil || parseErr != nil || skew > 30*time.Second || skew < -30*time.Second {
		status = "warning"
	}
	add("clock", status, language.Text("Relógio sincronizado com o Pier (até 30 segundos)"), language.Text("Ative a sincronização NTP no servidor, por exemplo: timedatectl set-ntp true."))
	return checks, version
}

func withoutContainer(output, container string) string {
	lines := []string{}
	for _, line := range strings.Split(output, "\n") {
		if name, _, _ := strings.Cut(line, "\t"); name != container {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func prepareCheckedServer(ctx context.Context, ex executor.Executor, srv Server, checks []Check) error {
	for _, check := range checks {
		if check.Status == "error" {
			detail := fmt.Errorf("%s: %s", check.Message, check.Fix)
			if check.Code == "docker" && check.cause != nil {
				return i18n.Errorf("Docker não respondeu: %w", check.cause)
			}
			if check.Code == "compose" && check.cause != nil {
				return i18n.Errorf("plugin docker compose ausente: %w", check.cause)
			}
			return detail
		}
	}
	if _, err := ex.Run(ctx, shell.Join("mkdir", "-p", srv.DataDir)); err != nil {
		return i18n.Errorf("não foi possível criar %s: %w", srv.DataDir, err)
	}
	return nil
}
