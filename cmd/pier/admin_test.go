package main

import (
	"context"
	"os"
	"path/filepath"
	"pier/internal/identity"
	"pier/internal/instance"
	"pier/internal/platform/sqlite"
	"testing"
	"time"
)

func TestAdminPasswordResetRequiresOfflineLockAndInvalidatesSessions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.Open(ctx, filepath.Join(dir, "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	service := identity.NewService(identity.NewStore(db), time.Hour)
	user, err := service.CreateUser(ctx, "Owner", "owner@example.com", "old-password-123")
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.OpenSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	file := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(file, []byte("new-password-456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"pier", "admin", "reset-password", "--data-dir", dir, "--email", user.Email, "--password-file", file}
	unlock, err := instance.Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := runAdmin(); err == nil {
		t.Fatal("reset accepted active instance")
	}
	unlock()
	if err := runAdmin(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlite.Open(ctx, filepath.Join(dir, "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service = identity.NewService(identity.NewStore(db), time.Hour)
	if _, err := service.AuthenticateSession(ctx, token); err == nil {
		t.Fatal("old session still valid")
	}
	if _, err := service.Login(ctx, user.Email, "new-password-456", identity.Client{}); err != nil {
		t.Fatal(err)
	}
}
