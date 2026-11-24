package compose

import (
	"context"
	"strings"
	"testing"

	"pier/internal/platform/executor"

	"gopkg.in/yaml.v3"
)

func TestParseRoute(t *testing.T) {
	valid := map[string]string{
		"https://app.example.com":     "https://app.example.com",
		"app.example.com":             "https://app.example.com",
		"HTTP://App.Example.com/":     "http://app.example.com",
		"https://site.com/api/":       "https://site.com/api",
		"http://whoami.localhost":     "http://whoami.localhost",
		"  https://a-b.example.com  ": "https://a-b.example.com",
	}
	for input, want := range valid {
		route, err := ParseRoute(input)
		if err != nil {
			t.Errorf("ParseRoute(%q) unexpected error: %v", input, err)
			continue
		}
		if got := route.String(); got != want {
			t.Errorf("ParseRoute(%q) = %q, want %q", input, got, want)
		}
	}
	// Anything that could break out of a Traefik rule or is not a plain host.
	invalid := []string{
		"", "ftp://example.com", "https://example.com:8443", "https://user:pw@example.com",
		"https://exa`mple.com", "https://example.com/a b", "https://example.com?x=1",
		"https://-bad.example.com", "https://under_score.example.com", "https://example.com/`)||Host(`x",
	}
	for _, input := range invalid {
		if _, err := ParseRoute(input); err == nil {
			t.Errorf("ParseRoute(%q) should fail", input)
		}
	}
}

func TestProxyLabelsHTTPSRedirectsAndTerminatesTLS(t *testing.T) {
	routes := []Route{{Scheme: "https", Host: "app.example.com", Path: "/api"}}
	labels := ProxyLabels("app1", "pier", routes, 3000)

	var httpRule, tlsResolver, redirect string
	for key, value := range labels {
		switch {
		case strings.HasSuffix(key, "-0-http.rule"):
			httpRule = value
		case strings.HasSuffix(key, "-0-https.tls.certresolver"):
			tlsResolver = value
		case strings.HasSuffix(key, "-0-http.middlewares"):
			redirect = value
		}
	}
	if httpRule != "Host(`app.example.com`) && PathPrefix(`/api`)" {
		t.Errorf("unexpected rule %q", httpRule)
	}
	if tlsResolver != "letsencrypt" {
		t.Errorf("https route must use the ACME resolver, got %q", tlsResolver)
	}
	if !strings.HasSuffix(redirect, "-https") {
		t.Errorf("http router of an https route must redirect, got middlewares %q", redirect)
	}
	if labels["traefik.docker.network"] != "pier" || labels["traefik.enable"] != "true" {
		t.Errorf("missing base labels: %v", labels)
	}
}

func TestProxyLabelsPlainHTTPHasNoTLSRouter(t *testing.T) {
	labels := ProxyLabels("app1", "pier", []Route{{Scheme: "http", Host: "a.localhost"}}, 80)
	for key := range labels {
		if strings.Contains(key, "-https.") && strings.Contains(key, "routers") {
			t.Errorf("http-only route must not define a TLS router: %s", key)
		}
	}
}

func TestProxyLabelsWithoutRoutesPublishesNothing(t *testing.T) {
	if labels := ProxyLabels("app1", "pier", nil, 80); len(labels) != 0 {
		t.Errorf("a resource without domains must get no proxy labels, got %v", labels)
	}
}

// Identical configuration must yield identical names (Traefik then merges old
// and new containers into one service during a rolling update); a different
// configuration must yield different names (otherwise Traefik drops both).
func TestProxyLabelNamesFollowConfiguration(t *testing.T) {
	routes := []Route{{Scheme: "https", Host: "app.example.com"}}
	first := ProxyLabels("app1", "pier", routes, 3000)
	same := ProxyLabels("app1", "pier", routes, 3000)
	otherPort := ProxyLabels("app1", "pier", routes, 4000)
	otherHost := ProxyLabels("app1", "pier", []Route{{Scheme: "https", Host: "new.example.com"}}, 3000)

	if !sameKeys(first, same) {
		t.Error("identical configuration produced different label names")
	}
	if sharesRouterKey(first, otherPort) || sharesRouterKey(first, otherHost) {
		t.Error("different configuration must not reuse router names")
	}
}

func sameKeys(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}

func sharesRouterKey(a, b map[string]string) bool {
	for key := range a {
		if _, ok := b[key]; ok && strings.Contains(key, ".routers.") {
			return true
		}
	}
	return false
}

func TestMarshalKeepsDollarSignsLiteral(t *testing.T) {
	file := &File{Services: map[string]*Service{"web": {
		Image:       "nginx",
		Environment: map[string]string{"PASSWORD": "pa$$w0rd$HOME"},
		Command:     []string{"sh", "-c", "echo $PATH"},
		Healthcheck: &Healthcheck{Test: []string{"CMD-SHELL", `pg_isready -U "$POSTGRES_USER"`}},
	}}}
	rendered, err := file.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded File
	if err := yaml.Unmarshal(rendered, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}
	web := decoded.Services["web"]
	// Compose turns "$$" back into "$", so every "$" must be doubled exactly once.
	if got := web.Environment["PASSWORD"]; got != "pa$$$$w0rd$$HOME" {
		t.Errorf("environment not escaped: %q", got)
	}
	if got := web.Command[2]; got != "echo $$PATH" {
		t.Errorf("command not escaped: %q", got)
	}
	if got := web.Healthcheck.Test[1]; got != `pg_isready -U "$$POSTGRES_USER"` {
		t.Errorf("healthcheck not escaped: %q", got)
	}
	if original := file.Services["web"].Environment["PASSWORD"]; original != "pa$$w0rd$HOME" {
		t.Errorf("Marshal must not mutate its input, got %q", original)
	}
}

func TestUpExplainsWhyItFailed(t *testing.T) {
	fake := executor.NewFake()
	fake.On("compose", " Container web Creating\n Container web Starting\nError response from daemon: Bind for 0.0.0.0:6443 failed: port is already allocated", 1)
	var streamed []string
	err := Project{Name: "p", Dir: "/data/p"}.Up(context.Background(), fake, func(line string) { streamed = append(streamed, line) })
	if err == nil || !strings.Contains(err.Error(), "port is already allocated") {
		t.Fatalf("the reason compose printed must be in the error, got %v", err)
	}
	if len(streamed) != 3 {
		t.Errorf("the sink must still receive every line, got %d", len(streamed))
	}
	if err := (Project{Name: "p", Dir: "/data/p"}).Up(context.Background(), executor.NewFake(), func(string) {}); err != nil {
		t.Errorf("a successful up must not fail: %v", err)
	}
}

// A proxy that answers HTTPS on another port (443 belongs to something else
// on the server) must redirect there, or browsers land on the other service.
// Found on a real server whose 80/443 are held by a Caddy.
func TestRedirectToHTTPSKeepsTheProxyPort(t *testing.T) {
	routes := []Route{{Scheme: "https", Host: "n8n.example.com"}}
	standard := Publication{Name: "app1", Network: "pier", Routes: routes, Port: 5678}.Labels()
	for key := range standard {
		if strings.HasSuffix(key, ".redirectscheme.port") {
			t.Errorf("the standard port needs no explicit redirect port: %s", key)
		}
	}
	// The names of a standard setup must not change with this feature.
	if legacy := ProxyLabels("app1", "pier", routes, 5678); len(legacy) != len(standard) {
		t.Errorf("ProxyLabels and a standard publication must agree")
	} else {
		for key, value := range legacy {
			if standard[key] != value {
				t.Errorf("label %s differs: %q vs %q", key, value, standard[key])
			}
		}
	}
	custom := Publication{Name: "app1", Network: "pier", Routes: routes, Port: 5678, Entry: ProxyEntry{HTTPSPort: 8843}}.Labels()
	found := false
	for key, value := range custom {
		if strings.HasSuffix(key, "-https.redirectscheme.port") {
			found = value == "8843"
		}
	}
	if !found {
		t.Errorf("the redirect must carry port 8843: %v", custom)
	}
}

// A temporary redirect (307) makes search engines keep http:// and https://
// as separate addresses; the move to HTTPS is permanent (308), as the panel's
// own route already says.
func TestRedirectToHTTPSIsPermanent(t *testing.T) {
	routes := []Route{{Scheme: "https", Host: "n8n.example.com"}}
	for _, entry := range []ProxyEntry{{}, {HTTPSPort: 8843}} {
		labels := Publication{Name: "app1", Network: "pier", Routes: routes, Port: 5678, Entry: entry}.Labels()
		permanent := ""
		for key, value := range labels {
			if strings.HasSuffix(key, "-https.redirectscheme.permanent") {
				permanent = value
			}
		}
		if permanent != "true" {
			t.Errorf("port %d: the redirect must be permanent, labels %v", entry.HTTPSPort, labels)
		}
	}
}

// Containers of the version that redirected temporarily declare a middleware
// of the same name with another value; Traefik drops a router whose
// definitions conflict, so the new labels must use new names.
func TestPermanentRedirectRenamesTheRouters(t *testing.T) {
	routes := []Route{{Scheme: "https", Host: "n8n.example.com"}}
	labels := Publication{Name: "app1", Network: "pier", Routes: routes, Port: 5678}.Labels()
	const temporaryRedirectService = "traefik.http.services.app1-43ce1b65.loadbalancer.server.port"
	if name := routerNames(labels); name == temporaryRedirectService {
		t.Errorf("router names must change with the permanent redirect, still %s", name)
	}
}

func TestBehindAnEdgeProxyEveryRouteIsPlainHTTP(t *testing.T) {
	routes := []Route{
		{Scheme: "https", Host: "n8n.example.com"},
		{Scheme: "https", Host: "n8n.example.com", Path: "/webhook"},
		{Scheme: "http", Host: "n8n.1.2.3.4.sslip.io"},
	}
	labels := Publication{Name: "app1", Network: "pier", Routes: routes, Port: 5678, Entry: ProxyEntry{Edge: true}}.Labels()
	for key, value := range labels {
		if strings.HasSuffix(key, ".entrypoints") && value != "http" {
			t.Errorf("%s = %q: the edge proxy forwards plain HTTP", key, value)
		}
		if strings.Contains(key, ".tls") || strings.Contains(key, "redirectscheme") {
			t.Errorf("TLS and redirects belong to the edge proxy: %s", key)
		}
	}
	want := "https://n8n.example.com,http://n8n.1.2.3.4.sslip.io"
	if got := labels[EdgeSitesLabel]; got != want {
		t.Errorf("sites = %q, want %q", got, want)
	}
	standard := Publication{Name: "app1", Network: "pier", Routes: routes, Port: 5678}.Labels()
	if standard[EdgeSitesLabel] != "" {
		t.Errorf("without an edge proxy there is nothing to forward")
	}
	if routerNames(labels) == routerNames(standard) {
		t.Errorf("switching to an edge proxy must rename the routers")
	}
}

func routerNames(labels map[string]string) string {
	for key := range labels {
		if strings.HasPrefix(key, "traefik.http.services.") {
			return key
		}
	}
	return ""
}

func TestMarshalRotatesLogsUnlessTheServiceChoseItsOwn(t *testing.T) {
	file := &File{Services: map[string]*Service{
		"app":    {Image: "app:1"},
		"custom": {Image: "custom:1", Logging: &Logging{Driver: "json-file"}},
	}}
	out, err := file.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Services map[string]struct {
			Logging Logging `yaml:"logging"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if got := parsed.Services["app"].Logging; got.Driver != "local" || got.Options["max-size"] != "10m" || got.Options["max-file"] != "3" {
		t.Fatalf("default logging = %+v", got)
	}
	if got := parsed.Services["custom"].Logging; got.Driver != "json-file" {
		t.Fatalf("custom logging overwritten: %+v", got)
	}
}
