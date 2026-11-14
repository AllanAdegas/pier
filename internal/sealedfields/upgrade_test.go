package sealedfields

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/secretbox"
	"pier/internal/platform/secretbox/secretboxtest"
	"pier/internal/platform/sqlite"
)

const testPassphrase = "sealed-fields-test-passphrase"

// invalidLegacyLiteral looks like a long v1 ciphertext but opens with no key.
var invalidLegacyLiteral = "v1:" + strings.Repeat("QUJD", 40)

// seededSecret is a registered location holding a v1 ciphertext.
type seededSecret struct {
	field     secretbox.Field
	path      string // JSON path, empty for a whole value
	plaintext string
}

// unregisteredText is user text at an unregistered location; the upgrade
// must leave it byte for byte.
type unregisteredText struct {
	query string
	value string
}

func openSchema(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// One connection so the pragma applies to every statement of the seeder.
	db.SetMaxOpenConns(1)
	mustExec(t, db, `PRAGMA foreign_keys = OFF`)
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func boxFor(t *testing.T, db *sql.DB, passphrase string) *secretbox.Box {
	t.Helper()
	salt, err := secretbox.LoadOrCreateSalt(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.NewWithSalt(passphrase, salt)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

// seedRegistry writes a v1 ciphertext at every registered location of the
// real schema, and copies of ciphertexts at unregistered locations.
func seedRegistry(t *testing.T, db *sql.DB) ([]seededSecret, []unregisteredText) {
	t.Helper()
	copied := secretboxtest.SealV1(t, testPassphrase, "copied into user text")
	var secrets []seededSecret
	seededTables := map[string]bool{}
	for i, field := range All() {
		plaintext := "secret of " + field.String()
		value := secretboxtest.SealV1(t, testPassphrase, plaintext)
		if len(field.JSONPaths) > 0 {
			value = jsonDocument(t, field.JSONPaths, value, copied)
		}
		writeField(t, db, field, value, seededTables[field.Table] && field.Row.Column == "", i+1)
		seededTables[field.Table] = true
		for _, path := range field.JSONPaths {
			secrets = append(secrets, seededSecret{field, path, plaintext})
		}
		if len(field.JSONPaths) == 0 {
			secrets = append(secrets, seededSecret{field, "", plaintext})
		}
	}
	mustExec(t, db, `INSERT INTO settings(key, value) VALUES ('audit_note', ?), ('pasted_literal', ?)`, copied, invalidLegacyLiteral)
	mustExec(t, db, `UPDATE applications SET description = ?`, copied)
	mustExec(t, db, `UPDATE private_keys SET description = ?`, invalidLegacyLiteral)
	return secrets, []unregisteredText{
		{`SELECT value FROM settings WHERE key = 'audit_note'`, copied},
		{`SELECT value FROM settings WHERE key = 'pasted_literal'`, invalidLegacyLiteral},
		{`SELECT description FROM applications`, copied},
		{`SELECT description FROM private_keys`, invalidLegacyLiteral},
	}
}

// jsonDocument nests the ciphertext at every path and the copy under an
// unregistered "note" key next to each of them.
func jsonDocument(t *testing.T, paths []string, ciphertext, copied string) string {
	t.Helper()
	root := map[string]any{"note": copied}
	for _, path := range paths {
		keys := strings.Split(path, ".")
		object := root
		for _, key := range keys[:len(keys)-1] {
			child, exists := object[key].(map[string]any)
			if !exists {
				child = map[string]any{"note": copied}
				object[key] = child
			}
			object = child
		}
		object[keys[len(keys)-1]] = ciphertext
	}
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func writeField(t *testing.T, db *sql.DB, field secretbox.Field, value string, rowExists bool, sequence int) {
	t.Helper()
	if rowExists {
		mustExec(t, db, fmt.Sprintf(`UPDATE %q SET %q = ?`, field.Table, field.Column), value)
		return
	}
	values := map[string]any{field.Column: value}
	if field.Row.Column != "" {
		values[field.Row.Column] = field.Row.Value
	}
	insertRow(t, db, field.Table, values, sequence)
}

func readField(t *testing.T, db *sql.DB, field secretbox.Field) string {
	t.Helper()
	query := fmt.Sprintf(`SELECT %q FROM %q`, field.Column, field.Table)
	var args []any
	if field.Row.Column != "" {
		query += fmt.Sprintf(` WHERE %q = ?`, field.Row.Column)
		args = append(args, field.Row.Value)
	}
	var value string
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("read %s: %v", field, err)
	}
	return value
}

// valueAt returns the stored ciphertext of a seeded secret.
func valueAt(t *testing.T, db *sql.DB, secret seededSecret) string {
	t.Helper()
	stored := readField(t, db, secret.field)
	if secret.path == "" {
		return stored
	}
	var current any
	if err := json.Unmarshal([]byte(stored), &current); err != nil {
		t.Fatalf("%s: %v", secret.field, err)
	}
	for _, key := range strings.Split(secret.path, ".") {
		current = current.(map[string]any)[key]
	}
	text, _ := current.(string)
	return text
}

func assertUnregisteredIntact(t *testing.T, db *sql.DB, texts []unregisteredText) {
	t.Helper()
	for _, text := range texts {
		var got string
		if err := db.QueryRow(text.query).Scan(&got); err != nil || got != text.value {
			t.Errorf("%s: unregistered text changed (%v)", text.query, err)
		}
	}
}

func TestUpgradeRewritesEveryRegisteredFieldOnTheRealSchema(t *testing.T) {
	db := openSchema(t)
	box := boxFor(t, db, testPassphrase)
	secrets, unregistered := seedRegistry(t, db)

	report, err := secretbox.UpgradeLegacy(context.Background(), db, box, All())
	if err != nil {
		t.Fatalf("UpgradeLegacy: %v", err)
	}
	if report.Ciphertexts != len(secrets) || report.Fields != len(All()) {
		t.Errorf("report = %+v, want %d ciphertexts in %d fields", report, len(secrets), len(All()))
	}
	for _, secret := range secrets {
		stored := valueAt(t, db, secret)
		if opened, err := box.Open(stored); !strings.HasPrefix(stored, "v2:") || err != nil || opened != secret.plaintext {
			t.Errorf("%s %s: not upgraded to a v2 ciphertext of the original secret (%v)", secret.field, secret.path, err)
		}
	}
	assertUnregisteredIntact(t, db, unregistered)
	for _, field := range All() {
		if len(field.JSONPaths) > 0 && !strings.Contains(readField(t, db, field), `"note":"v1:`) {
			t.Errorf("%s: a ciphertext copied into an unregistered JSON key was rewritten", field)
		}
	}
}

func TestUpgradeWithWrongKeyRollsBackOnTheRealSchema(t *testing.T) {
	db := openSchema(t)
	secrets, unregistered := seedRegistry(t, db)
	before := map[string]string{}
	for _, field := range All() {
		before[field.String()] = readField(t, db, field)
	}
	wrong := boxFor(t, db, "a-different-but-long-passphrase")
	if _, err := secretbox.UpgradeLegacy(context.Background(), db, wrong, All()); err == nil {
		t.Fatal("upgrading with the wrong key must fail")
	}
	for _, field := range All() {
		if readField(t, db, field) != before[field.String()] {
			t.Errorf("%s changed after a failed upgrade", field)
		}
	}
	assertUnregisteredIntact(t, db, unregistered)
	var marker sql.NullString
	db.QueryRow(`SELECT legacy_upgraded_at FROM secretbox_state`).Scan(&marker)
	if marker.Valid || len(secrets) == 0 {
		t.Error("a failed upgrade must not be marked as done")
	}
}
