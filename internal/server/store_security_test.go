package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/sqlite"
)

func securityStore(t *testing.T) (*Store, Server) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := NewStore(db)
	srv := Server{ID: "remote", Name: "remote", Kind: KindSSH, Host: "192.0.2.1", Port: 22, User: "root", Proxy: DefaultProxySettings()}
	if err := store.insert(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
	srv, err = store.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	return store, srv
}
func TestStaleServerUpdatePreservesPinAndEndpointChangeClearsIt(t *testing.T) {
	store, stale := securityStore(t)
	ctx := context.Background()
	if err := store.setHostKey(ctx, stale.ID, "pin", stale.Host, stale.Port); err != nil {
		t.Fatal(err)
	}
	stale.Name = "renamed"
	if err := store.update(ctx, stale); err != nil {
		t.Fatal(err)
	}
	current, _ := store.Get(ctx, stale.ID)
	if current.HostKey != "pin" {
		t.Fatal("stale update erased pin")
	}
	stale.Port++
	if err := store.update(ctx, stale); err != nil {
		t.Fatal(err)
	}
	current, _ = store.Get(ctx, stale.ID)
	if current.HostKey != "" {
		t.Fatal("endpoint change did not clear pin")
	}
	if err := store.setHostKey(ctx, stale.ID, "old-endpoint", stale.Host, stale.Port-1); err == nil {
		t.Fatal("old endpoint pinned")
	}
	if err := store.setHostKey(ctx, stale.ID, "new-endpoint", stale.Host, stale.Port); err != nil {
		t.Fatal(err)
	}
	if err := store.setHostKey(ctx, stale.ID, "overwrite", stale.Host, stale.Port); err == nil {
		t.Fatal("existing pin overwritten")
	}
	if err := store.setHostKey(ctx, "absent", "pin", stale.Host, stale.Port); err == nil {
		t.Fatal("missing row accepted")
	}
}
func TestProxyTokenIsStoredStableAndNeverSerialized(t *testing.T) {
	store, srv := securityStore(t)
	if len(srv.ProxyToken) < 32 {
		t.Fatal("route token is missing or too short")
	}
	if err := store.update(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
	again, _ := store.Get(context.Background(), srv.ID)
	if again.ProxyToken != srv.ProxyToken {
		t.Fatal("token changed on update")
	}
	output, _ := json.Marshal(srv)
	if strings.Contains(string(output), srv.ProxyToken) {
		t.Fatal("server JSON exposes routing token")
	}
	file := ProxyCompose(srv)
	command := strings.Join(file.Services["traefik"].Command, " ")
	if !strings.Contains(command, "--providers.docker.constraints=Label(`pier.proxy`, `"+srv.ProxyToken+"`)") {
		t.Fatal("provider constraint missing")
	}
	if ProxyRouteLabels(srv)[ProxyLabel] != srv.ProxyToken {
		t.Fatal("renderer helper does not match provider")
	}
	if file.Services["traefik"].Networks[PanelNetwork] == nil {
		t.Fatal("dedicated panel network missing")
	}
}
func TestValidationChecksSurviveServerUpdates(t *testing.T) {
	store, srv := securityStore(t)
	checks := []Check{{Code: "disk", Status: "warning", Message: "disk", Fix: "cleanup"}}
	if err := store.setChecks(context.Background(), srv.ID, checks); err != nil {
		t.Fatal(err)
	}
	if err := store.update(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
	srv, err := store.Get(context.Background(), srv.ID)
	if err != nil || len(srv.Checks) != 1 || srv.Checks[0].Code != "disk" {
		t.Fatalf("checks=%+v err=%v", srv.Checks, err)
	}
}
