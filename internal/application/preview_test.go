package application

import (
	"context"
	"encoding/json"
	"net/http"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"strings"
	"testing"
	"time"
)

func previewFixture(t *testing.T) (*deployFixture, *Service, *PreviewService) {
	config := gitConfig()
	config.Health.Enabled = true
	f := newDeployFixture(t, config)
	s := NewService(f.store, f.env, fakeConnector{f.ex}, NewDispatcher(f.store, f.deployer, f.deployer.logger, time.Hour))
	p := NewPreviewService(s, f.deployer.logger)
	_, err := p.UpdateSettings(t.Context(), f.app.ID, PreviewSettings{Enabled: true, DomainTemplate: "pr-{number}.preview.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return f, s, p
}
func previewPayload(action string, fork bool) []byte {
	repo := map[string]any{"id": 1, "full_name": "acme/app"}
	head := repo
	if fork {
		head = map[string]any{"id": 2, "full_name": "stranger/app"}
	}
	b, _ := json.Marshal(map[string]any{"action": action, "number": 7, "pull_request": map[string]any{"head": map[string]any{"ref": "feature/test", "sha": strings.Repeat("a", 40), "repo": head}, "base": map[string]any{"ref": "main", "repo": repo}}})
	return b
}
func TestPreviewLifecycleLimitAndJanitor(t *testing.T) {
	f, _, p := previewFixture(t)
	ctx := t.Context()
	result, err := p.HandlePullRequest(ctx, f.app, ProviderGitHub, previewPayload("opened", false))
	if err != nil || !result.Deployed {
		t.Fatal(result, err)
	}
	rows, _ := p.List(ctx, f.app.ID)
	if len(rows) != 1 || rows[0].ID == f.app.ID {
		t.Fatal(rows)
	}
	id := rows[0].ID
	result, err = p.HandlePullRequest(ctx, f.app, ProviderGitHub, previewPayload("synchronize", false))
	if err != nil || result.Deployed {
		t.Fatal(result, err)
	}
	updatedBody := []byte(strings.ReplaceAll(string(previewPayload("synchronize", false)), strings.Repeat("a", 40), strings.Repeat("b", 40)))
	result, err = p.HandlePullRequest(ctx, f.app, ProviderGitHub, updatedBody)
	if err != nil || !result.Deployed {
		t.Fatal(result, err)
	}
	updatedRows, _ := p.List(ctx, f.app.ID)
	if len(updatedRows) != 1 || updatedRows[0].ID != id || updatedRows[0].CommitSHA != strings.Repeat("b", 40) {
		t.Fatal(updatedRows)
	}
	settings, _ := p.Settings(ctx, f.app.ID)
	settings.MaxPreviews = 1
	p.UpdateSettings(ctx, f.app.ID, settings)
	_, reason, err := p.admit(ctx, f.app, settings, previewRequest{Number: 8, CommitSHA: strings.Repeat("b", 40), Branch: "next"})
	if err != nil || reason == "" {
		t.Fatal(reason, err)
	}
	// Settle the queued job, so cleanup never waits for an intentionally idle dispatcher.
	if err = f.store.finishDeployment(ctx, rows[0].LastDeploymentID, DeploymentCancelled, ""); err != nil {
		t.Fatal(err)
	}
	p.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if n := p.Sweep(ctx); n != 1 {
		t.Fatal(n)
	}
	rows, _ = p.List(ctx, f.app.ID)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
	for _, command := range f.ex.Commands {
		if strings.Contains(command, "rm") && strings.Contains(command, f.app.ID) {
			t.Fatalf("cleanup touched main app: %s (preview %s)", command, id)
		}
	}
}
func TestPreviewForkRefusalFreezeAndClose(t *testing.T) {
	f, s, p := previewFixture(t)
	ctx := t.Context()
	r, err := p.HandlePullRequest(ctx, f.app, ProviderGitHub, previewPayload("opened", true))
	if err != nil || r.Deployed || !strings.Contains(r.Reason, "fork") {
		t.Fatal(r, err)
	}
	s.SetDeployGuard(previewFrozen{})
	if _, err = p.HandlePullRequest(ctx, f.app, ProviderGitHub, previewPayload("opened", false)); err == nil {
		t.Fatal("frozen environment accepted preview")
	}
	s.SetDeployGuard(nil)
	r, err = p.HandlePullRequest(ctx, f.app, ProviderGitHub, previewPayload("opened", false))
	if err != nil {
		t.Fatal(err)
	}
	r, err = p.HandlePullRequest(ctx, f.app, ProviderGitHub, previewPayload("closed", false))
	if err != nil {
		t.Fatal(r, err)
	}
	rows, _ := p.List(ctx, f.app.ID)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}

type previewFrozen struct{}

func (previewFrozen) CheckDeploy(context.Context, string) error { return apperr.Conflict("frozen") }
func TestPreviewWebhookProvidersAndSignature(t *testing.T) {
	for _, provider := range []GitProvider{ProviderGitHub, ProviderGitea, ProviderGitLab} {
		t.Run(string(provider), func(t *testing.T) {
			f, s, p := previewFixture(t)
			_ = p
			body := previewPayload("opened", false)
			h := http.Header{}
			switch provider {
			case ProviderGitHub:
				h.Set("X-GitHub-Event", "pull_request")
			case ProviderGitea:
				h.Set("X-Gitea-Event", "pull_request")
			case ProviderGitLab:
				h.Set("X-Gitlab-Event", "Merge Request Hook")
				body = []byte(`{"object_attributes":{"action":"open","iid":7,"source_branch":"feature","target_branch":"main","source_project_id":1,"target_project_id":1,"last_commit":{"id":"` + strings.Repeat("a", 40) + `"}}}`)
			}
			if _, err := s.HandleGitPush(t.Context(), f.app.ID, provider, h, body); err == nil {
				t.Fatal("unsigned event accepted")
			}
			secret, _ := s.WebhookSecret(t.Context(), f.app.ID)
			switch provider {
			case ProviderGitHub:
				h.Set("X-Hub-Signature-256", "sha256="+previewSignature(secret, body))
			case ProviderGitea:
				h.Set("X-Gitea-Signature", previewSignature(secret, body))
			case ProviderGitLab:
				h.Set("X-Gitlab-Token", secret)
			}
			r, err := s.HandleGitPush(t.Context(), f.app.ID, provider, h, body)
			if err != nil || !r.Deployed {
				t.Fatal(r, err)
			}
		})
	}
}
func TestPreviewDomainAndIsolation(t *testing.T) {
	for _, bad := range []string{"preview.example.com", "999999999.example.com", "https://999999999.example.com/{number}", "{unknown}.{number}.com", "https://{number}.bad_host.com"} {
		if _, err := parseDomainTemplate(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if _, err := parseDomainTemplate("pr-{number}-{branch}.example.com"); err != nil {
		t.Fatal(err)
	}
	f, _, p := previewFixture(t)
	_ = p
	f.app.Config.Network.PortMappings = []string{"8080:80"}
	f.app.Config.Storages = []Storage{{Kind: StorageBind, Source: "/host", Target: "/host"}, {Kind: StorageVolume, Source: "data", Target: "/data"}}
	v := previewApplication(f.app, Preview{ID: "previewid", Number: 7, CommitSHA: strings.Repeat("b", 40), URL: "https://pr-7.example.com"})
	if v.ID == f.app.ID || len(v.Config.Network.PortMappings) != 0 || len(v.Config.Storages) != 1 || v.Config.Source.Commit == "" {
		t.Fatal(v)
	}
	if len(f.app.Config.Storages) != 2 {
		t.Fatal("mutated main config")
	}
	f.env.Set(t.Context(), envvar.Owner{Type: envvar.OwnerApplication, ID: f.app.ID}, "MAIN_SECRET", "private", false)
	f.env.Set(t.Context(), envvar.Owner{Type: PreviewEnvOwner, ID: f.app.ID}, "PREVIEW", "value", false)
	vars, err := f.deployer.previewVariables(t.Context(), f.app, Preview{Fork: true})
	if err != nil || len(vars) != 1 || vars[0].Key != "PREVIEW" {
		t.Fatal(vars, err)
	}
}

func TestPreviewDeploymentNeverChangesMainApplication(t *testing.T) {
	f, _, p := previewFixture(t)
	ctx := t.Context()
	f.ex.On("git clone", commitMarker+strings.Repeat("a", 40)+" test", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	result, err := p.HandlePullRequest(ctx, f.app, ProviderGitHub, previewPayload("opened", false))
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := f.store.claimDeployment(ctx, result.DeploymentID); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	f.deployer.Run(ctx, result.DeploymentID)
	dep, _ := f.store.GetDeployment(ctx, result.DeploymentID)
	if dep.Status != DeploymentFinished {
		t.Fatal(dep.Error, f.logText(t, dep.ID))
	}
	main, _ := f.store.Get(ctx, f.app.ID)
	if main.CurrentImage != f.app.CurrentImage || main.Status != f.app.Status {
		t.Fatalf("main changed: %+v", main)
	}
	rows, _ := p.List(ctx, f.app.ID)
	if len(rows) != 1 || rows[0].Status != PreviewRunning || rows[0].Image == "" {
		t.Fatal(rows)
	}
	preview := rows[0]
	if _, ok := f.ex.Files["/data/pier/applications/"+preview.ID+"/docker-compose.yaml"]; !ok {
		t.Fatal("missing isolated compose")
	}
	if _, ok := f.ex.Files["/data/pier/applications/"+f.app.ID+"/docker-compose.yaml"]; ok {
		t.Fatal("main compose touched")
	}
}
