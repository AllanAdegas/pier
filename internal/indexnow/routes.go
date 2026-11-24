package indexnow

import (
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// RouteFileName is the file of the proxy's file provider that routes the
// keys, next to the panel's pier-panel.yaml.
const RouteFileName = "pier-indexnow.yaml"

// PanelService is the panel container as the proxy of its own server
// reaches it (the pier-panel network).
const PanelService = "http://pier-panel:8700"

// KeyPath is the panel route that answers with a key.
const KeyPath = "/indexnow/"

// routePriority puts the key above every generated route of the host, whose
// priority is the length of its rule.
const routePriority = 1_000_000

// RouteTarget describes the proxy that serves the keys.
type RouteTarget struct {
	// Local is the server the panel runs on: the proxy reaches the panel
	// container directly.
	Local bool
	// PanelURL is the public URL of the panel, how the proxies of the other
	// servers reach it. Empty means they cannot.
	PanelURL string
	// Edge means a Caddy in front terminates TLS and forwards plain HTTP.
	Edge bool
}

// Reachable tells whether this proxy can forward key requests to the panel.
func (t RouteTarget) Reachable() bool { return t.Local || panelOrigin(t.PanelURL) != "" }

type dynamicFile struct {
	HTTP dynamicHTTP `yaml:"http"`
}

type dynamicHTTP struct {
	Routers     map[string]router     `yaml:"routers"`
	Middlewares map[string]middleware `yaml:"middlewares"`
	Services    map[string]service    `yaml:"services"`
}

type router struct {
	Rule        string    `yaml:"rule"`
	EntryPoints []string  `yaml:"entryPoints"`
	Priority    int       `yaml:"priority"`
	Middlewares []string  `yaml:"middlewares"`
	Service     string    `yaml:"service"`
	TLS         *struct{} `yaml:"tls,omitempty"`
}

type middleware struct {
	ReplacePath struct {
		Path string `yaml:"path"`
	} `yaml:"replacePath"`
}

type service struct {
	LoadBalancer struct {
		Servers []struct {
			URL string `yaml:"url"`
		} `yaml:"servers"`
		PassHostHeader bool `yaml:"passHostHeader"`
	} `yaml:"loadBalancer"`
}

// RouteFile renders the dynamic configuration that serves the key of each
// site at https://<host>/<key>.txt. Nil means there is nothing to route and
// the file must not exist. The hosts were validated by compose.ParseRoute,
// so they are safe inside a rule.
func RouteFile(sites []Site, target RouteTarget) ([]byte, error) {
	if len(sites) == 0 || !target.Reachable() {
		return nil, nil
	}
	file := dynamicFile{HTTP: dynamicHTTP{
		Routers: map[string]router{}, Middlewares: map[string]middleware{},
		Services: map[string]service{"pier-indexnow": panelService(target)},
	}}
	for _, site := range sites {
		name := "pier-indexnow-" + site.ID
		file.HTTP.Routers[name] = keyRouter(site, name, target.Edge)
		var rewrite middleware
		rewrite.ReplacePath.Path = KeyPath + site.Key + ".txt"
		file.HTTP.Middlewares[name] = rewrite
	}
	return yaml.Marshal(file)
}

func keyRouter(site Site, name string, edge bool) router {
	result := router{
		Rule:     "Host(`" + site.Host + "`) && Path(`/" + site.Key + ".txt`)",
		Priority: routePriority, Middlewares: []string{name}, Service: "pier-indexnow",
		EntryPoints: []string{"https"}, TLS: &struct{}{},
	}
	if edge {
		// The Caddy in front forwards plain HTTP to this entrypoint.
		result.EntryPoints, result.TLS = []string{"http"}, nil
	}
	// Without a resolver, the proxy serves the certificate it already holds
	// for the host and never asks Let's Encrypt for another one.
	return result
}

func panelService(target RouteTarget) service {
	var result service
	address := PanelService
	if !target.Local {
		address = panelOrigin(target.PanelURL)
	}
	// A remote panel answers on its own domain, not on the site's.
	result.LoadBalancer.PassHostHeader = target.Local
	result.LoadBalancer.Servers = []struct {
		URL string `yaml:"url"`
	}{{URL: address}}
	return result
}

// panelOrigin reduces the public URL of the panel to scheme and host, or
// returns "" when it is not an absolute http(s) URL.
func panelOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
