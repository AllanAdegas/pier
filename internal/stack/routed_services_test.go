package stack

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"pier/internal/server"
)

func routedWhoami() CreateInput {
	return CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "web", Template: "whoami",
		Routes: []Route{{Service: "whoami", Port: 80, Domain: "https://whoami.example.com"}}}
}

func configWith(t *testing.T, labels map[string]string) string {
	t.Helper()
	output, err := json.Marshal(map[string]any{"services": map[string]any{
		"whoami": map[string]any{"image": "traefik/whoami:v1.12.0", "labels": labels}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

// deployRouted creates and deploys the whoami stack, returning its log and
// whether `docker compose up` ran.
func deployRouted(t *testing.T, service *Service, fake interface{ Ran(string) bool }, input CreateInput) (string, string, bool) {
	t.Helper()
	ctx := context.Background()
	stack, err := service.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := service.Deploy(ctx, stack.ID); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	snapshot := waitOperation(t, service, stack.ID)
	return snapshot.Error, strings.Join(snapshot.Lines, "\n"), fake.Ran(" up ")
}

func TestAnImageWithReservedLabelsIsRefusedBeforeTheStackStarts(t *testing.T) {
	for _, label := range []string{"traefik.http.routers.evil.rule", "pier.id"} {
		service, fake := newService(t)
		fake.On("config --format json", configWith(t, nil), 0)
		fake.On("docker image inspect", `{"`+label+`":"x","org.opencontainers.image.title":"ok"}`, 0)
		failure, _, started := deployRouted(t, service, fake, routedWhoami())
		if started {
			t.Errorf("%s: the stack must not start", label)
		}
		for _, want := range []string{label, `"whoami"`, "traefik/whoami:v1.12.0"} {
			if !strings.Contains(failure, want) {
				t.Errorf("%s: the error must name %q: %s", label, want, failure)
			}
		}
	}
}

func TestReservedLabelsWrittenInTheComposeOfARoutedServiceAreRefused(t *testing.T) {
	service, fake := newService(t)
	fake.On("config --format json", configWith(t, map[string]string{"traefik.http.routers.evil.rule": "Host(`bank.com`)"}), 0)
	fake.On("docker image inspect", "null", 0)
	failure, _, started := deployRouted(t, service, fake, routedWhoami())
	if started || !strings.Contains(failure, "traefik.http.routers.evil.rule") || !strings.Contains(failure, `"whoami"`) {
		t.Errorf("started=%v, error: %s", started, failure)
	}
}

func TestTheLabelsPierGeneratesAreNotMistakenForReservedOnes(t *testing.T) {
	ctx := context.Background()
	service, fake := newService(t)
	stack, err := service.Create(ctx, routedWhoami())
	if err != nil {
		t.Fatal(err)
	}
	reached, err := service.reach(ctx, stack.ID)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := publish(reached.stack, "whoami", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	fake.On("config --format json", configWith(t, generated.labels), 0)
	fake.On("docker image inspect", "null", 0)
	if err := service.Deploy(ctx, stack.ID); err != nil {
		t.Fatal(err)
	}
	if snapshot := waitOperation(t, service, stack.ID); snapshot.Error != "" || !fake.Ran(" up ") {
		t.Errorf("error: %s", snapshot.Error)
	}
}

func TestServicesWithoutRoutesAreNotInspected(t *testing.T) {
	service, fake := newService(t)
	input := routedWhoami()
	input.Routes = nil
	failure, _, started := deployRouted(t, service, fake, input)
	if failure != "" || !started || fake.Ran("docker image inspect") {
		t.Errorf("started=%v inspected=%v, error: %s", started, fake.Ran("docker image inspect"), failure)
	}
}

func TestADirectoryStackBuildsBeforeItsImagesAreInspected(t *testing.T) {
	ctx := context.Background()
	service, fake := newService(t)
	fake.Files[folder+"/compose.yaml"] = []byte(folderCompose)
	fake.On("config --services", "web\nworker", 0)
	fake.On("config --format json", `{"services":{"web":{}}}`, 0)
	fake.On("docker image inspect", `{"traefik.enable":"true"}`, 0)
	stack, err := service.Create(ctx, directoryInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Deploy(ctx, stack.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := waitOperation(t, service, stack.ID)
	if !strings.Contains(snapshot.Error, "traefik.enable") || fake.Ran(" up ") {
		t.Errorf("a built image with reserved labels must stop the deployment: %s", snapshot.Error)
	}
	built, inspected := -1, -1
	for index, command := range fake.Commands {
		if strings.HasSuffix(command, " build") || strings.Contains(command, " build ") && built < 0 {
			built = index
		}
		if strings.Contains(command, "docker image inspect") {
			inspected = index
		}
	}
	if built < 0 || inspected < built {
		t.Errorf("build must come first (build=%d, inspect=%d):\n%s", built, inspected, strings.Join(fake.Commands, "\n"))
	}
	if !fake.Ran("shop-") && !fake.Ran(stack.composeProject()+"-web") {
		t.Errorf("a service without image is inspected under the compose default name:\n%s", strings.Join(fake.Commands, "\n"))
	}
}

type fakeDomains struct {
	result server.DomainCheck
	err    error
	asked  []string
}

func (f *fakeDomains) CheckDomain(_ context.Context, _, domain string) (server.DomainCheck, error) {
	f.asked = append(f.asked, domain)
	return f.result, f.err
}

func deployWithDomains(t *testing.T, checker *fakeDomains) (rendered, log string, asked []string) {
	t.Helper()
	service, fake := newService(t)
	service.SetDomainChecker(checker)
	fake.On("config --format json", configWith(t, nil), 0)
	fake.On("docker image inspect", "null", 0)
	input := routedWhoami()
	input.Routes = append(input.Routes, Route{Service: "whoami", Port: 80, Domain: "https://whoami.example.com/api"})
	stack, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Deploy(context.Background(), stack.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := waitOperation(t, service, stack.ID)
	if snapshot.Error != "" {
		t.Fatalf("deploy failed: %s", snapshot.Error)
	}
	return string(fake.Files["/data/pier/stacks/"+stack.ID+"/docker-compose.yaml"]), strings.Join(snapshot.Lines, "\n"), checker.asked
}

func TestAHostThatDoesNotPointToTheServerIsPublishedOverHTTPWithTheRecordHint(t *testing.T) {
	checker := &fakeDomains{result: server.DomainCheck{OK: false, RecordHint: "Crie um registro A para whoami.example.com apontando para 203.0.113.7."}}
	rendered, log, asked := deployWithDomains(t, checker)
	if strings.Contains(rendered, "certresolver") {
		t.Errorf("no certificate may be requested yet:\n%s", rendered)
	}
	if !strings.Contains(rendered, "whoami.example.com") {
		t.Errorf("the route must still be published:\n%s", rendered)
	}
	if !strings.Contains(log, "Crie um registro A para whoami.example.com apontando para 203.0.113.7.") {
		t.Errorf("the log must carry the exact record hint:\n%s", log)
	}
	if len(asked) != 1 {
		t.Errorf("each host is checked once, got %v", asked)
	}
}

func TestAHostThatPointsToTheServerKeepsItsCertificate(t *testing.T) {
	rendered, log, _ := deployWithDomains(t, &fakeDomains{result: server.DomainCheck{OK: true}})
	if !strings.Contains(rendered, "certresolver") || strings.Contains(log, "ainda não aponta") {
		t.Errorf("rendered:\n%s\nlog:\n%s", rendered, log)
	}
}

func TestAFailedDnsCheckKeepsTheRouteAsConfigured(t *testing.T) {
	rendered, log, _ := deployWithDomains(t, &fakeDomains{err: errors.New("resolver down")})
	if !strings.Contains(rendered, "certresolver") || !strings.Contains(log, "Não foi possível verificar o DNS") {
		t.Errorf("rendered:\n%s\nlog:\n%s", rendered, log)
	}
}
