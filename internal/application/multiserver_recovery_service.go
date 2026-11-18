package application

import (
	"context"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/server"
)

// ResumeRollout queues the deployment that finishes an interrupted
// multi-server rollout. Asking again while it is queued or running returns
// the same deployment.
func (s *Service) ResumeRollout(ctx context.Context, id string) (Deployment, error) {
	return s.queueRolloutAction(ctx, id, TriggerRolloutResume, false)
}

// RollBackRollout queues the deployment that puts the previous version back
// on the servers an interrupted rollout changed.
func (s *Service) RollBackRollout(ctx context.Context, id string) (Deployment, error) {
	return s.queueRolloutAction(ctx, id, TriggerRolloutRollback, false)
}

// RecoverRollouts applies the startup policy to rollouts a restart
// interrupted, once per rollout: one cut while releasing is resumed (the
// release command already ran on the first server, so going back could put
// old code on a migrated database, and the image is already on every
// server); one cut while compensating keeps compensating. A frozen
// environment or a server in maintenance refuses it, and the rollout waits
// for the operator, shown as interrupted. Call it after the dispatcher starts.
func (s *Service) RecoverRollouts(ctx context.Context) error {
	if s.multi == nil {
		return nil
	}
	ids, err := s.multi.journal.interruptedApplications(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		journal, found, err := s.multi.journal.load(ctx, id)
		if err != nil {
			return err
		}
		if !found || journal.AutomaticRecoveries > 0 {
			continue
		}
		trigger := TriggerRolloutResume
		if journal.InterruptedIn == RolloutCompensating || journal.Image == "" {
			trigger = TriggerRolloutRollback
		}
		if _, err := s.queueRolloutAction(ctx, id, trigger, true); err != nil {
			message := i18n.Instance().Sprintf("A ação automática foi recusada: %s", i18n.Localize(err, i18n.Instance()))
			if noteErr := s.multi.journal.noteAutomatic(ctx, id, message); noteErr != nil {
				return noteErr
			}
		}
	}
	return nil
}

func (s *Service) queueRolloutAction(ctx context.Context, id string, trigger Trigger, automatic bool) (Deployment, error) {
	if s.multi == nil {
		return Deployment{}, apperr.Conflict("Esta aplicação não tem rollout interrompido.")
	}
	app, err := s.Get(ctx, id)
	if err != nil {
		return Deployment{}, err
	}
	s.multi.actionMu.Lock()
	defer s.multi.actionMu.Unlock()
	journal, found, err := s.multi.journal.load(ctx, id)
	if err != nil {
		return Deployment{}, err
	}
	if !found || journal.State != RolloutInterrupted {
		return Deployment{}, apperr.Conflict("Esta aplicação não tem rollout interrompido.")
	}
	if pending, ok, err := s.pendingRolloutAction(ctx, journal, trigger); ok || err != nil {
		return pending, err
	}
	if trigger == TriggerRolloutResume {
		if err := safeResume(app, journal); err != nil {
			return Deployment{}, err
		}
	}
	if trigger == TriggerRolloutResume && journal.Image == "" {
		return Deployment{}, apperr.Conflict("O rollout parou antes de fixar a imagem; só é possível revertê-lo.")
	}
	if err := s.checkDeploy(ctx, app); err != nil {
		return Deployment{}, err
	}
	for _, target := range journal.Servers {
		if err := server.CheckConnectorDeployment(ctx, s.connector, target.ServerID); err != nil {
			return Deployment{}, err
		}
	}
	deployment := Deployment{ID: newDeploymentID(), ApplicationID: id, Trigger: trigger}
	if trigger == TriggerRolloutResume {
		deployment.RollbackImage = journal.Image
		if origin, err := s.store.GetDeployment(ctx, journal.DeploymentID); err == nil {
			deployment.CommitSHA, deployment.CommitMessage = origin.CommitSHA, origin.CommitMessage
		}
	}
	// Claimed before it is queued: the worker acts only on the claimed action.
	if err := s.multi.journal.claim(ctx, id, deployment.ID, automatic); err != nil {
		return Deployment{}, err
	}
	return s.enqueue(ctx, deployment)
}

// pendingRolloutAction returns the action already queued or running for the
// rollout: the same one again, or a conflict when it is the other action.
func (s *Service) pendingRolloutAction(ctx context.Context, journal RolloutJournal, trigger Trigger) (Deployment, bool, error) {
	if journal.ActionDeploymentID == "" {
		return Deployment{}, false, nil
	}
	action, err := s.store.GetDeployment(ctx, journal.ActionDeploymentID)
	if err != nil || action.Status.Terminal() {
		return Deployment{}, false, nil
	}
	if action.Trigger != trigger {
		return Deployment{}, true, apperr.Conflict("Já há uma ação em andamento para este rollout. Aguarde a conclusão.")
	}
	return action, true, nil
}
