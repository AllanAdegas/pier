package server

import (
	"context"

	"pier/internal/platform/apperr"
)

// EnsureRemovable fails when Delete would refuse the server, so a caller can
// check before an irreversible step elsewhere (destroying the cloud machine).
func (s *Service) EnsureRemovable(ctx context.Context, id string) error {
	target, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if target.Kind == KindLocal {
		return apperr.Conflict("O servidor local não pode ser removido.")
	}
	resources, err := s.store.countResources(ctx, id)
	if err != nil {
		return err
	}
	if resources > 0 {
		return apperr.Conflict("O servidor ainda hospeda %d recurso(s). Remova-os antes.", resources)
	}
	return nil
}

// Probe runs a no-op command over the server's connection. A failure drops
// the cached connection, so the next probe dials again (a new machine
// accepts the key only once cloud-init has written it).
func (s *Service) Probe(ctx context.Context, id string) error {
	ex, _, err := s.connector.Executor(ctx, id)
	if err == nil {
		_, err = ex.Run(ctx, "true")
	}
	if err != nil {
		s.connector.Forget(id)
	}
	return err
}
