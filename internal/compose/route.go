package compose

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"pier/internal/platform/i18n"
)

var pathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

var hostnamePattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Route is a public URL served by the proxy: scheme, host and optional path prefix.
type Route struct {
	Scheme string
	Host   string
	Path   string
}

// ParseRoute validates a domain entered by the user, e.g. "https://app.example.com/api".
// A value without scheme is treated as https.
func ParseRoute(raw string) (Route, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Route{}, i18n.Errorf("domínio vazio")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return Route{}, i18n.Errorf("URL inválida")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Route{}, i18n.Errorf("use http:// ou https://")
	}
	if parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Route{}, i18n.Errorf("informe apenas esquema, host e caminho (sem porta ou query)")
	}
	host := strings.ToLower(parsed.Hostname())
	if len(host) > 253 || !hostnamePattern.MatchString(host) {
		return Route{}, i18n.Errorf("host inválido")
	}
	// The decoded path is validated against an allowlist: it is interpolated
	// into a Traefik rule, so nothing outside plain URL segments may pass.
	path := strings.TrimSuffix(parsed.Path, "/")
	if path != "" && !pathPattern.MatchString(path) {
		return Route{}, i18n.Errorf("caminho inválido: use apenas letras, números, '-', '_', '.' e '/'")
	}
	return Route{Scheme: parsed.Scheme, Host: host, Path: path}, nil
}

// String renders the route back to its canonical URL.
func (r Route) String() string { return r.Scheme + "://" + r.Host + r.Path }

func (r Route) rule() string {
	rule := "Host(`" + r.Host + "`)"
	if r.Path != "" {
		rule += " && PathPrefix(`" + r.Path + "`)"
	}
	return rule
}

// labelSchema changes whenever the layout of the generated labels changes, so
// containers created by different versions never share router names.
const labelSchema = "v3"

// ProxyLabels returns the Traefik labels that publish a container port on the
// given routes, reached through the given docker network, behind a proxy that
// answers HTTPS on the standard port.
func ProxyLabels(resourceName, network string, routes []Route, port int) map[string]string {
	return Publication{Name: resourceName, Network: network, Routes: routes, Port: port}.Labels()
}

// DefaultHTTPSPort is where browsers look for HTTPS when the URL has no port.
const DefaultHTTPSPort = 443

// EdgeSitesLabel lists, on every container published behind an edge proxy,
// the sites ("https://host", "http://host") the edge must forward to Pier.
const EdgeSitesLabel = "pier.edge.sites"

// ProxyEntry is how browsers reach the proxy of a server.
type ProxyEntry struct {
	// HTTPSPort is where the proxy answers HTTPS. Zero means the standard
	// 443. A proxy on another port (because 443 belongs to something else on
	// the server) needs the HTTP→HTTPS redirect to say so, or browsers are
	// sent to port 443.
	HTTPSPort int
	// Edge means another proxy in front of Pier's (a Caddy that owns ports
	// 80/443) terminates TLS and forwards plain HTTP, so every route is
	// served on the HTTP entrypoint, without redirect or certificate.
	Edge bool
}

// Publication is a container port published on routes through the proxy.
type Publication struct {
	Name    string
	Network string
	Routes  []Route
	// Port is the port of the container.
	Port  int
	Entry ProxyEntry
	// Indexed are the hosts search engines may index. Every route of any
	// other host answers noindex: nothing is indexed unless marked.
	Indexed map[string]bool
}

// noIndexHeader keeps search engines from indexing a response.
const noIndexHeader = "noindex, nofollow"

// Labels returns the Traefik labels of the publication.
//
// Router names end with a fingerprint of the configuration. Containers with
// identical configuration (the usual rolling update) share the names, so
// Traefik merges them into one load-balanced service; when the configuration
// changes between deployments the names differ, which avoids the conflicting
// definitions that would make Traefik drop the router while both are running.
func (p Publication) Labels() map[string]string {
	network, routes, port := p.Network, p.Routes, p.Port
	if len(routes) == 0 {
		return map[string]string{}
	}
	name := p.Name + "-" + p.fingerprint()
	if p.Entry.Edge {
		return p.edgeLabels(name)
	}
	httpsPort := p.Entry.httpsPort()
	labels := map[string]string{
		"traefik.enable":         "true",
		"traefik.docker.network": network,
		"traefik.http.services." + name + ".loadbalancer.server.port": strconv.Itoa(port),
		"traefik.http.middlewares." + name + "-gzip.compress":         "true",
		// During a rolling update the old container may vanish before the
		// proxy notices; retrying sends the request to the new one.
		"traefik.http.middlewares." + name + "-retry.retry.attempts":        "3",
		"traefik.http.middlewares." + name + "-retry.retry.initialinterval": "100ms",
		"traefik.http.middlewares." + name + "-https.redirectscheme.scheme": "https",
		// Permanent (308): search engines then treat the HTTPS address as the
		// only one, as the panel's own route does.
		"traefik.http.middlewares." + name + "-https.redirectscheme.permanent": "true",
	}
	if httpsPort != DefaultHTTPSPort {
		labels["traefik.http.middlewares."+name+"-https.redirectscheme.port"] = strconv.Itoa(httpsPort)
	}
	p.addNoIndex(labels, name)
	for index, route := range routes {
		httpRouter := fmt.Sprintf("traefik.http.routers.%s-%d-http", name, index)
		labels[httpRouter+".rule"] = route.rule()
		labels[httpRouter+".entrypoints"] = "http"
		labels[httpRouter+".service"] = name
		if route.Scheme == "http" {
			labels[httpRouter+".middlewares"] = p.serving(name, route)
			continue
		}
		labels[httpRouter+".middlewares"] = name + "-https"
		httpsRouter := fmt.Sprintf("traefik.http.routers.%s-%d-https", name, index)
		labels[httpsRouter+".rule"] = route.rule()
		labels[httpsRouter+".entrypoints"] = "https"
		labels[httpsRouter+".service"] = name
		labels[httpsRouter+".middlewares"] = p.serving(name, route)
		labels[httpsRouter+".tls"] = "true"
		labels[httpsRouter+".tls.certresolver"] = "letsencrypt"
	}
	return labels
}

func (e ProxyEntry) httpsPort() int {
	if e.HTTPSPort == 0 {
		return DefaultHTTPSPort
	}
	return e.HTTPSPort
}

func (p Publication) fingerprint() string {
	input := fmt.Append(nil, labelSchema, p.Network, p.Routes, p.Port)
	// Only when they differ from the standard, so the names of existing
	// standard setups do not change.
	if httpsPort := p.Entry.httpsPort(); httpsPort != DefaultHTTPSPort {
		input = fmt.Append(input, httpsPort)
	}
	if p.Entry.Edge {
		input = fmt.Append(input, "edge")
	}
	// Marking a host changes the middlewares of its routers.
	input = fmt.Append(input, p.indexedHosts())
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:4])
}

// edgeLabels serves every route as plain HTTP: the edge proxy in front
// already terminated TLS and redirected HTTP to HTTPS where the route asks.
func (p Publication) edgeLabels(name string) map[string]string {
	labels := map[string]string{
		"traefik.enable":         "true",
		"traefik.docker.network": p.Network,
		"traefik.http.services." + name + ".loadbalancer.server.port":       strconv.Itoa(p.Port),
		"traefik.http.middlewares." + name + "-gzip.compress":               "true",
		"traefik.http.middlewares." + name + "-retry.retry.attempts":        "3",
		"traefik.http.middlewares." + name + "-retry.retry.initialinterval": "100ms",
		EdgeSitesLabel: strings.Join(EdgeSites(p.Routes), ","),
	}
	p.addNoIndex(labels, name)
	for index, route := range p.Routes {
		router := fmt.Sprintf("traefik.http.routers.%s-%d-http", name, index)
		labels[router+".rule"] = route.rule()
		labels[router+".entrypoints"] = "http"
		labels[router+".service"] = name
		labels[router+".middlewares"] = p.serving(name, route)
	}
	return labels
}

// serving is the middleware chain of a router that serves the container.
func (p Publication) serving(name string, route Route) string {
	chain := name + "-retry," + name + "-gzip"
	if !p.Indexed[route.Host] {
		chain += "," + name + "-noindex"
	}
	return chain
}

// addNoIndex declares the noindex middleware when a route needs it.
func (p Publication) addNoIndex(labels map[string]string, name string) {
	for _, route := range p.Routes {
		if !p.Indexed[route.Host] {
			labels["traefik.http.middlewares."+name+"-noindex.headers.customresponseheaders.X-Robots-Tag"] = noIndexHeader
			return
		}
	}
}

// indexedHosts lists, sorted, the routed hosts that may be indexed: the
// only part of Indexed that changes the labels.
func (p Publication) indexedHosts() []string {
	hosts := []string{}
	for _, route := range p.Routes {
		if p.Indexed[route.Host] && !slices.Contains(hosts, route.Host) {
			hosts = append(hosts, route.Host)
		}
	}
	slices.Sort(hosts)
	return hosts
}

// EdgeSites returns the distinct "scheme://host" sites of the routes, in
// order: an edge proxy forwards whole hosts, the paths are Pier's business.
func EdgeSites(routes []Route) []string {
	sites := []string{}
	seen := map[string]bool{}
	for _, route := range routes {
		site := route.Scheme + "://" + route.Host
		if !seen[site] {
			seen[site] = true
			sites = append(sites, site)
		}
	}
	return sites
}
