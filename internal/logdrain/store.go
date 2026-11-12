package logdrain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"pier/internal/platform/apperr"
	"pier/internal/platform/sqlite"
)

type store struct{ db *sql.DB }

const drainColumns = `id, name, kind, url, token, labels, scope_type, scope_id, enabled, created_at, updated_at`

// stored is a drain with its sealed token, as the database keeps it.
type stored struct {
	Drain
	sealedToken string
}

func scanDrain(row interface{ Scan(...any) error }) (stored, error) {
	var drain stored
	var labels, created, updated string
	err := row.Scan(&drain.ID, &drain.Name, &drain.Kind, &drain.URL, &drain.sealedToken, &labels, &drain.ScopeType,
		&drain.ScopeID, &drain.Enabled, &created, &updated)
	if err != nil {
		return stored{}, err
	}
	drain.Labels = map[string]string{}
	if err := json.Unmarshal([]byte(labels), &drain.Labels); err != nil {
		return stored{}, err
	}
	drain.HasToken = drain.sealedToken != ""
	drain.CreatedAt, drain.UpdatedAt = sqlite.ParseTime(created), sqlite.ParseTime(updated)
	return drain, nil
}

func (s store) list(ctx context.Context, onlyEnabled bool) ([]stored, error) {
	query := `SELECT ` + drainColumns + ` FROM log_drains`
	if onlyEnabled {
		query += ` WHERE enabled = 1`
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	drains := []stored{}
	for rows.Next() {
		drain, err := scanDrain(rows)
		if err != nil {
			return nil, err
		}
		drains = append(drains, drain)
	}
	return drains, rows.Err()
}

func (s store) get(ctx context.Context, id string) (stored, error) {
	drain, err := scanDrain(s.db.QueryRowContext(ctx, `SELECT `+drainColumns+` FROM log_drains WHERE id = ?`, id))
	return drain, drainNotFound(sqlite.NotFoundIfNoRows(err))
}

func (s store) save(ctx context.Context, drain stored) error {
	labels, err := json.Marshal(drain.Labels)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO log_drains (`+drainColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, kind = excluded.kind, url = excluded.url, token = excluded.token,
			labels = excluded.labels, scope_type = excluded.scope_type, scope_id = excluded.scope_id,
			enabled = excluded.enabled, updated_at = excluded.updated_at`,
		drain.ID, drain.Name, drain.Kind, drain.URL, drain.sealedToken, string(labels), drain.ScopeType, drain.ScopeID,
		drain.Enabled, sqlite.FormatTime(drain.CreatedAt), sqlite.FormatTime(drain.UpdatedAt))
	if err != nil && sqliteUnique(err) {
		return apperr.Conflict("Já existe um encaminhamento com este nome.")
	}
	return err
}

func (s store) delete(ctx context.Context, id string) error {
	return drainNotFound(sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM log_drains WHERE id = ?`, id)))
}

func drainNotFound(err error) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound("Encaminhamento de logs")
	}
	return err
}

func sqliteUnique(err error) bool { return strings.Contains(err.Error(), "UNIQUE") }
