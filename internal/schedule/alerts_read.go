package schedule

import (
	"context"
	"database/sql"
	"pier/internal/platform/sqlite"
	"time"
)

// EnabledTasks returns the schedules monitored for missed successful runs.
func (s *TaskService) EnabledTasks(ctx context.Context) ([]Task, error) {
	return s.list(ctx, `WHERE enabled=1`)
}

// LastSuccess returns the latest successful execution, independent of the history page size.
func (s *TaskService) LastSuccess(ctx context.Context, id string) (*time.Time, error) {
	var value sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT MAX(finished_at) FROM task_executions WHERE task_id=? AND status='succeeded'`, id).Scan(&value)
	return sqlite.ParseOptionalTime(value), err
}
