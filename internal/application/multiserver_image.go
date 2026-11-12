package application

import (
	"context"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/registry"
)

// PlacementBuilder is the source/build hook. The executor belongs to the build
// server, independently of every runtime destination. It must return a local
// image reference and redact build secrets using the existing build logger.
type PlacementBuilder interface {
	BuildPlacement(context.Context, Application, executor.Executor) (string, error)
}

// BuildAndRollout composes the tested registry and release hooks. Wiring the
// production builder and staged runtime is deliberately required by the caller.
func (s *MultiServer) BuildAndRollout(ctx context.Context, id string, builder PlacementBuilder, connector Connector, images *registry.Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if builder == nil || connector == nil || images == nil || s.runtime == nil {
		return apperr.Unavailable("Integração de release multisservidor indisponível.")
	}
	app, err := s.apps.Get(ctx, id)
	if err != nil {
		return err
	}
	if s.guard == nil {
		return apperr.Unavailable("Proteção de ambiente indisponível.")
	}
	if err = s.guard.CheckDeploy(ctx, app.EnvironmentID); err != nil {
		return err
	}
	p, err := s.Settings(ctx, id)
	if err != nil {
		return err
	}
	ex, _, err := connector.Executor(ctx, p.BuildServerID)
	if err != nil {
		return err
	}
	if c, ok := connector.(interface {
		CheckDeployment(context.Context, string) error
	}); ok {
		for _, id := range append([]string{p.BuildServerID}, p.Destinations...) {
			if err = c.CheckDeployment(ctx, id); err != nil {
				return err
			}
		}
	}
	local, err := builder.BuildPlacement(ctx, app, ex)
	if err != nil {
		return err
	}
	image := local
	if !registry.Immutable(local) {
		image, err = images.Publish(ctx, p.RegistryID, ex, local, p.Repository)
	}
	if err != nil {
		return err
	}
	for _, target := range p.Destinations {
		remote, _, e := connector.Executor(ctx, target)
		if e != nil {
			return e
		}
		if e = images.Pull(ctx, p.RegistryID, remote, image); e != nil {
			return e
		}
	}
	return s.rollout(ctx, id, image)
}
