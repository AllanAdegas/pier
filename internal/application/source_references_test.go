package application

import (
	"context"
	"strings"
	"testing"

	"pier/internal/database"
)

type referenceDatabaseFake struct {
	databases  []database.Database
	connection database.Connection
	requested  []string
}

func (f *referenceDatabaseFake) List(context.Context, string) ([]database.Database, error) {
	return f.databases, nil
}
func (f *referenceDatabaseFake) Connection(_ context.Context, id string) (database.Connection, error) {
	f.requested = append(f.requested, id)
	return f.connection, nil
}

func TestSourceDatabaseReferencesEnforcePlacementAndUseCurrentConnection(t *testing.T) {
	f := &referenceDatabaseFake{databases: []database.Database{{ID: "db1", Name: "main-db", EnvironmentID: "env1", ServerID: "srv1"}}, connection: database.Connection{Host: "db1", Port: 5432, InternalURL: "postgres://internal"}}
	f.connection.Credentials.Username = "app"
	f.connection.Credentials.Password = "new-password"
	f.connection.Credentials.Database = "appdb"
	resolver := DatabaseReferences{Databases: f}
	for field, want := range map[string]string{"url": "postgres://internal", "host": "db1", "port": "5432", "user": "app", "password": "new-password", "database": "appdb"} {
		got, err := resolver.Resolve(context.Background(), "env1", "srv1", "main-db", field)
		if err != nil || got != want {
			t.Errorf("%s=%q (%v), want %q", field, got, err, want)
		}
	}
	f.connection.Credentials.Password = "rotated"
	if value, _ := resolver.Resolve(context.Background(), "env1", "srv1", "db1", "password"); value != "rotated" {
		t.Error("did not use current credentials")
	}
	count := len(f.requested)
	if _, err := resolver.Resolve(context.Background(), "env2", "srv1", "db1", "password"); err == nil || len(f.requested) != count {
		t.Error("read credentials across environments")
	}
	if _, err := resolver.Resolve(context.Background(), "env1", "srv2", "db1", "url"); err == nil {
		t.Error("used an unreachable internal URL")
	}
	f.connection.PublicURL = "postgres://user:password@public.example:15432/db"
	for field, want := range map[string]string{"url": f.connection.PublicURL, "host": "public.example", "port": "15432"} {
		got, err := resolver.Resolve(context.Background(), "env1", "srv2", "db1", field)
		if err != nil || got != want {
			t.Errorf("public %s=%q (%v), want %q", field, got, err, want)
		}
	}
	f.databases = append(f.databases, database.Database{ID: "db2", Name: "main-db", EnvironmentID: "env1", ServerID: "srv1"})
	if _, err := resolver.Resolve(context.Background(), "env1", "srv1", "main-db", "url"); err == nil || !strings.Contains(err.Error(), "ID") {
		t.Error("accepted ambiguous name")
	}
	if _, err := resolver.Resolve(context.Background(), "env1", "srv1", "db1", "url"); err != nil {
		t.Fatal(err)
	}
}
