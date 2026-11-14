package secretbox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"
)

// LoadOrCreateSalt returns the installation salt stored in secretbox_state,
// creating it on first use. Concurrent first calls agree on one salt.
func LoadOrCreateSalt(ctx context.Context, db *sql.DB) ([]byte, error) {
	candidate := make([]byte, SaltLength)
	if _, err := rand.Read(candidate); err != nil {
		return nil, fmt.Errorf("secretbox: generate salt: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO secretbox_state (id, salt, created_at) VALUES (1, ?, ?)`,
		candidate, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, fmt.Errorf("secretbox: store salt: %w", err)
	}
	var salt []byte
	if err := db.QueryRowContext(ctx, `SELECT salt FROM secretbox_state WHERE id = 1`).Scan(&salt); err != nil {
		return nil, fmt.Errorf("secretbox: read salt: %w", err)
	}
	if err := validateSalt(salt); err != nil {
		return nil, err
	}
	return salt, nil
}
