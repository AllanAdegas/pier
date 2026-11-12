package database

import (
	"context"
	"pier/internal/platform/apperr"
)

func (s *Service) acquire(id string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[id] {
		return nil, apperr.Conflict("Aguarde a operação do banco terminar.")
	}
	s.active[id] = true
	return func() { s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }, nil
}

func (s *Service) ensureIdle(ctx context.Context, id string) error {
	var count int
	err := s.store.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM backup_executions WHERE database_id=? AND status='running') + (SELECT COUNT(*) FROM database_operations WHERE database_id=? AND status='running')`, id, id).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return apperr.Conflict("Aguarde a operação do banco terminar.")
	}
	return nil
}
