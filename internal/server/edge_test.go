package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
)

const edgeHostFile = "/opt/concord/docker/Caddyfile"

func edgeServer() Server {
	return Server{ID: "srv1", ProxyToken: "edge-test-token", EdgeTrustedIP: "172.18.0.1", DataDir: "/var/lib/pier", Proxy: ProxySettings{
		HTTPPort: 8880, HTTPSPort: 8843, Edge: EdgeProxy{Container: "docker-caddy-1", Caddyfile: DefaultCaddyfile},
	}}
}

// edgeFake answers like the test VPS: Caddy mounts the Caddyfile from a file
// of the server and sits on a network whose gateway is 172.18.0.1.
func edgeFake() *edgeTestExecutor {
	fake := executor.NewFake()
	fake.On("{{range .Mounts}}", "/var/lib/docker/volumes/docker_caddy_data/_data\t/data\n"+edgeHostFile+"\t/etc/caddy/Caddyfile", 0)
	fake.On("{{range .NetworkSettings.Networks}}", "172.18.0.1", 0)
	fake.On("docker ps", "https://n8n.example.com,http://n8n.1.2.3.4.sslip.io\n\nhttps://n8n.example.com", 0)
	fake.On("test -e", "", 1)
	fake.Files[edgeHostFile] = []byte(foreignCaddyfile)
	return &edgeTestExecutor{Fake: fake}
}

func TestSyncEdgeForwardsThePublishedSites(t *testing.T) {
	fake := edgeFake()
	result, err := SyncEdge(context.Background(), fake, edgeServer())
	if err != nil {
		t.Fatal(err)
	}
	written := string(fake.Files[edgeHostFile])
	if !result.Changed || !strings.Contains(written, "http://n8n.1.2.3.4.sslip.io, n8n.example.com {\n\treverse_proxy 172.18.0.1:8880\n}") {
		t.Fatalf("unexpected Caddyfile (%+v):\n%s", result, written)
	}
	if string(fake.Files["/var/lib/pier/proxy/edge/Caddyfile.before-pier"]) != foreignCaddyfile {
		t.Error("the original Caddyfile must be kept before the first edit")
	}
	if !fake.Ran("caddy validate") || !fake.Ran("caddy reload --adapter caddyfile --config /etc/caddy/Caddyfile") {
		t.Errorf("the candidate must be validated and Caddy reloaded: %v", fake.Commands)
	}

	again, err := SyncEdge(context.Background(), fake, edgeServer())
	if err != nil || again.Changed {
		t.Errorf("nothing changed on the server, nothing to write: %+v %v", again, err)
	}
}

func TestSyncEdgeLeavesTheFileWhenCaddyRefusesIt(t *testing.T) {
	fake := edgeFake().On("caddy validate", "Error: ambiguous site definition: n8n.example.com", 1)
	_, err := SyncEdge(context.Background(), fake, edgeServer())
	if err == nil || !strings.Contains(err.Error(), "ambiguous site definition") {
		t.Fatalf("Caddy's reason must reach the user: %v", err)
	}
	if string(fake.Files[edgeHostFile]) != foreignCaddyfile || fake.Ran("caddy reload") {
		t.Error("a refused candidate must not be written nor loaded")
	}
}

func TestSyncEdgeRestoresTheFileWhenTheReloadFails(t *testing.T) {
	fake := edgeFake().On("caddy reload", "Error: loading new config: listen tcp :443: bind: address already in use", 1)
	if _, err := SyncEdge(context.Background(), fake, edgeServer()); err == nil {
		t.Fatal("a failed reload is an error")
	}
	if string(fake.Files[edgeHostFile]) != foreignCaddyfile {
		t.Errorf("the previous Caddyfile must be back:\n%s", fake.Files[edgeHostFile])
	}
}

func TestSyncEdgeRefusesACaddyfileThatIsNotOnTheServer(t *testing.T) {
	fake := edgeFake().On("{{range .Mounts}}", "/var/lib/docker/volumes/docker_caddy_data/_data\t/data", 0)
	if _, err := SyncEdge(context.Background(), fake, edgeServer()); err == nil {
		t.Fatal("a Caddyfile baked into the image cannot be edited durably")
	}
}

func TestHostPathOfFollowsDirectoryMounts(t *testing.T) {
	mounts := "/srv/caddy\t/etc/caddy\n/srv/data\t/data"
	if got, _ := hostPathOf(mounts, "/etc/caddy/Caddyfile"); got != "/srv/caddy/Caddyfile" {
		t.Errorf("got %q", got)
	}
	if _, found := hostPathOf("/srv/x\t/etc/cad", "/etc/caddy/Caddyfile"); found {
		t.Error("a mount of /etc/cad does not cover /etc/caddy")
	}
}

func TestEdgeSettingsNeedOtherPorts(t *testing.T) {
	var server Server
	fields := apperr.Fields{}
	applyProxy(&server, ProxySettings{HTTPPort: 80, HTTPSPort: 443, Edge: EdgeProxy{Container: "docker-caddy-1"}}, fields)
	if len(fields) == 0 {
		t.Error("ports 80/443 belong to the edge proxy")
	}
	fields = apperr.Fields{}
	applyProxy(&server, ProxySettings{HTTPPort: 8880, HTTPSPort: 8843, Edge: EdgeProxy{Container: " docker-caddy-1 "}}, fields)
	if len(fields) != 0 || server.Proxy.Edge.Caddyfile != DefaultCaddyfile || server.Proxy.Edge.Container != "docker-caddy-1" {
		t.Errorf("valid edge settings: %v %+v", fields, server.Proxy.Edge)
	}
}

func TestTheProxyTrustsTheEdgeForwardedHeaders(t *testing.T) {
	trusts := func(srv Server) bool {
		for _, argument := range ProxyCompose(srv).Services["traefik"].Command {
			if strings.HasPrefix(argument, "--entrypoints.http.forwardedheaders.trustedips=") {
				return true
			}
		}
		return false
	}
	if !trusts(edgeServer()) {
		t.Error("behind Caddy, applications must see the https scheme Caddy received")
	}
	if trusts(Server{Proxy: DefaultProxySettings()}) {
		t.Error("without an edge proxy no forwarded header is trusted")
	}
}

func TestCaddyContainersAreDetectedByImage(t *testing.T) {
	dockerPS := "docker-caddy-1\tcaddy:2-alpine\n" +
		"pier-proxy\ttraefik:v3.6\n" +
		"edge\tlucaslorentz/caddy-docker-proxy:2.9\n" +
		"web\tghcr.io/acme/caddy@sha256:abc\n" +
		"caddy-named-nginx\tnginx:1.27\n" +
		"n8n\tdocker.n8n.io/n8nio/n8n"
	got := strings.Join(caddyContainers(dockerPS), ",")
	if got != "docker-caddy-1,edge,web" {
		t.Errorf("got %q: the image decides, not the container name", got)
	}
	if len(caddyContainers("pier-proxy\ttraefik:v3.6")) != 0 {
		t.Error("a server without Caddy offers nothing")
	}
}

func TestACraftedLabelCannotInjectCaddyDirectives(t *testing.T) {
	output := "https://ok.example.com,http://plain.example.com\n" +
		"https://evil.example.com {\n\treverse_proxy attacker:80\n}\n" +
		"evil.example.com\nhttps://x.example.com/path,ftp://y.example.com,https://bad host.com"
	got := strings.Join(parseEdgeSites(output), " ")
	if got != "https://ok.example.com http://plain.example.com" {
		t.Errorf("only well-formed scheme://host sites may reach the Caddyfile, got %q", got)
	}
}

func TestOnlyContainersPierManagesAreForwarded(t *testing.T) {
	fake := edgeFake()
	if _, err := SyncEdge(context.Background(), fake, edgeServer()); err != nil {
		t.Fatal(err)
	}
	if !fake.Ran("--filter label=pier.managed=true --filter label=pier.edge.sites") {
		t.Errorf("the listing must filter by the managed label: %v", fake.Commands)
	}
}

// The fake emulates only the container's namespace, never a host file read.
type edgeTestExecutor struct{ *executor.Fake }

func (e *edgeTestExecutor) On(match, output string, code int) *edgeTestExecutor {
	e.Fake.On(match, output, code)
	return e
}
func (e *edgeTestExecutor) RunInput(ctx context.Context, command string, content []byte) (string, error) {
	output, err := e.Fake.RunInput(ctx, command, content)
	if err != nil {
		return output, err
	}
	if strings.Contains(command, "head -c") && output == "" {
		return string(e.Files[edgeHostFile]), nil
	}
	if strings.Contains(command, "&& cat >") {
		e.Files[edgeHostFile] = append([]byte(nil), content...)
	}
	return output, nil
}
func TestEdgeContentStaysOffArgvAndAccessStaysInsideContainer(t *testing.T) {
	fake := edgeFake()
	if _, err := SyncEdge(context.Background(), fake, edgeServer()); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range fake.Commands {
		if strings.Contains(cmd, "chmod") || strings.Contains(cmd, "APP_DOMAIN") || strings.Contains(cmd, "cat "+edgeHostFile) || strings.Contains(cmd, "head -c 1048577 -- "+edgeHostFile) {
			t.Fatalf("operator content or host access in command: %s", cmd)
		}
	}
	found := false
	for command, input := range fake.Inputs {
		if strings.Contains(command, "caddy validate") {
			found = true
			if !strings.Contains(string(input), "APP_DOMAIN") || !strings.Contains(command, "/dev/stdin") {
				t.Fatal("candidate not validated through stdin")
			}
		}
	}
	if !found {
		t.Fatal("validation missing")
	}
}
func TestEdgeRejectsOversizedConfigurationBeforeValidationOrBackup(t *testing.T) {
	fake := edgeFake().On("head -c", strings.Repeat("x", maxCaddyfileBytes+1), 0)
	if _, err := SyncEdge(context.Background(), fake, edgeServer()); err == nil {
		t.Fatal("oversized configuration accepted")
	}
	if fake.Ran("caddy validate") || len(fake.Files) != 1 {
		t.Fatal("oversized configuration reached validation or backup")
	}
}
func TestEdgeSitesRequireServerRouteToken(t *testing.T) {
	fake := edgeFake()
	srv := edgeServer()
	if _, err := publishedEdgeSites(context.Background(), fake, srv); err != nil {
		t.Fatal(err)
	}
	if !fake.Ran("--filter label=pier.proxy=" + srv.ProxyToken) {
		t.Fatal("missing route token filter")
	}
	srv.ProxyToken = ""
	sites, err := publishedEdgeSites(context.Background(), fake, srv)
	if err != nil || len(sites) != 0 {
		t.Fatal("missing token must fail closed")
	}
}
func TestTrustedHeadersAreLimitedToOneAddress(t *testing.T) {
	srv := edgeServer()
	command := strings.Join(ProxyCompose(srv).Services["traefik"].Command, " ")
	if !strings.Contains(command, "trustedips=172.18.0.1/32") || strings.Contains(command, "172.16.0.0/12") {
		t.Fatalf("broad trust: %s", command)
	}
	srv.EdgeTrustedIP = ""
	if strings.Contains(strings.Join(ProxyCompose(srv).Services["traefik"].Command, " "), "trustedips") {
		t.Fatal("unknown address must not be trusted")
	}
}

func TestEdgeLockHonorsCancellation(t *testing.T) {
	srv := edgeServer()
	srv.ID = t.Name()
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	edgeLocks.Store(srv.ID, gate)
	defer edgeLocks.Delete(srv.ID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SyncEdge(ctx, edgeFake(), srv); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
