package application

import (
	"context"
	"strings"

	"pier/internal/platform/i18n"
	"pier/internal/server"
)

// Triggers of the actions on an interrupted multi-server rollout.
const (
	// TriggerRolloutResume finishes the rollout on the servers not on its image yet.
	TriggerRolloutResume Trigger = "rollout_resume"
	// TriggerRolloutRollback puts the previous version back on the servers it changed.
	TriggerRolloutRollback Trigger = "rollout_rollback"
)

func isRolloutAction(trigger Trigger) bool {
	return trigger == TriggerRolloutResume || trigger == TriggerRolloutRollback
}

// recoverRollout runs a resume or rollback deployment. It acts only when the
// rollout is still interrupted and this deployment is the action claimed for
// it, so a repeated or stale action changes nothing.
func (d *Deployer) recoverRollout(ctx context.Context, deployment Deployment, app Application, log *deployLog) error {
	if d.multi == nil {
		return i18n.Errorf("os destinos múltiplos não estão disponíveis")
	}
	journal, found, err := d.multi.journal.load(ctx, app.ID)
	if err != nil {
		return err
	}
	if !found || journal.State != RolloutInterrupted || journal.ActionDeploymentID != deployment.ID {
		log.Info("Não há rollout interrompido para esta ação; nenhum servidor foi alterado.")
		return nil
	}
	if err := d.multi.checkGuard(ctx, app); err != nil {
		return err
	}
	r := &rollout{d: d, app: app, deployment: deployment, log: log, image: journal.Image,
		plan:  placementPlan{servers: journal.serverIDs(), buildServer: app.ServerID},
		done:  map[string]bool{},
		known: map[string]string{},
	}
	for _, id := range r.plan.servers {
		if err := server.CheckConnectorDeployment(ctx, d.connector, id); err != nil {
			return err
		}
	}
	if deployment.Trigger == TriggerRolloutRollback {
		return r.rollBackInterrupted(ctx, journal)
	}
	return r.resume(ctx, journal)
}

// checkGuard applies the environment freeze to an action on a rollout.
func (s *MultiServer) checkGuard(ctx context.Context, app Application) error {
	if s.guard == nil {
		return nil
	}
	return s.guard.CheckDeploy(ctx, app.EnvironmentID)
}

// resume finishes an interrupted rollout. Servers already on the image stay
// as they are (and are compensated like any updated server if the resume
// fails); the others are released in the original order. The release command
// runs again only if the first server never finished.
func (r *rollout) resume(ctx context.Context, journal RolloutJournal) error {
	if err := safeResume(r.app, journal); err != nil {
		return err
	}
	if journal.Image == "" {
		return i18n.Errorf("o rollout interrompido não tem imagem fixada; use Reverter rollout")
	}
	r.log.Info("Retomando o rollout interrompido em %d servidor(es) com a imagem %s.", len(journal.Servers), journal.Image)
	pending := []string{}
	for _, step := range journal.Servers {
		if step.State == StepReleased {
			r.done[step.ServerID] = true
			r.released = append(r.released, releasedServer{id: step.ServerID, name: r.serverName(ctx, step.ServerID), previous: step.PreviousImage})
			r.log.Info("%s já roda a nova versão.", r.serverName(ctx, step.ServerID))
			continue
		}
		if step.State != StepPending {
			r.known[step.ServerID] = step.PreviousImage
		}
		pending = append(pending, step.ServerID)
	}
	if err := r.d.store.setDeploymentImage(ctx, r.deployment.ID, r.image); err != nil {
		return err
	}
	if err := r.distributeTo(ctx, pending); err != nil {
		return err
	}
	if err := r.journal().setState(ctx, r.app.ID, RolloutReleasing, ""); err != nil {
		return err
	}
	r.gate = func(ctx context.Context) error { return r.d.multi.checkGuard(ctx, r.app) }
	if err := r.releaseAll(ctx); err != nil {
		return err
	}
	return r.conclude(ctx)
}

// rollBackInterrupted puts the previous version back, newest first, on every
// server the rollout may have changed; servers never reached are untouched.
// A server that cannot be restored keeps the rollout interrupted.
func (r *rollout) rollBackInterrupted(ctx context.Context, journal RolloutJournal) error {
	r.log.Info("Revertendo o rollout interrompido para a versão anterior.")
	for _, step := range journal.Servers {
		switch step.State {
		case StepReleasing, StepReleased, StepRestoreFailed:
			r.released = append(r.released, releasedServer{id: step.ServerID, name: r.serverName(ctx, step.ServerID), previous: step.PreviousImage})
		}
	}
	if err := r.journal().setState(ctx, r.app.ID, RolloutCompensating, ""); err != nil {
		return err
	}
	failures, cause := r.restoreReleased(ctx)
	if len(failures) > 0 {
		err := i18n.Errorf("a reversão falhou em %s; o rollout continua interrompido: %w", strings.Join(failures, ", "), cause)
		if journalErr := r.journal().interrupt(context.WithoutCancel(ctx), r.app.ID, RolloutCompensating,
			r.log.mask(describeFailure(err))); journalErr != nil {
			r.log.Info("Não foi possível registrar o estado do rollout: %s", journalErr)
		}
		return err
	}
	r.setJournalState(ctx, RolloutRolledBack, nil)
	r.log.Info("Os servidores alterados voltaram para a versão anterior.")
	return nil
}

func (r *rollout) serverName(ctx context.Context, id string) string {
	if _, target, err := r.d.connector.Executor(context.WithoutCancel(ctx), id); err == nil {
		return target.Name
	}
	return id
}

// A crash can happen after a one-shot command but before its success is journaled.
// Never replay such a command automatically or on an explicit resume.
func safeResume(app Application, journal RolloutJournal) error {
	if len(journal.Servers) > 0 && journal.Servers[0].State != StepPending && journal.Servers[0].State != StepReleased &&
		(app.Config.Release.Command != "" || app.Config.Hooks.PreDeploy != "") {
		return i18n.Errorf("O primeiro servidor foi interrompido durante o release; verifique os comandos antes de reverter o rollout.")
	}
	return nil
}
