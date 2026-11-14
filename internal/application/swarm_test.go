package application

import (
	"context"
	"pier/internal/docker"
	"pier/internal/platform/executor"
	"strings"
	"testing"
	"time"
)

func TestSwarmContainerReadinessRequiresActiveTasks(t *testing.T) {
	for _, names := range [][]string{nil, {"service"}} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := (&Deployer{}).swarmContainerReadiness(ctx, executor.NewFake(), nil, names); err == nil {
			t.Fatal("accepted readiness without active tasks")
		}
	}
}

func TestSwarmComposeIsolationAndSecrets(t *testing.T) {
	c := DefaultConfig(PackImage)
	c.Network.Domains = []string{"https://example.test"}
	data, names, err := swarmCompose(releaseSpec{App: Application{ID: "app-one", Config: c}, ContainerName: "app", Image: "nginx:stable", Env: map[string]string{"TOKEN": "secret$value"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"pier.id: app-one", "deploy:", "traefik.swarm.network", docker.SwarmNetwork, "secret$$value"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	if len(names) != 1 || names[0] != "pier-app-one_app" || strings.Contains(text, "container_name") {
		t.Fatal(names, text)
	}
	log := &deployLog{}
	log.redact("secret$value")
	if strings.Contains(log.mask("error secret$value"), "secret$value") {
		t.Fatal("secret disclosed")
	}
}

func TestSwarmReadinessFailureAndSuccess(t *testing.T) {
	ex := executor.NewFake().On("service inspect", "paused", 0)
	if err := swarmReady(context.Background(), ex, []string{"pier-app_app"}); err == nil {
		t.Fatal("paused accepted")
	}
	ex = executor.NewFake().On("service inspect", "completed", 0).On("service ps", "id\tapp\tnode\trunning\tRunning 10 seconds ago\t\tnginx", 0)
	if err := swarmReady(context.Background(), ex, []string{"pier-app_app"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := swarmReady(ctx, executor.NewFake(), []string{"pier-app_app"}); err == nil {
		t.Fatal("missing task accepted")
	}
}

func TestApplicationDispatchSelectsSwarm(t *testing.T) {
	c := DefaultConfig(PackImage)
	c.Source.Image = "nginx:stable"
	f := newPlacementFixture(t, c)
	ctx := context.Background()
	_, err := f.store.db.Exec(`INSERT INTO swarm_clusters(id,name,advertise_addr,created_at) VALUES('sw','sw','10.0.0.1','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.multi.Save(ctx, f.app.ID, Placement{SwarmClusterID: "sw"})
	if err != nil {
		t.Fatal(err)
	}
	if p.SwarmClusterID != "sw" {
		t.Fatal(p)
	}
	// A Swarm target without an available manager must fail before a local container release.
	if err = f.deploy(t, Deployment{Trigger: TriggerManual}); err == nil {
		t.Fatal("fell through to local release")
	}
	if f.ex.Ran("compose up") {
		t.Fatal(f.ex.Commands)
	}
	if _, err = f.multi.Save(ctx, "foreign-app", p); err == nil {
		t.Fatal("unknown application accepted")
	}
}

func TestSwarmSuccessfulDeployAndReadinessCompensation(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "readiness-failure"}[failure], func(t *testing.T) {
			c := DefaultConfig(PackImage)
			c.Source.Image = "nginx:stable"
			f := newPlacementFixture(t, c)
			_, err := f.store.db.Exec(`INSERT INTO swarm_clusters(id,name,advertise_addr,created_at) VALUES('sw','sw','10.0.0.1','2026-01-01T00:00:00Z'); INSERT INTO swarm_nodes(server_id,cluster_id,node_id,role,joined_at) VALUES('srv1','sw','abcdefghijklmnopqrstuvwxy','manager','2026-01-01T00:00:00Z')`)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.multi.Save(context.Background(), f.app.ID, Placement{SwarmClusterID: "sw"}); err != nil {
				t.Fatal(err)
			}
			image := digestRef("acme/test", "a")
			f.ex.On("docker inspect --type task", "abcdefghijklmnopqrstuvwxy|container123", 0).On(".Swarm.LocalNodeState", "active\tabcdefghijklmnopqrstuvwxy\ttrue", 0).On("mktemp -d /tmp/pier-swarm-", "/tmp/pier-swarm-abcdefghij", 0).On("service inspect", "completed", 0).On("service ps", "id\tapp\tnode\trunning\tRunning 10 seconds ago\t\t"+image, 0)
			if failure {
				f.ex.On("service inspect", "paused", 0)
			}
			err = f.deploy(t, Deployment{Trigger: TriggerRollback, RollbackImage: image})
			if failure {
				if err == nil || !f.ex.Ran("stack rm") {
					t.Fatal("missing compensation", err, f.ex.Commands)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				app, e := f.store.Get(context.Background(), f.app.ID)
				if e != nil || app.CurrentImage != image {
					t.Fatal(app, e)
				}
			}
			if !f.ex.Ran("stack deploy --with-registry-auth") || f.ex.Ran("compose up") {
				t.Fatal(f.ex.Commands)
			}
		})
	}
}
