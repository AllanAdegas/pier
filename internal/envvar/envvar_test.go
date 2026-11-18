package envvar

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box := testbox.Box(t)
	return NewStore(db, box)
}

func TestParseDotenv(t *testing.T) {
	variables, err := ParseDotenv(`
# comment
PLAIN=value
export EXPORTED=1
QUOTED="with spaces"
SINGLE='single'
EMPTY=
URL=postgres://u:p@h/db?x=1&y=2
`)
	if err != nil {
		t.Fatalf("ParseDotenv: %v", err)
	}
	want := map[string]string{
		"PLAIN": "value", "EXPORTED": "1", "QUOTED": "with spaces", "SINGLE": "single", "EMPTY": "",
		"URL": "postgres://u:p@h/db?x=1&y=2",
	}
	if len(variables) != len(want) {
		t.Fatalf("got %d variables, want %d: %+v", len(variables), len(want), variables)
	}
	for _, variable := range variables {
		if want[variable.Key] != variable.Value {
			t.Errorf("%s = %q, want %q", variable.Key, variable.Value, want[variable.Key])
		}
	}
	for _, invalid := range []string{"NO_EQUALS", "1BAD=x", "WITH SPACE=x"} {
		if _, err := ParseDotenv(invalid); err == nil {
			t.Errorf("ParseDotenv(%q) should fail", invalid)
		}
	}
}

func TestFormatDotenvKeepsValuesLiteral(t *testing.T) {
	rendered := string(FormatDotenv(map[string]string{"B": "it's $HOME", "A": "plain $VAR"}))
	// Sorted, single-quoted when possible so compose does not interpolate.
	if !strings.HasPrefix(rendered, "A='plain $VAR'\n") {
		t.Errorf("unexpected output:\n%s", rendered)
	}
	if !strings.Contains(rendered, `B="it's \$HOME"`) {
		t.Errorf("a value with a single quote must be double-quoted with $ escaped:\n%s", rendered)
	}
}

func TestStoreEncryptsAtRestAndScopesByOwner(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	app := Owner{Type: OwnerApplication, ID: "app1"}
	other := Owner{Type: OwnerStack, ID: "app1"} // same id, different kind

	if err := store.Set(ctx, app, "TOKEN", "plaintext-secret", true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	var stored string
	if err := store.db.QueryRow(`SELECT value FROM env_vars WHERE key = 'TOKEN'`).Scan(&stored); err != nil {
		t.Fatalf("read raw row: %v", err)
	}
	if strings.Contains(stored, "plaintext-secret") {
		t.Errorf("value stored in clear text: %q", stored)
	}
	variables, _ := store.List(ctx, app)
	if len(variables) != 1 || variables[0].Value != "plaintext-secret" || !variables[0].BuildTime {
		t.Errorf("unexpected variables: %+v", variables)
	}
	if leaked, _ := store.List(ctx, other); len(leaked) != 0 {
		t.Errorf("variables leaked across owners: %+v", leaked)
	}
	if err := store.Set(ctx, app, "bad-key", "x", false); err == nil {
		t.Error("an invalid key must be rejected")
	}
}

func TestSetMissingNeverOverwritesAndReplaceAllIsExact(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	owner := Owner{Type: OwnerStack, ID: "stk1"}

	_ = store.Set(ctx, owner, "PIER_PASSWORD_DB", "original", false)
	if err := store.SetMissing(ctx, owner, map[string]string{"PIER_PASSWORD_DB": "regenerated", "PIER_SECRET_APP": "new"}); err != nil {
		t.Fatalf("SetMissing: %v", err)
	}
	values, _ := store.Map(ctx, owner)
	// Regenerating a password would lock a stack out of its own database.
	if values["PIER_PASSWORD_DB"] != "original" || values["PIER_SECRET_APP"] != "new" {
		t.Errorf("unexpected values: %v", values)
	}

	if err := store.ReplaceAll(ctx, owner, []Variable{{Key: "ONLY", Value: "1"}}); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	if values, _ := store.Map(ctx, owner); len(values) != 1 || values["ONLY"] != "1" {
		t.Errorf("ReplaceAll must leave exactly the given set: %v", values)
	}
	if err := store.ReplaceAll(ctx, owner, []Variable{{Key: "A", Value: "1"}, {Key: "A", Value: "2"}}); err == nil {
		t.Error("duplicated keys must be rejected")
	}
	if values, _ := store.Map(ctx, owner); values["ONLY"] != "1" {
		t.Errorf("a rejected replace must not change anything: %v", values)
	}
}
