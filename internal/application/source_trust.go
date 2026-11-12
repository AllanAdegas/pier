package application

import (
	"context"
	"path"

	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// ResetGitHostKey forgets this application's pinned Git hosts. Only call after
// the operator has checked the replacement fingerprint through a trusted channel.
func (s *Service) ResetGitHostKey(ctx context.Context, applicationID string) error {
	app, err := s.Get(ctx, applicationID)
	if err != nil {
		return err
	}
	ex, target, err := s.connector.Executor(ctx, app.ServerID)
	if err != nil {
		return err
	}
	// Serialize resets with clones so an active clone cannot repopulate the pin.
	release, err := acquireBuild(ctx, app.ServerID, nil)
	if err != nil {
		return err
	}
	defer release()
	knownHosts := path.Join(target.ResourceDir("application", app.ID), "git", "known_hosts")
	if _, err := ex.Run(ctx, shell.Join("rm", "-f", knownHosts)); err != nil {
		return i18n.Errorf("não foi possível redefinir a confiança SSH do host Git: %w", err)
	}
	return nil
}
