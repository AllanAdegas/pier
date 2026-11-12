package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"pier/internal/envvar"
)

func sharedFixture(t *testing.T) (*deployFixture, *Service) {
	t.Helper()
	fixture := newDeployFixture(t, imageConfig())
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	return fixture, NewService(fixture.store, fixture.env, fakeConnector{fixture.ex}, nil)
}

func setSharedValue(t *testing.T, service *Service, scope envvar.SharedScope, scopeID, key, value string, secret bool) envvar.SharedVariable {
	t.Helper()
	variable, err := service.SetSharedVariable(context.Background(), scope, scopeID, envvar.SharedInput{Key: key, Value: &value, Secret: secret})
	if err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	return variable
}

func TestSharedVariableChangeMarksDependentsWithoutRedeploying(t *testing.T) {
	fixture, service := sharedFixture(t)
	ctx := context.Background()
	owner := envvar.Owner{Type: envvar.OwnerApplication, ID: fixture.app.ID}
	if err := fixture.env.Set(ctx, owner, "API_TOKEN", "Bearer ${{project.TOKEN}}", false); err != nil {
		t.Fatal(err)
	}
	other, _ := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "other", Config: imageConfig()})
	masked := setSharedValue(t, service, envvar.SharedProject, "prj1", "TOKEN", "shared-secret-v1", true)
	if masked.Value != "" || !masked.Secret {
		t.Fatalf("API answer must mask the secret: %+v", masked)
	}
	if status, _ := service.RedeployStatus(ctx, fixture.app.ID); status.Required {
		t.Fatal("an application never deployed needs no redeploy")
	}
	deployment := fixture.deploy(t)
	if deployment.Status != DeploymentFinished {
		t.Fatalf("deploy: %s", deployment.Error)
	}
	if text := fixture.logText(t, deployment.ID); strings.Contains(text, "shared-secret-v1") {
		t.Fatal("resolved shared secret leaked into the deployment log")
	}
	time.Sleep(2 * time.Millisecond)
	commandsBefore := len(fixture.ex.Commands)
	setSharedValue(t, service, envvar.SharedProject, "prj1", "TOKEN", "shared-secret-v2", true)
	status, err := service.RedeployStatus(ctx, fixture.app.ID)
	if err != nil || !status.Required || !strings.Contains(status.Reason, "${{project.TOKEN}}") {
		t.Fatalf("dependent not marked: %+v %v", status, err)
	}
	if status, _ := service.RedeployStatus(ctx, other.ID); status.Required || status.MarkedAt != nil {
		t.Fatalf("an application without the reference was marked: %+v", status)
	}
	if len(fixture.ex.Commands) != commandsBefore {
		t.Fatal("changing a shared variable must not touch the server")
	}
	if queued, _ := fixture.store.ListDeployments(ctx, fixture.app.ID, 10); len(queued) != 1 {
		t.Fatalf("a deployment was queued automatically: %d", len(queued))
	}
	time.Sleep(2 * time.Millisecond)
	fixture.deploy(t)
	if status, _ := service.RedeployStatus(ctx, fixture.app.ID); status.Required {
		t.Fatal("a successful deployment clears the redeploy status")
	}
	dependents, _ := service.SharedVariableDependents(ctx, envvar.SharedProject, "prj1", "TOKEN")
	if len(dependents) != 1 || dependents[0].ID != fixture.app.ID {
		t.Fatalf("dependents = %v", dependents)
	}
	listed, _ := service.ListSharedVariables(ctx, envvar.SharedProject, "prj1")
	if len(listed) != 1 || listed[0].Value != "" {
		t.Fatalf("list must mask secrets: %+v", listed)
	}
}

func TestSharedVariableMissingReferenceFailsBeforeBuild(t *testing.T) {
	config := gitConfig()
	fixture := newDeployFixture(t, config)
	owner := envvar.Owner{Type: envvar.OwnerApplication, ID: fixture.app.ID}
	if err := fixture.env.Set(context.Background(), owner, "API_URL", "${{environment.API_URL}}", true); err != nil {
		t.Fatal(err)
	}
	deployment := fixture.deploy(t)
	if deployment.Status != DeploymentFailed || !strings.Contains(deployment.Error, "API_URL usa ${{environment.API_URL}}") {
		t.Fatalf("missing reference: %s %q", deployment.Status, deployment.Error)
	}
	if fixture.ex.Ran("alpine/git") || fixture.ex.Ran("docker buildx") {
		t.Fatal("nothing may be cloned or built when a reference is missing")
	}
}

func TestSharedVariableDeleteMarksDependents(t *testing.T) {
	fixture, service := sharedFixture(t)
	ctx := context.Background()
	setSharedValue(t, service, envvar.SharedEnvironment, "env1", "REGION", "sa-east-1", false)
	if err := fixture.env.Set(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: fixture.app.ID}, "REGION", "${{environment.REGION}}", false); err != nil {
		t.Fatal(err)
	}
	fixture.deploy(t)
	time.Sleep(2 * time.Millisecond)
	if err := service.DeleteSharedVariable(ctx, envvar.SharedEnvironment, "env1", "REGION"); err != nil {
		t.Fatal(err)
	}
	if status, _ := service.RedeployStatus(ctx, fixture.app.ID); !status.Required {
		t.Fatal("deleting a referenced variable must mark its dependents")
	}
}
