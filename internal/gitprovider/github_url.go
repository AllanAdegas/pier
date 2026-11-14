package gitprovider

import (
	"context"
	"net/url"
	"strings"
)

func normalizeGitHubAPI(raw string) (string, error) {
	if raw == "" || raw == githubAPI {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/v3" {
		return "", invalid()
	}
	return "https://" + strings.ToLower(u.Host) + "/api/v3", nil
}
func (s *Service) githubAPIBase(ctx context.Context) string {
	c, err := s.config(ctx)
	if err == nil && c.APIURL != "" {
		return c.APIURL
	}
	return githubAPI
}
func githubWebBase(c Config) string {
	if c.APIURL != "" {
		return strings.TrimSuffix(c.APIURL, "/api/v3")
	}
	return "https://github.com"
}
func githubInstallURL(c Config) string {
	return githubWebBase(c) + "/apps/" + c.Slug + "/installations/new"
}
func (s *Service) cloneURL(ctx context.Context, selection Selection) string {
	c, err := s.config(ctx)
	if err != nil {
		return selection.CloneURL()
	}
	return githubWebBase(c) + "/" + selection.Repository + ".git"
}
