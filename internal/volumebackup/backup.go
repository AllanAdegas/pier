package volumebackup

import (
	"context"
	"errors"
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
)

const cleanupTimeout = time.Minute

// backupDirectory keeps volume archives apart from database dumps.
func backupDirectory(target interface{ BackupDir(string) string }, resourceID string) string {
	return target.BackupDir("volumes-" + resourceID)
}

// Start validates the request, records the execution and runs the archive in
// the background, returning immediately.
func (s *Service) Start(ctx context.Context, kind, id, trigger string) (Backup, error) {
	resource, err := s.resolve(ctx, kind, id)
	if err != nil {
		return Backup{}, err
	}
	release, err := s.admit(ctx, kind, id)
	if err != nil {
		return Backup{}, err
	}
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	policy, err := s.store.policy(ctx, kind, id)
	if err != nil {
		return Backup{}, err
	}
	ex, target, err := s.reach(ctx, resource.ServerID)
	if err != nil {
		return Backup{}, err
	}
	mounts, _, err := discoverMounts(ctx, ex, id, target.DataDir, policy)
	if err != nil {
		return Backup{}, apperr.Unavailable("Não foi possível listar os volumes: %s", err)
	}
	chosen := included(mounts)
	if len(chosen) == 0 {
		return Backup{}, apperr.Conflict("Nenhum volume para salvar. Faça um deploy ou inclua os diretórios externos.")
	}
	backup := Backup{ID: idgen.New(), ResourceType: kind, ResourceID: id, serverID: resource.ServerID,
		Trigger: trigger, Status: StatusRunning, StartedAt: time.Now(), Mounts: chosen}
	if err := s.store.insertBackup(ctx, backup); err != nil {
		return Backup{}, err
	}
	if err := s.store.setMounts(ctx, backup.ID, chosen); err != nil {
		return Backup{}, err
	}
	started = true
	go func() { defer release(); s.runBackup(resource, policy, backup) }()
	return backup, nil
}

func (s *Service) runBackup(resource Resource, policy Policy, backup Backup) {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	language := i18n.Instance()
	filePath, size, err := s.archive(ctx, resource, policy, backup)
	if err != nil {
		reason := i18n.Localize(err, language)
		_ = s.store.finishBackup(ctx, backup.ID, StatusFailed, 0, reason)
		s.publish(notify.BackupFailed, resource, language.Sprintf("Backup de volumes falhou: %s", resource.Name), reason)
		return
	}
	message := ""
	if policy.S3 {
		if err := s.upload(ctx, resource, backup.ID, filePath); err != nil {
			message = language.Sprintf("Backup local concluído; a cópia S3 falhou: %s", i18n.Localize(err, language))
			s.publish(notify.BackupFailed, resource, language.Sprintf("Backup de volumes falhou: %s", resource.Name), message)
		}
	}
	_ = s.store.finishBackup(ctx, backup.ID, StatusSucceeded, size, message)
	s.publish(notify.BackupSucceeded, resource, language.Sprintf("Backup de volumes concluído: %s", resource.Name),
		language.Sprintf("Arquivo %s (%d bytes).", path.Base(filePath), size))
	s.applyRetention(ctx, resource, policy)
}

func (s *Service) publish(kind notify.EventType, resource Resource, title, message string) {
	s.events.Publish(notify.Event{Type: kind, Resource: resource.Name, ResourceType: resource.Kind,
		Title: title, Message: message, OccurredAt: time.Now()})
}

// archive writes the tar.gz on the server and returns its path and size.
func (s *Service) archive(ctx context.Context, resource Resource, policy Policy, backup Backup) (resultPath string, resultSize int64, resultErr error) {
	ex, target, err := s.reach(ctx, resource.ServerID)
	if err != nil {
		return "", 0, err
	}
	directory := backupDirectory(target, resource.ID)
	file := backup.StartedAt.UTC().Format("20060102-150405") + "-" + backup.ID + ".tar.gz"
	filePath := path.Join(directory, file)
	helper := helperBackupPrefix + backup.ID
	if err := s.store.setFile(ctx, backup.ID, filePath); err != nil {
		return "", 0, err
	}
	success := false
	defer func() {
		if !success {
			cleanup(ex, helper, filePath)
		}
	}()
	prepare := "umask 077 && mkdir -p " + shell.Quote(directory) + " && chmod 700 " + shell.Quote(directory)
	if _, err := ex.Run(ctx, prepare); err != nil {
		return "", 0, err
	}
	if policy.Quiesce {
		resume, err := s.quiesce(ctx, ex, resource.ID, backup.ID)
		if err != nil {
			return "", 0, err
		}
		defer func() { resultErr = errors.Join(resultErr, resume()) }()
	}
	if _, err := ex.Run(ctx, backupCommand(helper, directory, file, backup.Mounts)); err != nil {
		return "", 0, i18n.Errorf("o arquivamento dos volumes falhou: %w", err)
	}
	size, err := verifyArchive(ctx, ex, filePath)
	if err != nil {
		return "", 0, err
	}
	success = true
	return filePath, size, nil
}

func verifyArchive(ctx context.Context, ex executor.Executor, filePath string) (int64, error) {
	if _, err := ex.Run(ctx, shell.Join("gzip", "-t", filePath)); err != nil {
		return 0, i18n.Errorf("o arquivo gerado está corrompido: %w", err)
	}
	output, err := ex.Run(ctx, "wc -c < "+shell.Quote(filePath))
	if err != nil {
		return 0, err
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if size == 0 {
		return 0, i18n.Errorf("o arquivamento gerou um arquivo vazio")
	}
	return size, nil
}

func cleanup(ex executor.Executor, helper, filePath string) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	_, _ = ex.Run(ctx, shell.Join("docker", "rm", "-fv", helper))
	_, _ = ex.Run(ctx, shell.Join("rm", "-f", filePath))
}

// quiesce stops the running containers of the resource and returns the
// function that starts them again. The stopped IDs are persisted first, so a
// restart of Pier in the middle can still bring the resource back.
func (s *Service) quiesce(ctx context.Context, ex executor.Executor, resourceID, backupID string) (func() error, error) {
	containers, err := docker.ListByResource(ctx, ex, resourceID)
	if err != nil {
		return nil, err
	}
	running := runningOf(containers)
	if err := s.store.setBackupStopped(ctx, backupID, ids(running)); err != nil {
		return nil, err
	}
	if err := docker.Stop(ctx, ex, running); err != nil {
		resumeErr := resumeContainers(ex, running)
		return nil, errors.Join(i18n.Errorf("não foi possível parar a aplicação: %w", err), resumeErr)
	}
	return func() error { return resumeContainers(ex, running) }, nil
}

func resumeContainers(ex executor.Executor, containers []docker.Container) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := docker.Start(ctx, ex, containers); err != nil {
		return i18n.Errorf("Não foi possível reiniciar o recurso após a operação: %w", err)
	}
	return nil
}

func runningOf(containers []docker.Container) []docker.Container {
	running := []docker.Container{}
	for _, container := range containers {
		if container.State == "running" {
			running = append(running, container)
		}
	}
	return running
}

func ids(containers []docker.Container) []string {
	list := make([]string, len(containers))
	for i, container := range containers {
		list[i] = container.ID
	}
	return list
}

// upload copies the archive to the instance S3 destination.
func (s *Service) upload(ctx context.Context, resource Resource, backupID, filePath string) error {
	ex, _, err := s.reach(ctx, resource.ServerID)
	if err != nil {
		return err
	}
	uri, err := s.offsite.Upload(ctx, ex, "volumes/"+resource.ID, filePath)
	if err != nil {
		return err
	}
	return s.store.setRemote(ctx, backupID, uri)
}

// applyRetention deletes the oldest successful archives beyond the policy.
func (s *Service) applyRetention(ctx context.Context, resource Resource, policy Policy) {
	backups, err := s.store.listBackups(ctx, resource.Kind, resource.ID)
	if err != nil {
		return
	}
	kept := 0
	for _, backup := range backups {
		if backup.Status != StatusSucceeded {
			continue
		}
		if kept++; kept > policy.Retention {
			_ = s.removeBackup(ctx, backup)
		}
	}
}

// Delete removes an archive: file, S3 object and record.
func (s *Service) Delete(ctx context.Context, backupID string) error {
	backup, err := s.store.backup(ctx, backupID)
	if err != nil {
		return notFound(err)
	}
	release, err := s.acquire(backup.ResourceType, backup.ResourceID)
	if err != nil {
		return err
	}
	defer release()
	return s.removeBackup(ctx, backup)
}

func (s *Service) removeBackup(ctx context.Context, backup Backup) error {
	if backup.Status == StatusRunning {
		return apperr.Conflict("Aguarde o backup terminar antes de removê-lo.")
	}
	running, err := s.store.busy(ctx, backup.ResourceType, backup.ResourceID)
	if err != nil {
		return err
	}
	if running > 0 {
		return apperr.Conflict("Aguarde a operação de volumes terminar.")
	}
	ex, _, err := s.reach(ctx, backup.serverID)
	if err != nil {
		return err
	}
	if backup.FilePath != "" {
		if _, err := ex.Run(ctx, shell.Join("rm", "-f", backup.FilePath)); err != nil {
			return apperr.Unavailable("Falha ao operar o servidor: %s", err)
		}
		if err := s.offsite.Remove(ctx, ex, path.Dir(backup.FilePath), backup.remoteKey); err != nil {
			return err
		}
	}
	return s.store.deleteBackup(ctx, backup.ID)
}

// Open streams an archive; the caller closes the reader.
func (s *Service) Open(ctx context.Context, backupID string) (io.ReadCloser, Backup, error) {
	backup, err := s.store.backup(ctx, backupID)
	if err != nil {
		return nil, Backup{}, notFound(err)
	}
	if backup.Status != StatusSucceeded || backup.FilePath == "" {
		return nil, Backup{}, apperr.Conflict("Este backup não possui arquivo disponível.")
	}
	ex, _, err := s.reach(ctx, backup.serverID)
	if err != nil {
		return nil, Backup{}, err
	}
	reader, err := ex.ReadFile(ctx, backup.FilePath)
	if err != nil {
		return nil, Backup{}, apperr.Unavailable("O arquivo de backup não pôde ser lido: %s", err)
	}
	return reader, backup, nil
}
