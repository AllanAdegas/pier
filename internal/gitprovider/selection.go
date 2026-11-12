package gitprovider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNoSelection is the only error that permits source-authentication fallback.
var ErrNoSelection = errors.New("no GitHub App repository selection")

type Selection struct {
	InstallationID int64  `json:"installation_id"`
	Repository     string `json:"repository"`
}

func (s *Service) Select(ctx context.Context, kind, id string, selection Selection) error {
	if (kind != "application" && kind != "stack") || id == "" || !validRepo(selection.Repository) {
		return invalid()
	}
	return s.locked(ctx, func() error {
		rows, err := s.repositories(ctx, selection.InstallationID)
		if err != nil {
			return err
		}
		found := false
		for _, r := range rows {
			if r.FullName == selection.Repository {
				found = true
			}
		}
		if !found {
			return invalid()
		}
		table := "applications"
		if kind == "stack" {
			table = "stacks"
		}
		var exists int
		if err = s.db.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE id=?", id).Scan(&exists); err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO git_provider_selections(kind,resource_id,installation_id,repository) VALUES(?,?,?,?) ON CONFLICT(kind,resource_id) DO UPDATE SET installation_id=excluded.installation_id,repository=excluded.repository`, kind, id, selection.InstallationID, selection.Repository)
		return err
	})
}
func (s *Service) Selection(ctx context.Context, kind, id string) (Selection, error) {
	var v Selection
	err := s.db.QueryRowContext(ctx, `SELECT installation_id,repository FROM git_provider_selections WHERE kind=? AND resource_id=?`, kind, id).Scan(&v.InstallationID, &v.Repository)
	return v, err
}

// WithCredentials scopes an in-memory credential to one operation. Never persist it.
func (s *Service) WithCredentials(ctx context.Context, kind, id string, fn func(repository, token string) error) error {
	selection, err := s.Selection(ctx, kind, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoSelection
	}
	if err != nil {
		return err
	}
	var token string
	err = s.locked(ctx, func() error {
		var err error
		token, err = s.repositoryToken(ctx, selection.InstallationID, selection.Repository, false)
		return err
	})
	if err != nil {
		return err
	}
	return fn(fmt.Sprintf("https://github.com/%s.git", selection.Repository), token)
}

// ClearSelection restores the ordinary public/deploy-key source authentication.
func (s *Service) ClearSelection(ctx context.Context, kind, id string) error {
	if (kind != "application" && kind != "stack") || id == "" {
		return invalid()
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM git_provider_selections WHERE kind=? AND resource_id=?`, kind, id)
	return err
}
