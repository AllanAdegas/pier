package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

const maxCaddyfileBytes = 1 << 20

// edgeLocks serializes the edits of one server's Caddyfile: two deployments
// finishing together must not interleave read-modify-write.
var edgeLocks sync.Map

// EdgeSync is the outcome of bringing the edge proxy in line with the server.
type EdgeSync struct {
	// Addresses are the Caddy site addresses forwarded to Pier.
	Addresses []string `json:"addresses"`
	// Caddyfile is the file edited, as a path of the server.
	Caddyfile string `json:"caddyfile"`
	Upstream  string `json:"upstream"`
	Changed   bool   `json:"changed"`
}

// edgeTarget is where the Caddyfile of the edge proxy lives and how Caddy
// reaches Pier's proxy.
type edgeTarget struct {
	edge      EdgeProxy
	hostPath  string
	upstream  string
	trustedIP string
}

// SyncEdge rewrites the Pier block of the edge proxy's Caddyfile with the
// sites published on the server (the containers carrying the edge label,
// running or not) and reloads Caddy. The candidate is validated by Caddy
// before the file is touched, and a failed reload restores the old file.
func SyncEdge(ctx context.Context, ex executor.Executor, srv Server) (EdgeSync, error) {
	edge := srv.Proxy.Edge
	if !edge.Enabled() {
		return EdgeSync{}, apperr.Invalid("Este servidor não tem um proxy na frente configurado.")
	}
	lock, _ := edgeLocks.LoadOrStore(srv.ID, make(chan struct{}, 1))
	gate := lock.(chan struct{})
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return EdgeSync{}, ctx.Err()
	}
	defer func() { <-gate }()

	target, err := locateEdge(ctx, ex, edge, srv.Proxy.HTTPPort)
	if err != nil {
		return EdgeSync{}, err
	}
	sites, err := publishedEdgeSites(ctx, ex, srv)
	if err != nil {
		return EdgeSync{}, unavailable(err)
	}
	result := EdgeSync{Addresses: edgeAddresses(sites), Caddyfile: target.hostPath, Upstream: target.upstream}
	current, err := readCaddyfile(ctx, ex, target.edge)
	if err != nil {
		return EdgeSync{}, unavailable(err)
	}
	next, err := replaceEdgeBlock(current, renderEdgeBlock(result.Addresses, target.upstream))
	if err != nil {
		return EdgeSync{}, apperr.Conflict("%s", err.Error())
	}
	if next == current {
		return result, nil
	}
	if err := keepOriginal(ctx, ex, srv, current); err != nil {
		return EdgeSync{}, unavailable(err)
	}
	if err := applyCaddyfile(ctx, ex, target, current, next); err != nil {
		return EdgeSync{}, err
	}
	result.Changed = true
	return result, nil
}

// ReportEdgeSync syncs the edge proxy after resources changed on the server,
// telling the outcome through report. Failures do not undo the deployment:
// the containers are serving, only the edge is behind.
func ReportEdgeSync(ctx context.Context, ex executor.Executor, srv Server, report func(string)) {
	if !srv.Proxy.Edge.Enabled() {
		return
	}
	result, err := SyncEdge(ctx, ex, srv)
	switch {
	case err != nil:
		report(i18n.Instance().Sprintf("AVISO: o Caddy na frente deste servidor não foi atualizado, "+
			"então os domínios podem não responder: %s", err))
	case result.Changed:
		report(i18n.Instance().Sprintf("Caddy na frente atualizado: %d endereço(s) encaminhados ao Pier.", len(result.Addresses)))
	}
}

// LogEdgeSync is ReportEdgeSync for flows without a visible log.
func LogEdgeSync(ctx context.Context, ex executor.Executor, srv Server) {
	ReportEdgeSync(ctx, ex, srv, func(message string) {
		slog.Info("edge proxy sync", "server", srv.ID, "result", message)
	})
}

// DetectEdgeCandidates lists the running Caddy containers of the server, the
// ones that can be set as the Caddy in front. The interface only offers the
// setting when there is one.
func DetectEdgeCandidates(ctx context.Context, ex executor.Executor) ([]string, error) {
	output, err := ex.Run(ctx, `docker ps --format '{{.Names}}\t{{.Image}}'`)
	if err != nil {
		return nil, err
	}
	return caddyContainers(output), nil
}

// caddyContainers picks, from `docker ps --format '{{.Names}}\t{{.Image}}'`,
// the containers whose image is a Caddy ("caddy:2-alpine",
// "lucaslorentz/caddy-docker-proxy", "ghcr.io/org/caddy").
func caddyContainers(dockerPS string) []string {
	names := []string{}
	for _, line := range strings.Split(dockerPS, "\n") {
		name, image, found := strings.Cut(strings.TrimSpace(line), "\t")
		if !found {
			continue
		}
		repository := image
		if slash := strings.LastIndex(repository, "/"); slash >= 0 {
			repository = repository[slash+1:]
		}
		repository, _, _ = strings.Cut(repository, ":")
		repository, _, _ = strings.Cut(repository, "@")
		if strings.Contains(repository, "caddy") {
			names = append(names, name)
		}
	}
	return names
}

func locateEdge(ctx context.Context, ex executor.Executor, edge EdgeProxy, httpPort int) (edgeTarget, error) {
	mounts, err := docker.Inspect(ctx, ex, edge.Container,
		"{{range .Mounts}}{{.Source}}\t{{.Destination}}\n{{end}}")
	if err != nil {
		return edgeTarget{}, apperr.Unavailable("Não foi possível inspecionar o container %s: %s",
			edge.Container, describeFailure(err))
	}
	hostPath, found := hostPathOf(mounts, edge.Caddyfile)
	if !found {
		return edgeTarget{}, apperr.Conflict("O Caddyfile %s do container %s não vem de um arquivo do servidor "+
			"(nenhuma montagem o cobre), então o Pier não tem como editá-lo de forma duradoura.",
			edge.Caddyfile, edge.Container)
	}
	gateways, err := docker.Inspect(ctx, ex, edge.Container,
		"{{range .NetworkSettings.Networks}}{{.Gateway}}\n{{end}}")
	gateway := firstLine(gateways)
	if _, parseErr := netip.ParseAddr(gateway); err != nil || parseErr != nil {
		return edgeTarget{}, apperr.Conflict("O container %s não está em uma rede Docker com gateway; "+
			"o Caddy não teria como alcançar o proxy do Pier.", edge.Container)
	}
	// Pier's proxy publishes its HTTP port on every address of the server,
	// the gateway of the Caddy network included.
	return edgeTarget{edge: edge, hostPath: hostPath, upstream: net.JoinHostPort(gateway, strconv.Itoa(httpPort)), trustedIP: gateway}, nil
}

// hostPathOf finds the server file behind a path of the container, given
// "source\tdestination" mount lines. A mount of the file itself or of one of
// its directories counts.
func hostPathOf(mounts, containerPath string) (string, bool) {
	best, bestDestination := "", ""
	for _, line := range strings.Split(mounts, "\n") {
		source, destination, found := strings.Cut(strings.TrimSpace(line), "\t")
		if !found || source == "" {
			continue
		}
		relative, inside := strings.CutPrefix(containerPath, strings.TrimSuffix(destination, "/"))
		if !inside || (relative != "" && !strings.HasPrefix(relative, "/")) {
			continue
		}
		if len(destination) > len(bestDestination) {
			best, bestDestination = path.Join(source, relative), destination
		}
	}
	return best, best != ""
}

// publishedEdgeSites collects the sites of the containers Pier published.
// The label of any other container is ignored, and every site is parsed as
// a route: the values end up in the Caddyfile, so a crafted label must not
// be able to add Caddy directives.
func publishedEdgeSites(ctx context.Context, ex executor.Executor, srv Server) ([]string, error) {
	if srv.ProxyToken == "" {
		return []string{}, nil
	}
	output, err := ex.Run(ctx, shell.Join("docker", "ps", "--all",
		"--filter", "label="+docker.LabelManaged+"=true", "--filter", "label="+compose.EdgeSitesLabel,
		"--filter", "label="+ProxyLabel+"="+srv.ProxyToken,
		"--format", `{{.Label "`+compose.EdgeSitesLabel+`"}}`))
	if err != nil {
		return nil, err
	}
	return parseEdgeSites(output), nil
}

func parseEdgeSites(output string) []string {
	sites := []string{}
	for _, line := range strings.Split(output, "\n") {
		for _, site := range strings.Split(line, ",") {
			route, err := compose.ParseRoute(strings.TrimSpace(site))
			if err != nil || route.Path != "" || !strings.Contains(site, "://") {
				continue
			}
			sites = append(sites, route.Scheme+"://"+route.Host)
		}
	}
	return sites
}

// keepOriginal saves the Caddyfile as it was before Pier first touched it,
// so whoever owns it can always go back.
func keepOriginal(ctx context.Context, ex executor.Executor, srv Server, content string) error {
	backup := path.Join(srv.ProxyDir(), "edge", "Caddyfile.before-pier")
	err := executor.WriteFileExclusive(ctx, ex, backup, []byte(content), 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	// Remote ln reports an existing file through its exit status.
	if err != nil {
		if _, checkErr := ex.Run(ctx, shell.Join("test", "!", "-L", backup)+" && "+shell.Join("test", "-f", backup)); checkErr == nil {
			return nil
		}
	}
	return err
}

func applyCaddyfile(ctx context.Context, ex executor.Executor, target edgeTarget, current, next string) error {
	if err := validateCaddyfile(ctx, ex, target.edge.Container, next); err != nil {
		return apperr.Conflict("O Caddy recusou a configuração com o bloco do Pier (nada foi alterado): %s",
			describeFailure(err))
	}
	// Written in place (same inode): a file bind mount would keep showing the
	// old content to the container if the file were replaced.
	if err := writeCaddyfile(ctx, ex, target.edge, next); err != nil {
		return unavailable(err)
	}
	if err := reloadCaddy(ctx, ex, target.edge); err != nil {
		restoreErr := writeCaddyfile(ctx, ex, target.edge, current)
		if restoreErr == nil {
			restoreErr = reloadCaddy(ctx, ex, target.edge)
		}
		if restoreErr != nil {
			return apperr.Unavailable("O Caddy não recarregou (%s) e restaurar o Caddyfile anterior também falhou "+
				"(%s). Confira %s no servidor.", describeFailure(err), describeFailure(restoreErr), target.hostPath)
		}
		return apperr.Unavailable("O Caddy não recarregou; o Caddyfile anterior foi restaurado: %s", describeFailure(err))
	}
	return nil
}

// validateCaddyfile runs `caddy validate` on the candidate inside the
// container, where the environment placeholders of the file resolve.
func validateCaddyfile(ctx context.Context, ex executor.Executor, container, content string) error {
	if len(content) > maxCaddyfileBytes {
		return executor.ErrFileTooLarge
	}
	_, err := executor.RunInput(ctx, ex, shell.Join("docker", "exec", "-i", container,
		"caddy", "validate", "--adapter", "caddyfile", "--config", "/dev/stdin"), []byte(content))
	return err
}

// All operator-file access stays in the edge container's namespace. A
// malicious mount or symlink cannot grant Pier additional host access.
func readCaddyfile(ctx context.Context, ex executor.Executor, edge EdgeProxy) (string, error) {
	script := "test -f " + shell.Quote(edge.Caddyfile) + " && head -c " + strconv.Itoa(maxCaddyfileBytes+1) + " -- " + shell.Quote(edge.Caddyfile)
	content, err := executor.RunInput(ctx, ex, shell.Join("docker", "exec", edge.Container, "sh", "-c", script), nil)
	if len(content) > maxCaddyfileBytes {
		return "", executor.ErrFileTooLarge
	}
	return content, err
}
func writeCaddyfile(ctx context.Context, ex executor.Executor, edge EdgeProxy, content string) error {
	if len(content) > maxCaddyfileBytes {
		return executor.ErrFileTooLarge
	}
	// Preserve the inode for file bind mounts and preserve the operator's mode.
	script := "test -f " + shell.Quote(edge.Caddyfile) + " && cat > " + shell.Quote(edge.Caddyfile)
	_, err := executor.RunInput(ctx, ex, shell.Join("docker", "exec", "-i", edge.Container, "sh", "-c", script), []byte(content))
	return err
}

func reloadCaddy(ctx context.Context, ex executor.Executor, edge EdgeProxy) error {
	_, err := ex.Run(ctx, shell.Join("docker", "exec", edge.Container,
		"caddy", "reload", "--adapter", "caddyfile", "--config", edge.Caddyfile))
	return err
}

func firstLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
