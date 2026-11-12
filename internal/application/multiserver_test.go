package application

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type placementGuard struct{}

func (placementGuard) CheckDeploy(context.Context, string) error { return nil }

type placementFake struct {
	readyFail, activateFail string
	restored, discarded     []string
}

func (f *placementFake) Architecture(context.Context, string) (string, error)       { return "amd64", nil }
func (f *placementFake) Prepare(context.Context, Application, string, string) error { return nil }
func (f *placementFake) Ready(_ context.Context, _ Application, target, _ string) error {
	if target == f.readyFail {
		return errors.New("secret")
	}
	return nil
}
func (f *placementFake) Activate(_ context.Context, _ Application, target, _ string) error {
	if target == f.activateFail {
		return errors.New("secret")
	}
	return nil
}
func (f *placementFake) Restore(_ context.Context, _ Application, target, _ string) error {
	f.restored = append(f.restored, target)
	return nil
}
func (f *placementFake) Discard(_ context.Context, _ Application, target, _ string) error {
	f.discarded = append(f.discarded, target)
	return nil
}
func TestPlacementFailures(t *testing.T) {
	for _, activate := range []bool{false, true} {
		t.Run(map[bool]string{false: "readiness", true: "activation"}[activate], func(t *testing.T) {
			ctx := context.Background()
			config := DefaultConfig(PackImage)
			config.Source.Image = "nginx:1.27"
			fixture := newDeployFixture(t, config)
			db := fixture.store.db
			for _, q := range []string{`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('srv2','two','local','/tmp','2026-01-01')`, `INSERT INTO registries VALUES('reg','registry.example.com','user','unused')`} {
				if _, e := db.Exec(q); e != nil {
					t.Fatal(e)
				}
			}
			f := &placementFake{}
			if activate {
				f.activateFail = "srv2"
			} else {
				f.readyFail = "srv2"
			}
			s := NewMultiServer(db, fixture.store, placementGuard{}, f)
			id := fixture.app.ID
			if e := s.Save(ctx, id, Placement{BuildServerID: "srv1", RegistryID: "reg", Repository: "app", Destinations: []string{"srv1", "srv2"}}); e != nil {
				t.Fatal(e)
			}
			old := "registry.example.com/app@sha256:" + strings.Repeat("b", 64)
			for _, target := range []string{"srv1", "srv2"} {
				if e := s.record(ctx, id, target, old, "", "healthy"); e != nil {
					t.Fatal(e)
				}
			}
			e := s.Rollout(ctx, id, "registry.example.com/app@sha256:"+strings.Repeat("a", 64))
			if e == nil || strings.Contains(e.Error(), "secret") {
				t.Fatal(e)
			}
			statuses, e := s.Status(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			for _, status := range statuses {
				if status.Image != old || status.Status != "preserved" {
					t.Fatal(status)
				}
			}
			if len(f.discarded) != 2 {
				t.Fatal(f.discarded)
			}
			if activate && len(f.restored) != 2 {
				t.Fatal(f.restored)
			}
			if !activate && len(f.restored) != 0 {
				t.Fatal(f.restored)
			}
		})
	}
}
