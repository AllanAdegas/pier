package secretbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/secretbox/secretboxtest"
	"pier/internal/platform/sqlite"
)

// The tests below use their own tables so they exercise UpgradeLegacy
// independently of Pier's schema; internal/sealedfields runs the upgrade
// against the real migrations and registry.

// testFields registers vault.secret (whole value), kv[key=whole] (row of a
// key/value table) and kv[key=config] (JSON paths). vault.note, the other kv
// rows and the other JSON keys are deliberately unregistered.
var testFields = []Field{
	{Table: "vault", Column: "secret"},
	{Table: "kv", Column: "value", Row: RowMatch{Column: "key", Value: "whole"}},
	{Table: "kv", Column: "value", Row: RowMatch{Column: "key", Value: "config"},
		JSONPaths: []string{"passphrase", "nested.token", "absent.path"}},
}

// invalidLegacyLiteral looks like a long v1 ciphertext but opens with no key.
var invalidLegacyLiteral = "v1:" + strings.Repeat("QUJD", 40)

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `CREATE TABLE vault (id INTEGER PRIMARY KEY, secret TEXT, note TEXT)`)
	mustExec(t, db, `CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT)`)
	return db
}

func newDatabaseBox(t *testing.T, db *sql.DB, passphrase string) *Box {
	t.Helper()
	salt, err := LoadOrCreateSalt(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	box, err := NewWithSalt(passphrase, salt)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func queryValue(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var value string
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

// legacyFixture is a database holding v1 ciphertexts at every registered
// location, plus copies of them where nothing is registered.
type legacyFixture struct {
	vaultSecret, whole, passphrase, token string
	copied, configDocument                string
}

func seedLegacyFixture(t *testing.T, db *sql.DB) legacyFixture {
	t.Helper()
	f := legacyFixture{
		vaultSecret: secretboxtest.SealV1(t, testPassphrase, "vault secret"),
		whole:       secretboxtest.SealV1(t, testPassphrase, "whole secret"),
		passphrase:  secretboxtest.SealV1(t, testPassphrase, "backup passphrase"),
		token:       secretboxtest.SealV1(t, testPassphrase, "nested token"),
	}
	f.copied = f.vaultSecret
	f.configDocument = `{"directory":"/srv/backups","retention":7,"passphrase":"` + f.passphrase +
		`","nested":{"token":"` + f.token + `","note":"` + f.copied + `"},"description":"` + f.copied + `"}`
	mustExec(t, db, `INSERT INTO vault (id, secret, note) VALUES (1, ?, ?), (2, NULL, ?)`,
		f.vaultSecret, f.copied, invalidLegacyLiteral)
	mustExec(t, db, `INSERT INTO kv (key, value) VALUES ('whole', ?), ('config', ?), ('audit', ?), ('garbage', ?)`,
		f.whole, f.configDocument, f.copied, invalidLegacyLiteral)
	return f
}

func assertOpensAsV2(t *testing.T, box *Box, name, stored, want string) {
	t.Helper()
	if !strings.HasPrefix(stored, "v2:") {
		t.Errorf("%s was not upgraded to v2", name)
		return
	}
	if got, err := box.Open(stored); err != nil || got != want {
		t.Errorf("%s opens to %q, %v; want %q", name, got, err, want)
	}
}

func TestUpgradeRewritesOnlyRegisteredFields(t *testing.T) {
	db := openTestDatabase(t)
	box := newDatabaseBox(t, db, testPassphrase)
	f := seedLegacyFixture(t, db)

	report, err := UpgradeLegacy(context.Background(), db, box, testFields)
	if err != nil {
		t.Fatalf("UpgradeLegacy: %v", err)
	}
	if report.Ciphertexts != 4 || report.Rows != 3 || report.Fields != 3 {
		t.Errorf("report = %+v, want 4 ciphertexts in 3 rows of 3 fields", report)
	}
	assertOpensAsV2(t, box, "vault.secret", queryValue(t, db, `SELECT secret FROM vault WHERE id = 1`), "vault secret")
	assertOpensAsV2(t, box, "kv[whole]", queryValue(t, db, `SELECT value FROM kv WHERE key = 'whole'`), "whole secret")

	var config struct {
		Directory, Passphrase, Description string
		Retention                          int
		Nested                             struct{ Token, Note string }
	}
	if err := json.Unmarshal([]byte(queryValue(t, db, `SELECT value FROM kv WHERE key = 'config'`)), &config); err != nil {
		t.Fatal(err)
	}
	assertOpensAsV2(t, box, "config/passphrase", config.Passphrase, "backup passphrase")
	assertOpensAsV2(t, box, "config/nested.token", config.Nested.Token, "nested token")
	if config.Directory != "/srv/backups" || config.Retention != 7 {
		t.Errorf("other JSON values changed: %+v", config)
	}
	if config.Description != f.copied || config.Nested.Note != f.copied {
		t.Error("a ciphertext copied into an unregistered JSON key was rewritten")
	}
	assertUnregisteredUntouched(t, db, f)
}

func assertUnregisteredUntouched(t *testing.T, db *sql.DB, f legacyFixture) {
	t.Helper()
	for name, check := range map[string]struct{ query, want string }{
		"vault.note (valid copy)":       {`SELECT note FROM vault WHERE id = 1`, f.copied},
		"vault.note (invalid literal)":  {`SELECT note FROM vault WHERE id = 2`, invalidLegacyLiteral},
		"kv[audit] (valid copy)":        {`SELECT value FROM kv WHERE key = 'audit'`, f.copied},
		"kv[garbage] (invalid literal)": {`SELECT value FROM kv WHERE key = 'garbage'`, invalidLegacyLiteral},
	} {
		if got := queryValue(t, db, check.query); got != check.want {
			t.Errorf("%s changed", name)
		}
	}
}

func TestUpgradeIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openTestDatabase(t)
	box := newDatabaseBox(t, db, testPassphrase)
	mustExec(t, db, `INSERT INTO vault (id, secret) VALUES (1, ?)`, secretboxtest.SealV1(t, testPassphrase, "s3"))

	if report, err := UpgradeLegacy(ctx, db, box, testFields); err != nil || report.Ciphertexts != 1 {
		t.Fatalf("first run: %+v, %v", report, err)
	}
	if report, err := UpgradeLegacy(ctx, db, box, testFields); err != nil || !report.AlreadyDone {
		t.Fatalf("second run must be skipped: %+v, %v", report, err)
	}
	mustExec(t, db, `UPDATE secretbox_state SET legacy_upgraded_at = NULL`)
	report, err := UpgradeLegacy(ctx, db, box, testFields)
	if err != nil || report.AlreadyDone || report.Ciphertexts != 0 {
		t.Fatalf("a forced rerun must find nothing to do: %+v, %v", report, err)
	}
}

// assertLegacyIntact checks a failed run left every value and the marker as
// they were.
func assertLegacyIntact(t *testing.T, db *sql.DB, f legacyFixture) {
	t.Helper()
	if queryValue(t, db, `SELECT secret FROM vault WHERE id = 1`) != f.vaultSecret ||
		queryValue(t, db, `SELECT value FROM kv WHERE key = 'whole'`) != f.whole ||
		queryValue(t, db, `SELECT value FROM kv WHERE key = 'config'`) != f.configDocument {
		t.Error("a failed upgrade changed registered values")
	}
	assertUnregisteredUntouched(t, db, f)
	var marker sql.NullString
	db.QueryRow(`SELECT legacy_upgraded_at FROM secretbox_state`).Scan(&marker)
	if marker.Valid {
		t.Error("a failed upgrade must not be marked as done")
	}
}

func TestUpgradeRollsBackOnDamagedRegisteredValueAndResumes(t *testing.T) {
	ctx := context.Background()
	db := openTestDatabase(t)
	box := newDatabaseBox(t, db, testPassphrase)
	f := seedLegacyFixture(t, db)
	// The last registered field holds a damaged ciphertext: the run fails
	// after having rewritten the other fields in its transaction.
	damaged := secretboxtest.SealV1(t, testPassphrase, "damaged")
	damaged = damaged[:len(damaged)-4] + flip(damaged[len(damaged)-4:])
	fields := append(append([]Field{}, testFields...), Field{Table: "kv", Column: "value", Row: RowMatch{Column: "key", Value: "damaged"}})
	mustExec(t, db, `INSERT INTO kv (key, value) VALUES ('damaged', ?)`, damaged)

	_, err := UpgradeLegacy(ctx, db, box, fields)
	if err == nil {
		t.Fatal("a registered ciphertext that fails authentication must abort the upgrade")
	}
	if strings.Contains(err.Error(), damaged) || strings.Contains(err.Error(), "damaged secret") {
		t.Errorf("the error must not carry values: %v", err)
	}
	assertLegacyIntact(t, db, f)

	mustExec(t, db, `DELETE FROM kv WHERE key = 'damaged'`)
	if report, err := UpgradeLegacy(ctx, db, box, fields); err != nil || report.Ciphertexts != 4 {
		t.Fatalf("the next start must finish the upgrade: %+v, %v", report, err)
	}
}

func TestUpgradeWithWrongKeyLeavesDataIntact(t *testing.T) {
	ctx := context.Background()
	db := openTestDatabase(t)
	f := seedLegacyFixture(t, db)
	wrong := newDatabaseBox(t, db, "a-different-but-long-passphrase")

	if _, err := UpgradeLegacy(ctx, db, wrong, testFields); err == nil {
		t.Fatal("upgrading with the wrong key must fail")
	}
	assertLegacyIntact(t, db, f)

	right := newDatabaseBox(t, db, testPassphrase)
	if report, err := UpgradeLegacy(ctx, db, right, testFields); err != nil || report.Ciphertexts != 4 {
		t.Fatalf("the right key must still upgrade everything: %+v, %v", report, err)
	}
}

func TestUpgradeRejectsANonJSONValueAtARegisteredJSONField(t *testing.T) {
	db := openTestDatabase(t)
	box := newDatabaseBox(t, db, testPassphrase)
	mustExec(t, db, `INSERT INTO kv (key, value) VALUES ('config', ?)`, `{"passphrase":"v1:x"} trailing`)
	_, err := UpgradeLegacy(context.Background(), db, box, testFields)
	if err == nil || strings.Contains(err.Error(), "trailing") {
		t.Fatalf("want a value-free error for a broken JSON document, got %v", err)
	}
}

func TestUpgradeValidatesFieldsBeforeTouchingTheDatabase(t *testing.T) {
	db := openTestDatabase(t)
	box := newDatabaseBox(t, db, testPassphrase)
	for name, fields := range map[string][]Field{
		"none":           nil,
		"no column":      {{Table: "vault"}},
		"half row":       {{Table: "kv", Column: "value", Row: RowMatch{Column: "key"}}},
		"empty JSON key": {{Table: "kv", Column: "value", JSONPaths: []string{"nested..token"}}},
	} {
		if _, err := UpgradeLegacy(context.Background(), db, box, fields); err == nil {
			t.Errorf("%s: want a validation error", name)
		}
	}
	if _, err := UpgradeLegacy(context.Background(), db, box, []Field{{Table: "missing", Column: "value"}}); err == nil {
		t.Error("a field whose table does not exist must fail")
	}
}

func TestUpgradeNeedsTheStoredSalt(t *testing.T) {
	db := openTestDatabase(t)
	box := newTestBox(t, testPassphrase)
	if _, err := UpgradeLegacy(context.Background(), db, box, testFields); err == nil {
		t.Error("the upgrade must fail fast when the salt was never created")
	}
}

func TestLoadOrCreateSaltIsStable(t *testing.T) {
	ctx := context.Background()
	db := openTestDatabase(t)
	first, err := LoadOrCreateSalt(ctx, db)
	if err != nil || len(first) != SaltLength {
		t.Fatalf("LoadOrCreateSalt: %x, %v", first, err)
	}
	second, _ := LoadOrCreateSalt(ctx, db)
	if string(first) != string(second) {
		t.Error("the salt must not change once stored")
	}
}

func TestCancelledUpgradeLeavesSecretsUntouched(t *testing.T) {
	db := openTestDatabase(t)
	box := newDatabaseBox(t, db, testPassphrase)
	f := seedLegacyFixture(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := UpgradeLegacy(ctx, db, box, testFields); err == nil {
		t.Fatal("a cancelled upgrade must fail")
	}
	assertLegacyIntact(t, db, f)
	if _, err := UpgradeLegacy(context.Background(), db, box, testFields); err != nil {
		t.Fatalf("retry after cancellation: %v", err)
	}
}

// SQLite column collations must not broaden a ciphertext replacement.
func TestUpgradePreservesCaseInsensitiveUserValues(t *testing.T) {
	db := openTestDatabase(t)
	box := newDatabaseBox(t, db, testPassphrase)
	mustExec(t, db, `CREATE TABLE user_values (value TEXT COLLATE NOCASE)`)
	legacy := secretboxtest.SealV1(t, testPassphrase, "secret")
	unrelated := "V1:" + legacy[3:]
	mustExec(t, db, `INSERT INTO user_values VALUES (?), (?)`, legacy, unrelated)
	report, err := UpgradeLegacy(context.Background(), db, box, []Field{{Table: "user_values", Column: "value"}})
	if err != nil || report.Rows != 1 {
		t.Fatalf("upgrade: %+v, %v", report, err)
	}
	var preserved int
	if err := db.QueryRow(`SELECT count(*) FROM user_values WHERE value COLLATE BINARY = ?`, unrelated).Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("unrelated value changed: count %d, %v", preserved, err)
	}
}

func TestFieldStringCarriesNoValues(t *testing.T) {
	field := Field{Table: "settings", Column: "value", Row: RowMatch{Column: "key", Value: "backup"}, JSONPaths: []string{"a.b"}}
	if got := field.String(); got != "settings.value[key=backup]/a.b" {
		t.Errorf("String() = %q", got)
	}
}
