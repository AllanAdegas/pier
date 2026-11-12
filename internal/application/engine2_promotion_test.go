package application

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"pier/internal/envvar"
	"pier/internal/platform/apperr"
)

const promotedImageID = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type promotionFixture struct {
	*deployFixture
	service *Service
	target  Application
}

// newPromotionFixture has a deployed staging application (the fixture app)
// and a production application on the same server, in another environment.
func newPromotionFixture(t *testing.T, targetServer string) promotionFixture {
	t.Helper()
	f := newDeployFixture(t, gitConfig())
	ctx := context.Background()
	for _, statement := range []string{
		`INSERT INTO environments (id, project_id, name, created_at) VALUES ('env2', 'prj1', 'staging', '2026-01-01T00:00:00Z')`,
		`INSERT INTO servers (id, name, kind, data_dir, created_at) VALUES ('srv2', 'other', 'local', '/data/other', '2026-01-01T00:00:00Z')`,
		`UPDATE applications SET current_image = 'pier/` + f.app.ID + `:dep1', environment_id = 'env2' WHERE id = '` + f.app.ID + `'`,
	} {
		if _, err := f.store.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(f.store, f.env, fakeConnector{f.ex}, NewDispatcher(f.store, f.deployer, f.deployer.logger, time.Hour))
	config := gitConfig()
	config.Release.Command = "./migrate"
	target, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: targetServer, Name: "demo", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	f.ex.On("docker image inspect --format '{{.Id}}'", promotedImageID, 0)
	return promotionFixture{deployFixture: f, service: service, target: target}
}

func (f promotionFixture) run(t *testing.T, deployment Deployment) Deployment {
	t.Helper()
	ctx := context.Background()
	if claimed, err := f.store.claimDeployment(ctx, deployment.ID); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	f.deployer.Run(ctx, deployment.ID)
	finished, err := f.store.GetDeployment(ctx, deployment.ID)
	if err != nil {
		t.Fatal(err)
	}
	return finished
}

func TestPromotionReleasesTheSourceImageWithTheTargetVariables(t *testing.T) {
	f := newPromotionFixture(t, "srv1")
	ctx := context.Background()
	owner := envvar.Owner{Type: envvar.OwnerApplication, ID: f.target.ID}
	if err := f.env.Set(ctx, owner, "DATABASE_URL", "postgres://production-only", false); err != nil {
		t.Fatal(err)
	}
	f.ex.On("docker image inspect --format '{{json .Config.Labels}}'", `{"pier.managed":"true","pier.id":"`+f.app.ID+`"}`, 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	queued, err := f.service.Promote(ctx, f.app.ID, f.target.ID)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if queued.Trigger != TriggerPromotion || queued.RollbackImage != promotedImageID || queued.PromotedFrom != f.app.ID ||
		!strings.Contains(queued.Source, "0123456789ab") {
		t.Fatalf("queued = %+v", queued)
	}
	finished := f.run(t, queued)

	if finished.Status != DeploymentFinished {
		t.Fatalf("status = %s (%s)\n%s", finished.Status, finished.Error, f.logText(t, finished.ID))
	}
	tag := "pier/" + f.target.ID + ":" + queued.ID
	if !f.ex.Ran("docker tag "+promotedImageID+" "+tag) || f.ex.Ran("git clone") || f.ex.Ran("buildx build") {
		t.Errorf("promotion must tag the pinned image without building:\n%s", strings.Join(f.ex.Commands, "\n"))
	}
	if !f.ex.Ran("./migrate") {
		t.Error("the target's release command must run")
	}
	compose := string(f.ex.Files["/data/pier/applications/"+f.target.ID+"/docker-compose.yaml"])
	if !strings.Contains(compose, "image: "+tag) || !strings.Contains(compose, "postgres://production-only") {
		t.Errorf("target compose:\n%s", compose)
	}
}

func TestPromotionRefusesForeignImageLabels(t *testing.T) {
	f := newPromotionFixture(t, "srv1")
	f.ex.On("docker image inspect --format '{{json .Config.Labels}}'", `{"pier.managed":"true","pier.id":"someone-else"}`, 0)
	queued, err := f.service.Promote(context.Background(), f.app.ID, f.target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished := f.run(t, queued); finished.Status != DeploymentFailed || !strings.Contains(finished.Error, "pier.id") {
		t.Errorf("an image labelled for another application must be refused: %s %s", finished.Status, finished.Error)
	}
}

func TestPromotionPreconditions(t *testing.T) {
	ctx := context.Background()
	other := newPromotionFixture(t, "srv2")
	_, err := other.service.Promote(ctx, other.app.ID, other.target.ID)
	if appErr, ok := apperr.As(err); !ok || appErr.Kind != apperr.KindConflict || !strings.Contains(err.Error(), "registry") {
		t.Errorf("cross-server promotion: %v", err)
	}
	f := newPromotionFixture(t, "srv1")
	if _, err := f.service.Promote(ctx, f.app.ID, f.app.ID); err == nil {
		t.Error("promoting onto itself must fail")
	}
	f.service.SetDeployGuard(frozenGuard{})
	if _, err := f.service.Promote(ctx, f.app.ID, f.target.ID); err == nil || !strings.Contains(err.Error(), "congelado") {
		t.Errorf("the target environment guard must apply: %v", err)
	}
	f.service.SetDeployGuard(nil)
	f.ex.On("docker image inspect --format '{{.Id}}'", "", 1)
	if _, err := f.service.Promote(ctx, f.app.ID, f.target.ID); err == nil {
		t.Error("a missing source image must fail before queueing")
	}
}

func TestADeployOfATagClonesTheTagInsteadOfTheBranch(t *testing.T) {
	config := gitConfig()
	config.Source.Commit = strings.Repeat("a", 40)
	f := newDeployFixture(t, config)
	f.ex.On("git clone --depth", "PIER_COMMIT 0123456789abcdef release", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	service := NewService(f.store, f.env, fakeConnector{f.ex}, NewDispatcher(f.store, f.deployer, f.deployer.logger, time.Hour))
	queued, err := service.DeployRef(context.Background(), f.app.ID, TriggerManual, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	promotion := promotionFixture{deployFixture: f, service: service}
	finished := promotion.run(t, queued)
	if finished.Ref != "v1.2.3" || finished.Status != DeploymentFinished {
		t.Fatalf("%+v\n%s", finished, f.logText(t, finished.ID))
	}
	env := string(f.ex.Files["/data/pier/builds/"+queued.ID+"/clone.env"])
	if env != "" && (!strings.Contains(env, "PIER_BRANCH=v1.2.3") || strings.Contains(env, "PIER_COMMIT=aaaa")) {
		t.Errorf("clone env:\n%s", env)
	}
	if !strings.Contains(f.logText(t, finished.ID), "--branch v1.2.3") {
		t.Errorf("log does not show the tag:\n%s", f.logText(t, finished.ID))
	}
	for _, ref := range []string{"v1..2", "-flag", "a//b", "x.lock", "tag/"} {
		if _, err := service.DeployRef(context.Background(), f.app.ID, TriggerManual, ref); err == nil {
			t.Errorf("ref %q accepted", ref)
		}
	}
}

func TestATagPatternDeploysMatchingTagsAndIgnoresBranches(t *testing.T) {
	config := gitConfig()
	config.AutoDeploy = true
	config.Source.TagPattern = "v*"
	f := newDeployFixture(t, config)
	service := NewService(f.store, f.env, fakeConnector{f.ex}, NewDispatcher(f.store, f.deployer, f.deployer.logger, time.Hour))
	secret, err := service.WebhookSecret(context.Background(), f.app.ID)
	if err != nil {
		t.Fatal(err)
	}
	send := func(event, body string) WebhookResult {
		header := http.Header{}
		header.Set("X-Gitlab-Event", event)
		header.Set("X-Gitlab-Token", secret)
		result, err := service.HandleGitPush(context.Background(), f.app.ID, ProviderGitLab, header, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := send("Push Hook", `{"ref":"refs/heads/main"}`); result.Deployed {
		t.Error("a branch push must not deploy when a tag pattern is set")
	}
	if result := send("Tag Push Hook", `{"ref":"refs/tags/nightly-1"}`); result.Deployed {
		t.Error("a tag outside the pattern must not deploy")
	}
	if result := send("Tag Push Hook", `{"ref":"refs/tags/v2.0.0","after":"0000000000000000000000000000000000000000"}`); result.Deployed {
		t.Error("a tag deletion must not deploy")
	}
	result := send("Tag Push Hook", `{"ref":"refs/tags/v2.0.0","after":"abc"}`)
	if !result.Deployed {
		t.Fatalf("matching tag not deployed: %+v", result)
	}
	deployment, err := f.store.GetDeployment(context.Background(), result.DeploymentID)
	if err != nil || deployment.Ref != "v2.0.0" || deployment.Trigger != TriggerWebhook {
		t.Errorf("deployment = %+v %v", deployment, err)
	}
}

func TestTagSourceRulesAreValidated(t *testing.T) {
	config := gitConfig()
	config.Source.Tag = "v1..0"
	config.Source.TagPattern = "v[1"
	fields := fieldsOf(t, config.Validate())
	if fields["source.tag"] == "" || fields["source.tag_pattern"] == "" {
		t.Errorf("fields = %v", fields)
	}
	config.Source.Tag, config.Source.TagPattern = " v1.0.0 ", "release/v*"
	if err := config.Validate(); err != nil || config.Source.Tag != "v1.0.0" {
		t.Errorf("valid rules refused: %v", err)
	}
}
