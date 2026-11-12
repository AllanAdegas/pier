package instance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

const testPassphrase = "test-backup-passphrase-long"

func testInstance(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(context.Background(), filepath.Join(dir, "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	key := "source-instance-key-for-tests"
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{DB: db, DataDir: dir, Key: key, Box: box}
}
func TestBackupRestoreRoundTripPreservesDatabaseAndEffectiveKey(t *testing.T) {
	ctx := context.Background()
	s := testInstance(t)
	value, err := s.Box.Seal("sensitive-db-value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO settings(key,value) VALUES('test_secret',?)`, value); err != nil {
		t.Fatal(err)
	}
	archive, err := s.Create(ctx, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(archive, []byte("sensitive-db-value")) || bytes.Contains(archive, []byte(s.Key)) {
		t.Fatal("plaintext in encrypted backup")
	}
	backupFile := filepath.Join(t.TempDir(), "instance.pierbak")
	if err = os.WriteFile(backupFile, archive, 0600); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	oldKey := []byte("old-instance-key-before-restore")
	os.WriteFile(filepath.Join(target, "secret.key"), oldKey, 0600)
	unlock, err := Lock(target)
	if err != nil {
		t.Fatal(err)
	}
	if err = Restore(ctx, backupFile, testPassphrase, target); err == nil {
		t.Fatal("restored a locked instance")
	}
	unlock()
	if err = Restore(ctx, backupFile, "wrong-backup-passphrase", target); err == nil {
		t.Fatal("accepted wrong passphrase")
	}
	unchanged, _ := os.ReadFile(filepath.Join(target, "secret.key"))
	if !bytes.Equal(unchanged, oldKey) {
		t.Fatal("failed restore modified existing files")
	}
	corrupt := append([]byte(nil), archive...)
	corrupt[len(corrupt)-1] ^= 1
	os.WriteFile(backupFile, corrupt, 0600)
	if err = Restore(ctx, backupFile, testPassphrase, target); err == nil {
		t.Fatal("accepted tampered backup")
	}
	os.WriteFile(backupFile, archive, 0600)
	if err = Restore(ctx, backupFile, testPassphrase, target); err != nil {
		t.Fatal(err)
	}
	key, _ := os.ReadFile(filepath.Join(target, "secret.key"))
	if strings.TrimSpace(string(key)) != s.Key {
		t.Fatal("effective encryption key was lost")
	}
	restored, err := sqlite.Open(ctx, filepath.Join(target, "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var encrypted string
	if err = restored.QueryRow(`SELECT value FROM settings WHERE key='test_secret'`).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	box, _ := secretbox.New(strings.TrimSpace(string(key)))
	plain, err := box.Open(encrypted)
	if err != nil || plain != "sensitive-db-value" {
		t.Fatalf("restored secret %q, %v", plain, err)
	}
	retained, _ := filepath.Glob(filepath.Join(target, "secret.key.before-restore-*"))
	if len(retained) != 1 {
		t.Fatal("old key not retained")
	}
	info, _ := os.Stat(filepath.Join(target, "pier.db"))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database permissions %o", info.Mode().Perm())
	}
}
func TestScheduledBackupsEncryptConfigurationAndRetainOnlyOwnedFiles(t *testing.T) {
	s := testInstance(t)
	ctx := context.Background()
	dir := t.TempDir()
	c, err := s.Configure(ctx, BackupConfig{Directory: dir, Cron: "* * * * *", Retention: 1, Passphrase: testPassphrase})
	if err != nil {
		t.Fatal(err)
	}
	if c.Passphrase != "" || !c.HasPassphrase {
		t.Fatal("configuration leaked passphrase or lost configured status")
	}
	var raw string
	s.DB.QueryRow(`SELECT value FROM settings WHERE key='instance_backup_config'`).Scan(&raw)
	if strings.Contains(raw, testPassphrase) {
		t.Fatal("saved plaintext passphrase")
	}
	os.WriteFile(filepath.Join(dir, "unrelated.pierbak"), []byte("keep"), 0600)
	now := time.Now().Truncate(time.Minute).Add(time.Second)
	if err = s.runScheduled(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err = s.runScheduled(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "pier-instance-*.pierbak"))
	if len(files) != 1 {
		t.Fatalf("retained %d", len(files))
	}
	if _, err = os.Stat(filepath.Join(dir, "unrelated.pierbak")); err != nil {
		t.Fatal("deleted unrelated file")
	}
	status, err := s.Status(ctx)
	if err != nil || status.LastBackupAt == nil {
		t.Fatalf("status %+v %v", status, err)
	}
	// An omitted passphrase preserves the encrypted value on ordinary edits.
	c.Cron = "0 4 * * *"
	if _, err = s.Configure(ctx, c); err != nil {
		t.Fatal(err)
	}
}
func TestBackupRejectsInvalidPolicyAndNonDatabasePayload(t *testing.T) {
	s := testInstance(t)
	ctx := context.Background()
	for _, c := range []BackupConfig{{Directory: "relative", Cron: "* * * * *", Retention: 1}, {Directory: t.TempDir(), Cron: "invalid", Retention: 1}, {Cron: "* * * * *", Retention: 0}} {
		if _, err := s.Configure(ctx, c); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	if _, err := s.Create(ctx, "short"); err == nil {
		t.Fatal("accepted weak passphrase")
	}
}

func TestRestoreRejectsAuthenticatedUnsafeOrInvalidContentsBeforeReplacement(t *testing.T) {
	s := testInstance(t)
	target := t.TempDir()
	keyPath := filepath.Join(target, "secret.key")
	oldKey := []byte("old-key-must-stay-unchanged")
	if err := os.WriteFile(keyPath, oldKey, 0600); err != nil {
		t.Fatal(err)
	}
	for _, entries := range [][]struct {
		name string
		kind byte
		body string
	}{
		{{"../outside", tar.TypeReg, "bad"}},
		{{"pier.db", tar.TypeSymlink, ""}},
		{{"pier.db", tar.TypeReg, "not a SQLite database"}, {"secret.key", tar.TypeReg, s.Key}},
		{{"secret.key", tar.TypeReg, s.Key}, {"secret.key", tar.TypeReg, s.Key}},
	} {
		var plain bytes.Buffer
		gz := gzip.NewWriter(&plain)
		tw := tar.NewWriter(gz)
		for _, entry := range entries {
			if err := tw.WriteHeader(&tar.Header{Name: entry.name, Typeflag: entry.kind, Size: int64(len(entry.body)), Mode: 0600}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
		tw.Close()
		gz.Close()
		encrypted, err := seal(plain.Bytes(), testPassphrase)
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "bad.pierbak")
		os.WriteFile(file, encrypted, 0600)
		if err = Restore(context.Background(), file, testPassphrase, target); err == nil {
			t.Fatalf("accepted entries %+v", entries)
		}
		got, _ := os.ReadFile(keyPath)
		if !bytes.Equal(got, oldKey) {
			t.Fatal("invalid contents replaced old data")
		}
	}
}
