package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox"
	"pier/internal/registry"
	"pier/internal/server"
)

type targetConnector map[string]*executor.Fake

func (c targetConnector) Executor(_ context.Context, id string) (executor.Executor, server.Server, error) {
	return c[id], server.Server{ID: id, DataDir: "/data/pier", ProxyToken: "token-" + id}, nil
}
func (c targetConnector) CheckDeployment(context.Context, string) error { return nil }
func TestPlacementEngineComposition(t *testing.T) {
	for _, failure := range []string{"", "build", "ready", "activate"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			config := DefaultConfig(PackImage)
			config.Source.Image = "nginx:1.27"
			config.Network.Domains = []string{"https://example.com"}
			config.Health.Enabled = true
			f := newDeployFixture(t, config)
			db := f.store.db
			if _, err := db.Exec(`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('srv2','two','local','/tmp','2026-01-01')`); err != nil {
				t.Fatal(err)
			}
			box, _ := secretbox.New("test-passphrase-123456")
			images := registry.New(db, box)
			if err := images.Save(ctx, registry.Input{Registry: registry.Registry{ID: "reg", Host: "registry.example.com", Username: "user"}, Password: "super-secret"}); err != nil {
				t.Fatal(err)
			}
			c := targetConnector{}
			for _, id := range []string{"srv1", "srv2"} {
				c[id] = executor.NewFake().On("mktemp -d", "/tmp/pier-registry-abc", 0).On(".Architecture", "amd64", 0).On("{{json .Config.Labels}}", "{}", 0).On(".State.Status", "running|healthy|0", 0)
			}
			if failure == "ready" {
				c["srv2"].On(".State.Status", "running|unhealthy|0", 0)
			}
			if failure == "activate" {
				c["srv2"].On("mv -f", "secret activation output", 1)
			}
			c["srv1"].On(".RepoDigests", `["registry.example.com/app@sha256:`+strings.Repeat("a", 64)+`"]`, 0)
			for _, ex := range c {
				ex.On("docker ps", "oldid\tprevious\toldimage\trunning\tUp\tapplication\t"+f.app.ID+"\tolddeployment", 0)
			}
			f.deployer.connector = c
			m := NewMultiServer(db, f.store, placementGuard{}, nil)
			f.deployer.SetMultiServer(m, images)
			if err := m.Save(ctx, f.app.ID, Placement{BuildServerID: "srv1", RegistryID: "reg", Repository: "app", Destinations: []string{"srv1", "srv2"}}); err != nil {
				t.Fatal(err)
			}
			image := "registry.example.com/app@sha256:" + strings.Repeat("a", 64)
			dep := Deployment{ID: newDeploymentID(), ApplicationID: f.app.ID, Trigger: TriggerRollback, RollbackImage: image}
			if failure == "build" {
				dep.Trigger = TriggerManual
				dep.RollbackImage = ""
			}
			if err := f.store.insertDeployment(ctx, dep); err != nil {
				t.Fatal(err)
			}
			log := &deployLog{store: f.store, hub: f.deployer.hub, deploymentID: dep.ID}
			err := f.deployer.execute(ctx, dep, log)
			if (failure == "" || failure == "build") != (err == nil) {
				t.Fatal(err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
			for id, ex := range c {
				if !ex.Ran("--config") {
					t.Fatalf("%s did not authenticate pull", id)
				}
				for name, data := range ex.Files {
					if strings.HasSuffix(name, "docker-compose.yaml") {
						if !strings.Contains(string(data), "traefik.enable: \"false\"") && !strings.Contains(string(data), "traefik.enable: 'false'") {
							t.Fatalf("candidate routes enabled: %s", data)
						}
					}
					if strings.HasSuffix(name, ".json.pending") {
						var doc map[string]any
						if json.Unmarshal(data, &doc) != nil {
							t.Fatal(string(data))
						}
					}
				}
				for _, cmd := range ex.Commands {
					if strings.Contains(cmd, "super-secret") {
						t.Fatal(cmd)
					}
				}
				if (failure == "ready" || failure == "activate") && ex.Ran("docker rm") {
					t.Fatal("retired old container during failed rollout")
				}
				if failure == "ready" && ex.Ran("mv -f") {
					t.Fatal("activated before all ready")
				}
			}
			if failure == "" || failure == "build" {
				service := NewService(f.store, f.env, c, NewDispatcher(f.store, f.deployer, f.deployer.logger, 0))
				service.SetMultiServer(m)
				for _, ex := range c {
					ex.On("docker ps", "cid\tapp\timage\trunning\tUp\tapplication\t"+f.app.ID+"\t"+dep.ID, 0)
				}
				if err := service.Stop(ctx, f.app.ID); err != nil {
					t.Fatal(err)
				}
				for _, ex := range c {
					if !ex.Ran("docker stop") {
						t.Fatal("destination not stopped")
					}
				}
				if err := m.CheckRecovery(ctx); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestPlacementRecoveryRefusesIncompleteJournal(t *testing.T) {
	config := DefaultConfig(PackImage)
	config.Source.Image = "nginx:1.27"
	f := newDeployFixture(t, config)
	m := NewMultiServer(f.store.db, f.store, placementGuard{}, nil)
	if err := m.record(context.Background(), f.app.ID, "srv1", "", "", "preparing"); err != nil {
		t.Fatal(err)
	}
	if m.CheckRecovery(context.Background()) == nil {
		t.Fatal("accepted incomplete journal")
	}
}
