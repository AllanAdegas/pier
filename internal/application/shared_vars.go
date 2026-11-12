package application

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"pier/internal/envvar"
	"pier/internal/platform/i18n"
	"pier/internal/platform/sqlite"
)

// RedeployStatus says whether a running application still uses old values of
// the shared variables it references. Pier never redeploys on its own.
type RedeployStatus struct {
	Required bool       `json:"required"`
	Reason   string     `json:"reason"`
	MarkedAt *time.Time `json:"marked_at"`
}

// ListSharedVariables returns the shared variables of a scope, secrets masked.
func (s *Service) ListSharedVariables(ctx context.Context, scope envvar.SharedScope, scopeID string) ([]envvar.SharedVariable, error) {
	variables, err := s.env.ListShared(ctx, scope, scopeID)
	if err != nil {
		return nil, err
	}
	for index := range variables {
		variables[index] = variables[index].Masked()
	}
	return variables, nil
}

// SetSharedVariable saves a shared variable and, when its value changed,
// marks the applications that reference it as needing a redeploy.
func (s *Service) SetSharedVariable(ctx context.Context, scope envvar.SharedScope, scopeID string, input envvar.SharedInput) (envvar.SharedVariable, error) {
	variable, changed, err := s.env.SetShared(ctx, scope, scopeID, input)
	if err != nil {
		return envvar.SharedVariable{}, err
	}
	if changed {
		if err := s.markDependents(ctx, scope, scopeID, variable.Key); err != nil {
			return envvar.SharedVariable{}, err
		}
	}
	return variable.Masked(), nil
}

// DeleteSharedVariable removes a shared variable. Applications that still
// reference it are marked: their next deployment fails until it is fixed.
func (s *Service) DeleteSharedVariable(ctx context.Context, scope envvar.SharedScope, scopeID, key string) error {
	if err := s.env.DeleteShared(ctx, scope, scopeID, key); err != nil {
		return err
	}
	return s.markDependents(ctx, scope, scopeID, key)
}

// SharedVariableDependents lists the applications that reference a variable.
func (s *Service) SharedVariableDependents(ctx context.Context, scope envvar.SharedScope, scopeID, key string) ([]Application, error) {
	environments, err := s.store.environmentsOf(ctx, scope, scopeID)
	if err != nil {
		return nil, err
	}
	reference := envvar.SharedReference(scope, key)
	dependents := []Application{}
	for _, environmentID := range environments {
		apps, err := s.store.List(ctx, environmentID)
		if err != nil {
			return nil, err
		}
		for _, app := range apps {
			uses, err := s.referencesValue(ctx, app.ID, reference)
			if err != nil {
				return nil, err
			}
			if uses {
				dependents = append(dependents, app)
			}
		}
	}
	return dependents, nil
}

func (s *Service) markDependents(ctx context.Context, scope envvar.SharedScope, scopeID, key string) error {
	dependents, err := s.SharedVariableDependents(ctx, scope, scopeID, key)
	if err != nil {
		return err
	}
	reason := i18n.Instance().Sprintf("A variável compartilhada %s mudou.", envvar.SharedReference(scope, key))
	for _, app := range dependents {
		if err := s.store.markRedeploy(ctx, app.ID, reason); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) referencesValue(ctx context.Context, applicationID, reference string) (bool, error) {
	variables, err := s.env.List(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: applicationID})
	if err != nil {
		return false, err
	}
	for _, variable := range variables {
		if strings.Contains(variable.Value, reference) {
			return true, nil
		}
	}
	return false, nil
}

// RedeployStatus reports whether the application was marked after its last
// successful deployment started. An application never deployed needs none.
func (s *Service) RedeployStatus(ctx context.Context, applicationID string) (RedeployStatus, error) {
	if _, err := s.Get(ctx, applicationID); err != nil {
		return RedeployStatus{}, err
	}
	var reason, markedAt string
	err := s.store.db.QueryRowContext(ctx, `SELECT reason, marked_at FROM application_redeploy_marks WHERE application_id = ?`,
		applicationID).Scan(&reason, &markedAt)
	if err == sql.ErrNoRows {
		return RedeployStatus{}, nil
	}
	if err != nil {
		return RedeployStatus{}, err
	}
	var lastStarted sql.NullString
	if err := s.store.db.QueryRowContext(ctx, `SELECT MAX(started_at) FROM deployments WHERE application_id = ? AND status = ?`,
		applicationID, DeploymentFinished).Scan(&lastStarted); err != nil {
		return RedeployStatus{}, err
	}
	marked := sqlite.ParseTime(markedAt)
	required := lastStarted.Valid && marked.After(sqlite.ParseTime(lastStarted.String))
	return RedeployStatus{Required: required, Reason: reason, MarkedAt: &marked}, nil
}

func (s *Store) markRedeploy(ctx context.Context, applicationID, reason string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO application_redeploy_marks (application_id, reason, marked_at) VALUES (?, ?, ?)
		ON CONFLICT (application_id) DO UPDATE SET reason = excluded.reason, marked_at = excluded.marked_at`,
		applicationID, reason, sqlite.FormatTime(time.Now()))
	return err
}

// environmentsOf lists the environments a shared variable reaches.
func (s *Store) environmentsOf(ctx context.Context, scope envvar.SharedScope, scopeID string) ([]string, error) {
	if scope == envvar.SharedEnvironment {
		return []string{scopeID}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM environments WHERE project_id = ? ORDER BY created_at`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
