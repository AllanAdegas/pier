package volumebackup

import (
	"context"
	"path"

	"pier/internal/docker"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// Recover runs at startup. Work that was running when Pier stopped is marked
// failed, its helper containers and partial files are removed, and the
// containers Pier had stopped for it are started again.
func (s *Service) Recover(ctx context.Context) error {
	backups, err := s.store.runningBackups(ctx)
	if err != nil {
		return err
	}
	for _, backup := range backups {
		message := s.recoverBackup(ctx, backup)
		if err := s.store.finishBackup(ctx, backup.ID, StatusFailed, 0, message); err != nil {
			return err
		}
	}
	operations, err := s.store.runningOperations(ctx)
	if err != nil {
		return err
	}
	for _, op := range operations {
		message := s.recoverOperation(ctx, op)
		if err := s.store.finishOperation(ctx, op.ID, StatusFailed, message, ""); err != nil {
			return err
		}
	}
	return nil
}

func interrupted() string {
	return i18n.Instance().Text("Interrompido pela reinicialização do Pier.")
}

func (s *Service) recoverBackup(ctx context.Context, backup Backup) string {
	ex, _, err := s.reach(ctx, backup.serverID)
	if err != nil {
		return i18n.Instance().Sprintf("A limpeza da operação interrompida falhou: %s", err)
	}
	if backup.FilePath != "" {
		_, err = ex.Run(ctx, "rm -f "+shell.Quote(path.Dir(backup.FilePath)+"/.s3-")+"*")
		cleanup(ex, helperBackupPrefix+backup.ID, backup.FilePath)
	}
	return interruptedMessage(err, startStopped(ctx, ex, backup.stopped))
}

func (s *Service) recoverOperation(ctx context.Context, op runningOperation) string {
	backup, err := s.store.backup(ctx, op.BackupID)
	if err != nil {
		return i18n.Instance().Sprintf("A limpeza da operação interrompida falhou: %s", err)
	}
	ex, _, err := s.reach(ctx, backup.serverID)
	if err != nil {
		return i18n.Instance().Sprintf("A limpeza da operação interrompida falhou: %s", err)
	}
	_, _ = ex.Run(ctx, shell.Join("docker", "rm", "-fv", helperRestorePrefix+op.ID))
	return interruptedMessage(nil, startStopped(ctx, ex, op.Stopped))
}

func interruptedMessage(cleanupErr, startErr error) string {
	for _, err := range []error{cleanupErr, startErr} {
		if err != nil {
			return i18n.Instance().Sprintf("A limpeza da operação interrompida falhou: %s", err)
		}
	}
	return interrupted()
}

func startStopped(ctx context.Context, ex executor.Executor, containerIDs []string) error {
	containers := make([]docker.Container, len(containerIDs))
	for i, id := range containerIDs {
		containers[i] = docker.Container{ID: id}
	}
	return docker.Start(ctx, ex, containers)
}
