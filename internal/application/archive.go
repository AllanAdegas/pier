package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

const (
	archiveSuffix = ".tar.gz"
	// archiveChunkBytes bounds memory while the archive travels to the server.
	archiveChunkBytes = 8 << 20
	sourceArchive     = "source.tar.gz"
)

// archiveStore keeps uploaded source archives, already sanitized, in the
// Pier data directory until their deployment ends.
type archiveStore struct{ dir string }

func (a archiveStore) configured() bool { return a.dir != "" }

func (a archiveStore) path(deploymentID string) string {
	return filepath.Join(a.dir, deploymentID+archiveSuffix)
}

// save sanitizes the upload into a private file named after the deployment.
func (a archiveStore) save(deploymentID string, body io.Reader) (archiveSummary, error) {
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return archiveSummary{}, err
	}
	file, err := os.CreateTemp(a.dir, ".upload-*")
	if err != nil {
		return archiveSummary{}, err
	}
	defer os.Remove(file.Name())
	summary, err := sanitizeArchive(body, file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return archiveSummary{}, err
	}
	return summary, os.Rename(file.Name(), a.path(deploymentID))
}

func (a archiveStore) remove(deploymentID string) {
	if a.configured() {
		_ = os.Remove(a.path(deploymentID))
	}
}

// sweep deletes archives whose deployment will no longer run.
func (a archiveStore) sweep(pending func(deploymentID string) bool) {
	if !a.configured() {
		return
	}
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		id, isArchive := strings.CutSuffix(entry.Name(), archiveSuffix)
		if strings.HasPrefix(entry.Name(), ".upload-") || (isArchive && !pending(id)) {
			_ = os.Remove(filepath.Join(a.dir, entry.Name()))
		}
	}
}

// describeArchive is the deployment source shown in the history.
func describeArchive(summary archiveSummary) string {
	return i18n.Instance().Sprintf("Arquivo enviado: %d arquivos, %s", summary.Files, formatBytes(summary.Bytes))
}

func formatBytes(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(size)/(1<<10))
	}
	return fmt.Sprintf("%d B", size)
}

// unpackScript extracts the sanitized archive without network access.
const unpackScript = `set -e
mkdir -p /work/src
tar -xzf /work/` + sourceArchive + ` -C /work/src
rm -f /work/` + sourceArchive

// unpackArchive replaces the Git checkout: the uploaded archive is copied to
// the build directory of the server and extracted there.
func (j *job) unpackArchive(ctx context.Context, archivePath string) error {
	release, err := acquireBuild(ctx, j.app.ServerID, j.log)
	if err != nil {
		return err
	}
	defer release()
	file, err := os.Open(archivePath)
	if err != nil {
		return i18n.Errorf("o arquivo enviado não está mais disponível; envie novamente")
	}
	defer file.Close()
	buildDir := j.server.BuildDir(j.deployment.ID)
	if _, err := j.ex.Run(ctx, shell.Join("mkdir", "-p", buildDir)); err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}
	j.log.Info("Enviando o código recebido para o servidor.")
	if err := copyToServer(ctx, j.ex, file, path.Join(buildDir, sourceArchive)); err != nil {
		return i18n.Errorf("não foi possível enviar o código ao servidor: %w", err)
	}
	command := shell.Join("docker", "run", "--rm", "--network", "none", "--volume", buildDir+":/work",
		"--entrypoint", "sh", gitImage, "-c", unpackScript)
	if err := j.ex.Stream(ctx, command, j.log.Output); err != nil {
		return i18n.Errorf("não foi possível extrair o código enviado: %w", err)
	}
	return j.ensureInsideSource(ctx)
}

// copyToServer appends the content in bounded chunks through stdin, so a
// large archive never sits whole in memory or in a command line.
func copyToServer(ctx context.Context, ex executor.Executor, content io.Reader, target string) error {
	if _, err := ex.Run(ctx, "umask 077 && : > "+shell.Quote(target)); err != nil {
		return err
	}
	chunk := make([]byte, archiveChunkBytes)
	for {
		n, err := io.ReadFull(content, chunk)
		if n > 0 {
			if _, runErr := executor.RunInput(ctx, ex, "cat >> "+shell.Quote(target), chunk[:n]); runErr != nil {
				return runErr
			}
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (s *Service) archives() archiveStore {
	if s.dispatcher == nil || s.dispatcher.deployer == nil {
		return archiveStore{}
	}
	return s.dispatcher.deployer.archives
}

// DeployArchive queues a deployment that builds an uploaded gzip tar of the
// source directory instead of cloning the repository.
func (s *Service) DeployArchive(ctx context.Context, applicationID string, body io.Reader) (Deployment, error) {
	app, err := s.Get(ctx, applicationID)
	if err != nil {
		return Deployment{}, err
	}
	if err := s.checkDeploy(ctx, app); err != nil {
		return Deployment{}, err
	}
	if app.Config.Build.Pack == PackImage {
		return Deployment{}, apperr.Conflict("Aplicações de imagem Docker não recebem código. Mude o build pack para Dockerfile, Nixpacks ou site estático.")
	}
	archives := s.archives()
	if !archives.configured() {
		return Deployment{}, apperr.Unavailable("O envio de código não está configurado nesta instância.")
	}
	deployment := Deployment{ID: newDeploymentID(), ApplicationID: applicationID, Trigger: TriggerArchive}
	summary, err := archives.save(deployment.ID, body)
	if err != nil {
		return Deployment{}, err
	}
	deployment.Source = describeArchive(summary)
	queued, err := s.enqueue(ctx, deployment)
	if err != nil {
		archives.remove(deployment.ID)
	}
	return queued, err
}
