package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenUsesTheExactPathEvenWithURISeparators(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data#1?x%")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pier.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created at %q: %v", path, err)
	}
}
