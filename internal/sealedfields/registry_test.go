package sealedfields

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/sqlite"
)

// nonSecretboxSealers lists production files whose Seal calls are not
// secretbox: they must not be registered, and must say why here.
var nonSecretboxSealers = map[string]string{
	"internal/instance/backup.go": "AES-GCM (cipher.AEAD) of the instance backup archive, a file, not a database field",
}

func TestRegisteredFieldsExistAfterAllMigrations(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, field := range All() {
		columns := tableColumns(t, db, field.Table)
		if !columns[field.Column] {
			t.Errorf("%s: column %s.%s does not exist", field, field.Table, field.Column)
		}
		if field.Row.Column != "" && !columns[field.Row.Column] {
			t.Errorf("%s: row match column %s does not exist", field, field.Row.Column)
		}
	}
}

func TestRegistryHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, field := range All() {
		if seen[field.String()] {
			t.Errorf("%s is registered twice", field)
		}
		seen[field.String()] = true
	}
}

// Every production file that seals values must own registered fields, so a
// new sealing store cannot skip the legacy upgrade unnoticed.
func TestEverySealingFileIsRegistered(t *testing.T) {
	root := repositoryRoot(t)
	sealers := sealingFiles(t, root)
	owners := map[string]bool{}
	for _, item := range registry {
		owners[item.owner] = true
		if !sealers[item.owner] {
			t.Errorf("%s owns %s but no longer calls Seal: update the registry", item.owner, item.field)
		}
	}
	for file := range sealers {
		if _, excluded := nonSecretboxSealers[file]; !excluded && !owners[file] {
			t.Errorf("%s calls Seal but owns no field in internal/sealedfields: register the table/column it writes", file)
		}
	}
	for file := range nonSecretboxSealers {
		if !sealers[file] {
			t.Errorf("%s is listed as a non-secretbox sealer but has no Seal call: remove it", file)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// sealingFiles returns the non-test Go files (slash paths relative to root)
// outside the secretbox package that call a method named Seal.
func sealingFiles(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if skippedDirectory(relative, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(relative, ".go") && !strings.HasSuffix(relative, "_test.go") && callsSeal(t, path) {
			found[relative] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func skippedDirectory(relative, name string) bool {
	if relative == "." {
		return false
	}
	switch name {
	case "node_modules", "testdata", "vendor":
		return true
	}
	return strings.HasPrefix(name, ".") || relative == "internal/platform/secretbox" ||
		relative == "web" || relative == "site"
}

func callsSeal(t *testing.T, path string) bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if call, isCall := node.(*ast.CallExpr); isCall {
			if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "Seal" {
				found = true
			}
		}
		return !found
	})
	return found
}
