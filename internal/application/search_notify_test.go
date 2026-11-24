package application

import (
	"context"
	"strings"
	"testing"
)

type recordingSearch struct {
	deployed []string
	indexed  map[string]bool
}

func (r *recordingSearch) IndexedHosts(context.Context, string) map[string]bool { return r.indexed }

func (r *recordingSearch) Deployed(_ context.Context, id string, report func(string)) {
	r.deployed = append(r.deployed, id)
	report("IndexNow: 2 URL(s) enviadas")
}

// Search engines hear about an application only when the whole deployment,
// hooks included, succeeded, and before "Deploy concluído" closes the log.
func TestSearchEnginesAreNotifiedOnlyAfterASuccessfulDeployment(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	search := &recordingSearch{}
	fixture.deployer.SetSearchNotifier(search)
	fixture.ex.On("docker ps", previousContainer, 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	deployment := fixture.deploy(t)

	if deployment.Status != DeploymentFinished || len(search.deployed) != 1 || search.deployed[0] != fixture.app.ID {
		t.Fatalf("status %s, notified %v", deployment.Status, search.deployed)
	}
	log := fixture.logText(t, deployment.ID)
	if strings.Index(log, "IndexNow: 2 URL(s)") > strings.Index(log, "Deploy concluído") || !strings.Contains(log, "IndexNow") {
		t.Errorf("the notification must be in the log before the end:\n%s", log)
	}
}

func TestAFailedDeploymentNotifiesNoSearchEngine(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	search := &recordingSearch{}
	fixture.deployer.SetSearchNotifier(search)
	fixture.ex.On("docker ps", previousContainer, 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|unhealthy|0", 0)

	if deployment := fixture.deploy(t); deployment.Status != DeploymentFailed || len(search.deployed) != 0 {
		t.Errorf("status %s, notified %v", deployment.Status, search.deployed)
	}
}

func TestPreviewsAreNeverAnnounced(t *testing.T) {
	search := &recordingSearch{}
	deployer := &Deployer{search: search}
	deployer.notifySearchEngines(context.Background(), Deployment{ApplicationID: "a", Trigger: TriggerPreview}, nil)
	if len(search.deployed) != 0 {
		t.Errorf("a preview must not notify search engines")
	}
}

// Nothing is indexed unless marked: the routes of an unmarked host answer
// noindex, the ones of a marked host do not.
func TestUnmarkedDomainsAreDeployedWithNoindex(t *testing.T) {
	for _, marked := range []bool{false, true} {
		fixture := newDeployFixture(t, imageConfig())
		search := &recordingSearch{}
		if marked {
			search.indexed = map[string]bool{"demo.example.com": true}
		}
		fixture.deployer.SetSearchNotifier(search)
		fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
		if deployment := fixture.deploy(t); deployment.Status != DeploymentFinished {
			t.Fatalf("status %s: %s", deployment.Status, deployment.Error)
		}
		compose := string(fixture.ex.Files["/data/pier/applications/"+fixture.app.ID+"/docker-compose.yaml"])
		if hidden := strings.Contains(compose, "X-Robots-Tag"); hidden == marked {
			t.Errorf("marked=%v: noindex present=%v\n%s", marked, hidden, compose)
		}
	}
}

// A preview is a temporary address: it answers noindex even when every host
// of the application is marked, and it never notifies search engines.
func TestPreviewsAlwaysAnswerNoindex(t *testing.T) {
	f, _, p := previewFixture(t)
	search := &recordingSearch{indexed: map[string]bool{}}
	for _, host := range []string{"demo.example.com", "pr-7.preview.example.com"} {
		search.indexed[host] = true
	}
	f.deployer.SetSearchNotifier(search)
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
	rows, _ := p.List(ctx, f.app.ID)
	if len(rows) != 1 {
		t.Fatalf("previews = %v", rows)
	}
	compose := string(f.ex.Files["/data/pier/applications/"+rows[0].ID+"/docker-compose.yaml"])
	if !strings.Contains(compose, "X-Robots-Tag") || len(search.deployed) != 0 {
		t.Errorf("a preview must answer noindex and notify nobody (notified %v):\n%s", search.deployed, compose)
	}
}
