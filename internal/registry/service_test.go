package registry

import (
	"context"
	"path/filepath"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"strings"
	"testing"
)

func TestCredentialIsolation(t *testing.T) {
	ctx := context.Background()
	db, e := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	box, _ := secretbox.New("a-long-enough-passphrase")
	s := New(db, box)
	if e = s.Save(ctx, Input{Registry: Registry{"test", "registry.example.com", "alice"}, Password: "secret-password"}); e != nil {
		t.Fatal(e)
	}
	var stored string
	db.QueryRow(`SELECT credential FROM registries`).Scan(&stored)
	if strings.Contains(stored, "secret-password") {
		t.Fatal("plaintext at rest")
	}
	ex := executor.NewFake().On("mktemp", "/tmp/pier-registry-abcdefgh", 0).On("pull", "secret-password", 1)
	e = s.Pull(ctx, "test", ex, "registry.example.com/app@sha256:"+strings.Repeat("a", 64))
	if e == nil || strings.Contains(e.Error(), "secret-password") {
		t.Fatal(e)
	}
	for _, cmd := range ex.Commands {
		if strings.Contains(cmd, "secret-password") {
			t.Fatal("secret in argv")
		}
	}
	if !ex.Ran("rm -rf") {
		t.Fatal("cleanup missing")
	}
}
func TestImmutable(t *testing.T) {
	for _, v := range []string{"app:latest", "registry.example.com/app@sha256:abc", "https://user:pass@host/app"} {
		if Immutable(v) {
			t.Fatal(v)
		}
	}
}
func TestPublishResolvesDigest(t *testing.T) {
	ctx := context.Background()
	db, e := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	box, _ := secretbox.New("a-long-enough-passphrase")
	s := New(db, box)
	if e = s.Save(ctx, Input{Registry: Registry{"test", "registry.example.com", "alice"}, Password: "secret-password"}); e != nil {
		t.Fatal(e)
	}
	want := "registry.example.com/app@sha256:" + strings.Repeat("a", 64)
	ex := executor.NewFake().On("mktemp", "/tmp/pier-registry-abcdefgh", 0).On("image inspect", `["`+want+`"]`, 0)
	got, e := s.Publish(ctx, "test", ex, "local:build", "app")
	if e != nil || got != want {
		t.Fatal(got, e)
	}
	if !ex.Ran(" push ") || !ex.Ran("rm -rf") {
		t.Fatal(ex.Commands)
	}
}
