package gitprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// bitbucketRequiredScopes are checked only when Bitbucket reports the
// scopes of the token (X-OAuth-Scopes): webhooks need "webhook".
var bitbucketRequiredScopes = []string{"webhook"}

// bitbucketClient talks to the Bitbucket Cloud API 2.0 with an access token
// (Bearer) or "username:app password" (Basic). Workspace narrows the
// listing; workspace and repository access tokens need it.
type bitbucketClient struct {
	connectionAPI
	token     string
	workspace string
}

func newBitbucketClient(s *Service, api, token, workspace string) *bitbucketClient {
	header := http.Header{}
	header.Set("Authorization", bitbucketAuthorization(token))
	return &bitbucketClient{
		connectionAPI: connectionAPI{service: s, provider: "Bitbucket", api: api, header: header},
		token:         token, workspace: workspace,
	}
}

func bitbucketRepository(path string) string {
	workspace, slug, _ := strings.Cut(path, "/")
	return "/repositories/" + url.PathEscape(workspace) + "/" + url.PathEscape(slug)
}

// bitbucketNameFilter is a BBQL "name contains" filter with the value quoted.
func bitbucketNameFilter(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(query)
	return `name ~ "` + escaped + `"`
}

type bitbucketRepositoryRow struct {
	FullName   string `json:"full_name"`
	IsPrivate  bool   `json:"is_private"`
	MainBranch *struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
}

// repository builds the clone address itself: never one the API returned.
func (row bitbucketRepositoryRow) repository() ConnectionRepository {
	result := ConnectionRepository{Path: row.FullName, Private: row.IsPrivate, CloneURL: bitbucketCloud + "/" + row.FullName + ".git"}
	if row.MainBranch != nil {
		result.DefaultBranch = row.MainBranch.Name
	}
	return result
}

func (c *bitbucketClient) account(ctx context.Context) (Account, error) {
	var user struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	var header http.Header
	err := c.do(ctx, http.MethodGet, "/user", nil, &user, &header)
	if err != nil && c.workspace != "" && (statusOf(err) == http.StatusForbidden || statusOf(err) == http.StatusUnauthorized) {
		// Workspace and repository access tokens have no user: reading the
		// workspace proves the token instead.
		route := "/repositories/" + url.PathEscape(c.workspace) + "?pagelen=1"
		if err := c.do(ctx, http.MethodGet, route, nil, &struct{}{}, &header); err != nil {
			return Account{}, err
		}
		user.Username, user.DisplayName, err = c.workspace, c.workspace, nil
	}
	if err != nil {
		return Account{}, err
	}
	account := Account{Username: user.Username, Name: user.DisplayName, Scopes: []string{}, Missing: []string{}}
	if raw := header.Get("X-OAuth-Scopes"); raw != "" {
		for _, scope := range strings.Split(raw, ",") {
			if scope = strings.TrimSpace(scope); scope != "" {
				account.Scopes = append(account.Scopes, scope)
			}
		}
		account.ScopesKnown = true
		account.Missing = missingScopes(account.Scopes, bitbucketRequiredScopes)
	}
	return account, nil
}

func (c *bitbucketClient) repositories(ctx context.Context, query string, page int) (RepositoryPage, error) {
	values := url.Values{}
	values.Set("pagelen", fmt.Sprint(pickerPageSize))
	values.Set("page", fmt.Sprint(page))
	values.Set("sort", "-updated_on")
	if query != "" {
		values.Set("q", bitbucketNameFilter(query))
	}
	route := "/repositories?role=member&" + values.Encode()
	if c.workspace != "" {
		route = "/repositories/" + url.PathEscape(c.workspace) + "?" + values.Encode()
	}
	var data struct {
		Values []bitbucketRepositoryRow `json:"values"`
		// Next is only read as "more pages exist": it is never followed.
		Next string `json:"next"`
	}
	if err := c.do(ctx, http.MethodGet, route, nil, &data, nil); err != nil {
		return RepositoryPage{}, err
	}
	result := RepositoryPage{Items: []ConnectionRepository{}, Page: page, HasMore: data.Next != ""}
	for _, row := range data.Values {
		if validRepositoryPath(ProviderBitbucket, row.FullName) {
			result.Items = append(result.Items, row.repository())
		}
	}
	return result, nil
}

func (c *bitbucketClient) repository(ctx context.Context, path string) (ConnectionRepository, error) {
	var row bitbucketRepositoryRow
	if err := c.do(ctx, http.MethodGet, bitbucketRepository(path), nil, &row, nil); err != nil {
		return ConnectionRepository{}, err
	}
	return row.repository(), nil
}

func (c *bitbucketClient) branches(ctx context.Context, path, query string, page int) (BranchPage, error) {
	values := url.Values{}
	values.Set("pagelen", fmt.Sprint(pickerPageSize))
	values.Set("page", fmt.Sprint(page))
	if query != "" {
		values.Set("q", bitbucketNameFilter(query))
	}
	var data struct {
		Values []struct {
			Name string `json:"name"`
		} `json:"values"`
		Next string `json:"next"`
	}
	if err := c.do(ctx, http.MethodGet, bitbucketRepository(path)+"/refs/branches?"+values.Encode(), nil, &data, nil); err != nil {
		return BranchPage{}, err
	}
	result := BranchPage{Items: []string{}, Page: page, HasMore: data.Next != ""}
	for _, row := range data.Values {
		result.Items = append(result.Items, row.Name)
	}
	return result, nil
}

func (c *bitbucketClient) findHook(ctx context.Context, path, address string) (string, error) {
	for page := 1; page <= maxHookPages; page++ {
		var data struct {
			Values []struct {
				UUID string `json:"uuid"`
				URL  string `json:"url"`
			} `json:"values"`
			Next string `json:"next"`
		}
		route := fmt.Sprintf("%s/hooks?pagelen=%d&page=%d", bitbucketRepository(path), hookPageSize, page)
		if err := c.do(ctx, http.MethodGet, route, nil, &data, nil); err != nil {
			return "", err
		}
		for _, row := range data.Values {
			if sameHookURL(row.URL, address) {
				return row.UUID, nil
			}
		}
		if data.Next == "" {
			return "", nil
		}
	}
	return "", invalid()
}

func bitbucketHookBody(spec hookSpec) map[string]any {
	events := []string{"repo:push"}
	if spec.pullRequests {
		events = append(events, "pullrequest:created", "pullrequest:updated", "pullrequest:fulfilled", "pullrequest:rejected")
	}
	return map[string]any{"description": "Pier", "url": spec.URL, "active": true, "secret": spec.Secret, "events": events}
}

func (c *bitbucketClient) createHook(ctx context.Context, path string, spec hookSpec) (string, error) {
	var created struct {
		UUID string `json:"uuid"`
	}
	if err := c.do(ctx, http.MethodPost, bitbucketRepository(path)+"/hooks", bitbucketHookBody(spec), &created, nil); err != nil {
		return "", err
	}
	return created.UUID, nil
}

func (c *bitbucketClient) updateHook(ctx context.Context, path, id string, spec hookSpec) error {
	return c.do(ctx, http.MethodPut, bitbucketRepository(path)+"/hooks/"+url.PathEscape(id), bitbucketHookBody(spec), nil, nil)
}

func (c *bitbucketClient) deleteHook(ctx context.Context, path, id string) error {
	return c.do(ctx, http.MethodDelete, bitbucketRepository(path)+"/hooks/"+url.PathEscape(id), nil, nil, nil)
}

// cloneCredentials: "user:app password" clones as that user; an access
// token clones as "x-token-auth".
func (c *bitbucketClient) cloneCredentials() (string, string) {
	if user, password, found := strings.Cut(c.token, ":"); found {
		return user, password
	}
	return "x-token-auth", c.token
}
