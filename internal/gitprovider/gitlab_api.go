package gitprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// gitlabRequiredScopes: "api" reads repositories, manages project webhooks
// and writes statuses/notes. read_api cannot create webhooks.
var gitlabRequiredScopes = []string{"api"}

// gitlabClient talks to the REST API v4 of gitlab.com or a self-hosted
// GitLab, including one served under a sub-path (<base>/api/v4).
type gitlabClient struct {
	connectionAPI
	token string
}

func newGitLabClient(s *Service, baseURL, token string) *gitlabClient {
	header := http.Header{}
	header.Set("PRIVATE-TOKEN", token)
	return &gitlabClient{connectionAPI: connectionAPI{service: s, provider: "GitLab", api: baseURL + "/api/v4", header: header}, token: token}
}

func gitlabProject(path string) string { return "/projects/" + url.PathEscape(path) }

type gitlabProjectRow struct {
	Path          string `json:"path_with_namespace"`
	Visibility    string `json:"visibility"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"http_url_to_repo"`
}

func (row gitlabProjectRow) repository() ConnectionRepository {
	return ConnectionRepository{
		Path: row.Path, Private: row.Visibility != "public", DefaultBranch: row.DefaultBranch, CloneURL: row.CloneURL,
	}
}

func (c *gitlabClient) account(ctx context.Context) (Account, error) {
	var user struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	}
	if err := c.do(ctx, http.MethodGet, "/user", nil, &user, nil); err != nil {
		return Account{}, err
	}
	account := Account{Username: user.Username, Name: user.Name, Scopes: []string{}, Missing: []string{}}
	var self struct {
		Scopes []string `json:"scopes"`
	}
	// Personal, group and project tokens describe themselves; other
	// credentials do not, and the scopes then stay unknown.
	if err := c.do(ctx, http.MethodGet, "/personal_access_tokens/self", nil, &self, nil); err == nil {
		account.Scopes, account.ScopesKnown = self.Scopes, true
		account.Missing = missingScopes(self.Scopes, gitlabRequiredScopes)
	}
	return account, nil
}

func (c *gitlabClient) repositories(ctx context.Context, query string, page int) (RepositoryPage, error) {
	values := url.Values{}
	values.Set("membership", "true")
	values.Set("simple", "true")
	values.Set("order_by", "last_activity_at")
	values.Set("per_page", fmt.Sprint(pickerPageSize))
	values.Set("page", fmt.Sprint(page))
	if query != "" {
		values.Set("search", query)
		values.Set("search_namespaces", "true")
	}
	var rows []gitlabProjectRow
	var header http.Header
	if err := c.do(ctx, http.MethodGet, "/projects?"+values.Encode(), nil, &rows, &header); err != nil {
		return RepositoryPage{}, err
	}
	result := RepositoryPage{Items: []ConnectionRepository{}, Page: page, HasMore: morePages(header, page, pickerPageSize, len(rows))}
	for _, row := range rows {
		result.Items = append(result.Items, row.repository())
	}
	return result, nil
}

func (c *gitlabClient) repository(ctx context.Context, path string) (ConnectionRepository, error) {
	var row gitlabProjectRow
	if err := c.do(ctx, http.MethodGet, gitlabProject(path), nil, &row, nil); err != nil {
		return ConnectionRepository{}, err
	}
	return row.repository(), nil
}

func (c *gitlabClient) branches(ctx context.Context, path, query string, page int) (BranchPage, error) {
	values := url.Values{}
	values.Set("per_page", fmt.Sprint(pickerPageSize))
	values.Set("page", fmt.Sprint(page))
	if query != "" {
		values.Set("search", query)
	}
	var rows []struct {
		Name string `json:"name"`
	}
	var header http.Header
	if err := c.do(ctx, http.MethodGet, gitlabProject(path)+"/repository/branches?"+values.Encode(), nil, &rows, &header); err != nil {
		return BranchPage{}, err
	}
	result := BranchPage{Items: []string{}, Page: page, HasMore: morePages(header, page, pickerPageSize, len(rows))}
	for _, row := range rows {
		result.Items = append(result.Items, row.Name)
	}
	return result, nil
}

func (c *gitlabClient) findHook(ctx context.Context, path, address string) (string, error) {
	for page := 1; page <= maxHookPages; page++ {
		var rows []struct {
			ID  int64  `json:"id"`
			URL string `json:"url"`
		}
		route := fmt.Sprintf("%s/hooks?per_page=%d&page=%d", gitlabProject(path), hookPageSize, page)
		if err := c.do(ctx, http.MethodGet, route, nil, &rows, nil); err != nil {
			return "", err
		}
		for _, row := range rows {
			if sameHookURL(row.URL, address) {
				return fmt.Sprint(row.ID), nil
			}
		}
		if len(rows) < hookPageSize {
			return "", nil
		}
	}
	return "", invalid()
}

func gitlabHookBody(spec hookSpec) map[string]any {
	return map[string]any{
		"url": spec.URL, "token": spec.Secret, "push_events": true, "tag_push_events": true,
		"merge_requests_events": spec.pullRequests, "enable_ssl_verification": true,
	}
}

func (c *gitlabClient) createHook(ctx context.Context, path string, spec hookSpec) (string, error) {
	var created struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, gitlabProject(path)+"/hooks", gitlabHookBody(spec), &created, nil); err != nil {
		return "", err
	}
	return fmt.Sprint(created.ID), nil
}

func (c *gitlabClient) updateHook(ctx context.Context, path, id string, spec hookSpec) error {
	return c.do(ctx, http.MethodPut, gitlabProject(path)+"/hooks/"+url.PathEscape(id), gitlabHookBody(spec), nil, nil)
}

func (c *gitlabClient) deleteHook(ctx context.Context, path, id string) error {
	return c.do(ctx, http.MethodDelete, gitlabProject(path)+"/hooks/"+url.PathEscape(id), nil, nil, nil)
}

// cloneCredentials: GitLab accepts any token as the password of "oauth2".
func (c *gitlabClient) cloneCredentials() (string, string) { return "oauth2", c.token }
