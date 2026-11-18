package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/i18n"
	"pier/internal/platform/secretbox/secretboxtest"
	"pier/internal/platform/sqlite"
)

func TestOpenSecretBoxWarnsAboutWeakKeysWithoutLoggingThem(t *testing.T) {
	previousLanguage := i18n.Instance()
	i18n.SetInstance(i18n.English)
	t.Cleanup(func() { i18n.SetInstance(previousLanguage) })
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	weak := "pierpierpier2026"
	if _, err := openSecretBox(ctx, db, weak, logger); err != nil {
		t.Fatalf("a weak key must not stop the start: %v", err)
	}
	if !strings.Contains(logs.String(), "looks weak") || strings.Contains(logs.String(), weak) {
		t.Fatalf("want a warning that does not contain the key, got: %s", logs.String())
	}
	logs.Reset()
	strong := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err := openSecretBox(ctx, db, strong, logger); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "looks weak") {
		t.Errorf("a random key must not be reported as weak: %s", logs.String())
	}
}

func TestOpenSecretBoxUpgradesLegacySecretsAndLogsOnlyCounts(t *testing.T) {
	previousLanguage := i18n.Instance()
	i18n.SetInstance(i18n.English)
	t.Cleanup(func() { i18n.SetInstance(previousLanguage) })
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	legacy := secretboxtest.SealV1(t, key, "registry-password")
	if _, err := db.Exec(`INSERT INTO settings(key, value) VALUES ('backup_s3', ?)`, legacy); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	box, err := openSecretBox(ctx, db, key, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	db.QueryRow(`SELECT value FROM settings WHERE key = 'backup_s3'`).Scan(&stored)
	if got, err := box.Open(stored); err != nil || got != "registry-password" || !strings.HasPrefix(stored, "v2:") {
		t.Fatalf("stored %.3s… opened %q, %v", stored, got, err)
	}
	out := logs.String()
	if !strings.Contains(out, "ciphertexts=1") || strings.Contains(out, legacy) || strings.Contains(out, "registry-password") {
		t.Fatalf("log must carry counts only: %s", out)
	}
}

func TestOpenSecretBoxStartsWhenTheUpgradeFails(t *testing.T) {
	previousLanguage := i18n.Instance()
	i18n.SetInstance(i18n.English)
	t.Cleanup(func() { i18n.SetInstance(previousLanguage) })
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	foreign := secretboxtest.SealV1(t, "a-key-of-another-installation", "secret")
	if _, err := db.Exec(`INSERT INTO settings(key, value) VALUES ('backup_s3', ?)`, foreign); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err := openSecretBox(ctx, db, key, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
		t.Fatalf("a failed upgrade must not stop the start: %v", err)
	}
	var stored string
	db.QueryRow(`SELECT value FROM settings WHERE key = 'backup_s3'`).Scan(&stored)
	if stored != foreign || !strings.Contains(logs.String(), "upgrade of v1 secrets failed") {
		t.Fatalf("data must stay intact and the failure be logged: %s", logs.String())
	}
}

func TestOpenSecretBoxReopensCurrentSecretsAfterDatabaseRestart(t *testing.T) {
	previousLanguage := i18n.Instance()
	i18n.SetInstance(i18n.English)
	t.Cleanup(func() { i18n.SetInstance(previousLanguage) })
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pier.db")
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	db, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	box, err := openSecretBox(ctx, db, key, logger)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	sealed, err := box.Seal("restart-secret")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO settings(key, value) VALUES ('restart', ?)`, sealed); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, err = openSecretBox(ctx, db, key, logger)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'restart'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if got, err := box.Open(stored); err != nil || got != "restart-secret" {
		t.Fatalf("reopen: %q, %v", got, err)
	}
}

func TestOpenSecretBoxDoesNotLogDatabaseErrorValues(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	legacy := secretboxtest.SealV1(t, key, "secret")
	if _, err := db.Exec(`INSERT INTO settings(key, value) VALUES ('backup_s3', ?)`, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_upgrade BEFORE UPDATE ON settings BEGIN SELECT RAISE(ABORT, 'sensitive-user-value'); END`); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	if _, err := openSecretBox(ctx, db, key, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "sensitive-user-value") {
		t.Fatal("database error value leaked into logs")
	}
	var stored string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'backup_s3'`).Scan(&stored); err != nil || stored != legacy {
		t.Fatalf("rollback: %v", err)
	}
}
