package application

import (
	"regexp"
	"strconv"
	"strings"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/i18n"
)

const (
	numberPlaceholder = "{number}"
	branchPlaceholder = "{branch}"
	// Validation renders the longest values a real preview can produce.
	sampleNumber = 999999999
	sampleBranch = "branch-name-of-thirty-chars-xx"
)

var templatePlaceholder = regexp.MustCompile(`\{[^{}]*\}`)

// parseDomainTemplate validates a preview domain template such as
// "pr-{number}.preview.example.com" (scheme optional, https by default).
// {number} must be in the host: the provider assigns it, so every preview
// gets its own host and a branch name alone can never choose one.
// An empty template is valid here; enabling previews requires one.
func parseDomainTemplate(raw string) (string, error) {
	template := strings.TrimSpace(raw)
	if template == "" {
		return "", nil
	}
	for _, token := range templatePlaceholder.FindAllString(template, -1) {
		if token != numberPlaceholder && token != branchPlaceholder {
			return "", i18n.Errorf("variável desconhecida %s: use {number} ou {branch}", token)
		}
	}
	route, err := compose.ParseRoute(renderTemplate(template, sampleNumber, sampleBranch))
	if err != nil {
		return "", i18n.Errorf("o modelo não gera um domínio válido: %w", err)
	}
	alternate, alternateErr := compose.ParseRoute(renderTemplate(template, 1, sampleBranch))
	if alternateErr != nil || alternate.Host == route.Host {
		return "", i18n.Errorf("use {number} no host, ex.: pr-{number}.preview.exemplo.com.br; {branch} é opcional")
	}
	return template, nil
}

// renderPreviewDomain is the URL of one preview. The branch becomes a DNS
// safe slug; an unusual branch name may still produce an invalid host.
func renderPreviewDomain(template string, number int, branch string) (compose.Route, error) {
	route, err := compose.ParseRoute(renderTemplate(template, number, docker.Slug(branch)))
	if err != nil {
		return compose.Route{}, i18n.Errorf("o modelo de domínio não gera um endereço válido para este pull request: %w", err)
	}
	return route, nil
}

func renderTemplate(template string, number int, branchSlug string) string {
	return strings.NewReplacer(numberPlaceholder, strconv.Itoa(number), branchPlaceholder, branchSlug).Replace(template)
}
