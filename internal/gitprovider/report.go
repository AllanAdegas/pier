package gitprovider

import (
	"context"
	"fmt"
	"strings"
)

// Report serializes status and PR comment updates to avoid duplicate comments.
func (s *Service) Report(ctx context.Context, selection Selection, sha string, number int, state, body string) error {
	if !validRepo(selection.Repository) || number <= 0 || len(sha) != 40 || strings.Trim(sha, "0123456789abcdefABCDEF") != "" {
		return invalid()
	}
	if state != "pending" && state != "success" && state != "failure" && state != "error" {
		return invalid()
	}
	if len(body) > 30000 {
		return invalid()
	}
	return s.locked(ctx, func() error {
		token, err := s.repositoryToken(ctx, selection.InstallationID, selection.Repository, true)
		if err != nil {
			return err
		}
		config, err := s.config(ctx)
		if err != nil {
			return err
		}
		base := repositoryPath(selection.Repository)
		if err = s.request(ctx, "POST", base+"/statuses/"+sha, token, map[string]string{"state": state, "context": "pier/preview"}, nil); err != nil {
			return err
		}
		marker := "<!-- pier:preview -->"
		body = marker + "\n" + body
		for page := 1; page <= 100; page++ {
			var comments []struct {
				ID  int64 `json:"id"`
				App *struct {
					ID int64 `json:"id"`
				} `json:"performed_via_github_app"`
				Body string `json:"body"`
				User struct {
					Type string `json:"type"`
				} `json:"user"`
			}
			if err = s.request(ctx, "GET", fmt.Sprintf("%s/issues/%d/comments?per_page=100&page=%d", base, number, page), token, nil, &comments); err != nil {
				return err
			}
			for _, comment := range comments {
				if strings.HasPrefix(comment.Body, marker) && comment.User.Type == "Bot" && comment.App != nil && comment.App.ID == config.AppID {
					if comment.Body == body {
						return nil
					}
					return s.request(ctx, "PATCH", fmt.Sprintf("%s/issues/comments/%d", base, comment.ID), token, map[string]string{"body": body}, nil)
				}
			}
			if len(comments) < 100 {
				return s.request(ctx, "POST", fmt.Sprintf("%s/issues/%d/comments", base, number), token, map[string]string{"body": body}, nil)
			}
		}
		return invalid()
	})
}
