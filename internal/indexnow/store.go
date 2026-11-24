package indexnow

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"pier/internal/platform/idgen"
	"pier/internal/platform/sqlite"
)

// errHostTaken is returned when another resource already notifies the host.
var errHostTaken = errors.New("host taken")

// Store persists the sites.
type Store struct{ db *sql.DB }

// NewStore creates the store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const siteColumns = `id, host, resource_kind, resource_id, key, created_at,
	last_attempt_at, last_submitted_at, last_status, last_message, last_url_count, last_problem`

// Create turns notification on for a host of a resource.
func (s *Store) Create(ctx context.Context, kind, resourceID, host string) (Site, error) {
	site := Site{ID: idgen.New(), Host: host, ResourceKind: kind, ResourceID: resourceID, Key: newKey(),
		CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO indexnow_sites (id, host, resource_kind, resource_id, key, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, site.ID, site.Host, kind, resourceID, site.Key, sqlite.FormatTime(site.CreatedAt))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return Site{}, errHostTaken
	}
	return site, err
}

// Delete removes a site.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM indexnow_sites WHERE id = ?`, id)
	return err
}

// ByResource lists the sites of a resource.
func (s *Store) ByResource(ctx context.Context, kind, resourceID string) ([]Site, error) {
	return s.query(ctx, `SELECT `+siteColumns+` FROM indexnow_sites
		WHERE resource_kind = ? AND resource_id = ? ORDER BY host`, kind, resourceID)
}

// All lists every site.
func (s *Store) All(ctx context.Context) ([]Site, error) {
	return s.query(ctx, `SELECT `+siteColumns+` FROM indexnow_sites ORDER BY host`)
}

// ByHost returns the site of a host.
func (s *Store) ByHost(ctx context.Context, host string) (Site, error) {
	return s.one(ctx, `SELECT `+siteColumns+` FROM indexnow_sites WHERE host = ?`, host)
}

// ByKey returns the site of a key.
func (s *Store) ByKey(ctx context.Context, key string) (Site, error) {
	return s.one(ctx, `SELECT `+siteColumns+` FROM indexnow_sites WHERE key = ?`, key)
}

// Record keeps the outcome of a submission.
func (s *Store) Record(ctx context.Context, id string, outcome Outcome) error {
	_, err := s.db.ExecContext(ctx, `UPDATE indexnow_sites SET last_attempt_at = ?, last_submitted_at = ?,
		last_status = ?, last_message = ?, last_url_count = ?, last_problem = ? WHERE id = ?`,
		sqlite.FormatOptionalTime(outcome.AttemptedAt), sqlite.FormatOptionalTime(outcome.SubmittedAt),
		outcome.Status, outcome.Message, outcome.URLCount, outcome.Problem, id)
	return err
}

func (s *Store) one(ctx context.Context, query string, args ...any) (Site, error) {
	sites, err := s.query(ctx, query, args...)
	if err != nil {
		return Site{}, err
	}
	if len(sites) == 0 {
		return Site{}, sqlite.ErrNotFound
	}
	return sites[0], nil
}

func (s *Store) query(ctx context.Context, query string, args ...any) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sites := []Site{}
	for rows.Next() {
		var site Site
		var created string
		var attempted, submitted sql.NullString
		if err := rows.Scan(&site.ID, &site.Host, &site.ResourceKind, &site.ResourceID, &site.Key, &created,
			&attempted, &submitted, &site.Last.Status, &site.Last.Message, &site.Last.URLCount,
			&site.Last.Problem); err != nil {
			return nil, err
		}
		site.CreatedAt = sqlite.ParseTime(created)
		site.Last.AttemptedAt = sqlite.ParseOptionalTime(attempted)
		site.Last.SubmittedAt = sqlite.ParseOptionalTime(submitted)
		sites = append(sites, site)
	}
	return sites, rows.Err()
}
