package gitprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// maxPages bounds every paginated listing (100 items per page).
const maxPages = 50

// Installation is an account (user or organization) where the App is installed.
type Installation struct {
	ID                  int64  `json:"id"`
	Account             string `json:"account"`
	AccountType         string `json:"account_type"`
	RepositorySelection string `json:"repository_selection"`
	Suspended           bool   `json:"suspended"`
}

// Repository is a repository an installation grants access to.
type Repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
}

type githubInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
	RepositorySelection string  `json:"repository_selection"`
	SuspendedAt         *string `json:"suspended_at"`
}

// Installations lists where the App is installed.
func (s *Service) Installations(ctx context.Context) ([]Installation, error) {
	jwt, err := s.jwt(ctx)
	if err != nil {
		return nil, err
	}
	result := []Installation{}
	for page := 1; page <= maxPages; page++ {
		var rows []githubInstallation
		path := fmt.Sprintf("/app/installations?per_page=100&page=%d", page)
		if err := s.github(ctx, http.MethodGet, path, jwt, nil, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			result = append(result, Installation{
				ID: row.ID, Account: row.Account.Login, AccountType: row.Account.Type,
				RepositorySelection: row.RepositorySelection, Suspended: row.SuspendedAt != nil,
			})
		}
		if len(rows) < 100 {
			return result, nil
		}
	}
	return result, nil
}

// Repositories lists the repositories an installation grants access to.
func (s *Service) Repositories(ctx context.Context, installation int64) ([]Repository, error) {
	result := []Repository{}
	err := s.withInstallationToken(ctx, tokenKey{installation: installation, purpose: purposeList}, func(token string) error {
		for page := 1; page <= maxPages; page++ {
			var data struct {
				Repositories []Repository `json:"repositories"`
			}
			path := fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page)
			if err := s.github(ctx, http.MethodGet, path, token, nil, &data); err != nil {
				return err
			}
			result = append(result, data.Repositories...)
			if len(data.Repositories) < 100 {
				return nil
			}
		}
		return nil
	})
	return result, err
}

// validRepo accepts "owner/name" with GitHub's characters only.
func validRepo(repo string) bool {
	owner, name, found := strings.Cut(repo, "/")
	return found && validSegment(owner) && validSegment(name)
}

func validSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." || len(segment) > 100 {
		return false
	}
	for _, c := range segment {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func repositoryPath(repo string) string {
	owner, name, _ := strings.Cut(repo, "/")
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}
