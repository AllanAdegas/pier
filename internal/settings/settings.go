// Package settings stores instance-wide preferences as key/value pairs.
package settings

import (
	"context"
	"database/sql"
	"net/url"
	"strings"

	"pier/internal/platform/apperr"
)

const (
	keyInstanceName = "instance_name"
	keyPublicURL    = "public_url"
)

// Settings are the preferences of the whole instance.
type Settings struct {
	InstanceName string `json:"instance_name"`
	// PublicURL is how the outside world reaches the panel; it is used to
	// build the webhook URLs shown to the user.
	PublicURL string `json:"public_url"`
}

// Store persists settings.
type Store struct {
	db *sql.DB
}

// NewStore creates the store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Get returns the current settings with defaults applied.
func (s *Store) Get(ctx context.Context) (Settings, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return Settings{}, err
	}
	defer rows.Close()
	current := Settings{InstanceName: "Pier"}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return Settings{}, err
		}
		switch key {
		case keyInstanceName:
			current.InstanceName = value
		case keyPublicURL:
			current.PublicURL = value
		}
	}
	return current, rows.Err()
}

// Update validates and saves the settings.
func (s *Store) Update(ctx context.Context, next Settings) (Settings, error) {
	next.InstanceName = strings.TrimSpace(next.InstanceName)
	next.PublicURL = strings.TrimRight(strings.TrimSpace(next.PublicURL), "/")
	fields := apperr.Fields{}
	if next.InstanceName == "" {
		fields.Add("instance_name", "Informe o nome da instância.")
	}
	if next.PublicURL != "" {
		parsed, err := url.Parse(next.PublicURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			fields.Add("public_url", "Informe uma URL http(s) válida, ex.: https://pier.exemplo.com.")
		}
	}
	if err := fields.Err(); err != nil {
		return Settings{}, err
	}
	for key, value := range map[string]string{keyInstanceName: next.InstanceName, keyPublicURL: next.PublicURL} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return Settings{}, err
		}
	}
	return next, nil
}
