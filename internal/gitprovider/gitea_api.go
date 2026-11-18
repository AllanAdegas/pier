package gitprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// giteaClient talks to the API v1 of Gitea or Forgejo (<base>/api/v1) with
// an access token (scopes read:user, write:repository and write:issue).
// Neither tells the scopes of a token through the API.
type giteaClient struct {
	connectionAPI
	token string
	login string
}

func newGiteaClient(s *Service, baseURL, token, login string) *giteaClient {
	header := http.Header{}
	header.Set("Authorization", "token "+token)
	return &giteaClient{
		connectionAPI: connectionAPI{service: s, provider: "Gitea", api: baseURL + "/api/v1", header: header},
		token:         token, login: login,
	}
}

// giteaRepository is "/repos/{owner}/{name}"; the path was validated as owner/name.
func giteaRepository(path string) string {
	owner, name, _ := strings.Cut(path, "/")
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

type giteaRepositoryRow struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
}

func (row giteaRepositoryRow) repository() ConnectionRepository {
	return ConnectionRepository{Path: row.FullName, Private: row.Private, DefaultBranch: row.DefaultBranch, CloneURL: row.CloneURL}
}

func (c *giteaClient) account(ctx context.Context) (Account, error) {
	var user struct {
		Login    string `json:"login"`
		FullName string `json:"full_name"`
	}
	if err := c.do(ctx, http.MethodGet, "/user", nil, &user, nil); err != nil {
		return Account{}, err
	}
	return Account{Username: user.Login, Name: user.FullName, Scopes: []string{}, Missing: []string{}}, nil
}

// repositories lists the repositories of the token owner; a search looks
// through every repository the token can see.
func (c *giteaClient) repositories(ctx context.Context, query string, page int) (RepositoryPage, error) {
	values := url.Values{}
	values.Set("limit", fmt.Sprint(pickerPageSize))
	values.Set("page", fmt.Sprint(page))
	var rows []giteaRepositoryRow
	var header http.Header
	var err error
	if query == "" {
		err = c.do(ctx, http.MethodGet, "/user/repos?"+values.Encode(), nil, &rows, &header)
	} else {
		values.Set("q", query)
		values.Set("sort", "updated")
		values.Set("order", "desc")
		var found struct {
			Data []giteaRepositoryRow `json:"data"`
		}
		err = c.do(ctx, http.MethodGet, "/repos/search?"+values.Encode(), nil, &found, &header)
		rows = found.Data
	}
	if err != nil {
		return RepositoryPage{}, err
	}
	result := RepositoryPage{Items: []ConnectionRepository{}, Page: page, HasMore: morePages(header, page, pickerPageSize, len(rows))}
	for _, row := range rows {
		result.Items = append(result.Items, row.repository())
	}
	return result, nil
}

func (c *giteaClient) repository(ctx context.Context, path string) (ConnectionRepository, error) {
	var row giteaRepositoryRow
	if err := c.do(ctx, http.MethodGet, giteaRepository(path), nil, &row, nil); err != nil {
		return ConnectionRepository{}, err
	}
	return row.repository(), nil
}

func (c *giteaClient) branches(ctx context.Context, path, query string, page int) (BranchPage, error) {
	var rows []struct {
		Name string `json:"name"`
	}
	var header http.Header
	route := fmt.Sprintf("%s/branches?limit=%d&page=%d", giteaRepository(path), pickerPageSize, page)
	if err := c.do(ctx, http.MethodGet, route, nil, &rows, &header); err != nil {
		return BranchPage{}, err
	}
	names := []string{}
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return BranchPage{Items: filterBranches(names, query), Page: page, HasMore: morePages(header, page, pickerPageSize, len(rows))}, nil
}

func (c *giteaClient) findHook(ctx context.Context, path, address string) (string, error) {
	for page := 1; page <= maxHookPages; page++ {
		var rows []struct {
			ID     int64 `json:"id"`
			Config struct {
				URL string `json:"url"`
			} `json:"config"`
		}
		route := fmt.Sprintf("%s/hooks?limit=%d&page=%d", giteaRepository(path), hookPageSize, page)
		if err := c.do(ctx, http.MethodGet, route, nil, &rows, nil); err != nil {
			return "", err
		}
		for _, row := range rows {
			if sameHookURL(row.Config.URL, address) {
				return fmt.Sprint(row.ID), nil
			}
		}
		if len(rows) < hookPageSize {
			return "", nil
		}
	}
	return "", invalid()
}

func giteaHookBody(spec hookSpec, create bool) map[string]any {
	events := []string{"push"}
	if spec.pullRequests {
		events = append(events, "pull_request")
	}
	body := map[string]any{
		"config": map[string]string{"url": spec.URL, "content_type": "json", "secret": spec.Secret},
		"events": events, "active": true,
	}
	if create {
		// Forgejo also accepts the "gitea" type and signs with X-Gitea-Signature.
		body["type"] = "gitea"
	}
	return body
}

func (c *giteaClient) createHook(ctx context.Context, path string, spec hookSpec) (string, error) {
	var created struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, giteaRepository(path)+"/hooks", giteaHookBody(spec, true), &created, nil); err != nil {
		return "", err
	}
	return fmt.Sprint(created.ID), nil
}

func (c *giteaClient) updateHook(ctx context.Context, path, id string, spec hookSpec) error {
	return c.do(ctx, http.MethodPatch, giteaRepository(path)+"/hooks/"+url.PathEscape(id), giteaHookBody(spec, false), nil, nil)
}

func (c *giteaClient) deleteHook(ctx context.Context, path, id string) error {
	return c.do(ctx, http.MethodDelete, giteaRepository(path)+"/hooks/"+url.PathEscape(id), nil, nil, nil)
}

// cloneCredentials: Gitea and Forgejo take the token as the password of its owner.
func (c *giteaClient) cloneCredentials() (string, string) {
	if c.login == "" {
		return "pier", c.token
	}
	return c.login, c.token
}
