package gitprovider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"pier/internal/platform/apperr"
)

// ErrNoSelection is the only error that permits source-authentication fallback.
var ErrNoSelection = errors.New("no GitHub App repository selection")

// Kinds of resources that deploy from a repository.
const (
	KindApplication = "application"
	KindStack       = "stack"
)

// Selection links a resource to a repository of an installation.
type Selection struct {
	InstallationID int64  `json:"installation_id"`
	Repository     string `json:"repository"`
}

// CloneURL is the HTTPS address the resource source must use.
func (s Selection) CloneURL() string { return fmt.Sprintf("https://github.com/%s.git", s.Repository) }

func validKind(kind string) bool { return kind == KindApplication || kind == KindStack }

// Select links a resource to a repository the installation can read.
func (s *Service) Select(ctx context.Context, kind, id string, selection Selection) error {
	if !validKind(kind) || id == "" || !validRepo(selection.Repository) || selection.InstallationID <= 0 {
		return apperr.Invalid("Escolha uma instalação e um repositório do GitHub App.")
	}
	if err := s.requireResource(ctx, kind, id); err != nil {
		return err
	}
	var linked int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM git_connection_links WHERE kind=? AND resource_id=? AND orphaned=0`, kind, id).Scan(&linked); err != nil {
		return err
	}
	if linked > 0 {
		return apperr.Conflict("Desvincule a conexão Git antes de usar o GitHub App.")
	}
	rows, err := s.Repositories(ctx, selection.InstallationID)
	if err != nil {
		return err
	}
	found := false
	for _, row := range rows {
		if strings.EqualFold(row.FullName, selection.Repository) {
			selection.Repository, found = row.FullName, true
		}
	}
	if !found {
		return apperr.Invalid("A instalação do GitHub App não dá acesso a %s.", selection.Repository)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO git_provider_selections(kind,resource_id,installation_id,repository)
		VALUES(?,?,?,?) ON CONFLICT(kind,resource_id) DO UPDATE SET
		installation_id=excluded.installation_id, repository=excluded.repository`,
		kind, id, selection.InstallationID, selection.Repository)
	return err
}

// Selection returns the link of a resource, or ErrNoSelection.
func (s *Service) Selection(ctx context.Context, kind, id string) (Selection, error) {
	var v Selection
	err := s.db.QueryRowContext(ctx, `SELECT installation_id,repository FROM git_provider_selections
		WHERE kind=? AND resource_id=?`, kind, id).Scan(&v.InstallationID, &v.Repository)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNoSelection
	}
	return v, err
}

// WithCredentials scopes a read-only clone token to one operation. Never persist it.
func (s *Service) WithCredentials(ctx context.Context, kind, id string, fn func(repository, token string) error) error {
	selection, err := s.Selection(ctx, kind, id)
	if err != nil {
		return err
	}
	if err := s.requireBoundSource(ctx, kind, id, s.cloneURL(ctx, selection)); err != nil {
		return err
	}
	key := tokenKey{installation: selection.InstallationID, repository: selection.Repository, purpose: purposeClone}
	token, err := s.installationToken(ctx, key)
	if err != nil {
		return err
	}
	return fn(s.cloneURL(ctx, selection), token)
}

// ClearSelection restores the ordinary public/deploy-key source authentication.
func (s *Service) ClearSelection(ctx context.Context, kind, id string) error {
	if !validKind(kind) || id == "" {
		return invalid()
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM git_provider_selections WHERE kind=? AND resource_id=?`, kind, id)
	return err
}

// Target is a resource linked to the repository of a webhook event.
type Target struct {
	Kind       string `json:"kind"`
	ResourceID string `json:"resource_id"`
	Repository string `json:"repository"`
}

func (s *Service) targets(ctx context.Context, installation int64, repository string) ([]Target, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind,resource_id,repository FROM git_provider_selections
		WHERE installation_id=? AND repository=? COLLATE NOCASE ORDER BY kind,resource_id`, installation, repository)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := []Target{}
	for rows.Next() {
		var target Target
		if err := rows.Scan(&target.Kind, &target.ResourceID, &target.Repository); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (s *Service) requireResource(ctx context.Context, kind, id string) error {
	table, what := "applications", "Aplicação"
	if kind == KindStack {
		table, what = "stacks", "Stack"
	}
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE id=?", id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFound(what)
	}
	return err
}

// requireBoundSource prevents a changed source from borrowing a stored binding.
func (s *Service) requireBoundSource(ctx context.Context, kind, id, repository string) error {
	var source sql.NullString
	if kind == KindStack {
		var sealed string
		if err := s.db.QueryRowContext(ctx, "SELECT repository FROM stack_git_sources WHERE stack_id=?", id).Scan(&sealed); err != nil {
			return err
		}
		raw, err := s.box.Open(sealed)
		if err != nil {
			return err
		}
		source = sql.NullString{String: raw, Valid: true}
	} else {
		if err := s.db.QueryRowContext(ctx, "SELECT json_extract(config, '$.source.repository') FROM applications WHERE id=?", id).Scan(&source); err != nil {
			return err
		}
	}
	if !source.Valid || !SameRepositoryURL(source.String, repository) {
		return apperr.Invalid("O repositório da origem não corresponde ao repositório vinculado. Selecione o repositório de novo.")
	}
	return nil
}
