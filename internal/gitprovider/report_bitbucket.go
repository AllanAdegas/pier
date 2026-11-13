package gitprovider

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"pier/internal/platform/apperr"
)

const bitbucketAPI = "https://api.bitbucket.org/2.0"

var shortSHAPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// bitbucketReporter uses the Bitbucket Cloud API 2.0. The token is a
// repository/workspace access token (Bearer) or "username:app password"
// (Basic); both need repository write and pull request write.
type bitbucketReporter struct {
	service *Service
	repo    string
	web     string
	token   string
}

func newBitbucketReporter(s *Service, location remote, token string) (Reporter, error) {
	parts, ok := location.segments(2)
	if !ok || location.host != "bitbucket.org" {
		return nil, apperr.Invalid("O retorno do Bitbucket exige um repositório do bitbucket.org.")
	}
	return &bitbucketReporter{
		service: s, token: token, web: "https://bitbucket.org/" + parts[0] + "/" + parts[1],
		repo: "/repositories/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]),
	}, nil
}

// bitbucketAuthorization builds the header for an access token or an app password.
func bitbucketAuthorization(token string) string {
	if strings.Contains(token, ":") {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(token))
	}
	return "Bearer " + token
}

func (s *Service) bitbucket(ctx context.Context, token, method, path string, input, output any) error {
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", bitbucketAuthorization(token))
	}
	return s.call(ctx, apiRequest{
		provider: "Bitbucket", method: method, url: bitbucketAPI + path, header: header, input: input, output: output,
	})
}

func bitbucketState(state State) string {
	switch state {
	case StatePending:
		return "INPROGRESS"
	case StateSuccess:
		return "SUCCESSFUL"
	case StateFailure:
		return "FAILED"
	}
	return "STOPPED"
}

func (r *bitbucketReporter) SetCommitStatus(ctx context.Context, status CommitStatus) error {
	if err := validStatus(status); err != nil {
		return err
	}
	target := status.TargetURL
	if target == "" {
		// Bitbucket requires a link; the repository page is the honest fallback.
		target = r.web
	}
	input := map[string]string{
		"key": truncate(status.Context, 40), "name": status.Context, "state": bitbucketState(status.State),
		"description": truncate(status.Description, 255), "url": target,
	}
	path := r.repo + "/commit/" + status.SHA + "/statuses/build"
	return r.service.bitbucket(ctx, r.token, http.MethodPost, path, input, nil)
}

type bitbucketComment struct {
	ID      int64 `json:"id"`
	Deleted bool  `json:"deleted"`
	Content struct {
		Raw string `json:"raw"`
	} `json:"content"`
	User struct {
		UUID string `json:"uuid"`
	} `json:"user"`
}

func (r *bitbucketReporter) UpsertComment(ctx context.Context, comment Comment) error {
	if err := validComment(comment); err != nil {
		return err
	}
	// Access tokens act as a bot user; when /user is not available to the
	// token, the marker alone finds the comment (Bitbucket only lets the
	// author edit it, so a forged marker makes the update fail, not leak).
	var user struct {
		UUID string `json:"uuid"`
	}
	_ = r.service.bitbucket(ctx, r.token, http.MethodGet, "/user", nil, &user)
	body := markedBody(comment)
	comments := fmt.Sprintf("%s/pullrequests/%d/comments", r.repo, comment.Number)
	for page := 1; page <= maxPages; page++ {
		var data struct {
			Values []bitbucketComment `json:"values"`
			Next   string             `json:"next"`
		}
		if err := r.service.bitbucket(ctx, r.token, http.MethodGet, fmt.Sprintf("%s?pagelen=100&page=%d", comments, page), nil, &data); err != nil {
			return err
		}
		for _, row := range data.Values {
			if row.Deleted || !hasMarker(row.Content.Raw, comment.Marker) || user.UUID != "" && row.User.UUID != user.UUID {
				continue
			}
			if row.Content.Raw == body {
				return nil
			}
			path := fmt.Sprintf("%s/%d", comments, row.ID)
			return r.service.bitbucket(ctx, r.token, http.MethodPut, path, bitbucketContent(body), nil)
		}
		if data.Next == "" {
			break
		}
	}
	return r.service.bitbucket(ctx, r.token, http.MethodPost, comments, bitbucketContent(body), nil)
}

func bitbucketContent(body string) map[string]any {
	return map[string]any{"content": map[string]string{"raw": body}}
}

// ResolveBitbucketCommit expands the abbreviated commit of a Bitbucket pull
// request event to the full SHA previews pin. It uses the resource token
// when one is stored, and no credential otherwise (public repositories).
func (s *Service) ResolveBitbucketCommit(ctx context.Context, kind, id, repository, commit string) (string, error) {
	commit = strings.ToLower(strings.TrimSpace(commit))
	if shaPattern.MatchString(commit) {
		return commit, nil
	}
	if !shortSHAPattern.MatchString(commit) || !validRepo(repository) {
		return "", apperr.Invalid("Commit do Bitbucket inválido.")
	}
	token, err := s.providerToken(ctx, kind, id, ProviderBitbucket, "https://bitbucket.org")
	if err != nil {
		return "", err
	}
	owner, name, _ := strings.Cut(repository, "/")
	var result struct {
		Hash string `json:"hash"`
	}
	path := "/repositories/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/commit/" + commit
	if err := s.bitbucket(ctx, token, http.MethodGet, path, nil, &result); err != nil {
		return "", err
	}
	full := strings.ToLower(result.Hash)
	if !shaPattern.MatchString(full) || !strings.HasPrefix(full, commit) {
		return "", apperr.Invalid("O Bitbucket devolveu um commit inesperado.")
	}
	return full, nil
}
