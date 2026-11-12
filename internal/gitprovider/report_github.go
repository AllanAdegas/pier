package gitprovider

import (
	"context"
	"fmt"
	"net/http"
)

// githubReporter reports through the GitHub App with a token narrowed to
// the linked repository (statuses and pull requests, write).
type githubReporter struct {
	service   *Service
	selection Selection
}

func (r *githubReporter) key() tokenKey {
	return tokenKey{installation: r.selection.InstallationID, repository: r.selection.Repository, purpose: purposeReport}
}

func (r *githubReporter) SetCommitStatus(ctx context.Context, status CommitStatus) error {
	if err := validStatus(status); err != nil {
		return err
	}
	state := string(status.State)
	input := map[string]string{
		"state": state, "context": status.Context, "description": truncate(status.Description, 140),
	}
	if status.TargetURL != "" {
		input["target_url"] = status.TargetURL
	}
	path := repositoryPath(r.selection.Repository) + "/statuses/" + status.SHA
	return r.service.withInstallationToken(ctx, r.key(), func(token string) error {
		return r.service.github(ctx, http.MethodPost, path, token, input, nil)
	})
}

type githubComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	App  *struct {
		ID int64 `json:"id"`
	} `json:"performed_via_github_app"`
}

func (r *githubReporter) UpsertComment(ctx context.Context, comment Comment) error {
	if err := validComment(comment); err != nil {
		return err
	}
	config, err := r.service.config(ctx)
	if err != nil {
		return err
	}
	body := markedBody(comment)
	base := repositoryPath(r.selection.Repository)
	return r.service.withInstallationToken(ctx, r.key(), func(token string) error {
		existing, err := r.findComment(ctx, token, base, comment, config.AppID)
		if err != nil {
			return err
		}
		switch {
		case existing == nil:
			path := fmt.Sprintf("%s/issues/%d/comments", base, comment.Number)
			return r.service.github(ctx, http.MethodPost, path, token, map[string]string{"body": body}, nil)
		case existing.Body == body:
			return nil
		}
		path := fmt.Sprintf("%s/issues/comments/%d", base, existing.ID)
		return r.service.github(ctx, http.MethodPatch, path, token, map[string]string{"body": body}, nil)
	})
}

// findComment returns the comment this App wrote with the marker. A marker
// in someone else's comment is ignored: it proves nothing.
func (r *githubReporter) findComment(ctx context.Context, token, base string, comment Comment, appID int64) (*githubComment, error) {
	for page := 1; page <= maxPages; page++ {
		var comments []githubComment
		path := fmt.Sprintf("%s/issues/%d/comments?per_page=100&page=%d", base, comment.Number, page)
		if err := r.service.github(ctx, http.MethodGet, path, token, nil, &comments); err != nil {
			return nil, err
		}
		for index := range comments {
			found := comments[index]
			if found.App != nil && found.App.ID == appID && hasMarker(found.Body, comment.Marker) {
				return &found, nil
			}
		}
		if len(comments) < 100 {
			return nil, nil
		}
	}
	return nil, nil
}
