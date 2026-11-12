package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/server"
)

type engineConnector struct{ ex executor.Executor }

func (c engineConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return c.ex, server.Server{ID: "srv1", Name: "test", DataDir: "/data/pier", ProxyToken: "persisted-route-token"}, nil
}

type heldDeployExecutor struct {
	*executor.Fake
	started, cancelled, release chan struct{}
}

func (e *heldDeployExecutor) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	if strings.Contains(command, "docker pull") {
		close(e.started)
		<-ctx.Done()
		close(e.cancelled)
		<-e.release
		return ctx.Err()
	}
	return e.Fake.Stream(ctx, command, sink)
}
func waitEngineSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for deploy barrier")
	}
}

func startHeldEngineDeploy(t *testing.T) (*deployFixture, *Service, *Dispatcher, *heldDeployExecutor) {
	t.Helper()
	f := newDeployFixture(t, imageConfig())
	ex := &heldDeployExecutor{Fake: f.ex, started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	connector := engineConnector{ex}
	f.deployer.connector = connector
	dispatcher := NewDispatcher(f.store, f.deployer, f.deployer.logger, time.Hour)
	service := NewService(f.store, f.env, connector, dispatcher)
	workerCtx, stop := context.WithCancel(context.Background())
	if err := dispatcher.Start(workerCtx, 1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop()
		select {
		case <-ex.release:
		default:
			close(ex.release)
		}
		dispatcher.Wait()
	})
	if _, err := service.Deploy(context.Background(), f.app.ID, TriggerManual); err != nil {
		t.Fatal(err)
	}
	waitEngineSignal(t, ex.started)
	return f, service, dispatcher, ex
}

func TestEngineDeleteClosesAdmissionAndWaitsForDeploymentCleanup(t *testing.T) {
	f, service, _, ex := startHeldEngineDeploy(t)
	done := make(chan error, 1)
	go func() { done <- service.Delete(context.Background(), f.app.ID, false, false) }()
	waitEngineSignal(t, ex.cancelled)
	if _, err := service.Deploy(context.Background(), f.app.ID, TriggerManual); err == nil {
		t.Fatal("new deployment admitted during removal")
	} else {
		requireConflict(t, err)
	}
	secret, err := service.WebhookSecret(context.Background(), f.app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.HandleDeployHook(context.Background(), f.app.ID, secret); err == nil {
		t.Fatal("webhook admitted during removal")
	} else {
		requireConflict(t, err)
	}
	if f.ex.Ran("docker ps") {
		t.Fatal("server cleanup began before deployment stopped")
	}
	select {
	case err := <-done:
		t.Fatalf("delete returned before worker cleanup: %v", err)
	default:
	}
	close(ex.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delete did not finish")
	}
	if _, err := service.Get(context.Background(), f.app.ID); err == nil {
		t.Fatal("application still exists")
	}
	if f.ex.Ran("up --detach") {
		t.Fatal("a deleted application's release was started")
	}
}

func TestEngineDeleteTimeoutConflictsWithoutCleaningServer(t *testing.T) {
	f, service, _, ex := startHeldEngineDeploy(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := service.Delete(ctx, f.app.ID, false, true)
	requireConflict(t, err)
	waitEngineSignal(t, ex.cancelled)
	if f.ex.Ran("docker ps") {
		t.Fatal("forced delete bypassed quiescence")
	}
	if _, err := service.Get(context.Background(), f.app.ID); err != nil {
		t.Fatal("record removed while deploy still running", err)
	}
	close(ex.release)
}

func TestEngineCancellationReasonDistinguishesShutdownAndUser(t *testing.T) {
	for _, user := range []bool{false, true} {
		t.Run(map[bool]string{false: "shutdown", true: "user"}[user], func(t *testing.T) {
			f := newDeployFixture(t, imageConfig())
			dep := Deployment{ID: "cancelled", ApplicationID: f.app.ID, Status: DeploymentQueued, Trigger: TriggerManual}
			if err := f.store.insertDeployment(context.Background(), dep); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancelCause(context.Background())
			if user {
				cancel(errUserCancelled)
			} else {
				cancel(context.Canceled)
			}
			f.deployer.Run(ctx, dep.ID)
			finished, err := f.store.GetDeployment(context.Background(), dep.ID)
			if err != nil {
				t.Fatal(err)
			}
			if finished.Status != DeploymentCancelled {
				t.Fatal(finished.Status)
			}
			if user && !strings.Contains(finished.Error, "usuário") || !user && !strings.Contains(finished.Error, "reinicialização") {
				t.Fatal(finished.Error)
			}
		})
	}
}

func TestEngineRetryQueueOverflowTerminatesDeployment(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	dispatcher := NewDispatcher(f.store, f.deployer, f.deployer.logger, time.Hour)
	dep := Deployment{ID: "retry", ApplicationID: f.app.ID, Status: DeploymentQueued, Trigger: TriggerManual}
	if err := f.store.insertDeployment(context.Background(), dep); err != nil {
		t.Fatal(err)
	}
	for range cap(dispatcher.queue) {
		if err := dispatcher.Enqueue("filler"); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dispatcher.retryLater(ctx, dep.ID)
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		got, err := f.store.GetDeployment(context.Background(), dep.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == DeploymentFailed {
			if got.Error == "" {
				t.Fatal("missing queue-full reason")
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("queued zombie after requeue overflow")
		case <-timer.C:
		}
	}
}
