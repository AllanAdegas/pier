package project

import (
	"context"
	"strings"

	"pier/internal/platform/apperr"
)

// CheckDeploy fails closed when the environment does not exist or is frozen.
func (s *Service) CheckDeploy(ctx context.Context, environmentID string) error {
	e, err := s.GetEnvironment(ctx, environmentID)
	if err != nil {
		return err
	}
	if e.Frozen {
		return apperr.Conflict("O ambiente %q está congelado. Descongele-o com uma justificativa antes de implantar.", e.Name)
	}
	return nil
}
func (s *Service) CheckDestroy(ctx context.Context, environmentID, confirmation string) error {
	e, err := s.GetEnvironment(ctx, environmentID)
	if err != nil {
		return err
	}
	if e.Protected && confirmation != e.Name {
		return apperr.Conflict("Digite o nome do ambiente %q para confirmar a remoção.", e.Name)
	}
	return nil
}
func (s *Service) SetProtection(ctx context.Context, id string, protected, frozen bool, reason ...string) (Environment, error) {
	validReason := len(reason) > 0 && len(strings.TrimSpace(reason[0])) >= 5 && len(reason[0]) <= 1000
	// Checking the old flag in this statement prevents a concurrent freeze
	// from being undone by an update that read the previous unfrozen state.
	result, err := s.db.ExecContext(ctx, `UPDATE environments SET protected=?,frozen=? WHERE id=? AND (frozen=0 OR ? OR ?)`, protected, frozen, id, frozen, validReason)
	if err != nil {
		return Environment{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Environment{}, err
	}
	if n == 0 {
		if _, err := s.GetEnvironment(ctx, id); err != nil {
			return Environment{}, err
		}
		return Environment{}, apperr.Invalid("Informe uma justificativa de 5 a 1000 caracteres para descongelar o ambiente.")
	}

	return s.GetEnvironment(ctx, id)
}
