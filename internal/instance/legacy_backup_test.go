package instance

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/secretbox"
	"pier/internal/platform/secretbox/secretboxtest"
	"pier/internal/platform/sqlite"
	"pier/internal/sealedfields"
)

// makePre08Database turns a fresh database into what Pier 0.7 left on disk:
// no secretbox_state table, secrets sealed in the v1 format.
func makePre08Database(t *testing.T, s *Service) map[string]string {
	t.Helper()
	for _, statement := range []string{
		`DROP TABLE secretbox_state`,
		`DELETE FROM schema_migrations WHERE name LIKE '%0025_secretbox.sql'`,
	} {
		if _, err := s.DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	secrets := map[string]string{"backup_s3": `{"secret_key":"s3-secret"}`}
	for key, plaintext := range secrets {
		if _, err := s.DB.Exec(`INSERT INTO settings(key, value) VALUES (?, ?)`, key, secretboxtest.SealV1(t, s.Key, plaintext)); err != nil {
			t.Fatal(err)
		}
	}
	config := `{"directory":"/srv/pier-backups","cron":"0 3 * * *","retention":3,"passphrase":"` +
		secretboxtest.SealV1(t, s.Key, testPassphrase) + `"}`
	if _, err := s.DB.Exec(`INSERT INTO settings(key, value) VALUES ('instance_backup_config', ?)`, config); err != nil {
		t.Fatal(err)
	}
	return secrets
}

// restoreAndStart restores archive into a new data directory and runs the
// startup sequence of the server: migrations, salt, keys, legacy upgrade.
func restoreAndStart(t *testing.T, archive []byte) *Service {
	t.Helper()
	ctx := context.Background()
	backupFile := filepath.Join(t.TempDir(), "instance.pierbak")
	if err := os.WriteFile(backupFile, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := Restore(ctx, backupFile, testPassphrase, target); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	key, err := secretbox.LoadOrCreateKey("", filepath.Join(target, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(ctx, filepath.Join(target, "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box := openBox(t, db, key)
	if _, err := secretbox.UpgradeLegacy(ctx, db, box, sealedfields.All()); err != nil {
		t.Fatalf("UpgradeLegacy: %v", err)
	}
	return &Service{DB: db, DataDir: target, Key: key, Box: box}
}

func assertSecrets(t *testing.T, s *Service, secrets map[string]string) {
	t.Helper()
	for key, want := range secrets {
		var stored string
		if err := s.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(stored, "v2:") {
			t.Errorf("%s was not upgraded to v2", key)
		}
		if got, err := s.Box.Open(stored); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, err, want)
		}
	}
	config, err := s.configuration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(config.Passphrase, "v2:") {
		t.Error("the scheduled backup passphrase was not upgraded to v2")
	}
	if got, err := s.Box.Open(config.Passphrase); err != nil || got != testPassphrase {
		t.Errorf("scheduled backup passphrase = %q, %v", got, err)
	}
}

func TestOldV1BackupRestoresAndUpgradesThenRoundTripsAsV2(t *testing.T) {
	ctx := context.Background()
	old := testInstance(t)
	secrets := makePre08Database(t, old)
	oldArchive, err := old.Create(ctx, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}

	upgraded := restoreAndStart(t, oldArchive)
	assertSecrets(t, upgraded, secrets)

	// A backup taken after the upgrade carries the salt inside pier.db, so a
	// restore on another machine opens the v2 secrets with the same key.
	newArchive, err := upgraded.Create(ctx, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	moved := restoreAndStart(t, newArchive)
	assertSecrets(t, moved, secrets)
	assertSameSalt(t, upgraded.DB, moved.DB)
}

func assertSameSalt(t *testing.T, first, second *sql.DB) {
	t.Helper()
	a, err := secretbox.LoadOrCreateSalt(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := secretbox.LoadOrCreateSalt(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("the restored database must keep the installation salt")
	}
}
