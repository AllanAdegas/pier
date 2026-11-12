package database

import (
	"context"
	"database/sql"
	"pier/internal/platform/sqlite"
	"time"
)

// LastSuccessfulBackup returns the latest successful backup without a history page limit.
func (s *Service) LastSuccessfulBackup(ctx context.Context, id string) (*time.Time, error) {
	var value sql.NullString
	err := s.store.db.QueryRowContext(ctx, `SELECT MAX(finished_at) FROM backup_executions WHERE database_id=? AND status='succeeded'`, id).Scan(&value)
	return sqlite.ParseOptionalTime(value), err
}
