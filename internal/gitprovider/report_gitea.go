package gitprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// giteaReporter uses the API v1 of Gitea or Forgejo (the host of the
// repository) with an access token that writes the repository and issues.
type giteaReporter struct {
	service *Service
	api     string
	repo    string
	token   string
}

func newGiteaReporter(s *Service, location remote, token string) (Reporter, error) {
	parts, ok := location.segments(2)
	if !ok {
		return nil, unknownRemote()
	}
	repo := "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	return &giteaReporter{service: s, api: location.origin + "/api/v1", repo: repo, token: token}, nil
}

func (r *giteaReporter) call(ctx context.Context, method, path string, input, output any) error {
	header := http.Header{}
	header.Set("Authorization", "token "+r.token)
	return r.service.call(ctx, apiRequest{
		provider: "Gitea", method: method, url: r.api + path, header: header, input: input, output: output,
	})
}

func (r *giteaReporter) SetCommitStatus(ctx context.Context, status CommitStatus) error {
	if err := validStatus(status); err != nil {
		return err
	}
	input := map[string]string{
		"state": string(status.State), "context": status.Context, "description": truncate(status.Description, 255),
	}
	if status.TargetURL != "" {
		input["target_url"] = status.TargetURL
	}
	return r.call(ctx, http.MethodPost, r.repo+"/statuses/"+status.SHA, input, nil)
}

type giteaComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

func (r *giteaReporter) UpsertComment(ctx context.Context, comment Comment) error {
	if err := validComment(comment); err != nil {
		return err
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := r.call(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return err
	}
	body := markedBody(comment)
	comments := fmt.Sprintf("%s/issues/%d/comments", r.repo, comment.Number)
	for page := 1; page <= maxPages; page++ {
		var rows []giteaComment
		if err := r.call(ctx, http.MethodGet, fmt.Sprintf("%s?limit=50&page=%d", comments, page), nil, &rows); err != nil {
			return err
		}
		for _, row := range rows {
			if row.User.ID == user.ID && hasMarker(row.Body, comment.Marker) {
				if row.Body == body {
					return nil
				}
				path := fmt.Sprintf("%s/issues/comments/%d", r.repo, row.ID)
				return r.call(ctx, http.MethodPatch, path, map[string]string{"body": body}, nil)
			}
		}
		if len(rows) < 50 {
			break
		}
	}
	return r.call(ctx, http.MethodPost, comments, map[string]string{"body": body}, nil)
}
