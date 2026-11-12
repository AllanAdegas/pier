// Package settings stores instance-wide preferences as key/value pairs.
package settings

import (
	"context"
	"database/sql"
	"net/url"
	"strings"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

const (
	keyInstanceName = "instance_name"
	keyPublicURL    = "public_url"
	keyLanguage     = "language"
)

// Settings are the preferences of the whole instance.
type Settings struct {
	InstanceName string `json:"instance_name"`
	// PublicURL is how the outside world reaches the panel; it is used to
	// build the webhook URLs shown to the user.
	PublicURL string `json:"public_url"`
	// Language is used where no reader asked for one: deployment logs,
	// notifications and API answers without Accept-Language.
	Language i18n.Language `json:"language"`
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
	current := Settings{InstanceName: "Pier", Language: i18n.Portuguese}
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
		case keyLanguage:
			if language, ok := i18n.Parse(value); ok {
				current.Language = language
			}
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
	next.Language = s.validLanguage(ctx, next.Language, fields)
	if err := fields.Err(); err != nil {
		return Settings{}, err
	}
	values := map[string]string{keyInstanceName: next.InstanceName, keyPublicURL: next.PublicURL, keyLanguage: string(next.Language)}
	for key, value := range values {
		if err := s.put(ctx, key, value); err != nil {
			return Settings{}, err
		}
	}
	i18n.SetInstance(next.Language)
	return next, nil
}

// validLanguage keeps the current language when a client sends none.
func (s *Store) validLanguage(ctx context.Context, requested i18n.Language, fields apperr.Fields) i18n.Language {
	if requested == "" {
		current, err := s.Get(ctx)
		if err != nil {
			return i18n.Portuguese
		}
		return current.Language
	}
	language, ok := i18n.Parse(string(requested))
	if !ok {
		fields.Add("language", "Idioma não suportado: use pt-BR ou en.")
	}
	return language
}

// LoadLanguage makes the saved language the instance's, at startup.
func (s *Store) LoadLanguage(ctx context.Context) error {
	current, err := s.Get(ctx)
	if err != nil {
		return err
	}
	i18n.SetInstance(current.Language)
	return nil
}

// AdoptLanguage saves the language of whoever set the instance up, so the
// logs and notifications speak it from the start.
func (s *Store) AdoptLanguage(ctx context.Context, language i18n.Language) error {
	if err := s.put(ctx, keyLanguage, string(language)); err != nil {
		return err
	}
	i18n.SetInstance(language)
	return nil
}

func (s *Store) put(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
