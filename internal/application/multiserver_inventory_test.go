package application

import (
	"context"
	"testing"
)

func TestPlacementInventoryFailureDoesNotMutateContainers(t *testing.T) {
	cfg := DefaultConfig(PackImage)
	cfg.Source.Image = "nginx:1.27"
	p := newPlacementFixture(t, cfg, "srv2")
	if _, err := p.multi.Save(context.Background(), p.app.ID, Placement{Destinations: []string{"srv2"}}); err != nil {
		t.Fatal(err)
	}
	p.servers["srv1"].On("docker ps", "inventory unavailable", 1)
	err := p.deploy(t, Deployment{Trigger: TriggerRollback, RollbackImage: digestRef("acme/api", "a")})
	if err == nil {
		t.Fatal("unknown previous version must abort deployment")
	}
	for id, ex := range p.servers {
		if ex.Ran("up --detach") || ex.Ran("docker rm -f") {
			t.Fatalf("server %s mutated despite unknown inventory: %v", id, ex.Commands)
		}
	}
}
