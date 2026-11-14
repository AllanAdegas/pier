package gitprovider

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"pier/internal/platform/apperr"
)

const (
	// pickerPageSize is the page of repositories and branches the picker shows.
	pickerPageSize = 30
	// hookPageSize and maxHookPages bound the search for an existing webhook.
	hookPageSize  = 50
	maxHookPages  = 10
	maxQueryRunes = 100
)

// providerClient is one provider API seen through a connection token. Each
// implementation builds request paths under its API base only.
type providerClient interface {
	account(ctx context.Context) (Account, error)
	repositories(ctx context.Context, query string, page int) (RepositoryPage, error)
	repository(ctx context.Context, path string) (ConnectionRepository, error)
	branches(ctx context.Context, path, query string, page int) (BranchPage, error)
	findHook(ctx context.Context, path, address string) (string, error)
	createHook(ctx context.Context, path string, spec hookSpec) (string, error)
	updateHook(ctx context.Context, path, id string, spec hookSpec) error
	deleteHook(ctx context.Context, path, id string) error
	// cloneCredentials is the user and password git sends over HTTPS.
	cloneCredentials() (user, password string)
}

// connectionAPI sends requests to one API base with one credential header.
// Every address is checked against the base before the token leaves.
type connectionAPI struct {
	service  *Service
	provider string
	api      string
	header   http.Header
}

func (c connectionAPI) do(ctx context.Context, method, path string, input, output any, received *http.Header) error {
	address := c.api + path
	if !withinBase(address, c.api) {
		return apperr.Invalid("Endereço da API do %s inválido.", c.provider)
	}
	header := http.Header{}
	for key, values := range c.header {
		header[key] = values
	}
	return c.service.call(ctx, apiRequest{
		provider: c.provider, method: method, url: address, header: header,
		input: input, output: output, received: received,
	})
}

// cleanQuery bounds a search typed in the picker.
func cleanQuery(query string) string {
	value := strings.TrimSpace(query)
	if runes := []rune(value); len(runes) > maxQueryRunes {
		value = string(runes[:maxQueryRunes])
	}
	return value
}

func validPage(page int) int {
	if page < 1 {
		return 1
	}
	if page > maxPages {
		return maxPages
	}
	return page
}

// morePages reads the usual pagination headers; without them a full page
// means there may be more.
func morePages(header http.Header, page, size, received int) bool {
	if next, found := header["X-Next-Page"]; found {
		return len(next) > 0 && strings.TrimSpace(next[0]) != ""
	}
	if total, err := strconv.Atoi(header.Get("X-Total-Count")); err == nil {
		return page*size < total
	}
	return received >= size
}

// filterBranches keeps the names containing query (providers without search).
func filterBranches(names []string, query string) []string {
	if query == "" {
		return names
	}
	kept := []string{}
	for _, name := range names {
		if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
			kept = append(kept, name)
		}
	}
	return kept
}

func sameHookURL(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}

// missingScopes lists the required scopes absent from scopes.
func missingScopes(scopes, required []string) []string {
	missing := []string{}
	for _, want := range required {
		found := false
		for _, scope := range scopes {
			found = found || strings.EqualFold(strings.TrimSpace(scope), want)
		}
		if !found {
			missing = append(missing, want)
		}
	}
	return missing
}
