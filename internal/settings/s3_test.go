package settings

import (
	"context"
	"path/filepath"
	"testing"

	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

func TestS3SettingsValidationAndEncryption(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "s3.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, err := secretbox.New("s3-test-key-at-least-16")
	if err != nil {
		t.Fatal(err)
	}
	store := NewS3Store(db, box)
	ctx := context.Background()
	good := S3Settings{Endpoint: "https://objects.example.com", Bucket: "backups", Region: "region", AccessKey: "access", SecretKey: "secret"}
	for _, bad := range []S3Settings{
		{Endpoint: "http://objects.example.com", Bucket: "backups", AccessKey: "access", SecretKey: "secret"},
		{Endpoint: "https://user:pass@objects.example.com", Bucket: "backups", AccessKey: "access", SecretKey: "secret"},
		{Endpoint: good.Endpoint, Bucket: "backups", AccessKey: "access", SecretKey: "secret\nAWS_CONFIG_FILE=/tmp/evil"},
		{Endpoint: good.Endpoint, Bucket: "backups/other", AccessKey: "access", SecretKey: "secret"},
	} {
		if err := store.Update(ctx, bad); err == nil {
			t.Fatalf("accepted invalid S3 settings: %+v", bad)
		}
	}
	if err := store.Update(ctx, good); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx)
	if err != nil || got != good {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}
