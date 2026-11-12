package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/shell"
	"pier/internal/platform/sqlite"
)

const (
	backupTimeout = 2 * time.Hour

	// BackupTriggerManual and BackupTriggerScheduled record who asked for a backup.
	BackupTriggerManual    = "manual"
	BackupTriggerScheduled = "scheduled"
)

// Schedule pairs a database with the cron expression of its backup policy.
type Schedule struct {
	DatabaseID string
	Cron       string
}

// BackupSchedules lists the policies the scheduler must run.
func (s *Service) BackupSchedules(ctx context.Context) ([]Schedule, error) {
	databases, err := s.store.List(ctx, "")
	if err != nil {
		return nil, err
	}
	schedules := []Schedule{}
	for _, database := range databases {
		if database.Config.Backup.Enabled {
			schedules = append(schedules, Schedule{DatabaseID: database.ID, Cron: database.Config.Backup.Cron})
		}
	}
	return schedules, nil
}

// RecoverBackups marks executions interrupted by a restart as failed.
func (s *Service) RecoverBackups(ctx context.Context) error {
	if err := s.recoverOperations(ctx); err != nil {
		return err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id FROM backup_executions WHERE status='running'`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.recoverBackup(ctx, id); err != nil {
			if err := s.store.finishBackup(ctx, id, BackupFailed, 0, i18n.Instance().Sprintf("A limpeza da operação interrompida falhou: %s", err)); err != nil {
				return err
			}
		}
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE database_operations SET status='failed', message=?, finished_at=? WHERE status='running'`, i18n.Instance().Text("Interrompido pela reinicialização do Pier."), sqlite.FormatTime(time.Now())); err != nil {
		return err
	}
	return s.store.failRunningBackups(ctx)
}

func (s *Service) recoverBackup(ctx context.Context, id string) error {

	b, ex, err := s.reachBackup(ctx, id)
	if err != nil {
		return err
	}
	if b.FilePath == "" {
		db, err := s.Get(ctx, b.DatabaseID)
		if err != nil {
			return err
		}
		_, target, err := s.connector.Executor(ctx, db.ServerID)
		if err != nil {
			return err
		}
		spec, _ := specFor(db.Engine)
		if spec.Dump != nil {
			b.FilePath = path.Join(target.BackupDir(db.ID), b.StartedAt.UTC().Format("20060102-150405")+"-"+b.ID+"."+spec.Dump.Extension)
		}
	}
	if b.FilePath != "" {
		if _, err := ex.Run(ctx, "rm -f "+shell.Quote(path.Dir(b.FilePath)+"/.s3-")+"*"); err != nil {
			return err
		}
	}
	if b.FilePath != "" {
		if _, err := ex.Run(ctx, shell.Join("rm", "-f", b.FilePath, b.FilePath+".gz")); err != nil {
			return err
		}
	}
	return nil
}

// ListBackups returns the executions of a database, newest first.
func (s *Service) ListBackups(ctx context.Context, databaseID string) ([]BackupExecution, error) {
	if _, err := s.Get(ctx, databaseID); err != nil {
		return nil, err
	}
	return s.store.listBackups(ctx, databaseID)
}

// StartBackup validates the request, records the execution and runs the dump
// in the background, returning immediately.
func (s *Service) StartBackup(ctx context.Context, databaseID, trigger string) (BackupExecution, error) {
	release, err := s.acquire(databaseID)
	if err != nil {
		return BackupExecution{}, err
	}
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	if err := s.ensureIdle(ctx, databaseID); err != nil {
		return BackupExecution{}, err
	}
	database, err := s.Get(ctx, databaseID)
	if err != nil {
		return BackupExecution{}, err
	}
	spec, _ := specFor(database.Engine)
	if spec.Dump == nil {
		return BackupExecution{}, apperr.Conflict("%s ainda não tem suporte a backup.", spec.Label)
	}
	backup := BackupExecution{
		ID: idgen.New(), DatabaseID: databaseID, Trigger: trigger, Status: BackupRunning, StartedAt: time.Now(),
	}
	if err := s.store.insertBackup(ctx, backup); err != nil {
		return BackupExecution{}, err
	}
	started = true
	go func() { defer release(); s.runBackup(database, *spec.Dump, backup) }()
	return backup, nil
}

func (s *Service) runBackup(database Database, dump dumpSpec, backup BackupExecution) {
	ctx, cancel := context.WithTimeout(context.Background(), backupTimeout)
	defer cancel()
	filePath, size, err := s.dump(ctx, database, dump, backup)
	language := i18n.Instance()
	if err != nil {
		reason := i18n.Localize(err, language)
		_ = s.store.finishBackup(context.WithoutCancel(ctx), backup.ID, BackupFailed, 0, reason)
		s.events.Publish(notify.Event{
			Type: notify.BackupFailed, Resource: database.Name,
			Title: language.Sprintf("Backup falhou: %s", database.Name), Message: reason,
		})
		return
	}
	message := ""
	if database.Config.Backup.S3 {
		if err := s.uploadBackup(ctx, database, backup.ID, filePath); err != nil {
			message = language.Sprintf("Backup local concluído; a cópia S3 falhou: %s", i18n.Localize(err, language))
			s.events.Publish(notify.Event{Type: notify.BackupFailed, Resource: database.Name, Title: language.Sprintf("Backup falhou: %s", database.Name), Message: message})
		}
	}
	_ = s.store.finishBackup(context.WithoutCancel(ctx), backup.ID, BackupSucceeded, size, message)
	s.events.Publish(notify.Event{
		Type: notify.BackupSucceeded, Resource: database.Name,
		Title:   language.Sprintf("Backup concluído: %s", database.Name),
		Message: language.Sprintf("Arquivo %s (%d bytes).", path.Base(filePath), size),
	})
	s.applyRetention(ctx, database)
}

// dump writes the backup file on the server and returns its path and size.
func (s *Service) dump(ctx context.Context, database Database, dump dumpSpec, backup BackupExecution) (string, int64, error) {
	ex, target, err := s.connector.Executor(ctx, database.ServerID)
	if err != nil {
		return "", 0, err
	}
	containers, err := docker.ListByResource(ctx, ex, database.ID)
	if err != nil {
		return "", 0, err
	}
	container := firstRunning(containers)
	if container == "" {
		return "", 0, i18n.Errorf("o banco precisa estar em execução para gerar o backup")
	}
	directory := target.BackupDir(database.ID)
	fileName := backup.StartedAt.UTC().Format("20060102-150405") + "-" + backup.ID + "." + dump.Extension
	filePath := path.Join(directory, fileName)
	// Dumps hold the whole database: private to the user running them,
	// whatever the umask of the server.
	if err := s.store.setBackupFile(ctx, backup.ID, filePath); err != nil {
		return "", 0, err
	}
	success := false
	defer func() {
		if !success {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_, _ = ex.Run(cleanupCtx, shell.Join("rm", "-f", path.Join(directory, fileName), path.Join(directory, fileName)+".gz"))
		}
	}()
	command := fmt.Sprintf("umask 077 && mkdir -p %s && chmod 700 %s && %s > %s", shell.Quote(directory),
		shell.Quote(directory), docker.Exec(container, dump.Script), shell.Quote(filePath))
	if _, err := ex.Run(ctx, command); err != nil {
		_, _ = ex.Run(ctx, shell.Join("rm", "-f", filePath))
		return "", 0, i18n.Errorf("o dump falhou: %w", err)
	}
	if dump.Compress {
		if _, err := ex.Run(ctx, shell.Join("gzip", "--force", filePath)); err != nil {
			return "", 0, i18n.Errorf("compactar o dump: %w", err)
		}
		filePath += ".gz"
	}
	if err := s.store.setBackupFile(ctx, backup.ID, filePath); err != nil {
		return "", 0, err
	}
	sizeOutput, err := ex.Run(ctx, "wc -c < "+shell.Quote(filePath))
	if err != nil {
		return "", 0, err
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(sizeOutput), 10, 64)
	if size == 0 {
		return "", 0, i18n.Errorf("o dump gerou um arquivo vazio")
	}
	success = true
	return filePath, size, nil
}

func firstRunning(containers []docker.Container) string {
	for _, container := range containers {
		if container.State == "running" {
			return container.ID
		}
	}
	return ""
}

// applyRetention deletes the oldest successful backups beyond the policy.
func (s *Service) applyRetention(ctx context.Context, database Database) {
	backups, err := s.store.listBackups(ctx, database.ID)
	if err != nil {
		return
	}
	kept := 0
	for _, backup := range backups {
		if backup.Status != BackupSucceeded {
			continue
		}
		kept++
		if kept > database.Config.Backup.Retention {
			_ = s.deleteBackup(ctx, backup.ID)
		}
	}
}

// DeleteBackup removes the execution record and its file.
func (s *Service) DeleteBackup(ctx context.Context, backupID string) error {
	b, err := s.store.getBackup(ctx, backupID)
	if err != nil {
		return err
	}
	release, err := s.acquire(b.DatabaseID)
	if err != nil {
		return err
	}
	defer release()
	return s.deleteBackup(ctx, backupID)
}
func (s *Service) deleteBackup(ctx context.Context, backupID string) error {
	backup, ex, err := s.reachBackup(ctx, backupID)
	if err != nil {
		return err
	}
	var running int
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM database_operations WHERE backup_id=? AND status='running'`, backupID).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return apperr.Conflict("Aguarde a operação do banco terminar.")
	}
	if backup.Status == BackupRunning {
		return apperr.Conflict("Aguarde o backup terminar antes de removê-lo.")
	}
	if backup.FilePath != "" {
		if _, err := ex.Run(ctx, shell.Join("rm", "-f", backup.FilePath)); err != nil {
			return unavailable(err)
		}
	}
	if err := s.removeRemote(ctx, backup, ex); err != nil {
		return err
	}
	return s.store.deleteBackup(ctx, backupID)
}

// OpenBackup streams a backup file; the caller closes the reader.
func (s *Service) OpenBackup(ctx context.Context, backupID string) (io.ReadCloser, BackupExecution, error) {
	backup, ex, err := s.reachBackup(ctx, backupID)
	if err != nil {
		return nil, BackupExecution{}, err
	}
	if backup.Status != BackupSucceeded || backup.FilePath == "" {
		return nil, BackupExecution{}, apperr.Conflict("Este backup não possui arquivo disponível.")
	}
	reader, err := ex.ReadFile(ctx, backup.FilePath)
	if err != nil {
		return nil, BackupExecution{}, apperr.Unavailable("O arquivo de backup não pôde ser lido: %s", err)
	}
	return reader, backup, nil
}

func (s *Service) reachBackup(ctx context.Context, backupID string) (BackupExecution, executor.Executor, error) {
	backup, err := s.store.getBackup(ctx, backupID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return BackupExecution{}, nil, apperr.NotFound("Backup")
	}
	if err != nil {
		return BackupExecution{}, nil, err
	}
	database, err := s.Get(ctx, backup.DatabaseID)
	if err != nil {
		return BackupExecution{}, nil, err
	}
	ex, _, err := s.connector.Executor(ctx, database.ServerID)
	return backup, ex, err
}
