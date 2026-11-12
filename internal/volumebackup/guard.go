package volumebackup

import (
	"context"

	"pier/internal/platform/apperr"
)

// acquire is the single-flight admission of a resource: one backup, restore
// or deletion at a time, in this process. Released when the work ends.
func (s *Service) acquire(kind, id string) (func(), error) {
	key := kind + ":" + id
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[key] {
		return nil, apperr.Conflict("Aguarde a operação de volumes terminar.")
	}
	s.active[key] = true
	return func() { s.mu.Lock(); delete(s.active, key); s.mu.Unlock() }, nil
}

// ensureIdle refuses while work persisted as running (it survives a restart
// in the table) or a deployment/operation of the resource is in progress.
func (s *Service) ensureIdle(ctx context.Context, kind, id string) error {
	running, err := s.store.busy(ctx, kind, id)
	if err != nil {
		return err
	}
	if running > 0 {
		return apperr.Conflict("Aguarde a operação de volumes terminar.")
	}
	return s.resources.EnsureIdle(ctx, kind, id)
}

// admit runs both checks and returns the release to call when the work ends.
func (s *Service) admit(ctx context.Context, kind, id string) (func(), error) {
	release, err := s.acquire(kind, id)
	if err != nil {
		return nil, err
	}
	if err := s.ensureIdle(ctx, kind, id); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
