package project

import (
	"context"
	"path/filepath"
	"testing"

	"pier/internal/platform/sqlite"
)

func TestEnvironmentProtectionPersistsAndGuardsOperations(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewService(db)
	p, err := s.Create(ctx, "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	id := p.Environments[0].ID
	if err = s.CheckDeploy(ctx, id); err != nil {
		t.Fatal(err)
	}
	e, err := s.SetProtection(ctx, id, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Frozen || !e.Protected {
		t.Fatal("flags not persisted")
	}
	if err = s.CheckDeploy(ctx, id); err == nil {
		t.Fatal("allowed frozen deployment")
	}
	if err = s.CheckDestroy(ctx, id, ""); err == nil {
		t.Fatal("allowed unconfirmed removal")
	}
	if err = s.CheckDestroy(ctx, id, e.Name); err != nil {
		t.Fatal(err)
	}
	listed, err := s.List(ctx)
	if err != nil || !listed[0].Environments[0].Frozen {
		t.Fatalf("list: %+v %v", listed, err)
	}
	if err = s.CheckDeploy(ctx, "missing"); err == nil {
		t.Fatal("missing environment allowed")
	}
}

func TestUnfreezingRequiresAReasonAtTheStorageBoundary(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewService(db)
	p, err := s.Create(ctx, "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	id := p.Environments[0].ID
	if _, err = s.SetProtection(ctx, id, false, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetProtection(ctx, id, false, false); err == nil {
		t.Fatal("unfroze without a reason")
	}
	if _, err = s.SetProtection(ctx, id, false, false, "maintenance complete"); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckDeploy(ctx, id); err != nil {
		t.Fatal(err)
	}
}
