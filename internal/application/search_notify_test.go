package application

import (
	"context"
	"strings"
	"testing"
)

type recordingSearch struct{ deployed []string }

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
