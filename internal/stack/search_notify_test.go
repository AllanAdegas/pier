package stack

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

// Only a deployment that brought the stack up tells search engines; stop and
// restart do not, and neither does a failed deployment.
func TestSearchEnginesHearOnlyAboutSuccessfulStackDeployments(t *testing.T) {
	ctx := context.Background()
	service, fake := newService(t)
	search := &recordingSearch{}
	service.SetSearchNotifier(search)
	stack, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "registry", Template: "docker-registry"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	fake.On("test -e", "", 0)
	if err := service.Deploy(ctx, stack.ID); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	snapshot := waitOperation(t, service, stack.ID)
	if snapshot.Error != "" || len(search.deployed) != 1 || search.deployed[0] != stack.ID {
		t.Fatalf("error %q, notified %v", snapshot.Error, search.deployed)
	}
	if !strings.Contains(strings.Join(snapshot.Lines, "\n"), "IndexNow: 2 URL(s)") {
		t.Errorf("the notification must be in the operation log: %v", snapshot.Lines)
	}
	for _, operate := range []func(context.Context, string) error{service.Stop, service.Restart} {
		if err := operate(ctx, stack.ID); err != nil {
			t.Fatal(err)
		}
		waitOperation(t, service, stack.ID)
	}
	fake.On("compose", "boom", 1)
	if err := service.Deploy(ctx, stack.ID); err != nil {
		t.Fatal(err)
	}
	if failed := waitOperation(t, service, stack.ID); failed.Error == "" || len(search.deployed) != 1 {
		t.Errorf("only the first deployment may notify: error %q, notified %v", failed.Error, search.deployed)
	}
}
