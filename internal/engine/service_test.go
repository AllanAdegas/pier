package engine

import (
	"context"
	"strings"
	"testing"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/server"
)

type fakeConnector struct{ ex *executor.Fake }

func (f fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return f.ex, server.Server{ID: "srv1"}, nil
}

func newService() (*Service, *executor.Fake) {
	fake := executor.NewFake()
	return NewService(fakeConnector{fake}), fake
}

const psOutput = `{"ID":"aaa111aaa111aaa1","Names":"blog","Image":"ghost:5","Command":"\"docker-entrypoint.s…\"","State":"running","Status":"Up 2 days","Ports":"0.0.0.0:2368->2368/tcp","Networks":"bridge","CreatedAt":"2026-10-10 10:00:00 -0300 -03","Labels":"maintainer=x,y,com.docker.compose.project=site"}
{"ID":"bbb222bbb222bbb2","Names":"app1-dep1","Image":"pier/app1:dep1","Command":"\"node\"","State":"running","Status":"Up 1 hour","Ports":"","Networks":"pier","CreatedAt":"2026-10-12 09:00:00 -0300 -03","Labels":"pier.id=app1,pier.managed=true,pier.type=application"}`

func kindOf(err error) apperr.Kind {
	if appErr, ok := apperr.As(err); ok {
		return appErr.Kind
	}
	return ""
}

func TestContainersListsEverythingAndFlagsWhatPierOwns(t *testing.T) {
	service, fake := newService()
	fake.On("docker ps", psOutput, 0)

	containers, err := service.Containers(context.Background(), "srv1")
	if err != nil {
		t.Fatalf("Containers: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("got %d containers, want 2", len(containers))
	}
	foreign, managed := containers[0], containers[1]
	if foreign.Managed || foreign.Owner != nil || foreign.ComposeProject != "site" || foreign.Name != "blog" {
		t.Errorf("unmanaged container misread: %+v", foreign)
	}
	if !managed.Managed || managed.Owner == nil || managed.Owner.Type != "application" || managed.Owner.ID != "app1" {
		t.Errorf("managed container misread: %+v", managed)
	}
	if !strings.Contains(fake.Commands[0], "--all") {
		t.Errorf("stopped containers must be listed too: %s", fake.Commands[0])
	}
}

func TestRemoveRefusesContainersOwnedByPier(t *testing.T) {
	service, fake := newService()
	fake.On("docker ps", psOutput, 0)

	if err := service.Remove(context.Background(), "srv1", "app1-dep1", true); kindOf(err) != apperr.KindConflict {
		t.Fatalf("removing a managed container must conflict, got %v", err)
	}
	if fake.Ran("docker rm") {
		t.Fatal("docker rm must not run for a managed container")
	}
	if err := service.Remove(context.Background(), "srv1", "blog", false); err != nil {
		t.Fatalf("removing a foreign container: %v", err)
	}
	if !fake.Ran("docker rm aaa111aaa111aaa1") || fake.Ran("--force") {
		t.Errorf("expected a plain rm by id; ran:\n%s", strings.Join(fake.Commands, "\n"))
	}
	if err := service.Remove(context.Background(), "srv1", "ghost-container", false); kindOf(err) != apperr.KindNotFound {
		t.Errorf("unknown container must be not found, got %v", err)
	}
}

// Identifiers arrive from the URL and go straight to a command line.
func TestIdentifiersThatLookLikeOptionsAreRejected(t *testing.T) {
	service, fake := newService()
	for _, id := range []string{"--privileged", "-f", "a b", "x;id", "$(id)", ""} {
		if err := service.Act(context.Background(), "srv1", id, "stop"); kindOf(err) != apperr.KindNotFound {
			t.Errorf("Act(%q) = %v, want not found", id, err)
		}
		if _, err := service.Inspect(context.Background(), "srv1", id); kindOf(err) != apperr.KindNotFound {
			t.Errorf("Inspect(%q) = %v, want not found", id, err)
		}
		if _, err := service.Exec(context.Background(), "srv1", id, "ls"); kindOf(err) != apperr.KindNotFound {
			t.Errorf("Exec(%q) = %v, want not found", id, err)
		}
	}
	if len(fake.Commands) != 0 {
		t.Errorf("nothing must reach the server, ran: %v", fake.Commands)
	}
	if err := service.Act(context.Background(), "srv1", "blog", "explode"); kindOf(err) != apperr.KindNotFound {
		t.Errorf("unknown action must be not found, got %v", err)
	}
}

func TestRunBuildsOneArgumentPerValue(t *testing.T) {
	service, fake := newService()
	fake.On("docker run", "f00dfeed", 0)
	id, err := service.Run(context.Background(), "srv1", RunSpec{
		Name: "cache", Image: "redis:7.4-alpine", Command: "redis-server --appendonly yes",
		Env: []string{"TZ=America/Sao_Paulo", "MOTD=hello world; rm -rf /"}, Ports: []string{"6379:6379"},
		Volumes: []string{"cache-data:/data", "/srv/conf:/etc/redis:ro"}, Network: "backend",
	})
	if err != nil || id != "f00dfeed" {
		t.Fatalf("Run = %q, %v", id, err)
	}
	command := fake.Commands[0]
	for _, expected := range []string{
		"docker run --detach --name cache --restart unless-stopped --label pier.standalone=true --network backend",
		"--env TZ=America/Sao_Paulo", `--env 'MOTD=hello world; rm -rf /'`, "--publish 6379:6379",
		"--volume cache-data:/data", "--volume /srv/conf:/etc/redis:ro", "redis:7.4-alpine redis-server --appendonly yes",
	} {
		if !strings.Contains(command, expected) {
			t.Errorf("command misses %q:\n%s", expected, command)
		}
	}
}

func TestRunRejectsUnsafeSpecs(t *testing.T) {
	service, fake := newService()
	valid := RunSpec{Name: "web", Image: "nginx:1.27"}
	cases := map[string]func(*RunSpec){
		"name":    func(s *RunSpec) { s.Name = "--rm" },
		"image":   func(s *RunSpec) { s.Image = "nginx --privileged" },
		"network": func(s *RunSpec) { s.Network = "host --pid=host" },
		"restart": func(s *RunSpec) { s.Restart = "sometimes" },
		"env":     func(s *RunSpec) { s.Env = []string{"no-equals"} },
		"ports":   func(s *RunSpec) { s.Ports = []string{"80"} },
		"volumes": func(s *RunSpec) { s.Volumes = []string{"data:/x:rw,--privileged"} },
	}
	for field, mutate := range cases {
		spec := valid
		mutate(&spec)
		_, err := service.Run(context.Background(), "srv1", spec)
		appErr, ok := apperr.As(err)
		if !ok || appErr.Fields[field] == "" {
			t.Errorf("expected a validation error on %q, got %v", field, err)
		}
	}
	if len(fake.Commands) != 0 {
		t.Errorf("an invalid spec must not reach docker: %v", fake.Commands)
	}
}

func TestExecReportsFailureAsOutput(t *testing.T) {
	service, fake := newService()
	fake.On("docker exec", "ls: /nope: No such file or directory", 2)
	output, err := service.Exec(context.Background(), "srv1", "blog", "ls /nope")
	if err != nil {
		t.Fatalf("a failing command is a normal outcome, got error %v", err)
	}
	if !strings.Contains(output, "No such file") || !strings.Contains(output, "status 2") {
		t.Errorf("output must carry what was printed and the exit status: %q", output)
	}
	if !strings.Contains(fake.Commands[0], "docker exec blog sh -c 'ls /nope'") {
		t.Errorf("command must run as a single shell argument: %s", fake.Commands[0])
	}
}

func TestNetworksReservedForDockerAndPierCannotBeRemoved(t *testing.T) {
	service, fake := newService()
	fake.On("docker network ls", `{"ID":"n1","Name":"bridge","Driver":"bridge","Scope":"local","Labels":""}
{"ID":"n2","Name":"pier","Driver":"bridge","Scope":"local","Labels":""}
{"ID":"n3","Name":"stk1_ingress","Driver":"bridge","Scope":"local","Labels":"pier.ingress=true"}
{"ID":"n4","Name":"mine","Driver":"bridge","Scope":"local","Labels":""}`, 0)

	for _, name := range []string{"bridge", "pier", "stk1_ingress"} {
		if err := service.RemoveNetwork(context.Background(), "srv1", name); kindOf(err) != apperr.KindConflict {
			t.Errorf("RemoveNetwork(%q) = %v, want conflict", name, err)
		}
	}
	if fake.Ran("docker network rm") {
		t.Fatal("reserved networks must never be removed")
	}
	if err := service.RemoveNetwork(context.Background(), "srv1", "mine"); err != nil {
		t.Fatalf("RemoveNetwork(mine): %v", err)
	}
	if !fake.Ran("docker network rm n4") {
		t.Error("the user's own network must be removable")
	}
}

func TestImagesAndReference(t *testing.T) {
	service, fake := newService()
	fake.On("docker images", `{"ID":"sha256:1","Repository":"nginx","Tag":"1.27","Size":"190MB","CreatedSince":"2 weeks ago"}
{"ID":"0a1b2c3d","Repository":"<none>","Tag":"<none>","Size":"12MB","CreatedSince":"3 days ago"}`, 0)
	images, err := service.Images(context.Background(), "srv1")
	if err != nil || len(images) != 2 {
		t.Fatalf("Images = %v, %v", images, err)
	}
	if images[0].Reference() != "nginx:1.27" || images[1].Reference() != "0a1b2c3d" {
		t.Errorf("references = %q, %q", images[0].Reference(), images[1].Reference())
	}
	if _, err := service.PullImage(context.Background(), "srv1", "nginx; id"); kindOf(err) != apperr.KindInvalid {
		t.Errorf("an invalid reference must be rejected, got %v", err)
	}
}

func TestRemovingWhatDoesNotExistIsNotFoundAndWhatIsInUseIsAConflict(t *testing.T) {
	service, fake := newService()
	ctx := context.Background()
	fake.On("docker rmi", "Error response from daemon: No such image: ghost:1", 1)
	if err := service.RemoveImage(ctx, "srv1", "ghost:1", false); kindOf(err) != apperr.KindNotFound {
		t.Errorf("a missing image must be not found, got %v", err)
	}
	fake.On("docker volume rm", "Error response from daemon: remove data: volume is in use - [3f2a]", 1)
	if err := service.RemoveVolume(ctx, "srv1", "data"); kindOf(err) != apperr.KindConflict {
		t.Errorf("a volume in use must be a conflict, got %v", err)
	}
}
