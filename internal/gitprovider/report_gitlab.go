package gitprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// gitlabReporter uses the REST API v4 of gitlab.com or a self-hosted GitLab
// (the host of the repository) with a personal, group or project access
// token that has the "api" scope.
type gitlabReporter struct {
	service *Service
	api     string
	project string
	token   string
}

func newGitLabReporter(s *Service, location remote, token string) (Reporter, error) {
	if _, ok := location.segments(0); !ok {
		return nil, unknownRemote()
	}
	project := url.PathEscape(location.path)
	return &gitlabReporter{service: s, api: location.origin + "/api/v4", project: project, token: token}, nil
}

func (r *gitlabReporter) call(ctx context.Context, method, path string, input, output any) error {
	header := http.Header{}
	header.Set("PRIVATE-TOKEN", r.token)
	return r.service.call(ctx, apiRequest{
		provider: "GitLab", method: method, url: r.api + "/projects/" + r.project + path,
		header: header, input: input, output: output,
	})
}

func gitlabState(state State) string {
	switch state {
	case StatePending:
		return "running"
	case StateSuccess:
		return "success"
	case StateFailure:
		return "failed"
	}
	return "canceled"
}

func (r *gitlabReporter) SetCommitStatus(ctx context.Context, status CommitStatus) error {
	if err := validStatus(status); err != nil {
		return err
	}
	input := map[string]string{
		"state": gitlabState(status.State), "name": status.Context, "description": truncate(status.Description, 255),
	}
	if status.TargetURL != "" {
		input["target_url"] = status.TargetURL
	}
	return r.call(ctx, http.MethodPost, "/statuses/"+status.SHA, input, nil)
}

type gitlabNote struct {
	ID     int64  `json:"id"`
	Body   string `json:"body"`
	Author struct {
		ID int64 `json:"id"`
	} `json:"author"`
}

func (r *gitlabReporter) UpsertComment(ctx context.Context, comment Comment) error {
	if err := validComment(comment); err != nil {
		return err
	}
	var user struct {
		ID int64 `json:"id"`
	}
	header := http.Header{}
	header.Set("PRIVATE-TOKEN", r.token)
	err := r.service.call(ctx, apiRequest{provider: "GitLab", method: http.MethodGet, url: r.api + "/user", header: header, output: &user})
	if err != nil {
		return err
	}
	body := markedBody(comment)
	notes := fmt.Sprintf("/merge_requests/%d/notes", comment.Number)
	for page := 1; page <= maxPages; page++ {
		var rows []gitlabNote
		if err := r.call(ctx, http.MethodGet, fmt.Sprintf("%s?sort=asc&per_page=100&page=%d", notes, page), nil, &rows); err != nil {
			return err
		}
		for _, note := range rows {
			if note.Author.ID == user.ID && hasMarker(note.Body, comment.Marker) {
				if note.Body == body {
					return nil
				}
				return r.call(ctx, http.MethodPut, fmt.Sprintf("%s/%d", notes, note.ID), map[string]string{"body": body}, nil)
			}
		}
		if len(rows) < 100 {
			break
		}
	}
	return r.call(ctx, http.MethodPost, notes, map[string]string{"body": body}, nil)
}
