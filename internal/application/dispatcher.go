package application

import (
	"context"
	"log/slog"
	"sync"

	"pier/internal/platform/apperr"
)

const queueCapacity = 1024

// Dispatcher is the deployment queue: a fixed pool of workers that runs one
// deployment per application at a time and supports cancellation.
type Dispatcher struct {
	store    *Store
	deployer *Deployer
	logger   *slog.Logger
	queue    chan string

	mu       sync.Mutex
	cancels  map[string]context.CancelFunc
	appLocks map[string]*sync.Mutex
	workers  sync.WaitGroup
}

// NewDispatcher creates the dispatcher; call Start to begin processing.
func NewDispatcher(store *Store, deployer *Deployer, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		store:    store,
		deployer: deployer,
		logger:   logger,
		queue:    make(chan string, queueCapacity),
		cancels:  make(map[string]context.CancelFunc),
		appLocks: make(map[string]*sync.Mutex),
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
		const reason = "Interrompido pela reinicialização do Pier."
		if err := d.store.finishDeployment(ctx, deployment.ID, DeploymentFailed, reason); err != nil {
			return err
		}
	}
	queued, err := d.store.deploymentsByStatus(ctx, DeploymentQueued)
	if err != nil {
		return err
	}
	for _, deployment := range queued {
		if err := d.Enqueue(deployment.ID); err != nil {
			return err
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
		cancel()
		return nil
	}
	return d.store.finishDeployment(ctx, deploymentID, DeploymentCancelled, "Cancelado antes de iniciar.")
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
	lock.Lock()
	defer lock.Unlock()

	claimed, err := d.store.claimDeployment(ctx, deploymentID)
	if err != nil {
		d.logger.Error("claim deployment", "deployment", deploymentID, "error", err)
		return
	}
	if !claimed {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.cancels[deploymentID] = cancel
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.cancels, deploymentID)
		d.mu.Unlock()
		cancel()
	}()
	d.deployer.Run(runCtx, deploymentID)
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
