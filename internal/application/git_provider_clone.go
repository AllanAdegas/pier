package application

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"pier/internal/gitprovider"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
)

// GitCredentials resolves optional per-resource GitHub App selections. Only a
// missing selection permits the ordinary public/deploy-key clone fallback.
type GitCredentials interface {
	WithCredentials(context.Context, string, string, func(repository, token string) error) error
}

func (d *Deployer) SetGitProvider(provider GitCredentials) { d.gitProvider = provider }

// CloneGitProvider resolves credentials at clone time, never into the resource
// config. A stale selection fails closed when the configured repository changes.
func CloneGitProvider(ctx context.Context, provider GitCredentials, kind, id string, ex executor.Executor, request CloneRequest, log CloneLog) (sha, message string, err error) {
	if provider == nil {
		return Clone(ctx, ex, request, log)
	}
	selected := false
	err = provider.WithCredentials(ctx, kind, id, func(repository, token string) error {
		selected = true
		if !sameGitHubRepository(request.Repository, repository) {
			return i18n.Errorf("O repositório da origem não corresponde à seleção do GitHub App. Selecione o repositório de novo.")
		}
		parsed, parseErr := url.Parse(repository)
		if parseErr != nil {
			return parseErr
		}
		parsed.User = url.UserPassword("x-access-token", token)
		request.Repository = parsed.String()
		request.PrivateKey = ""
		log.Redact(token)
		var cloneErr error
		sha, message, cloneErr = Clone(ctx, ex, request, log)
		return cloneErr
	})
	if !selected && errors.Is(err, gitprovider.ErrNoSelection) {
		return Clone(ctx, ex, request, log)
	}
	return
}

func sameGitHubRepository(source, selected string) bool {
	normalize := func(value string) string {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.RawQuery != "" || u.Fragment != "" {
			return ""
		}
		return strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), ".git"))
	}
	a, b := normalize(source), normalize(selected)
	return a != "" && a == b
}

// SameGitHubRepository reports whether a source address is the GitHub
// repository "owner/name" (HTTPS, case-insensitive, with or without ".git").
func SameGitHubRepository(source, repository string) bool {
	return sameGitHubRepository(source, "https://github.com/"+repository+".git")
}
