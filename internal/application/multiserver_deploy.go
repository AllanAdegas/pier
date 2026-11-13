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
	"pier/internal/server"
)

// compensationTimeout bounds putting the previous version back on the
// servers already updated; it runs even when the deployment was cancelled.
const compensationTimeout = 15 * time.Minute

// SetMultiServer turns on placements and registry credentials for
// deployments. Without it every deployment is single-server.
func (d *Deployer) SetMultiServer(m *MultiServer) {
	d.multi = m
	if m != nil {
		d.images = m.images
	}
}

// placementHook runs a deployment through its placement when the
// application has one; handled is false for single-server applications.
func (d *Deployer) placementHook(ctx context.Context, deployment Deployment, app Application, log *deployLog) (bool, error) {
	if isRolloutAction(deployment.Trigger) {
		return true, d.recoverRollout(ctx, deployment, app, log)
	}
	if d.multi == nil || deployment.Trigger == TriggerPreview {
		return false, nil
	}
	plan, ok, err := d.multi.plan(ctx, app)
	if err != nil {
		return true, err
	}
	if !ok {
		return false, nil
	}
	r := &rollout{d: d, app: app, deployment: deployment, plan: plan, log: log}
	return true, r.run(ctx)
}

// rollout is one deployment of an application to the servers of its
// placement: the image is produced once, pinned by digest, pulled on every
// server, then each server is released in turn by the regular release
// (proxy labels, DNS check, rolling update, readiness of every replica).
//
// Failure policy: a server whose release fails keeps its previous version
// (the regular release guarantees it); servers already updated are released
// again with the image they ran before, or emptied when they had none.
// Servers not reached yet are never touched.
type rollout struct {
	d          *Deployer
	app        Application
	deployment Deployment
	plan       placementPlan
	log        *deployLog
	image      string
	released   []releasedServer
	// done are servers already on the image (a resumed rollout skips them).
	done map[string]bool
	// known holds what a server ran before the rollout, from the journal; a
	// live inspection could see the new version left by a crash.
	known map[string]string
	// gate runs before each server of a resumed rollout (environment
	// freeze); a refusal interrupts the rollout instead of compensating.
	gate func(context.Context) error
}

// releasedServer is a server that runs the new version and what it ran before.
type releasedServer struct {
	id, name, previous string
	recovery           *releaseRecovery
}

func (r *rollout) run(ctx context.Context) error {
	journal, found, err := r.journal().load(ctx, r.app.ID)
	if err != nil {
		return err
	}
	if found && journal.State != RolloutCompleted && journal.State != RolloutFailed && journal.State != RolloutRolledBack {
		return i18n.Errorf("Esta aplicação tem rollout interrompido. Retome ou reverta antes de outro deploy.")
	}
	r.log.repository(r.app.Config.Source.Repository)
	r.log.Info("Iniciando o deploy de %q em %d servidor(es), com gatilho %s.", r.app.Name, len(r.plan.servers), r.deployment.Trigger)
	if err := r.journal().begin(ctx, r.app.ID, r.deployment.ID, r.plan.servers); err != nil {
		return i18n.Errorf("registrar o rollout: %w", err)
	}
	if err := r.prepare(ctx); err != nil {
		r.setJournalState(ctx, RolloutFailed, err)
		return err
	}
	if err := r.releaseAll(ctx); err != nil {
		return err
	}
	if err := r.conclude(ctx); err != nil {
		return err
	}
	r.decommission(context.WithoutCancel(ctx))
	return nil
}

// prepare checks every server, produces and pins the image and pulls it
// everywhere. No server changes before it succeeds.
func (r *rollout) prepare(ctx context.Context) error {
	for _, id := range append([]string{r.plan.buildServer}, r.plan.servers...) {
		if err := server.CheckConnectorDeployment(ctx, r.d.connector, id); err != nil {
			return err
		}
	}
	if err := r.produce(ctx); err != nil {
		return err
	}
	if err := r.d.store.setDeploymentImage(ctx, r.deployment.ID, r.image); err != nil {
		return err
	}
	if err := r.journal().setImage(ctx, r.app.ID, r.image); err != nil {
		return err
	}
	if err := r.distribute(ctx); err != nil {
		return err
	}
	return r.journal().setState(ctx, r.app.ID, RolloutReleasing, "")
}

// releaseAll releases every server not done yet, in order, and compensates
// on the first failure.
func (r *rollout) releaseAll(ctx context.Context) error {
	for index, id := range r.plan.servers {
		if r.done[id] {
			continue
		}
		if r.gate != nil {
			if err := r.gate(ctx); err != nil {
				message := r.log.mask(describeFailure(err))
				if journalErr := r.journal().interrupt(context.WithoutCancel(ctx), r.app.ID, RolloutReleasing, message); journalErr != nil {
					r.log.Info("Não foi possível registrar o estado do rollout: %s", journalErr)
				}
				return err
			}
		}
		if err := r.releaseOn(ctx, id, index); err != nil {
			return r.compensate(ctx, id, err)
		}
	}
	return nil
}

// conclude records the image as current once every server runs it.
func (r *rollout) conclude(ctx context.Context) error {
	if err := r.d.store.setCurrentImage(ctx, r.app.ID, r.image); err != nil {
		return err
	}
	for _, released := range r.released {
		if released.recovery != nil && released.recovery.touched {
			j, err := r.job(ctx, released.id)
			if err == nil {
				err = docker.Remove(ctx, j.ex, released.recovery.previous)
			}
			if err != nil {
				r.log.Info("Não foi possível remover containers antigos de %s: %s", released.name, err)
			}
		}
	}
	r.setJournalState(ctx, RolloutCompleted, nil)
	return nil
}

func (r *rollout) journal() rolloutJournal { return r.d.multi.journal }

// setJournalState records the outcome of the rollout; like record, a
// failure to write only changes what the panel shows.
func (r *rollout) setJournalState(ctx context.Context, state string, cause error) {
	message := ""
	if cause != nil {
		message = r.log.mask(describeFailure(cause))
	}
	if err := r.journal().setState(context.WithoutCancel(ctx), r.app.ID, state, message); err != nil {
		r.log.Info("Não foi possível registrar o estado do rollout: %s", err)
	}
}

// setStep records the progress on one server after it changed.
func (r *rollout) setStep(ctx context.Context, serverID, state string) {
	if err := r.journal().step(context.WithoutCancel(ctx), r.app.ID, serverID, state, nil); err != nil {
		r.log.Info("Não foi possível registrar o estado do rollout: %s", err)
	}
}

// job prepares the working state of this deployment on one server, with
// the variables resolved for that server (references to databases on the
// same server use internal addresses, others the public ones).
func (r *rollout) job(ctx context.Context, serverID string) (*job, error) {
	ex, target, err := r.d.connector.Executor(ctx, serverID)
	if err != nil {
		return nil, i18n.Errorf("conectar ao servidor: %w", err)
	}
	variables, err := r.d.env.ListForDeploy(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: r.app.ID}, r.app.EnvironmentID, serverID)
	if err != nil {
		return nil, i18n.Errorf("carregar variáveis de ambiente: %w", err)
	}
	for _, variable := range variables {
		r.log.redact(variable.Value)
	}
	r.log.redact(target.ProxyToken)
	app := r.app
	app.ServerID = serverID
	return &job{app: app, deployment: r.deployment, server: target, ex: ex, log: r.log, env: variables,
		domains: r.d.domains, gitProvider: r.d.gitProvider, registries: r.d.images}, nil
}

// releaseOn releases the pinned image on one server. Only the first server
// runs the release command and the pre-deploy hook: they usually migrate a
// shared database, which must happen once.
func (r *rollout) releaseOn(ctx context.Context, serverID string, index int) error {
	j, err := r.job(ctx, serverID)
	if err != nil {
		r.record(ctx, serverID, PlacementFailed, "", "", err)
		return err
	}
	r.log.Info("Servidor %s: atualização %d de %d.", j.server.Name, index+1, len(r.plan.servers))
	if err := docker.EnsureNetwork(ctx, j.ex); err != nil {
		r.record(ctx, serverID, PlacementFailed, "", "", err)
		return err
	}
	previous, known := r.known[serverID]
	if !known {
		if previous, err = previousImage(ctx, j); err != nil {
			r.record(ctx, serverID, PlacementFailed, "", "", err)
			return err
		}
	}
	// Written before the server changes: a crash from here on is recovered
	// with this previous version.
	if err := r.journal().step(ctx, r.app.ID, serverID, StepReleasing, &previous); err != nil {
		r.record(ctx, serverID, PlacementFailed, "", "", err)
		return i18n.Errorf("registrar o rollout: %w", err)
	}
	r.record(ctx, serverID, PlacementDeploying, r.image, previous, nil)
	if index > 0 {
		withoutOneShotSteps(&j.app)
	}
	recovery := &releaseRecovery{retain: true}
	err = j.releaseRecoverable(ctx, r.image, environmentOf(j.env), recovery)
	r.deployment.Hints, r.deployment.RetiredLogs = j.deployment.Hints, j.deployment.RetiredLogs
	if err != nil {
		if recovery.touched || runsDeployment(context.WithoutCancel(ctx), j) {
			r.released = append(r.released, releasedServer{id: serverID, name: j.server.Name, previous: previous, recovery: recovery})
		} else {
			r.setStep(ctx, serverID, StepFailed)
		}
		r.record(ctx, serverID, PlacementFailed, previous, "", err)
		return err
	}
	r.released = append(r.released, releasedServer{id: serverID, name: j.server.Name, previous: previous, recovery: recovery})
	r.setStep(ctx, serverID, StepReleased)
	r.record(ctx, serverID, PlacementHealthy, r.image, previous, nil)
	r.d.pruneImages(context.WithoutCancel(ctx), j)
	return nil
}

// compensate puts the previous version back on every server already
// updated, newest first, and explains the outcome.
func (r *rollout) compensate(ctx context.Context, failedID string, cause error) error {
	failedName := failedID
	if _, target, err := r.d.connector.Executor(context.WithoutCancel(ctx), failedID); err == nil {
		failedName = target.Name
	}
	var outcome error
	if len(r.released) == 0 {
		outcome = i18n.Errorf("o deploy falhou em %s; os servidores continuam na versão anterior: %w", failedName, cause)
		r.setJournalState(ctx, RolloutFailed, outcome)
		return outcome
	}
	if err := r.journal().setState(context.WithoutCancel(ctx), r.app.ID, RolloutCompensating, ""); err != nil {
		return errors.Join(cause, err)
	}
	if failures, restoreErr := r.restoreReleased(ctx); len(failures) > 0 {
		cause = errors.Join(cause, restoreErr)
		outcome = i18n.Errorf("o deploy falhou em %s e a reversão falhou em %s; verifique esses servidores: %w",
			failedName, strings.Join(failures, ", "), cause)
		_ = r.journal().interrupt(context.WithoutCancel(ctx), r.app.ID, RolloutCompensating, r.log.mask(describeFailure(outcome)))
		return outcome
	} else {
		outcome = i18n.Errorf("o deploy falhou em %s; os servidores já atualizados voltaram para a versão anterior: %w", failedName, cause)
	}
	r.setJournalState(ctx, RolloutFailed, outcome)
	return outcome
}

// restoreReleased puts the previous version back on every released server,
// newest first, and returns the servers where that failed, with the errors.
func (r *rollout) restoreReleased(ctx context.Context) ([]string, error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), compensationTimeout)
	defer cancel()
	failures := []string{}
	var errs error
	for index := len(r.released) - 1; index >= 0; index-- {
		released := r.released[index]
		r.log.Info("Revertendo %s para a versão anterior.", released.name)
		if err := r.restore(cleanup, released); err != nil {
			r.setStep(cleanup, released.id, StepRestoreFailed)
			r.record(cleanup, released.id, PlacementRollbackFailed, r.image, released.previous, err)
			failures = append(failures, released.name)
			errs = errors.Join(errs, err)
			continue
		}
		r.setStep(cleanup, released.id, StepRestored)
		r.record(cleanup, released.id, PlacementRolledBack, released.previous, r.image, nil)
	}
	return failures, errs
}

// restore releases the previous image again (without release command or
// hooks), or removes the new release where there was nothing before.
func (r *rollout) restore(ctx context.Context, released releasedServer) error {
	j, err := r.job(ctx, released.id)
	if err != nil {
		return err
	}
	if released.recovery != nil && released.recovery.touched {
		return released.recovery.restore(ctx, j)
	}
	if released.previous == "" {
		containers, err := docker.ListByResource(ctx, j.ex, r.app.ID)
		if err != nil {
			return err
		}
		r.log.Info("%s não tinha versão anterior: a nova foi removida.", released.name)
		return docker.Remove(ctx, j.ex, containers)
	}
	if err := r.d.ensureImage(ctx, j, released.previous); err != nil {
		return err
	}
	j.deployment = Deployment{ID: newDeploymentID(), ApplicationID: r.app.ID, Trigger: TriggerRollback, RollbackImage: released.previous}
	withoutOneShotSteps(&j.app)
	j.app.Config.Hooks.PostDeploy = ""
	return j.release(ctx, released.previous, environmentOf(j.env))
}

// decommission removes the application from servers taken out of the
// placement, once the new version runs on the others. A server that cannot
// be reached keeps its record and is tried again on the next deployment.
func (r *rollout) decommission(ctx context.Context) {
	for _, id := range r.plan.stale {
		ex, target, err := r.d.connector.Executor(ctx, id)
		if err == nil {
			err = removeRelease(ctx, ex, target, r.app.ID)
		}
		if err != nil {
			r.log.Info("Não foi possível remover a aplicação de um servidor que saiu dos destinos: %s", err)
			continue
		}
		r.log.Info("Aplicação removida de %s, que saiu dos destinos.", target.Name)
		_ = r.d.multi.forget(ctx, r.app.ID, id)
	}
	if len(r.plan.servers) == 1 && r.plan.buildServer == r.app.ServerID {
		_ = r.d.multi.forget(ctx, r.app.ID, r.app.ServerID)
	}
}

func removeRelease(ctx context.Context, ex executor.Executor, target server.Server, applicationID string) error {
	containers, err := docker.ListByResource(ctx, ex, applicationID)
	if err != nil {
		return err
	}
	if err := docker.Remove(ctx, ex, containers); err != nil {
		return err
	}
	project := compose.Project{Name: applicationID, Dir: target.ResourceDir(docker.TypeApplication, applicationID)}
	return project.RemoveDir(ctx, ex)
}

// record persists the state of one server; a failure to record never
// changes the outcome of the deployment, only what the panel shows.
func (r *rollout) record(ctx context.Context, serverID, status, image, previous string, cause error) {
	message := ""
	if cause != nil {
		message = r.log.mask(describeFailure(cause))
	}
	err := r.d.multi.record(context.WithoutCancel(ctx), r.app.ID, DestinationStatus{
		ServerID: serverID, DeploymentID: r.deployment.ID, Image: image, PreviousImage: previous, Status: status, Message: message,
	})
	if err != nil {
		r.log.Info("Não foi possível registrar o estado do servidor: %s", err)
	}
}

// previousImage is what the server runs now, the version to go back to.
func previousImage(ctx context.Context, j *job) (string, error) {
	containers, err := docker.ListByResource(ctx, j.ex, j.app.ID)
	if err != nil {
		return "", err
	}
	for _, container := range containers {
		if container.Image != "" {
			return container.Image, nil
		}
	}
	return "", nil
}

// runsDeployment tells whether the new release already serves on the
// server even though its release reported an error (a late failure).
func runsDeployment(ctx context.Context, j *job) bool {
	containers, err := docker.ListByResource(ctx, j.ex, j.app.ID)
	if err != nil {
		return true
	}
	for _, container := range containers {
		if container.Deployment == j.deployment.ID && container.State == "running" {
			return true
		}
	}
	return false
}

// withoutOneShotSteps drops what must run once per deployment, not once per
// server: the release command and the pre-deploy hook.
func withoutOneShotSteps(app *Application) {
	app.Config.Release.Command = ""
	app.Config.Hooks.PreDeploy = ""
}

func environmentOf(variables []envvar.Variable) map[string]string {
	env := make(map[string]string, len(variables))
	for _, variable := range variables {
		env[variable.Key] = variable.Value
	}
	return env
}
