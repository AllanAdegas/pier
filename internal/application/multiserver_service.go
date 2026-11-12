package application

import (
	"context"
	"errors"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/registry"
)

// SetMultiServer makes lifecycle operations and promotion placement-aware.
func (s *Service) SetMultiServer(m *MultiServer) { s.multi = m }

// placementTargets are the servers Delete must clean: the application's own
// server and, with a placement, every server that may still run it.
func (s *Service) placementTargets(ctx context.Context, app Application) ([]string, error) {
	if s.multi == nil {
		return []string{app.ServerID}, nil
	}
	return s.multi.targets(ctx, app)
}

// placementOperate runs start/stop/restart on every server of a placement;
// handled is false for single-server applications.
func (s *Service) placementOperate(ctx context.Context, id, status string, operation containerOperation) (bool, error) {
	if s.multi == nil {
		return false, nil
	}
	app, err := s.Get(ctx, id)
	if err != nil {
		return true, err
	}
	servers, err := s.store.PlacementDestinations(ctx, app)
	if err != nil || len(servers) == 0 {
		return err != nil, err
	}
	resume, err := s.dispatcher.quiesce(ctx, id)
	if err != nil {
		return true, err
	}
	defer resume()
	var failures error
	operated := 0
	for _, serverID := range servers {
		ex, target, err := s.connector.Executor(ctx, serverID)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		containers, err := docker.ListByResource(ctx, ex, id)
		if err == nil && len(containers) > 0 {
			err = operation(ctx, ex, containers)
			operated++
		}
		if err != nil {
			failures = errors.Join(failures, i18n.Errorf("%s: %w", target.Name, err))
		}
	}
	if operated == 0 && failures == nil {
		return true, apperr.Conflict("A aplicação ainda não possui containers. Faça um deploy primeiro.")
	}
	if failures != nil {
		return true, apperr.Unavailable("A operação falhou em parte dos servidores: %s", failures)
	}
	return true, s.store.SetStatus(ctx, id, status)
}

// promoteByDigest is the Promote hook for a source that runs a pushed
// digest: the target pulls that same digest wherever it runs, so source and
// target may be on different servers. Other sources promote as before.
func (s *Service) promoteByDigest(ctx context.Context, source, target Application) (Deployment, bool, error) {
	image := source.CurrentImage
	if s.multi == nil || s.multi.images == nil || !registry.Immutable(image) {
		return Deployment{}, false, nil
	}
	if exists, err := s.multi.images.ManifestExists(ctx, image); err == nil && !exists {
		return Deployment{}, true, apperr.Conflict("A imagem atual da origem não existe mais no registro. Faça um novo deploy da origem antes de promover.")
	}
	deployment := Deployment{
		ApplicationID: target.ID, Trigger: TriggerPromotion, RollbackImage: image, PromotedFrom: source.ID,
		Source: i18n.Instance().Sprintf("Promovido de %s (%s)", source.Name, shortImageID(digestOf(image))),
	}
	if origin, err := s.store.deploymentOfImage(ctx, source.ID, image); err == nil {
		deployment.CommitSHA, deployment.CommitMessage = origin.CommitSHA, origin.CommitMessage
		if origin.PromotedFrom != "" {
			deployment.PromotedFrom = origin.PromotedFrom
		}
	}
	created, err := s.enqueue(ctx, deployment)
	return created, true, err
}

func digestOf(image string) string {
	for index := len(image) - 1; index >= 0; index-- {
		if image[index] == '@' {
			return image[index+1:]
		}
	}
	return image
}
