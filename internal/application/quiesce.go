package application

import (
	"context"
	"errors"
	"time"

	"pier/internal/platform/apperr"
)

var (
	errUserCancelled       = errors.New("user cancelled deployment")
	errApplicationDeleting = errors.New("application deletion")
)

const deploymentStopTimeout = 30 * time.Second

// quiesce closes admission before cancelling, then takes the same lock the
// worker holds through all deployment cleanup. A timeout never starts removal.
func (d *Dispatcher) quiesce(ctx context.Context, applicationID string) (func(), error) {
	d.mu.Lock()
	if d.deleting[applicationID] {
		d.mu.Unlock()
		return nil, apperr.Conflict("A aplicação está sendo removida. Aguarde a conclusão.")
	}
	d.deleting[applicationID] = true
	d.mu.Unlock()
	unblock := func() { d.mu.Lock(); delete(d.deleting, applicationID); d.mu.Unlock() }
	for _, status := range []DeploymentStatus{DeploymentQueued, DeploymentInProgress} {
		deployments, err := d.store.deploymentsByStatus(ctx, status)
		if err != nil {
			unblock()
			return nil, err
		}
		for _, dep := range deployments {
			if dep.ApplicationID != applicationID {
				continue
			}
			d.mu.Lock()
			cancel := d.cancels[dep.ID]
			d.mu.Unlock()
			if cancel != nil {
				cancel(errApplicationDeleting)
			} else if err := d.store.finishDeployment(ctx, dep.ID, DeploymentCancelled, ""); err != nil {
				unblock()
				return nil, err
			}
		}
	}
	lock := d.lockFor(applicationID)
	waitCtx, cancel := context.WithTimeout(ctx, deploymentStopTimeout)
	defer cancel()
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		if lock.TryLock() {
			return func() { lock.Unlock(); unblock() }, nil
		}
		select {
		case <-waitCtx.Done():
			unblock()
			return nil, apperr.Conflict("O deploy ainda está encerrando. Tente remover a aplicação novamente em instantes.")
		case <-timer.C:
		}
	}
}
