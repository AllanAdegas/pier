package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

// Store persists databases and their backup executions.
type Store struct {
	db  *sql.DB
	box *secretbox.Box
}

// NewStore creates the store.
func NewStore(db *sql.DB, box *secretbox.Box) *Store { return &Store{db: db, box: box} }

const databaseColumns = `id, environment_id, server_id, name, description, engine, config, status, created_at, updated_at`

func (s *Store) insert(ctx context.Context, database Database, credentials Credentials) error {
	config, err := json.Marshal(database.Config)
	if err != nil {
		return err
	}
	rawCredentials, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	sealed, err := s.box.Seal(string(rawCredentials))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO databases
		(id, environment_id, server_id, name, description, engine, config, credentials, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		database.ID, database.EnvironmentID, database.ServerID, database.Name, database.Description, database.Engine,
		string(config), sealed, database.Status, sqlite.FormatTime(database.CreatedAt), sqlite.FormatTime(database.UpdatedAt))
	return err
}

func (s *Store) update(ctx context.Context, database Database) error {
	config, err := json.Marshal(database.Config)
	if err != nil {
		return err
	}
	return sqlite.RequireAffected(s.db.ExecContext(ctx,
		`UPDATE databases SET name = ?, description = ?, config = ?, updated_at = ? WHERE id = ?`,
		database.Name, database.Description, string(config), sqlite.FormatTime(time.Now()), database.ID))
}

// SetStatus records the runtime status observed on the server.
func (s *Store) SetStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE databases SET status = ? WHERE id = ? AND status <> ?`, status, id, status)
	return err
}

func (s *Store) delete(ctx context.Context, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM databases WHERE id = ?`, id))
}

// Get loads one database.
func (s *Store) Get(ctx context.Context, id string) (Database, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+databaseColumns+` FROM databases WHERE id = ?`, id)
	database, err := scanDatabase(row)
	return database, sqlite.NotFoundIfNoRows(err)
}

// List loads every database, optionally restricted to one environment.
func (s *Store) List(ctx context.Context, environmentID string) ([]Database, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+databaseColumns+` FROM databases
		WHERE (? = '' OR environment_id = ?) ORDER BY created_at`, environmentID, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	databases := []Database{}
	for rows.Next() {
		database, err := scanDatabase(rows)
		if err != nil {
			return nil, err
		}
		databases = append(databases, database)
	}
	return databases, rows.Err()
}

func (s *Store) credentials(ctx context.Context, id string) (Credentials, error) {
	var sealed string
	err := s.db.QueryRowContext(ctx, `SELECT credentials FROM databases WHERE id = ?`, id).Scan(&sealed)
	if err != nil {
		return Credentials{}, sqlite.NotFoundIfNoRows(err)
	}
	raw, err := s.box.Open(sealed)
	if err != nil {
		return Credentials{}, err
	}
	var credentials Credentials
	return credentials, json.Unmarshal([]byte(raw), &credentials)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDatabase(row rowScanner) (Database, error) {
	var database Database
	var config, createdAt, updatedAt string
	err := row.Scan(&database.ID, &database.EnvironmentID, &database.ServerID, &database.Name, &database.Description,
		&database.Engine, &config, &database.Status, &createdAt, &updatedAt)
	if err != nil {
		return Database{}, err
	}
	if err := json.Unmarshal([]byte(config), &database.Config); err != nil {
		return Database{}, fmt.Errorf("decode config of database %s: %w", database.ID, err)
	}
	database.CreatedAt = sqlite.ParseTime(createdAt)
	database.UpdatedAt = sqlite.ParseTime(updatedAt)
	return database, nil
}

const backupColumns = `id, database_id, trigger, status, file_path, size_bytes, message, started_at, finished_at`

func (s *Store) insertBackup(ctx context.Context, backup BackupExecution) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO backup_executions
		(id, database_id, trigger, status, file_path, started_at) VALUES (?, ?, ?, ?, ?, ?)`,
		backup.ID, backup.DatabaseID, backup.Trigger, backup.Status, backup.FilePath, sqlite.FormatTime(backup.StartedAt))
	return err
}

func (s *Store) setBackupFile(ctx context.Context, id, filePath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE backup_executions SET file_path = ? WHERE id = ?`, filePath, id)
	return err
}

func (s *Store) finishBackup(ctx context.Context, id string, status BackupStatus, sizeBytes int64, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE backup_executions
		SET status = ?, size_bytes = ?, message = ?, finished_at = ? WHERE id = ?`,
		status, sizeBytes, message, sqlite.FormatTime(time.Now()), id)
	return err
}

func (s *Store) getBackup(ctx context.Context, id string) (BackupExecution, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+backupColumns+` FROM backup_executions WHERE id = ?`, id)
	backup, err := scanBackup(row)
	return backup, sqlite.NotFoundIfNoRows(err)
}

func (s *Store) listBackups(ctx context.Context, databaseID string) ([]BackupExecution, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+backupColumns+` FROM backup_executions
		WHERE database_id = ? ORDER BY started_at DESC`, databaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	backups := []BackupExecution{}
	for rows.Next() {
		backup, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}
	return backups, rows.Err()
}

func (s *Store) deleteBackup(ctx context.Context, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM backup_executions WHERE id = ?`, id))
}

func (s *Store) failRunningBackups(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE backup_executions SET status = ?, message = ?, finished_at = ?
		WHERE status = ?`, BackupFailed, "Interrompido pela reinicialização do Pier.",
		sqlite.FormatTime(time.Now()), BackupRunning)
	return err
}

func scanBackup(row rowScanner) (BackupExecution, error) {
	var backup BackupExecution
	var startedAt string
	var finishedAt sql.NullString
	err := row.Scan(&backup.ID, &backup.DatabaseID, &backup.Trigger, &backup.Status, &backup.FilePath,
		&backup.SizeBytes, &backup.Message, &startedAt, &finishedAt)
	if err != nil {
		return BackupExecution{}, err
	}
	backup.StartedAt = sqlite.ParseTime(startedAt)
	backup.FinishedAt = sqlite.ParseOptionalTime(finishedAt)
	return backup, nil
}
