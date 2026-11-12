package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

const queueCapacity = 1024

// busyRetryDelay is how long a deployment of an application that is already
// deploying waits, off the workers, before it is offered to them again.
const busyRetryDelay = 3 * time.Second

// Dispatcher is the deployment queue: a fixed pool of workers that runs one
// deployment per application at a time and supports cancellation.
type Dispatcher struct {
	store    *Store
	deployer *Deployer
	logger   *slog.Logger
	queue    chan string
	// timeout bounds a whole deployment, so a build that hangs cannot hold a
	// worker forever.
	timeout time.Duration

	mu       sync.Mutex
	cancels  map[string]context.CancelCauseFunc
	deleting map[string]bool
	appLocks map[string]*sync.Mutex
	workers  sync.WaitGroup
}

// NewDispatcher creates the dispatcher; call Start to begin processing.
func NewDispatcher(store *Store, deployer *Deployer, logger *slog.Logger, timeout time.Duration) *Dispatcher {
	return &Dispatcher{
		store:    store,
		deployer: deployer,
		logger:   logger,
		timeout:  timeout,
		queue:    make(chan string, queueCapacity),
		cancels:  make(map[string]context.CancelCauseFunc),
		appLocks: make(map[string]*sync.Mutex),
		deleting: make(map[string]bool),
	}
}

// Start recovers deployments left over from a previous process and launches
// the workers. They stop when ctx is cancelled.
func (d *Dispatcher) Start(ctx context.Context, workerCount int) error {
	if err := d.recover(ctx); err != nil {
		return err
	}
	for range workerCount {
		d.workers.Add(1)
		go d.work(ctx)
	}
	return nil
}

// Wait blocks until every worker has stopped.
func (d *Dispatcher) Wait() { d.workers.Wait() }

func (d *Dispatcher) recover(ctx context.Context) error {
	interrupted, err := d.store.deploymentsByStatus(ctx, DeploymentInProgress)
	if err != nil {
		return err
	}
	for _, deployment := range interrupted {
		reason := i18n.Instance().Text("Interrompido pela reinicialização do Pier.")
		if err := d.store.finishDeployment(ctx, deployment.ID, DeploymentFailed, reason); err != nil {
			return err
		}
	}
	queued, err := d.store.deploymentsByStatus(ctx, DeploymentQueued)
	if err != nil {
		return err
	}
	pending := map[string]bool{}
	for _, deployment := range queued {
		pending[deployment.ID] = true
	}
	d.deployer.archives.sweep(func(id string) bool { return pending[id] })
	for _, deployment := range queued {
		if err := d.Enqueue(deployment.ID); err != nil {
			// Refusing to boot over a backlog would leave the panel down; the
			// surplus fails visibly and can be deployed again.
			reason := i18n.Instance().Text("A fila de deploys estava cheia quando o Pier reiniciou.")
			if err := d.store.finishDeployment(ctx, deployment.ID, DeploymentFailed, reason); err != nil {
				return err
			}
		}
	}
	return nil
}

// Enqueue schedules a queued deployment for execution.
func (d *Dispatcher) Enqueue(deploymentID string) error {
	select {
	case d.queue <- deploymentID:
		return nil
	default:
		return apperr.Unavailable("A fila de deploys está cheia. Tente novamente em instantes.")
	}
}

// Cancel stops a deployment: a queued one never starts, a running one is
// interrupted. It reports whether anything was cancelled.
func (d *Dispatcher) Cancel(ctx context.Context, deploymentID string) error {
	d.mu.Lock()
	cancel, running := d.cancels[deploymentID]
	d.mu.Unlock()
	if running {
		cancel(errUserCancelled)
		return nil
	}
	err := d.store.finishDeployment(ctx, deploymentID, DeploymentCancelled, i18n.Instance().Text("Cancelado antes de iniciar."))
	if deployment, loadErr := d.store.GetDeployment(ctx, deploymentID); loadErr == nil && deployment.Status.Terminal() {
		d.deployer.archives.remove(deploymentID)
	}
	return err
}

func (d *Dispatcher) work(ctx context.Context) {
	defer d.workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case deploymentID := <-d.queue:
			d.process(ctx, deploymentID)
		}
	}
}

func (d *Dispatcher) process(ctx context.Context, deploymentID string) {
	deployment, err := d.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		d.logger.Error("load queued deployment", "deployment", deploymentID, "error", err)
		return
	}
	lock := d.lockFor(deployment.ApplicationID)
	// One deployment per application at a time. Waiting here would hold a
	// worker, and two deployments of one application would starve all the
	// others: the job steps aside and comes back later instead.
	if !lock.TryLock() {
		d.retryLater(ctx, deploymentID)
		return
	}
	defer lock.Unlock()

	// The cancel function is published before the deployment is claimed, so
	// a cancellation arriving in between stops it instead of being lost.
	timeoutCtx, stopTimeout := context.WithTimeout(ctx, d.timeout)
	defer stopTimeout()
	runCtx, cancel := context.WithCancelCause(timeoutCtx)
	d.mu.Lock()
	if d.deleting[deployment.ApplicationID] {
		d.mu.Unlock()
		cancel(errApplicationDeleting)
		_ = d.store.finishDeployment(context.WithoutCancel(ctx), deploymentID, DeploymentCancelled, i18n.Instance().Text("Aplicação em remoção."))
		return
	}
	d.cancels[deploymentID] = cancel
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.cancels, deploymentID)
		d.mu.Unlock()
		cancel(nil)
	}()
	claimed, err := d.store.claimDeployment(ctx, deploymentID)
	if err != nil {
		d.logger.Error("claim deployment", "deployment", deploymentID, "error", err)
		return
	}
	if !claimed {
		return
	}
	d.deployer.Run(runCtx, deploymentID)
}

// retryLater offers a deployment to the workers again after a short delay.
// It stays queued in the database meanwhile, so a restart recovers it.
func (d *Dispatcher) retryLater(ctx context.Context, deploymentID string) {
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(busyRetryDelay):
			if err := d.Enqueue(deploymentID); err != nil {
				d.logger.Warn("requeue deployment", "deployment", deploymentID, "error", err)
				_ = d.store.finishDeployment(context.WithoutCancel(ctx), deploymentID, DeploymentFailed, i18n.Instance().Text("A fila de deploys está cheia. Tente novamente em instantes."))
			}
		}
	}()
}

func (d *Dispatcher) lockFor(applicationID string) *sync.Mutex {
	d.mu.Lock()
	defer d.mu.Unlock()
	lock, ok := d.appLocks[applicationID]
	if !ok {
		lock = &sync.Mutex{}
		d.appLocks[applicationID] = lock
	}
	return lock
}
