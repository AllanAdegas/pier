package envvar

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type referenceFunc func(context.Context, string, string, string, string) (string, error)

func (f referenceFunc) Resolve(ctx context.Context, environment, server, database, field string) (string, error) {
	return f(ctx, environment, server, database, field)
}

func TestBuildEnvSecretFlagsAndReferenceResolutionDoNotMutateStorage(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	owner := Owner{Type: OwnerApplication, ID: "app"}
	original := "postgres://${{db.user}}:${{db.password}}@${{db.host}}/${{db.database}}"
	if err := store.Set(ctx, owner, "DATABASE_URL", original, false, true); err != nil {
		t.Fatal(err)
	}
	resolver := referenceFunc(func(_ context.Context, environment, server, database, field string) (string, error) {
		if environment != "env1" || server != "srv1" || database != "db" {
			t.Fatalf("incorrect scope: %s %s %s", environment, server, database)
		}
		return map[string]string{"user": "u", "password": "p${{literal.url}}", "host": "db-internal", "database": "app"}[field], nil
	})
	store.SetReferenceResolver(resolver)
	resolved, err := store.ListForDeploy(ctx, owner, "env1", "srv1")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Value != "postgres://u:p${{literal.url}}@db-internal/app" || !resolved[0].SecretBuild || resolved[0].BuildTime {
		t.Fatalf("unexpected resolution: %+v", resolved)
	}
	stored, err := store.List(ctx, owner)
	if err != nil || stored[0].Value != original || !stored[0].SecretBuild {
		t.Fatalf("storage changed: %+v %v", stored, err)
	}
	if err := store.ReplaceAll(ctx, owner, []Variable{{Key: "TOKEN", Value: "secret", BuildTime: true, SecretBuild: true}}); err != nil {
		t.Fatal(err)
	}
	stored, _ = store.List(ctx, owner)
	if len(stored) != 1 || !stored[0].BuildTime || !stored[0].SecretBuild {
		t.Fatalf("bulk edit lost flags: %+v", stored)
	}
	if err := store.Set(ctx, owner, "TOKEN", "public", true); err != nil {
		t.Fatal(err)
	}
	stored, _ = store.List(ctx, owner)
	if stored[0].SecretBuild || !stored[0].BuildTime {
		t.Fatalf("upsert did not update flags: %+v", stored)
	}
}

func TestBuildEnvReferencesFailClosed(t *testing.T) {
	resolver := referenceFunc(func(context.Context, string, string, string, string) (string, error) {
		return "", errors.New("unknown database")
	})
	for _, value := range []string{"${{db.url}}", "${{db.unknown}}", "${{db.url}", "${{missing}}", "${{db.url}} ${{bad}", "${{bad} ${{db.url}}"} {
		if _, err := ResolveReferences(context.Background(), []Variable{{Key: "X", Value: value}}, "env", "server", resolver); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if _, err := ResolveReferences(context.Background(), []Variable{{Key: "X", Value: "${{db.url}}"}}, "env", "server", nil); err == nil {
		t.Error("accepted reference without resolver")
	}
	untouched, err := ResolveReferences(context.Background(), []Variable{{Key: "X", Value: "ordinary ${ENV}"}}, "env", "server", nil)
	if err != nil || !strings.Contains(untouched[0].Value, "${ENV}") {
		t.Fatalf("normal interpolation changed: %+v %v", untouched, err)
	}
}
