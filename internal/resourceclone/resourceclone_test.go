package resourceclone

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"pier/internal/application"
	"pier/internal/database"
	"pier/internal/envvar"
	"pier/internal/platform/executor"
	"pier/internal/platform/oplog"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
	"pier/internal/stack"
)

type fakeConnector struct{}

func (fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return executor.NewFake(), server.Server{ID: "srv1", DataDir: "/data/pier"}, nil
}

func newCloner(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range []string{
		`INSERT INTO servers (id, name, kind, data_dir, created_at) VALUES ('srv1', 'test', 'local', '/data/pier', '2026-01-01T00:00:00Z')`,
		`INSERT INTO projects (id, name, created_at) VALUES ('prj1', 'p', '2026-01-01T00:00:00Z'), ('prj2', 'q', '2026-01-01T00:00:00Z')`,
		`INSERT INTO environments (id, project_id, name, created_at) VALUES ('env1', 'prj1', 'production', '2026-01-01T00:00:00Z'),
			('env-staging', 'prj1', 'staging', '2026-01-01T00:00:00Z'), ('env-other', 'prj2', 'production', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	box, _ := secretbox.New("test-passphrase-123456")
	env := envvar.NewStore(db, box)
	return &Service{
		Databases: database.NewService(database.NewStore(db, box), fakeConnector{}, nil),
		Stacks:    stack.NewService(db, env, fakeConnector{}, oplog.NewRegistry()),
		Env:       env,
	}, db
}

func matrix(run func(t *testing.T, target string, options application.CloneOptions)) func(t *testing.T) {
	return func(t *testing.T) {
		for _, target := range []string{"env1", "env-staging", "env-other"} {
			for _, flags := range [][3]bool{{false, false, false}, {true, false, false}, {true, true, false}, {true, false, true}, {true, true, true}, {false, true, true}} {
				options := application.CloneOptions{TargetEnvironmentID: target, CopyVariables: flags[0], CopyDomains: flags[1], CopySecrets: flags[2]}
				t.Run(fmt.Sprintf("%s/%v", target, flags), func(t *testing.T) { run(t, target, options) })
			}
		}
	}
}

func expectedSecret(target string, options application.CloneOptions, value string) string {
	if !options.CopyVariables {
		return "<absent>"
	}
	if target != "env-other" || options.CopySecrets {
		return value
	}
	return ""
}

func valueOr(values map[string]string, key string) string {
	value, found := values[key]
	if !found {
		return "<absent>"
	}
	return value
}

func TestCloneDatabaseMatrix(t *testing.T) {
	matrix(func(t *testing.T, target string, options application.CloneOptions) {
		cloner, _ := newCloner(t)
		ctx := context.Background()
		source, err := cloner.Databases.Create(ctx, database.CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "main",
			Engine: "postgresql", Username: "shop", Database: "orders"})
		if err != nil {
			t.Fatal(err)
		}
		config := source.Config
		config.Memory, config.Backup.Retention = "1g", 14
		if _, err := cloner.Databases.Update(ctx, source.ID, database.UpdateInput{Name: "main", Config: config}); err != nil {
			t.Fatal(err)
		}
		if err := cloner.Env.Set(ctx, envvar.Owner{Type: envvar.OwnerDatabase, ID: source.ID}, "POSTGRES_INITDB_ARGS", "--data-checksums", false); err != nil {
			t.Fatal(err)
		}
		clone, err := cloner.CloneDatabase(ctx, source.ID, options)
		if err != nil {
			t.Fatal(err)
		}
		if clone.EnvironmentID != target || clone.Engine != source.Engine || clone.Config.Memory != "1g" || clone.Config.PublicPort != 0 || clone.Config.Backup.Retention != 14 {
			t.Fatalf("clone = %+v", clone)
		}
		sourceConnection, _ := cloner.Databases.Connection(ctx, source.ID)
		cloneConnection, _ := cloner.Databases.Connection(ctx, clone.ID)
		if cloneConnection.Credentials.Username != "shop" || cloneConnection.Credentials.Database != "orders" ||
			cloneConnection.Credentials.Password == sourceConnection.Credentials.Password {
			t.Fatalf("credentials must keep names and get a new password: %+v", cloneConnection.Credentials)
		}
		values, _ := cloner.Env.Map(ctx, envvar.Owner{Type: envvar.OwnerDatabase, ID: clone.ID})
		if got, want := valueOr(values, "POSTGRES_INITDB_ARGS"), expectedSecret(target, options, "--data-checksums"); got != want {
			t.Errorf("variable = %q, want %q", got, want)
		}
	})(t)
}

const composeFile = "services:\n  web:\n    image: nginx:1.27\n    environment:\n      TOKEN: ${TOKEN}\n"

func TestCloneStackMatrix(t *testing.T) {
	matrix(func(t *testing.T, target string, options application.CloneOptions) {
		cloner, _ := newCloner(t)
		ctx := context.Background()
		source, err := cloner.Stacks.Create(ctx, stack.CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "site", Compose: composeFile,
			Routes: []stack.Route{{Service: "web", Port: 80, Domain: "https://site.example.com"}}, Env: map[string]string{"TOKEN": "stack-secret"}})
		if err != nil {
			t.Fatal(err)
		}
		clone, err := cloner.CloneStack(ctx, source.ID, options)
		if err != nil {
			t.Fatal(err)
		}
		if clone.EnvironmentID != target || clone.Compose != composeFile {
			t.Fatalf("clone = %+v", clone)
		}
		if wantRoutes := map[bool]int{true: 1, false: 0}[options.CopyDomains]; len(clone.Routes) != wantRoutes {
			t.Errorf("routes = %v", clone.Routes)
		}
		values, _ := cloner.Env.Map(ctx, envvar.Owner{Type: envvar.OwnerStack, ID: clone.ID})
		if got, want := valueOr(values, "TOKEN"), expectedSecret(target, options, "stack-secret"); got != want {
			t.Errorf("TOKEN = %q, want %q", got, want)
		}
	})(t)
}

func TestCloneStackRefusesServerDirectory(t *testing.T) {
	cloner, db := newCloner(t)
	ctx := context.Background()
	source, err := cloner.Stacks.Create(ctx, stack.CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "site", Compose: composeFile})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE stacks SET compose = '', directory = '/srv/site', compose_file = 'compose.yaml' WHERE id = ?`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := cloner.CloneStack(ctx, source.ID, application.CloneOptions{TargetEnvironmentID: "env-staging"}); err == nil {
		t.Fatal("a directory stack must not be cloned")
	}
}
