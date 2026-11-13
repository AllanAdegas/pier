package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/docker"
	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

type fakeConnector struct{ ex *executor.Fake }

func (f fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return f.ex, server.Server{ID: "srv1", Kind: server.KindSSH, Host: "db.example.com", DataDir: "/data/pier"}, nil
}

type discardEvents struct{}

func (discardEvents) Publish(notify.Event) {}

func newService(t *testing.T) (*Service, *executor.Fake) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range []string{
		`INSERT INTO servers (id, name, kind, data_dir, created_at) VALUES ('srv1', 'test', 'local', '/data/pier', '2026-01-01T00:00:00Z')`,
		`INSERT INTO projects (id, name, created_at) VALUES ('prj1', 'p', '2026-01-01T00:00:00Z')`,
		`INSERT INTO environments (id, project_id, name, created_at) VALUES ('env1', 'prj1', 'production', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	box, _ := secretbox.New("test-passphrase-123456")
	fake := executor.NewFake()
	return NewService(NewStore(db, box), fakeConnector{fake}, discardEvents{}), fake
}

func create(t *testing.T, service *Service, engine Engine) Database {
	t.Helper()
	database, err := service.Create(context.Background(), CreateInput{
		EnvironmentID: "env1", ServerID: "srv1", Name: "main", Engine: engine,
	})
	if err != nil {
		t.Fatalf("Create(%s): %v", engine, err)
	}
	return database
}

func TestEveryEngineProducesAComposeFileAndAConnectionURL(t *testing.T) {
	for _, info := range Engines(i18n.Portuguese) {
		t.Run(string(info.Engine), func(t *testing.T) {
			service, fake := newService(t)
			database := create(t, service, info.Engine)
			if database.Config.Image != info.DefaultImage {
				t.Errorf("default image = %q, want %q", database.Config.Image, info.DefaultImage)
			}
			if err := service.Start(context.Background(), database.ID); err != nil {
				t.Fatalf("Start: %v", err)
			}
			compose := string(fake.Files["/data/pier/databases/"+database.ID+"/docker-compose.yaml"])
			// Readable container name; service, alias and volume keep the ID,
			// so connection strings and data survive a rename.
			for _, expected := range []string{"image: " + info.DefaultImage, database.ID + "_data:", "pier.type: database", "healthcheck:", "logging:", "driver: local", "max-size:", "max-file:",
				"container_name: " + docker.ReadableName(database.Name, database.ID), "- " + database.ID, "    " + database.ID + ":"} {
				if !strings.Contains(compose, expected) {
					t.Errorf("compose file misses %q:\n%s", expected, compose)
				}
			}
			if strings.Contains(compose, "ports:") {
				t.Error("a database must not be published on the host unless a public port is set")
			}
			connection, err := service.Connection(context.Background(), database.ID)
			if err != nil {
				t.Fatalf("Connection: %v", err)
			}
			if !strings.Contains(connection.InternalURL, "@"+database.ID+":") || connection.PublicURL != "" {
				t.Errorf("unexpected connection: %+v", connection)
			}
			if len(connection.Credentials.Password) != passwordLength {
				t.Errorf("generated password has length %d", len(connection.Credentials.Password))
			}
		})
	}
}

func TestPublicPortExposesTheDatabaseAndBuildsThePublicURL(t *testing.T) {
	service, fake := newService(t)
	database := create(t, service, EnginePostgreSQL)
	config := database.Config
	config.PublicPort = 54321
	if _, err := service.Update(context.Background(), database.ID, UpdateInput{Name: "main", Config: config}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := service.Start(context.Background(), database.ID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	compose := string(fake.Files["/data/pier/databases/"+database.ID+"/docker-compose.yaml"])
	if !strings.Contains(compose, "54321:5432") {
		t.Errorf("public port not mapped:\n%s", compose)
	}
	connection, _ := service.Connection(context.Background(), database.ID)
	if !strings.Contains(connection.PublicURL, "@db.example.com:54321/") {
		t.Errorf("public URL = %q", connection.PublicURL)
	}
}

func TestValidationRejectsUnsafeDatabaseSettings(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	inputs := map[string]CreateInput{
		"engine":         {EnvironmentID: "env1", ServerID: "srv1", Name: "x", Engine: "oracle"},
		"name":           {EnvironmentID: "env1", ServerID: "srv1", Engine: EnginePostgreSQL},
		"username":       {EnvironmentID: "env1", ServerID: "srv1", Name: "x", Engine: EnginePostgreSQL, Username: "Robert'); DROP"},
		"database":       {EnvironmentID: "env1", ServerID: "srv1", Name: "x", Engine: EnginePostgreSQL, Database: "my db"},
		"config.image":   {EnvironmentID: "env1", ServerID: "srv1", Name: "x", Engine: EnginePostgreSQL, Image: "postgres; id"},
		"environment_id": {ServerID: "srv1", Name: "x", Engine: EnginePostgreSQL},
	}
	for field, input := range inputs {
		_, err := service.Create(ctx, input)
		appErr, ok := apperr.As(err)
		if !ok {
			t.Errorf("%s: expected a validation error, got %v", field, err)
			continue
		}
		if _, reported := appErr.Fields[field]; !reported {
			t.Errorf("expected an error on %q, got %v", field, appErr.Fields)
		}
	}
	database := create(t, service, EngineClickHouse)
	config := database.Config
	config.Backup = BackupPolicy{Enabled: true, Cron: "0 3 * * *"}
	if _, err := service.Update(ctx, database.ID, UpdateInput{Name: "x", Config: config}); err == nil {
		t.Error("scheduling backups on an engine without dump support must be rejected")
	}
	postgres := create(t, service, EnginePostgreSQL)
	bad := postgres.Config
	bad.Backup = BackupPolicy{Enabled: true, Cron: "every day"}
	if _, err := service.Update(ctx, postgres.ID, UpdateInput{Name: "x", Config: bad}); err == nil {
		t.Error("an invalid cron expression must be rejected")
	}
}

// The data volume and the dump files hold the same information, so they
// follow the same explicit choice when a database is deleted.
func TestDeleteDestroysDataOnlyWhenAsked(t *testing.T) {
	t.Run("keeping the data", func(t *testing.T) {
		service, fake := newService(t)
		database := create(t, service, EnginePostgreSQL)
		if err := service.Delete(context.Background(), database.ID, false, false); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if fake.Ran("docker volume rm") || fake.Ran("/data/pier/backups/") {
			t.Errorf("volume and backups must be kept; ran:\n%s", strings.Join(fake.Commands, "\n"))
		}
		if !fake.Ran("rm -rf /data/pier/databases/" + database.ID) {
			t.Error("the compose project directory must always be removed")
		}
	})
	t.Run("destroying the data", func(t *testing.T) {
		service, fake := newService(t)
		database := create(t, service, EnginePostgreSQL)
		if err := service.Delete(context.Background(), database.ID, true, false); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if !fake.Ran("docker volume rm --force "+database.ID+"_data") || !fake.Ran("rm -rf /data/pier/backups/"+database.ID) {
			t.Errorf("volume and backups must both be removed; ran:\n%s", strings.Join(fake.Commands, "\n"))
		}
		if _, err := service.Get(context.Background(), database.ID); err == nil {
			t.Error("the record must be gone")
		}
	})
	t.Run("unreachable server", func(t *testing.T) {
		service, fake := newService(t)
		database := create(t, service, EnginePostgreSQL)
		fake.On("docker ps", "cannot connect", 1)
		if err := service.Delete(context.Background(), database.ID, true, false); err == nil {
			t.Fatal("deleting without force must fail when the server cannot be cleaned")
		}
		if _, err := service.Get(context.Background(), database.ID); err != nil {
			t.Error("the record must survive a failed cleanup")
		}
		if err := service.Delete(context.Background(), database.ID, true, true); err == nil {
			t.Fatal("force must not claim successful data destruction on an unreachable server")
		}
		if err := service.Delete(context.Background(), database.ID, false, true); err != nil {
			t.Errorf("forced delete must remove the record: %v", err)
		}
	})
}
