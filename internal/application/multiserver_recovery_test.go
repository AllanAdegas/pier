package application

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestJournalRestartClassificationAndIdempotence(t *testing.T) {
	for _, phase := range []string{RolloutPreparing, RolloutReleasing, RolloutCompensating} {
		t.Run(phase, func(t *testing.T) {
			p := newPlacementFixture(t, recoveryConfig(), "srv2")
			ctx := context.Background()
			j := p.multi.journal
			if err := j.begin(ctx, p.app.ID, "origin", []string{"srv1", "srv2"}); err != nil {
				t.Fatal(err)
			}
			previous := digestRef("acme/old", "a")
			if err := j.step(ctx, p.app.ID, "srv1", StepReleasing, &previous); err != nil {
				t.Fatal(err)
			}
			if err := j.setState(ctx, p.app.ID, phase, ""); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := p.multi.Recover(ctx); err != nil {
					t.Fatal(err)
				}
			}
			got, found, err := j.load(ctx, p.app.ID)
			if err != nil || !found {
				t.Fatalf("load: %v", err)
			}
			want := RolloutInterrupted
			if phase == RolloutPreparing {
				want = RolloutFailed
			}
			if got.State != want || got.Servers[0].PreviousImage != previous || got.Servers[1].State != StepPending {
				t.Fatalf("journal: %+v", got)
			}
			if phase != RolloutPreparing && got.InterruptedIn != phase {
				t.Fatalf("phase: %s", got.InterruptedIn)
			}
		})
	}
}

func TestResumeSkipsReleasedServerAndKeepsPinnedImage(t *testing.T) {
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx"
	p := newPlacementFixture(t, config, "srv2")
	ctx := context.Background()
	j := p.multi.journal
	image := digestRef("acme/web", "c")
	for _, err := range []error{j.begin(ctx, p.app.ID, "origin", []string{"srv1", "srv2"}), j.setImage(ctx, p.app.ID, image), j.step(ctx, p.app.ID, "srv1", StepReleased, nil), j.interrupt(ctx, p.app.ID, RolloutReleasing, ""), j.claim(ctx, p.app.ID, "resume", false)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	deployment := Deployment{ID: "resume", ApplicationID: p.app.ID, Trigger: TriggerRolloutResume}
	if err := p.store.insertDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	log := &deployLog{store: p.store, hub: p.deployer.hub, deploymentID: deployment.ID}
	if err := p.deployer.recoverRollout(ctx, deployment, p.app, log); err != nil {
		t.Fatal(err)
	}
	if p.servers["srv1"].Ran("docker compose") {
		t.Fatal("released server changed")
	}
	if got := lastCompose(p.servers["srv2"]); got == "" {
		t.Fatal("pending server not released")
	}
	got, _, err := j.load(ctx, p.app.ID)
	if err != nil || got.State != RolloutCompleted || got.Image != image {
		t.Fatalf("journal: %+v %v", got, err)
	}
	before := len(p.servers["srv2"].Commands)
	if err := p.deployer.recoverRollout(ctx, deployment, p.app, log); err != nil {
		t.Fatal(err)
	}
	if len(p.servers["srv2"].Commands) != before {
		t.Fatal("stale action changed server")
	}
}

func TestAmbiguousReleaseNeverReplaysOneShotCommand(t *testing.T) {
	app := Application{Config: DefaultConfig(PackImage)}
	app.Config.Release.Command = "migrate"
	journal := RolloutJournal{Servers: []RolloutStep{{State: StepReleasing}}}
	if safeResume(app, journal) == nil {
		t.Fatal("ambiguous command allowed")
	}
	journal.Servers[0].State = StepReleased
	if err := safeResume(app, journal); err != nil {
		t.Fatal(err)
	}
}

func TestPerServerViewsRejectUnrelatedServersBeforeExecution(t *testing.T) {
	p := newPlacementFixture(t, recoveryConfig(), "srv2", "foreign")
	ctx := context.Background()
	svc := NewService(p.store, nil, p.servers, nil)
	svc.SetMultiServer(p.multi)
	if _, err := p.multi.Save(ctx, p.app.ID, Placement{Destinations: []string{"srv2"}}); err != nil {
		t.Fatal(err)
	}
	if _, id, err := svc.ServerFor(ctx, p.app.ID, "srv2"); err != nil || id != "srv2" {
		t.Fatalf("target: %s %v", id, err)
	}
	if _, err := svc.ContainerLogSource(ctx, p.app.ID, "foreign", "anything"); err == nil {
		t.Fatal("foreign server accepted")
	}
	if len(p.servers["foreign"].Commands) != 0 {
		t.Fatal("foreign executor reached")
	}
	if _, err := svc.ContainerLogSource(ctx, p.app.ID, "srv2", "foreign-container"); err == nil {
		t.Fatal("foreign container accepted")
	}
}

func TestRecoveryQueueIsIdempotentAndAutomaticRecoveryRunsOnce(t *testing.T) {
	p := newPlacementFixture(t, recoveryConfig(), "srv2")
	ctx := context.Background()
	j := p.multi.journal
	dispatcher := NewDispatcher(p.store, p.deployer, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute)
	svc := NewService(p.store, nil, p.servers, dispatcher)
	svc.SetMultiServer(p.multi)
	for _, err := range []error{j.begin(ctx, p.app.ID, "origin", []string{"srv1", "srv2"}), j.setImage(ctx, p.app.ID, digestRef("acme/web", "c")), j.interrupt(ctx, p.app.ID, RolloutReleasing, "")} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RecoverRollouts(ctx); err != nil {
		t.Fatal(err)
	}
	first, _, _ := j.load(ctx, p.app.ID)
	if first.AutomaticRecoveries != 1 || first.ActionDeploymentID == "" {
		t.Fatalf("automatic: %+v", first)
	}
	again, err := svc.ResumeRollout(ctx, p.app.ID)
	if err != nil || again.ID != first.ActionDeploymentID {
		t.Fatalf("duplicate: %+v %v", again, err)
	}
	if _, err := svc.RollBackRollout(ctx, p.app.ID); err == nil {
		t.Fatal("conflicting action accepted")
	}
	if err := svc.RecoverRollouts(ctx); err != nil {
		t.Fatal(err)
	}
	if len(dispatcher.queue) != 1 {
		t.Fatal("queued more than one recovery")
	}
}

func TestInterruptedRollbackRemovesOnlyChangedServers(t *testing.T) {
	p := newPlacementFixture(t, recoveryConfig(), "srv2")
	ctx := context.Background()
	j := p.multi.journal
	for _, err := range []error{j.begin(ctx, p.app.ID, "origin", []string{"srv1", "srv2"}), j.step(ctx, p.app.ID, "srv1", StepReleasing, nil), j.interrupt(ctx, p.app.ID, RolloutReleasing, ""), j.claim(ctx, p.app.ID, "rollback", false)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	deployment := Deployment{ID: "rollback", ApplicationID: p.app.ID, Trigger: TriggerRolloutRollback}
	if err := p.store.insertDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	log := &deployLog{store: p.store, hub: p.deployer.hub, deploymentID: deployment.ID}
	if err := p.deployer.recoverRollout(ctx, deployment, p.app, log); err != nil {
		t.Fatal(err)
	}
	journal, _, err := j.load(ctx, p.app.ID)
	if err != nil || journal.State != RolloutRolledBack || journal.Servers[0].State != StepRestored || journal.Servers[1].State != StepPending {
		t.Fatalf("rollback: %+v %v", journal, err)
	}
	if p.servers["srv2"].Ran("docker") {
		t.Fatal("untouched server changed")
	}
}

func recoveryConfig() Config {
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	return config
}

func TestJournalRetainsRemovedServerAsUnresolvedTarget(t *testing.T) {
	p := newPlacementFixture(t, recoveryConfig(), "srv2")
	ctx := context.Background()
	if err := p.multi.journal.begin(ctx, p.app.ID, "origin", []string{"srv1", "srv2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.store.db.ExecContext(ctx, `DELETE FROM servers WHERE id = 'srv2'`); err != nil {
		t.Fatal(err)
	}
	got, _, err := p.multi.journal.load(ctx, p.app.ID)
	if err != nil || len(got.Servers) != 2 || got.Servers[1].ServerID != "srv2" {
		t.Fatalf("removed target silently forgotten: %+v %v", got, err)
	}
}
