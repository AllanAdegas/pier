package server

import (
	"context"
	"path/filepath"
	"testing"

	"pier/internal/platform/sqlite"
)

// The local server's directory follows PIER_DATA_DIR. When the data
// directory moves (here, the project folder was renamed), the record must
// follow it, or every deployment would write to a path that is gone.
func TestEnsureLocalFollowsTheDataDirectory(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := NewStore(db)
	service := NewService(store, nil, nil)

	if err := service.EnsureLocal(ctx, "/old/cais/.data/runtime"); err != nil {
		t.Fatal(err)
	}
	remote := Server{ID: "remote1", Name: "vps", Kind: KindSSH, Host: "203.0.113.9", Port: 22, User: "root",
		DataDir: "/data/cais", Proxy: DefaultProxySettings()}
	if err := store.insert(ctx, remote); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureLocal(ctx, "/new/pier/.data/runtime"); err != nil {
		t.Fatal(err)
	}
	servers, _ := store.List(ctx)
	if len(servers) != 2 {
		t.Fatalf("no server must be created or lost: %+v", servers)
	}
	for _, server := range servers {
		switch server.Kind {
		case KindLocal:
			if server.DataDir != "/new/pier/.data/runtime" {
				t.Errorf("the local server must follow the data directory: %q", server.DataDir)
			}
		default:
			// A remote server's directory is the user's choice and stays.
			if server.DataDir != "/data/cais" {
				t.Errorf("a remote server's directory must not change: %q", server.DataDir)
			}
		}
	}
}
