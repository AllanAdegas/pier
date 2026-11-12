package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

func TestPanelFileProviderUsesDedicatedNetworkBackendWithoutCapability(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := server.NewService(server.NewStore(db), nil, nil)
	dir := t.TempDir()
	if err = svc.EnsureLocal(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if err = configurePanel(context.Background(), svc, "panel.example.com"); err != nil {
		t.Fatal(err)
	}
	all, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(all[0].ProxyDir(), "dynamic", "pier-panel.yaml")
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = yaml.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "http://pier-panel:8700") || strings.Contains(string(b), "pier.proxy") || !strings.Contains(string(b), "certResolver: letsencrypt") {
		t.Fatalf("config %s", b)
	}
	info, _ := os.Stat(name)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	for _, domain := range []string{"example.com/path", "example.com`)", "https://example.com"} {
		if err = configurePanel(context.Background(), svc, domain); err == nil {
			t.Fatalf("accepted %s", domain)
		}
	}
	if err = configurePanel(context.Background(), svc, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("stale panel route not removed")
	}
}
