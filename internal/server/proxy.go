package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/netip"
	"path"
	"strings"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
)

const (
	// ProxyImage is the Traefik release Pier is tested against.
	ProxyImage       = "traefik:v3.6"
	proxyProjectName = "pier-proxy"
	proxyLogTail     = 300
)

// ProxyService manages the Traefik reverse proxy of each server.
type ProxyService struct {
	connector *Connector
}

// NewProxyService creates the service.
func NewProxyService(connector *Connector) *ProxyService {
	return &ProxyService{connector: connector}
}

// ProxyCompose builds the compose file of the proxy for a server.
func ProxyCompose(server Server) *compose.File {
	command := []string{
		"--ping=true",
		"--api.dashboard=false",
		"--entrypoints.http.address=:80",
		"--entrypoints.https.address=:443",
		"--entrypoints.https.http3=true",
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.docker.constraints=" + proxyConstraint(server),
		"--providers.docker.network=" + docker.Network,
		"--providers.file.directory=/traefik/dynamic/",
		"--providers.file.watch=true",
		// A container removed mid-rollout stops answering; fail fast so the
		// retry middleware can move the request to the new container.
		"--serverstransport.forwardingtimeouts.dialtimeout=2s",
		"--certificatesresolvers.letsencrypt.acme.httpchallenge=true",
		"--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=http",
		"--certificatesresolvers.letsencrypt.acme.storage=/traefik/acme.json",
	}
	if server.Proxy.Edge.Enabled() {
		if ip, err := netip.ParseAddr(server.EdgeTrustedIP); err == nil {
			prefix := "/32"
			if ip.Is6() {
				prefix = "/128"
			}
			command = append(command, "--entrypoints.http.forwardedheaders.trustedips="+ip.String()+prefix)
		}
	}

	if server.Proxy.ACMEEmail != "" {
		command = append(command, "--certificatesresolvers.letsencrypt.acme.email="+server.Proxy.ACMEEmail)
	}
	return &compose.File{
		Services: map[string]*compose.Service{
			"traefik": {
				Image:         ProxyImage,
				ContainerName: docker.ProxyContainer,
				Restart:       "unless-stopped",
				Command:       command,
				Labels:        docker.Labels(docker.TypeProxy, "proxy"),
				Ports: []string{
					fmt.Sprintf("%d:80", server.Proxy.HTTPPort),
					fmt.Sprintf("%d:443", server.Proxy.HTTPSPort),
					fmt.Sprintf("%d:443/udp", server.Proxy.HTTPSPort),
				},
				Volumes: []string{
					"/var/run/docker.sock:/var/run/docker.sock:ro",
					server.ProxyDir() + ":/traefik",
				},
				Networks:   map[string]*compose.NetworkAttachment{docker.Network: {}, PanelNetwork: {}},
				ExtraHosts: []string{"host.docker.internal:host-gateway"},
				Healthcheck: &compose.Healthcheck{
					Test:     []string{"CMD", "traefik", "healthcheck", "--ping"},
					Interval: "10s", Timeout: "5s", Retries: 5,
				},
			},
		},
		Networks: map[string]*compose.Network{docker.Network: {Name: docker.Network, External: true}, PanelNetwork: {Name: PanelNetwork, External: true}},
	}
}

func proxyProject(server Server) compose.Project {
	return compose.Project{Name: proxyProjectName, Dir: server.ProxyDir()}
}

// Start writes the configuration and (re)creates the proxy container.
func (p *ProxyService) Start(ctx context.Context, serverID string) error {
	ex, server, err := p.connector.Executor(ctx, serverID)
	if err != nil {
		return err
	}
	if err := requireFreePorts(ctx, ex, server.Proxy); err != nil {
		return err
	}
	if err := docker.EnsureNetwork(ctx, ex); err != nil {
		return unavailable(err)
	}
	if server.Proxy.Edge.Enabled() {
		target, err := locateEdge(ctx, ex, server.Proxy.Edge, server.Proxy.HTTPPort)
		if err != nil {
			return err
		}
		server.EdgeTrustedIP = target.trustedIP
	}
	if _, err := ex.Run(ctx, "docker network inspect "+PanelNetwork+" >/dev/null 2>&1 || docker network create "+PanelNetwork); err != nil {
		return unavailable(err)
	}
	yaml, err := ProxyCompose(server).Marshal()
	if err != nil {
		return err
	}
	project := proxyProject(server)
	if err := project.Write(ctx, ex, yaml, nil); err != nil {
		return unavailable(err)
	}
	// Traefik refuses to start when the watched directory is missing.
	keepFile := path.Join(server.ProxyDir(), "dynamic", ".keep")
	if err := ex.WriteFile(ctx, keepFile, nil, 0o644); err != nil {
		return unavailable(err)
	}
	if err := project.Up(ctx, ex, func(string) {}, "--remove-orphans"); err != nil {
		return unavailable(err)
	}
	return unavailable(docker.ConnectProxyToIngressNetworks(ctx, ex))
}

// Stop removes the proxy container; certificates stay on disk.
func (p *ProxyService) Stop(ctx context.Context, serverID string) error {
	ex, _, err := p.connector.Executor(ctx, serverID)
	if err != nil {
		return err
	}
	containers, err := p.containers(ctx, ex)
	if err != nil {
		return unavailable(err)
	}
	return unavailable(docker.Remove(ctx, ex, containers))
}

// State reports whether the proxy is running.
func (p *ProxyService) State(ctx context.Context, serverID string) (ProxyState, error) {
	ex, _, err := p.connector.Executor(ctx, serverID)
	if err != nil {
		return ProxyState{}, err
	}
	containers, err := p.containers(ctx, ex)
	if err != nil {
		return ProxyState{}, unavailable(err)
	}
	if len(containers) == 0 {
		return ProxyState{Status: "Parado", Image: ProxyImage}, nil
	}
	container := containers[0]
	return ProxyState{Running: container.State == "running", Status: container.Status, Image: container.Image}, nil
}

// Configuration returns the compose file the proxy runs with.
func (p *ProxyService) Configuration(ctx context.Context, serverID string) (string, error) {
	ex, server, err := p.connector.Executor(ctx, serverID)
	if err != nil {
		return "", err
	}
	if server.Proxy.Edge.Enabled() {
		target, err := locateEdge(ctx, ex, server.Proxy.Edge, server.Proxy.HTTPPort)
		if err != nil {
			return "", err
		}
		server.EdgeTrustedIP = target.trustedIP
	}
	yaml, err := ProxyCompose(server).Marshal()
	if server.ProxyToken == "" {
		return string(yaml), err
	}
	return strings.ReplaceAll(string(yaml), server.ProxyToken, "[redacted]"), err
}

// EdgeCandidates lists the Caddy containers running on the server. Detection
// is best effort: an unreachable server simply offers none.
func (p *ProxyService) EdgeCandidates(ctx context.Context, serverID string) []string {
	ex, _, err := p.connector.Executor(ctx, serverID)
	if err != nil {
		return []string{}
	}
	candidates, err := DetectEdgeCandidates(ctx, ex)
	if err != nil {
		return []string{}
	}
	return candidates
}

// SyncEdge brings the Caddy in front of the server in line with the domains
// published on it.
func (p *ProxyService) SyncEdge(ctx context.Context, serverID string) (EdgeSync, error) {
	ex, server, err := p.connector.Executor(ctx, serverID)
	if err != nil {
		return EdgeSync{}, err
	}
	return SyncEdge(ctx, ex, server)
}

// Logs returns the most recent proxy log lines.
func (p *ProxyService) Logs(ctx context.Context, serverID string) (string, error) {
	ex, _, err := p.connector.Executor(ctx, serverID)
	if err != nil {
		return "", err
	}
	containers, err := p.containers(ctx, ex)
	if err != nil {
		return "", unavailable(err)
	}
	if len(containers) == 0 {
		return "", nil
	}
	output, err := ex.Run(ctx, docker.LogsCommand(containers[0].ID, proxyLogTail, false))
	return output, unavailable(err)
}

func (p *ProxyService) containers(ctx context.Context, ex executor.Executor) ([]docker.Container, error) {
	managed, err := docker.ListManaged(ctx, ex)
	if err != nil {
		return nil, err
	}
	proxies := []docker.Container{}
	for _, container := range managed {
		if container.Type == docker.TypeProxy {
			proxies = append(proxies, container)
		}
	}
	return proxies, nil
}

func unavailable(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	return apperr.Unavailable("Falha ao operar o servidor: %s", describeFailure(err))
}

// ProxyLabel authenticates route labels added by Pier rather than an image.
const ProxyLabel = "pier.proxy"

// PanelNetwork isolates the panel backend from ordinary application networks.
const PanelNetwork = "pier-panel"

// ProxyRouteLabels must be merged into every routed service by its renderer.
// The server token is internal and must never be returned by read APIs.
func ProxyRouteLabels(server Server) map[string]string {
	if server.ProxyToken == "" {
		return map[string]string{}
	}
	return map[string]string{ProxyLabel: server.ProxyToken}
}
func proxyConstraint(server Server) string {
	token := server.ProxyToken
	if token == "" {
		token = "!unconfigured!"
	}
	return "Label(`" + ProxyLabel + "`, `" + token + "`) && Label(`" + docker.LabelManaged + "`, `true`)"
}

// NewProxyToken creates a per-server route capability with 256 bits of entropy.
func NewProxyToken() string { return rand.Text() + rand.Text() }
