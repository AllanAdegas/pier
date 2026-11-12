package registry

import (
	"regexp"
	"strings"
)

// DockerHub is the host Docker assumes when a reference names none.
const DockerHub = "docker.io"

// dockerHubAliases are the other names Docker Hub goes by.
var dockerHubAliases = map[string]bool{"index.docker.io": true, "registry-1.docker.io": true}

// pathComponent is one component of an OCI repository name.
const pathComponent = `[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*`

var (
	repositoryPattern = regexp.MustCompile(`^` + pathComponent + `(?:/` + pathComponent + `)*$`)
	hostPattern       = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*(?::[0-9]{1,5})?$`)
	digestPattern     = regexp.MustCompile(`^([^/@]+)/([^@]+)@(sha256:[a-f0-9]{64})$`)
	tagPattern        = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

// HostOf returns the registry of an image reference with Docker's rule: the
// first component is a host only when it has a dot or a port, or is
// "localhost". Everything else lives on Docker Hub.
func HostOf(image string) string {
	first, _, found := strings.Cut(image, "/")
	if !found || (!strings.ContainsAny(first, ".:") && first != "localhost") {
		return DockerHub
	}
	first = strings.ToLower(first)
	if dockerHubAliases[first] {
		return DockerHub
	}
	return first
}

// Canonical qualifies a reference with its registry, as `docker image
// inspect` leaves Docker Hub implicit ("user/app@sha256:…").
func Canonical(reference string) string {
	if HostOf(reference) != DockerHub {
		return reference
	}
	first, rest, found := strings.Cut(reference, "/")
	if found && (first == DockerHub || dockerHubAliases[strings.ToLower(first)]) {
		reference = rest
	}
	if !strings.Contains(strings.SplitN(reference, "@", 2)[0], "/") {
		reference = "library/" + reference
	}
	return DockerHub + "/" + reference
}

// Immutable reports whether image is a fully qualified reference pinned by
// a sha256 digest: the only form Pier distributes between servers.
func Immutable(image string) bool {
	_, _, _, ok := splitDigest(image)
	return ok
}

// splitDigest breaks host/repository@sha256:… into its parts.
func splitDigest(image string) (host, repository, digest string, ok bool) {
	match := digestPattern.FindStringSubmatch(image)
	if match == nil || !validHost(match[1]) || !repositoryPattern.MatchString(match[2]) {
		return "", "", "", false
	}
	if !strings.ContainsAny(match[1], ".:") && match[1] != "localhost" {
		return "", "", "", false
	}
	return match[1], match[2], match[3], true
}

// repositoryOf drops the tag and digest of a canonical reference.
func repositoryOf(reference string) string {
	name, _, _ := strings.Cut(reference, "@")
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		if colon := strings.LastIndex(name[slash:], ":"); colon >= 0 {
			name = name[:slash+colon]
		}
	}
	return name
}

func validHost(host string) bool { return len(host) <= 255 && hostPattern.MatchString(host) }

// ValidRepository reports whether name is an OCI repository path.
func ValidRepository(name string) bool {
	return len(name) <= 255 && repositoryPattern.MatchString(name)
}

// apiHost is where the registry HTTP API of a host answers.
func apiHost(host string) string {
	if host == DockerHub {
		return "registry-1.docker.io"
	}
	return host
}

// authKeys are the keys Docker looks up in config.json for a host.
func authKeys(host string) []string {
	if host == DockerHub {
		return []string{"https://index.docker.io/v1/", DockerHub}
	}
	return []string{host}
}
