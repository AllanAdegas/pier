package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pier/internal/docker"
	"pier/internal/platform/executor"
	"pier/internal/server"
)

// Models host-port occupancy and original container identity. Starting the old
// ID preserves its environment/config; recreating an image cannot satisfy this fake.
type recoveryExecutor struct {
	*executor.Fake
	id, app, fail                    string
	oldRunning, nextRunning, removed bool
	restoreFail                      bool
	order                            *[]string
}

func (e *recoveryExecutor) Run(ctx context.Context, command string) (string, error) {
	output, err := e.Fake.Run(ctx, command)
	if err != nil {
		return "", err
	}
	switch {
	case strings.Contains(command, "docker ps"):
		state := "exited"
		if e.oldRunning {
			state = "running"
		}
		if e.removed {
			return "", nil
		}
		return fmt.Sprintf("old-%s\told\told:image\t%s\tUp\tapplication\t%s\tolddeployment", e.id, state, e.app), nil
	case strings.Contains(command, "docker stop old-"):
		e.oldRunning = false
		if e.fail == "stop" {
			return "", errors.New("partial stop failure")
		}
	case strings.Contains(command, "docker rm -f old-"):
		e.removed = true
	case strings.Contains(command, "docker rm -f"):
		e.nextRunning = false
	case strings.Contains(command, "docker start old-"):
		*e.order = append(*e.order, e.id)
		if e.restoreFail {
			return "", errors.New("restore start denied")
		}
		if e.removed || e.nextRunning {
			return "", errors.New("original missing or port busy")
		}
		e.oldRunning = true
	case strings.Contains(command, ".State.Status"):
		if strings.HasSuffix(command, "old-"+e.id) {
			if e.oldRunning {
				return "running|healthy|0", nil
			}
			return "exited|none|0", nil
		}
		if e.fail == "readiness" {
			return "running|unhealthy|0", nil
		}
		return "running|healthy|0", nil
	}
	return output, nil
}
func (e *recoveryExecutor) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	if err := e.Fake.Stream(ctx, command, sink); err != nil {
		return err
	}
	if strings.Contains(command, "up --detach") {
		if e.oldRunning {
			return errors.New("host port busy")
		}
		e.nextRunning = true
		if e.fail == "up" {
			return errors.New("up failed after creation")
		}
	}
	return nil
}

type recoveryConnector struct {
	base   serverConnector
	states map[string]*recoveryExecutor
}

func (c recoveryConnector) Executor(ctx context.Context, id string) (executor.Executor, server.Server, error) {
	_, s, err := c.base.Executor(ctx, id)
	return c.states[id], s, err
}
func TestHostPortRecoveryOnFirstAndSecondTarget(t *testing.T) {
	for _, target := range []string{"srv1", "srv2"} {
		for _, failure := range []string{"up", "readiness", "stop"} {
			t.Run(target+failure, func(t *testing.T) {
				cfg := imageConfig()
				cfg.Network.PortMappings = []string{"8080:80"}
				cfg.Release.Command = "migrate"
				p := newPlacementFixture(t, cfg, "srv2")
				if _, err := p.multi.Save(context.Background(), p.app.ID, Placement{Destinations: []string{"srv2"}}); err != nil {
					t.Fatal(err)
				}
				order := []string{}
				states := map[string]*recoveryExecutor{}
				for id, ex := range p.servers {
					states[id] = &recoveryExecutor{Fake: ex, id: id, app: p.app.ID, oldRunning: true, order: &order}
				}
				states[target].fail = failure
				p.deployer.connector = recoveryConnector{p.servers, states}
				err := p.deploy(t, Deployment{Trigger: TriggerRollback, RollbackImage: digestRef("acme/api", "a")})
				if err == nil {
					t.Fatal("failure hidden")
				}
				for id, e := range states {
					if !e.oldRunning || e.removed || e.nextRunning {
						t.Fatalf("%s not restored: %+v", id, e)
					}
				}
				want := "srv1"
				if target == "srv2" {
					want = "srv2,srv1"
				}
				if strings.Join(order, ",") != want {
					t.Fatalf("recovery order %v want %s", order, want)
				}
				for _, e := range states {
					count := 0
					for _, cmd := range e.Commands {
						if strings.Contains(cmd, "run --rm --no-deps") {
							count++
						}
					}
					if count > 1 {
						t.Fatal("migration repeated")
					}
				}
			})
		}
	}
}
func TestSingleServerHostPortRecovery(t *testing.T) {
	for _, failure := range []string{"up", "readiness", "stop"} {
		t.Run(failure, func(t *testing.T) {
			cfg := imageConfig()
			cfg.Network.PortMappings = []string{"8080:80"}
			p := newPlacementFixture(t, cfg)
			order := []string{}
			e := &recoveryExecutor{Fake: p.ex, id: "srv1", app: p.app.ID, oldRunning: true, fail: failure, order: &order}
			p.deployer.connector = recoveryConnector{p.servers, map[string]*recoveryExecutor{"srv1": e}}
			err := p.deploy(t, Deployment{Trigger: TriggerRollback, RollbackImage: digestRef("acme/api", "a")})
			if err == nil || !e.oldRunning || e.nextRunning || e.removed {
				t.Fatalf("service not restored: %v %+v", err, e)
			}
		})
	}
}
func TestRecoveryReportsRestartFailure(t *testing.T) {
	cfg := imageConfig()
	cfg.Network.PortMappings = []string{"8080:80"}
	p := newPlacementFixture(t, cfg, "srv2")
	_, err := p.multi.Save(context.Background(), p.app.ID, Placement{Destinations: []string{"srv2"}})
	if err != nil {
		t.Fatal(err)
	}
	order := []string{}
	states := map[string]*recoveryExecutor{}
	for id, ex := range p.servers {
		states[id] = &recoveryExecutor{Fake: ex, id: id, app: p.app.ID, oldRunning: true, order: &order}
	}
	states["srv2"].fail = "up"
	states["srv2"].restoreFail = true
	p.deployer.connector = recoveryConnector{p.servers, states}
	err = p.deploy(t, Deployment{Trigger: TriggerRollback, RollbackImage: digestRef("acme/api", "a")})
	if err == nil || !strings.Contains(err.Error(), "restore start denied") || !strings.Contains(err.Error(), "up failed") {
		t.Fatalf("real causes lost: %v", err)
	}
	if !states["srv1"].oldRunning {
		t.Fatal("remaining compensation abandoned")
	}
}

func TestRecoveryDoesNotStartPreviouslyStoppedContainers(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	ex := f.ex.On(".State.Status", "running|healthy|0", 0)
	r := releaseRecovery{previous: []docker.Container{{ID: "active", State: "running"}, {ID: "stopped", State: "exited"}}}
	if err := r.restore(context.Background(), &job{ex: ex, app: Application{Config: imageConfig()}, log: &deployLog{store: f.store, hub: f.deployer.hub}}); err != nil {
		t.Fatal(err)
	}
	if !ex.Ran("docker start active") || ex.Ran("docker start stopped") {
		t.Fatal(ex.Commands)
	}
}
