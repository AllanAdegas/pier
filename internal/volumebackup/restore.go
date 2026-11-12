package volumebackup

import (
	"context"
	"path"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/shell"
)

const maxLogBytes = 64 * 1024

// StartRestore validates the request and restores the archive into the same
// volumes in the background. It requires the typed resource name, and refuses
// while a deploy or another operation of the resource runs.
func (s *Service) StartRestore(ctx context.Context, backupID string, input RestoreInput) (Operation, error) {
	backup, err := s.store.backup(ctx, backupID)
	if err != nil {
		return Operation{}, notFound(err)
	}
	if backup.Status != StatusSucceeded {
		return Operation{}, apperr.Conflict("Selecione um backup concluído para restaurar.")
	}
	resource, err := s.resolve(ctx, backup.ResourceType, backup.ResourceID)
	if err != nil {
		return Operation{}, err
	}
	if input.Confirmation != resource.Name {
		return Operation{}, apperr.Invalid("Digite o nome do recurso para confirmar a restauração.")
	}
	release, err := s.admit(ctx, backup.ResourceType, backup.ResourceID)
	if err != nil {
		return Operation{}, err
	}
	op := Operation{ID: idgen.New(), ResourceType: backup.ResourceType, ResourceID: backup.ResourceID,
		BackupID: backup.ID, Kind: "restore", Status: StatusRunning, StartedAt: time.Now()}
	if err := s.store.insertOperation(ctx, op); err != nil {
		release()
		return Operation{}, err
	}
	go func() { defer release(); s.runRestore(resource, backup, op) }()
	return op, nil
}

func (s *Service) runRestore(resource Resource, backup Backup, op Operation) {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	var logs strings.Builder
	log := func(line string) {
		if logs.Len() < maxLogBytes {
			logs.WriteString(line + "\n")
			_ = s.store.setOperationLogs(context.WithoutCancel(ctx), op.ID, logs.String())
		}
	}
	err := s.restore(ctx, resource, backup, op, log)
	status, message := StatusSucceeded, ""
	if err != nil {
		status, message = StatusFailed, i18n.Localize(err, i18n.Instance())
	}
	_ = s.store.finishOperation(context.WithoutCancel(ctx), op.ID, status, message, logs.String())
}

func (s *Service) restore(ctx context.Context, resource Resource, backup Backup, op Operation, log executor.LineSink) error {
	ex, target, err := s.reach(ctx, resource.ServerID)
	if err != nil {
		return err
	}
	if err := s.ensureLocal(ctx, ex, backup); err != nil {
		return err
	}
	policy, err := s.store.policy(ctx, resource.Kind, resource.ID)
	if err != nil {
		return err
	}
	policy.IncludeExternal = true
	current, containers, err := discoverMounts(ctx, ex, resource.ID, target.DataDir, policy)
	if err != nil {
		return err
	}
	targets, err := restoreTargets(backup.Mounts, current)
	if err != nil {
		return err
	}
	running := runningOf(containers)
	if err := s.store.setOperationStopped(ctx, op.ID, ids(running)); err != nil {
		return err
	}
	log(i18n.Instance().Text("Parando o recurso para restaurar os volumes."))
	defer func() {
		log(i18n.Instance().Text("Iniciando o recurso novamente."))
		resumeContainers(ex, running)
	}()
	if err := docker.Stop(ctx, ex, running); err != nil {
		return i18n.Errorf("não foi possível parar o recurso: %w", err)
	}
	return s.extractAll(ctx, ex, backup, op, targets, log)
}

func (s *Service) extractAll(ctx context.Context, ex executor.Executor, backup Backup, op Operation, targets []Mount, log executor.LineSink) error {
	helper := helperRestorePrefix + op.ID
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		_, _ = ex.Run(c, shell.Join("docker", "rm", "-fv", helper))
	}()
	for _, mount := range targets {
		log(i18n.Instance().Sprintf("Restaurando %s.", mount.Label))
		command := restoreCommand(helper, path.Dir(backup.FilePath), path.Base(backup.FilePath), mount)
		// Tool output may contain file names or data: phases only in the log.
		if _, err := ex.Run(ctx, command); err != nil {
			return i18n.Errorf("A restauração de %s falhou: %w", mount.Label, err)
		}
	}
	log(i18n.Instance().Text("Restauração concluída."))
	return nil
}

// restoreTargets matches the mounts stored in the archive with the ones the
// resource has now. A mount that no longer exists, or that moved, is an
// error: nothing is restored into a path the resource does not use.
func restoreTargets(archived, current []Mount) ([]Mount, error) {
	targets := []Mount{}
	for _, wanted := range archived {
		var found *Mount
		for i := range current {
			if current[i].Label == wanted.Label && current[i].Kind == wanted.Kind && current[i].Source == wanted.Source {
				found = &current[i]
			}
		}
		if found == nil || !found.Included {
			return nil, i18n.Errorf("o volume %s não é mais usado por este recurso; a restauração foi cancelada", wanted.Label)
		}
		targets = append(targets, *found)
	}
	return targets, nil
}

// ensureLocal makes the archive present on the server, fetching it from S3
// when the local copy is gone.
func (s *Service) ensureLocal(ctx context.Context, ex executor.Executor, backup Backup) error {
	if _, err := ex.Run(ctx, shell.Join("test", "-s", backup.FilePath)); err == nil {
		return nil
	}
	if backup.remoteKey == "" {
		return apperr.Conflict("O arquivo de backup não está disponível no servidor nem no S3.")
	}
	return s.offsite.Fetch(ctx, ex, backup.remoteKey, backup.FilePath)
}
