package gitprovider

import (
	"net/url"
	"strings"

	"pier/internal/platform/apperr"
)

const (
	gitlabCloud    = "https://gitlab.com"
	bitbucketCloud = "https://bitbucket.org"
)

// connectionProvider reports whether provider uses a connection (GitHub uses the App).
func connectionProvider(provider string) bool {
	return provider == ProviderGitLab || provider == ProviderGitea || provider == ProviderBitbucket
}

// normalizeBaseURL validates the web address of a provider and returns its
// canonical form: https, lower-case host, optional port and sub-path, no
// credentials, query, fragment or trailing slash. Bitbucket is Cloud only.
func normalizeBaseURL(provider, raw string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	switch {
	case !connectionProvider(provider):
		return "", apperr.Invalid("Provedor Git desconhecido.")
	case provider == ProviderBitbucket && (value == "" || strings.EqualFold(value, bitbucketCloud)):
		return bitbucketCloud, nil
	case provider == ProviderBitbucket:
		return "", apperr.Invalid("O Bitbucket é suportado apenas na nuvem (bitbucket.org).")
	case provider == ProviderGitLab && value == "":
		return gitlabCloud, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", invalidBaseURL()
	}
	path := strings.Trim(parsed.Path, "/")
	if path != "" {
		for _, segment := range strings.Split(path, "/") {
			if !validSegment(segment) {
				return "", invalidBaseURL()
			}
		}
		path = "/" + path
	}
	return "https://" + strings.ToLower(parsed.Host) + path, nil
}

func invalidBaseURL() error {
	return apperr.Invalid("Endereço do provedor inválido: use https://host[:porta][/caminho], sem usuário nem parâmetros.")
}

// withinBase reports whether address is under base (same scheme and host,
// path inside the base path). It guards every URL a token is sent to.
func withinBase(address, base string) bool {
	target, err := url.Parse(address)
	if err != nil || target.User != nil || target.Fragment != "" {
		return false
	}
	root, err := url.Parse(base)
	if err != nil {
		return false
	}
	if target.Scheme != root.Scheme || !strings.EqualFold(target.Host, root.Host) {
		return false
	}
	for _, part := range strings.Split(target.Path, "/") {
		if part == ".." || part == "." {
			return false
		}
	}
	prefix := strings.TrimRight(root.Path, "/") + "/"
	return strings.HasPrefix(target.Path, prefix) && !strings.Contains(target.Path, "/../")
}

// sameRepositoryURL compares two HTTPS repository addresses ignoring
// credentials, letter case, a trailing slash and ".git".
func sameRepositoryURL(a, b string) bool {
	normalize := func(value string) string {
		parsed, err := url.Parse(strings.TrimSpace(value))
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return ""
		}
		path := strings.TrimSuffix(strings.TrimSuffix(parsed.Path, "/"), ".git")
		if strings.EqualFold(parsed.Host, "github.com") {
			path = strings.ToLower(path)
		}
		return strings.ToLower(parsed.Host) + path
	}
	left, right := normalize(a), normalize(b)
	return left != "" && left == right
}

// SameRepositoryURL is sameRepositoryURL for other packages (clone checks).
func SameRepositoryURL(a, b string) bool { return sameRepositoryURL(a, b) }

// validRepositoryPath accepts "owner/name" or, for GitLab, nested groups.
func validRepositoryPath(provider, path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) < 2 || provider != ProviderGitLab && len(parts) != 2 || len(parts) > 20 {
		return false
	}
	for _, part := range parts {
		if !validSegment(part) {
			return false
		}
	}
	return true
}
