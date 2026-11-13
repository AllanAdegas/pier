package gitprovider

import (
	"net"
	"net/url"
	"strings"

	"pier/internal/platform/apperr"
)

// remote is a repository address reduced to what an API needs: where the
// web server is and the repository path. Credentials are dropped.
type remote struct {
	// origin is scheme://host[:port] of the web server (and of its API).
	origin string
	host   string
	// path is the repository path without ".git", e.g. "group/sub/project".
	path string
}

// parseRemote reads HTTPS, SSH and scp-like ("git@host:path") addresses.
// SSH remotes map to the HTTPS origin of the same host.
func parseRemote(repository string) (remote, error) {
	value := strings.TrimSpace(repository)
	if !strings.Contains(value, "://") {
		value = scpToURL(value)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return remote{}, unknownRemote()
	}
	origin := "https://" + parsed.Hostname()
	switch parsed.Scheme {
	case "https", "http":
		origin = parsed.Scheme + "://" + parsed.Host
	case "ssh":
	default:
		return remote{}, unknownRemote()
	}
	path := strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
	for _, segment := range strings.Split(path, "/") {
		if !validSegment(segment) {
			return remote{}, unknownRemote()
		}
	}
	return remote{origin: origin, host: strings.ToLower(parsed.Hostname()), path: path}, nil
}

// scpToURL turns "user@host:path" into "ssh://user@host/path".
func scpToURL(value string) string {
	at := strings.Index(value, "@")
	colon := strings.Index(value, ":")
	if at < 1 || colon <= at+1 {
		return value
	}
	host := value[at+1 : colon]
	if net.ParseIP(strings.Trim(host, "[]")) == nil && strings.ContainsAny(host, "/") {
		return value
	}
	return "ssh://" + value[:colon] + "/" + value[colon+1:]
}

func unknownRemote() error {
	return apperr.Invalid("Não foi possível reconhecer o repositório da origem para falar com a API do provedor Git.")
}

// segments splits the path and requires exactly n parts when n > 0.
func (r remote) segments(n int) ([]string, bool) {
	parts := strings.Split(r.path, "/")
	if n > 0 && len(parts) != n {
		return nil, false
	}
	for _, part := range parts {
		if !validSegment(part) {
			return nil, false
		}
	}
	return parts, len(parts) >= 2
}
