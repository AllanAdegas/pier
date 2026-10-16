package server

import (
	"fmt"
	"sort"
	"strings"
)

// The block of the Caddyfile that Pier owns. Everything outside the markers
// belongs to whoever set Caddy up and is never changed.
const (
	edgeBlockStart = "# >>> pier: bloco gerenciado pelo Pier. Edições aqui são sobrescritas."
	edgeBlockEnd   = "# <<< pier"
)

// edgeAddresses turns sites ("https://host", "http://host") into Caddy site
// addresses. A host served only over HTTPS is written bare, so Caddy issues
// its certificate and redirects HTTP to HTTPS; a host with an http:// route
// is served on both schemes without redirect, as Pier would.
func edgeAddresses(sites []string) []string {
	schemes := map[string]map[string]bool{}
	for _, site := range sites {
		scheme, host, found := strings.Cut(site, "://")
		if !found || host == "" {
			continue
		}
		if schemes[host] == nil {
			schemes[host] = map[string]bool{}
		}
		schemes[host][scheme] = true
	}
	hosts := make([]string, 0, len(schemes))
	for host := range schemes {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	addresses := []string{}
	for _, host := range hosts {
		switch {
		case !schemes[host]["http"]:
			addresses = append(addresses, host)
		case schemes[host]["https"]:
			addresses = append(addresses, "http://"+host, "https://"+host)
		default:
			addresses = append(addresses, "http://"+host)
		}
	}
	return addresses
}

// renderEdgeBlock is the Pier block forwarding the addresses to upstream
// (Pier's proxy). No address, no block.
func renderEdgeBlock(addresses []string, upstream string) string {
	if len(addresses) == 0 {
		return ""
	}
	return fmt.Sprintf("%s\n%s {\n\treverse_proxy %s\n}\n%s\n",
		edgeBlockStart, strings.Join(addresses, ", "), upstream, edgeBlockEnd)
}

// replaceEdgeBlock swaps the Pier block of a Caddyfile for block (which may
// be empty), appending it at the end the first time.
func replaceEdgeBlock(caddyfile, block string) (string, error) {
	start := strings.Index(caddyfile, edgeBlockStart)
	end := strings.Index(caddyfile, edgeBlockEnd)
	outside := caddyfile
	switch {
	case start < 0 && end < 0:
	case start >= 0 && end > start:
		after := caddyfile[end+len(edgeBlockEnd):]
		outside = strings.TrimRight(caddyfile[:start], "\n") + "\n" + strings.TrimLeft(after, "\n")
	default:
		return "", fmt.Errorf("o Caddyfile tem as marcações do Pier incompletas ou fora de ordem; " +
			"corrija ou apague o bloco “# >>> pier … # <<< pier” à mão")
	}
	outside = strings.TrimRight(outside, "\n") + "\n"
	if block == "" {
		return outside, nil
	}
	return outside + "\n" + block, nil
}
