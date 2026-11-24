package indexnow

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func renderRoutes(t *testing.T, target RouteTarget, sites ...Site) dynamicFile {
	t.Helper()
	content, err := RouteFile(sites, target)
	if err != nil {
		t.Fatal(err)
	}
	var file dynamicFile
	if err := yaml.Unmarshal(content, &file); err != nil {
		t.Fatalf("invalid yaml: %v\n%s", err, content)
	}
	return file
}

var asigas = Site{ID: "s1", Host: "asigas.com", Key: "0123456789abcdef0123456789abcdef"}

func TestTheLocalProxyRoutesTheKeyToThePanelContainer(t *testing.T) {
	file := renderRoutes(t, RouteTarget{Local: true}, asigas)
	route := file.HTTP.Routers["pier-indexnow-s1"]
	if route.Rule != "Host(`asigas.com`) && Path(`/0123456789abcdef0123456789abcdef.txt`)" {
		t.Errorf("rule = %q", route.Rule)
	}
	if route.Priority <= len("Host(`asigas.com`)") || route.TLS == nil || strings.Join(route.EntryPoints, ",") != "https" {
		t.Errorf("the key must win over the app's routes on https: %+v", route)
	}
	if got := file.HTTP.Middlewares["pier-indexnow-s1"].ReplacePath.Path; got != "/indexnow/0123456789abcdef0123456789abcdef.txt" {
		t.Errorf("rewrite = %q", got)
	}
	balancer := file.HTTP.Services["pier-indexnow"].LoadBalancer
	if balancer.Servers[0].URL != PanelService || !balancer.PassHostHeader {
		t.Errorf("local service = %+v", balancer)
	}
}

func TestARemoteProxyRoutesTheKeyToThePublicURLOfThePanel(t *testing.T) {
	file := renderRoutes(t, RouteTarget{PanelURL: "https://pier.example.com/dashboard"}, asigas)
	balancer := file.HTTP.Services["pier-indexnow"].LoadBalancer
	if balancer.Servers[0].URL != "https://pier.example.com" || balancer.PassHostHeader {
		t.Errorf("the remote proxy must call the panel on its own domain: %+v", balancer)
	}
}

func TestBehindAnEdgeProxyTheKeyIsServedOnPlainHTTP(t *testing.T) {
	route := renderRoutes(t, RouteTarget{Local: true, Edge: true}, asigas).HTTP.Routers["pier-indexnow-s1"]
	if strings.Join(route.EntryPoints, ",") != "http" || route.TLS != nil {
		t.Errorf("the Caddy in front forwards plain HTTP: %+v", route)
	}
}

func TestNothingIsRoutedWithoutSitesOrAWayToThePanel(t *testing.T) {
	for name, target := range map[string]RouteTarget{"no sites": {Local: true}, "remote without URL": {}, "bad URL": {PanelURL: "ftp://x"}} {
		sites := []Site{asigas}
		if name == "no sites" {
			sites = nil
		}
		content, err := RouteFile(sites, target)
		if err != nil || content != nil {
			t.Errorf("%s: want no file, got %q (%v)", name, content, err)
		}
	}
}
