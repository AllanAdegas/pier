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
func (s *Service) RecoverBackups(ctx context.Context) error { return s.store.failRunningBackups(ctx) }

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
	go s.runBackup(database, *spec.Dump, backup)
	return backup, nil
}

func (s *Service) runBackup(database Database, dump dumpSpec, backup BackupExecution) {
	ctx, cancel := context.WithTimeout(context.Background(), backupTimeout)
	defer cancel()
	filePath, size, err := s.dump(ctx, database, dump, backup)
	if err != nil {
		_ = s.store.finishBackup(ctx, backup.ID, BackupFailed, 0, err.Error())
		s.events.Publish(notify.Event{
			Type: notify.BackupFailed, Resource: database.Name,
			Title: "Backup falhou: " + database.Name, Message: err.Error(),
		})
		return
	}
	_ = s.store.finishBackup(ctx, backup.ID, BackupSucceeded, size, "")
	s.events.Publish(notify.Event{
		Type: notify.BackupSucceeded, Resource: database.Name,
		Title:   "Backup concluído: " + database.Name,
		Message: fmt.Sprintf("Arquivo %s (%d bytes).", path.Base(filePath), size),
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
		return "", 0, errors.New("o banco precisa estar em execução para gerar o backup")
	}
	directory := target.BackupDir(database.ID)
	fileName := backup.StartedAt.UTC().Format("20060102-150405") + "-" + backup.ID + "." + dump.Extension
	filePath := path.Join(directory, fileName)
	command := fmt.Sprintf("mkdir -p %s && %s > %s", shell.Quote(directory),
		docker.Exec(container, dump.Script), shell.Quote(filePath))
	if _, err := ex.Run(ctx, command); err != nil {
		_, _ = ex.Run(ctx, shell.Join("rm", "-f", filePath))
		return "", 0, fmt.Errorf("o dump falhou: %w", err)
	}
	if dump.Compress {
		if _, err := ex.Run(ctx, shell.Join("gzip", "--force", filePath)); err != nil {
			return "", 0, fmt.Errorf("compactar o dump: %w", err)
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
		return "", 0, errors.New("o dump gerou um arquivo vazio")
	}
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
			_ = s.DeleteBackup(ctx, backup.ID)
		}
	}
}

// DeleteBackup removes the execution record and its file.
func (s *Service) DeleteBackup(ctx context.Context, backupID string) error {
	backup, ex, err := s.reachBackup(ctx, backupID)
	if err != nil {
		return err
	}
	if backup.Status == BackupRunning {
		return apperr.Conflict("Aguarde o backup terminar antes de removê-lo.")
	}
	if backup.FilePath != "" {
		if _, err := ex.Run(ctx, shell.Join("rm", "-f", backup.FilePath)); err != nil {
			return unavailable(err)
		}
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
