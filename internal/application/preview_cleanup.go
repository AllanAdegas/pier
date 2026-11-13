package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
	"pier/internal/server"
)

const previewWaitTick = 20 * time.Millisecond

// previewProject is the compose project of a preview, apart from the application's.
func previewProject(target server.Server, previewID string) compose.Project {
	return compose.Project{Name: previewID, Dir: target.ResourceDir(docker.TypeApplication, previewID)}
}

// removePreviewFromServer erases what a preview created on the server:
// containers, volumes, images and files, all selected by the preview's own
// identity. The guard refuses an identity that could be the application's.
func removePreviewFromServer(ctx context.Context, connector Connector, app Application, preview Preview) error {
	if preview.ID == "" || preview.ID == app.ID {
		return i18n.Errorf("identidade de preview inválida")
	}
	ex, target, err := connector.Executor(ctx, app.ServerID)
	if err != nil {
		return err
	}
	containers, err := docker.ListByResource(ctx, ex, preview.ID)
	if err != nil {
		return err
	}
	if err := docker.Remove(ctx, ex, containers); err != nil {
		return err
	}
	server.LogEdgeSync(ctx, ex, target)
	removePreviewVolumes(ctx, ex, preview.ID)
	removeImages(ctx, ex, preview.ID)
	return previewProject(target, preview.ID).RemoveDir(ctx, ex)
}

// removePreviewVolumes deletes the named volumes of a preview, "<id>_<name>".
func removePreviewVolumes(ctx context.Context, ex executor.Executor, previewID string) {
	output, err := ex.Run(ctx, shell.Join("docker", "volume", "ls", "--quiet", "--filter", "name="+previewID+"_"))
	if err != nil {
		return
	}
	volumes := []string{}
	for _, name := range strings.Fields(output) {
		if strings.HasPrefix(name, previewID+"_") {
			volumes = append(volumes, name)
		}
	}
	if len(volumes) > 0 {
		_, _ = ex.Run(ctx, shell.Join(append([]string{"docker", "volume", "rm"}, volumes...)...))
	}
}

// removePreviewsFromServer erases every preview of an application being
// deleted, with its preview-only variables. Rows go with the application.
func (s *Service) removePreviewsFromServer(ctx context.Context, app Application) error {
	previews, err := s.store.previews(ctx, app.ID)
	if err != nil {
		return err
	}
	var failures error
	for _, preview := range previews {
		failures = errors.Join(failures, removePreviewFromServer(ctx, s.connector, app, preview))
	}
	if err := s.env.DeleteAll(ctx, envvar.Owner{Type: PreviewEnvOwner, ID: app.ID}); err != nil {
		return errors.Join(failures, err)
	}
	return failures
}

// waitForPreviewDeployments polls until no deployment of the preview runs.
func waitForPreviewDeployments(ctx context.Context, store *Store, applicationID, ref string) error {
	waitCtx, cancel := context.WithTimeout(ctx, deploymentStopTimeout)
	defer cancel()
	ticker := time.NewTicker(previewWaitTick)
	defer ticker.Stop()
	for {
		active, err := store.activePreviewDeployments(waitCtx, applicationID, ref)
		if err == nil && len(active) == 0 {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-ticker.C:
		}
	}
}
