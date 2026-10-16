package server

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
)

// The proxy publishes fixed ports. When another program already holds one
// (a Caddy or an nginx that was on the server before Pier), docker refuses
// with a technical message and leaves a created container behind. Checking
// first lets Pier say who holds the port and what to do about it.

// publishedPort matches a host port in the output of `docker ps`, such as
// "0.0.0.0:80->80/tcp" or "[::]:443->443/tcp".
var publishedPort = regexp.MustCompile(`(?:[\d.]+|\[[0-9a-f:]*\]):(\d+)->`)

// listeningPort matches the local address column of `ss -ltn`, such as
// "0.0.0.0:80", "[::]:443" or "127.0.0.1:8080".
var listeningPort = regexp.MustCompile(`^(?:\S*):(\d+)$`)

// ssProcess matches the process name `ss -p` prints: users:(("caddy",pid=1,fd=3)).
var ssProcess = regexp.MustCompile(`users:\(\("([^"]+)"`)

// portHolders reports, for each wanted port, who already uses it: a container
// (other than the proxy itself) or, when docker does not know, a process.
// dockerPS is `docker ps --format '{{.Names}}\t{{.Image}}\t{{.Ports}}'`;
// sockets is `ss -Hltnp` (empty when ss is not available).
func portHolders(dockerPS, sockets string, wanted []int) map[int]string {
	want := map[int]bool{}
	for _, port := range wanted {
		want[port] = true
	}
	holders := map[int]string{}
	for _, line := range strings.Split(dockerPS, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 || fields[0] == docker.ProxyContainer {
			continue
		}
		for _, match := range publishedPort.FindAllStringSubmatch(fields[2], -1) {
			port, _ := strconv.Atoi(match[1])
			if want[port] && holders[port] == "" {
				holders[port] = fmt.Sprintf("o container %s (%s)", fields[0], fields[1])
			}
		}
	}
	for _, line := range strings.Split(sockets, "\n") {
		columns := strings.Fields(line)
		if len(columns) < 4 {
			continue
		}
		match := listeningPort.FindStringSubmatch(columns[3])
		if match == nil {
			continue
		}
		port, _ := strconv.Atoi(match[1])
		if !want[port] || holders[port] != "" {
			continue
		}
		process := ssProcess.FindStringSubmatch(line)
		switch {
		case process != nil && process[1] == "docker-proxy":
			// A port docker publishes; already reported by name above, or it
			// belongs to the proxy itself.
		case process != nil:
			holders[port] = "o processo " + process[1]
		default:
			holders[port] = "outro programa"
		}
	}
	return holders
}

// requireFreePorts refuses to start the proxy on ports another program holds.
func requireFreePorts(ctx context.Context, ex executor.Executor, settings ProxySettings) error {
	dockerPS, err := ex.Run(ctx, `docker ps --format '{{.Names}}\t{{.Image}}\t{{.Ports}}'`)
	if err != nil {
		// Without the list there is nothing to say; docker will refuse anyway.
		return nil
	}
	sockets, _ := ex.Run(ctx, "ss -Hltnp 2>/dev/null || ss -Hltn 2>/dev/null || true")
	holders := portHolders(dockerPS, sockets, []int{settings.HTTPPort, settings.HTTPSPort})
	if len(holders) == 0 {
		return nil
	}
	ports := make([]int, 0, len(holders))
	for port := range holders {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	parts := make([]string, len(ports))
	for index, port := range ports {
		parts[index] = fmt.Sprintf("a porta %d já é usada por %s", port, holders[port])
	}
	advice := "Escolha outras portas em “Portas e certificados” (e acesse os domínios com a porta no endereço) " +
		"ou libere essas portas no servidor."
	if strings.Contains(strings.Join(parts, " "), "caddy") {
		advice = "Para manter esse Caddy, use outras portas (ex.: 8880 e 8843) e configure-o como “Caddy na frente” " +
			"em “Portas e certificados”: o Pier passa a incluir os domínios dele no Caddyfile."
	}
	return apperr.Conflict("O proxy não pode subir: %s. %s", strings.Join(parts, "; "), advice)
}
