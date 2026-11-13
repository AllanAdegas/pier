package volumebackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"pier/internal/platform/sqlite"
)

// Store persists policies, archives and restore operations.
type Store struct{ db *sql.DB }

// NewStore creates the store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) policy(ctx context.Context, kind, id string) (Policy, error) {
	policy := Policy{Cron: defaultCron, Retention: defaultRetention}
	err := s.db.QueryRowContext(ctx, `SELECT enabled, cron, retention, s3, quiesce, include_external
		FROM volume_backup_policies WHERE resource_type = ? AND resource_id = ?`, kind, id).
		Scan(&policy.Enabled, &policy.Cron, &policy.Retention, &policy.S3, &policy.Quiesce, &policy.IncludeExternal)
	if err == sql.ErrNoRows {
		return policy, nil
	}
	return policy, err
}

func (s *Store) setPolicy(ctx context.Context, kind, id string, p Policy) error {
	now := sqlite.FormatTime(time.Now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO volume_backup_policies
		(resource_type, resource_id, enabled, cron, retention, s3, quiesce, include_external, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(resource_type, resource_id) DO UPDATE SET enabled=excluded.enabled, cron=excluded.cron,
		retention=excluded.retention, s3=excluded.s3, quiesce=excluded.quiesce,
		include_external=excluded.include_external, updated_at=excluded.updated_at`,
		kind, id, p.Enabled, p.Cron, p.Retention, p.S3, p.Quiesce, p.IncludeExternal, now, now)
	return err
}

type policyRow struct {
	Kind, ID string
	Created  time.Time
	Policy   Policy
}

func (s *Store) enabledPolicies(ctx context.Context) ([]policyRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT resource_type, resource_id, cron, retention, s3, quiesce,
		include_external, created_at FROM volume_backup_policies WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []policyRow
	for rows.Next() {
		row := policyRow{Policy: Policy{Enabled: true}}
		var created string
		if err := rows.Scan(&row.Kind, &row.ID, &row.Policy.Cron, &row.Policy.Retention, &row.Policy.S3,
			&row.Policy.Quiesce, &row.Policy.IncludeExternal, &created); err != nil {
			return nil, err
		}
		row.Created = sqlite.ParseTime(created)
		result = append(result, row)
	}
	return result, rows.Err()
}

const backupColumns = `id, resource_type, resource_id, trigger, status, file_path, size_bytes, mounts, stopped,
	remote_key, message, started_at, finished_at, server_id`

func (s *Store) insertBackup(ctx context.Context, b Backup) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO volume_backups
		(id, resource_type, resource_id, server_id, trigger, status, started_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.ResourceType, b.ResourceID, b.serverID, b.Trigger, b.Status, sqlite.FormatTime(b.StartedAt))
	return err
}

func (s *Store) setFile(ctx context.Context, id, filePath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE volume_backups SET file_path = ? WHERE id = ?`, filePath, id)
	return err
}

func (s *Store) setMounts(ctx context.Context, id string, mounts []Mount) error {
	raw, err := json.Marshal(mounts)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE volume_backups SET mounts = ? WHERE id = ?`, string(raw), id)
	return err
}

func (s *Store) setBackupStopped(ctx context.Context, id string, containers []string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE volume_backups SET stopped = ? WHERE id = ?`, encodeList(containers), id)
	return err
}

func (s *Store) setRemote(ctx context.Context, id, key string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE volume_backups SET remote_key = ? WHERE id = ?`, key, id)
	return err
}

func (s *Store) finishBackup(ctx context.Context, id, status string, size int64, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE volume_backups SET status = ?, size_bytes = ?, message = ?,
		stopped = '[]', finished_at = ? WHERE id = ?`, status, size, message, sqlite.FormatTime(time.Now()), id)
	return err
}

func (s *Store) backup(ctx context.Context, id string) (Backup, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+backupColumns+` FROM volume_backups WHERE id = ?`, id)
	b, err := scanBackup(row)
	return b, sqlite.NotFoundIfNoRows(err)
}

func (s *Store) backups(ctx context.Context, where string, args ...any) ([]Backup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+backupColumns+` FROM volume_backups `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Backup{}
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (s *Store) listBackups(ctx context.Context, kind, id string) ([]Backup, error) {
	return s.backups(ctx, `WHERE resource_type = ? AND resource_id = ? ORDER BY started_at DESC`, kind, id)
}

func (s *Store) runningBackups(ctx context.Context) ([]Backup, error) {
	return s.backups(ctx, `WHERE status = 'running'`)
}

func (s *Store) lastSuccess(ctx context.Context, kind, id string) (*time.Time, error) {
	var started sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT MAX(started_at) FROM volume_backups
		WHERE resource_type = ? AND resource_id = ? AND status = 'succeeded'`, kind, id).Scan(&started)
	if err != nil || !started.Valid {
		return nil, err
	}
	at := sqlite.ParseTime(started.String)
	return &at, nil
}

func (s *Store) deleteBackup(ctx context.Context, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM volume_backups WHERE id = ?`, id))
}

// busy counts executions and operations still running for the resource, as
// persisted: it also covers work that survived in the table across a restart.
func (s *Store) busy(ctx context.Context, kind, id string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM volume_backups WHERE resource_type = ? AND resource_id = ? AND status = 'running') +
		(SELECT COUNT(*) FROM volume_operations WHERE resource_type = ? AND resource_id = ? AND status = 'running')`,
		kind, id, kind, id).Scan(&count)
	return count, err
}

func scanBackup(row interface{ Scan(...any) error }) (Backup, error) {
	var b Backup
	var mounts, stopped, started string
	var finished sql.NullString
	if err := row.Scan(&b.ID, &b.ResourceType, &b.ResourceID, &b.Trigger, &b.Status, &b.FilePath, &b.SizeBytes,
		&mounts, &stopped, &b.remoteKey, &b.Message, &started, &finished, &b.serverID); err != nil {
		return Backup{}, err
	}
	if err := json.Unmarshal([]byte(mounts), &b.Mounts); err != nil {
		return Backup{}, err
	}
	b.stopped = decodeList(stopped)
	b.Remote = b.remoteKey != ""
	b.StartedAt = sqlite.ParseTime(started)
	b.FinishedAt = sqlite.ParseOptionalTime(finished)
	return b, nil
}

func (s *Store) insertOperation(ctx context.Context, op Operation) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO volume_operations
		(id, resource_type, resource_id, backup_id, kind, status, started_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		op.ID, op.ResourceType, op.ResourceID, op.BackupID, op.Kind, op.Status, sqlite.FormatTime(op.StartedAt))
	return err
}

func (s *Store) setOperationStopped(ctx context.Context, id string, containers []string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE volume_operations SET stopped = ? WHERE id = ?`, encodeList(containers), id)
	return err
}

func (s *Store) setOperationLogs(ctx context.Context, id, logs string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE volume_operations SET logs = ? WHERE id = ?`, logs, id)
	return err
}

func (s *Store) finishOperation(ctx context.Context, id, status, message, logs string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE volume_operations SET status = ?, message = ?, logs = ?,
		stopped = '[]', finished_at = ? WHERE id = ?`, status, message, logs, sqlite.FormatTime(time.Now()), id)
	return err
}

func (s *Store) operations(ctx context.Context, kind, id string) ([]Operation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, resource_type, resource_id, backup_id, kind, status, message,
		logs, started_at, finished_at FROM volume_operations WHERE resource_type = ? AND resource_id = ?
		ORDER BY started_at DESC LIMIT 50`, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Operation{}
	for rows.Next() {
		var op Operation
		var started string
		var finished sql.NullString
		if err := rows.Scan(&op.ID, &op.ResourceType, &op.ResourceID, &op.BackupID, &op.Kind, &op.Status,
			&op.Message, &op.Logs, &started, &finished); err != nil {
			return nil, err
		}
		op.StartedAt = sqlite.ParseTime(started)
		op.FinishedAt = sqlite.ParseOptionalTime(finished)
		result = append(result, op)
	}
	return result, rows.Err()
}

type runningOperation struct {
	ID, Kind, ResourceType, ResourceID, BackupID string
	Stopped                                      []string
}

func (s *Store) runningOperations(ctx context.Context) ([]runningOperation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, resource_type, resource_id, backup_id, stopped
		FROM volume_operations WHERE status = 'running'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []runningOperation
	for rows.Next() {
		var op runningOperation
		var stopped string
		if err := rows.Scan(&op.ID, &op.Kind, &op.ResourceType, &op.ResourceID, &op.BackupID, &stopped); err != nil {
			return nil, err
		}
		op.Stopped = decodeList(stopped)
		result = append(result, op)
	}
	return result, rows.Err()
}

func encodeList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}

func decodeList(raw string) []string {
	var values []string
	_ = json.Unmarshal([]byte(raw), &values)
	return values
}
