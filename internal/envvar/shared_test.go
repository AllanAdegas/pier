package envvar

import (
	"context"
	"strings"
	"testing"
)

func sharedStore(t *testing.T) *Store {
	t.Helper()
	store := newStore(t)
	for _, statement := range []string{
		`INSERT INTO projects (id, name, created_at) VALUES ('prj1', 'p', '2026-01-01T00:00:00Z'), ('prj2', 'q', '2026-01-01T00:00:00Z')`,
		`INSERT INTO environments (id, project_id, name, created_at) VALUES ('env1', 'prj1', 'production', '2026-01-01T00:00:00Z'),
			('env2', 'prj1', 'staging', '2026-01-01T00:00:00Z'), ('env3', 'prj2', 'production', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := store.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func setShared(t *testing.T, store *Store, scope SharedScope, scopeID, key, value string, secret bool) {
	t.Helper()
	if _, _, err := store.SetShared(context.Background(), scope, scopeID, SharedInput{Key: key, Value: &value, Secret: secret}); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

type fixedResolver map[string]string

func (r fixedResolver) Resolve(_ context.Context, _, _, database, field string) (string, error) {
	return r[database+"."+field], nil
}

func TestSharedResolutionOrder(t *testing.T) {
	store := sharedStore(t)
	setShared(t, store, SharedProject, "prj1", "API_URL", "https://project.example", false)
	setShared(t, store, SharedEnvironment, "env1", "API_URL", "https://env1.example", false)
	setShared(t, store, SharedEnvironment, "env2", "API_URL", "https://env2.example", false)
	setShared(t, store, SharedProject, "prj1", "TRICKY", "placeholder", false)
	store.SetReferenceResolver(fixedResolver{"db.url": "postgres://db", "project.url": "postgres://project-db"})
	if err := store.ReplaceAll(context.Background(), Owner{Type: OwnerApplication, ID: "app"}, []Variable{
		{Key: "FROM_PROJECT", Value: "${{project.API_URL}}"},
		{Key: "FROM_ENVIRONMENT", Value: "${{environment.API_URL}}/v1"},
		{Key: "MIXED", Value: "${{db.url}}|${{project.API_URL}}|${{environment.API_URL}}"},
		{Key: "DATABASE_NAMED_PROJECT", Value: "${{project.url}}"},
		{Key: "API_URL", Value: "own"},
	}); err != nil {
		t.Fatal(err)
	}
	// The literal is stored before the shared store forbids it, to prove a
	// resolved value is never scanned again.
	if _, err := store.db.Exec(`UPDATE shared_variables SET value = ? WHERE key = 'TRICKY'`, mustSeal(t, store, "${{db.url}}")); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), Owner{Type: OwnerApplication, ID: "app"}, "LITERAL", "${{project.TRICKY}}", false); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.ListForDeploy(context.Background(), Owner{Type: OwnerApplication, ID: "app"}, "env1", "srv1")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, variable := range resolved {
		got[variable.Key] = variable.Value
	}
	want := map[string]string{
		"FROM_PROJECT": "https://project.example", "FROM_ENVIRONMENT": "https://env1.example/v1",
		"MIXED": "postgres://db|https://project.example|https://env1.example", "DATABASE_NAMED_PROJECT": "postgres://project-db",
		"API_URL": "own", "LITERAL": "${{db.url}}",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %q, want %q", key, got[key], value)
		}
	}
	stored, _ := store.Map(context.Background(), Owner{Type: OwnerApplication, ID: "app"})
	if stored["FROM_PROJECT"] != "${{project.API_URL}}" {
		t.Error("the stored value must keep the reference")
	}
}

func mustSeal(t *testing.T, store *Store, value string) string {
	t.Helper()
	sealed, err := store.box.Seal(value)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestSharedMissingReferenceFailsWithoutValues(t *testing.T) {
	store := sharedStore(t)
	setShared(t, store, SharedProject, "prj2", "ONLY_OTHER_PROJECT", "other-project-secret", true)
	setShared(t, store, SharedEnvironment, "env2", "ONLY_STAGING", "staging-secret", true)
	cases := map[string]string{
		"${{project.ONLY_OTHER_PROJECT}}": "o projeto não tem a variável compartilhada ONLY_OTHER_PROJECT",
		"${{environment.ONLY_STAGING}}":   "o ambiente não tem a variável compartilhada ONLY_STAGING",
		"x-${{environment.MISSING}}-y":    "MY_VAR usa ${{environment.MISSING}}",
	}
	for value, message := range cases {
		_, err := ResolveAll(context.Background(), []Variable{{Key: "MY_VAR", Value: value}}, mustShared(t, store, "env1"), "env1", "srv1", nil)
		if err == nil || !strings.Contains(err.Error(), message) {
			t.Errorf("%s: error = %v, want %q", value, err, message)
		}
		if err != nil && (strings.Contains(err.Error(), "-secret")) {
			t.Errorf("error leaked a value: %v", err)
		}
	}
}

func mustShared(t *testing.T, store *Store, environmentID string) SharedValues {
	t.Helper()
	values, err := store.SharedValuesFor(context.Background(), environmentID)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func TestSharedVariablesStoreSealsMasksAndValidates(t *testing.T) {
	store := sharedStore(t)
	ctx := context.Background()
	setShared(t, store, SharedProject, "prj1", "TOKEN", "super-secret-token", true)
	var raw string
	if err := store.db.QueryRow(`SELECT value FROM shared_variables WHERE key = 'TOKEN'`).Scan(&raw); err != nil || strings.Contains(raw, "super-secret") {
		t.Fatalf("value not sealed at rest: %q %v", raw, err)
	}
	variables, _ := store.ListShared(ctx, SharedProject, "prj1")
	if masked := variables[0].Masked(); masked.Value != "" || !masked.Secret {
		t.Fatalf("secret not masked: %+v", masked)
	}
	_, changed, err := store.SetShared(ctx, SharedProject, "prj1", SharedInput{Key: "TOKEN", Secret: true})
	if err != nil || changed {
		t.Fatalf("re-flagging without a value must keep it: changed=%v %v", changed, err)
	}
	if variables, _ = store.ListShared(ctx, SharedProject, "prj1"); variables[0].Value != "super-secret-token" {
		t.Fatal("value lost on update without value")
	}
	other := "rotated"
	if _, changed, _ = store.SetShared(ctx, SharedProject, "prj1", SharedInput{Key: "TOKEN", Value: &other, Secret: true}); !changed {
		t.Fatal("a new value must report a change")
	}
	for _, input := range []SharedInput{{Key: "1BAD", Value: &other}, {Key: "NEW"}, {Key: "REF", Value: ptr("${{project.X}}")}} {
		if _, _, err := store.SetShared(ctx, SharedProject, "prj1", input); err == nil {
			t.Errorf("invalid input accepted: %+v", input)
		}
	}
	if _, _, err := store.SetShared(ctx, SharedScope("team"), "prj1", SharedInput{Key: "A", Value: &other}); err == nil {
		t.Error("unknown scope accepted")
	}
	if err := store.DeleteShared(ctx, SharedProject, "prj1", "TOKEN"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteShared(ctx, SharedProject, "prj1", "TOKEN"); err == nil {
		t.Fatal("deleting a missing variable must fail")
	}
	setShared(t, store, SharedEnvironment, "env3", "GONE", "x", false)
	if _, err := store.db.Exec(`DELETE FROM projects WHERE id = 'prj2'`); err != nil {
		t.Fatal(err)
	}
	if left, _ := store.ListShared(ctx, SharedEnvironment, "env3"); len(left) != 0 {
		t.Fatal("shared variables must go with their environment")
	}
}

func ptr(value string) *string { return &value }

func TestForCloneKeepsOnlyReferencesWithoutSecrets(t *testing.T) {
	variables := []Variable{
		{Key: "SECRET", Value: "s3cr3t", SecretBuild: true},
		{Key: "REF", Value: "${{db.url}}${{project.API}}"},
		{Key: "MIXED", Value: "prefix-${{db.url}}"},
	}
	blank := ForClone(variables, false)
	if blank[0].Value != "" || blank[1].Value != "${{db.url}}${{project.API}}" || blank[2].Value != "" || !blank[0].SecretBuild {
		t.Fatalf("cross-project copy = %+v", blank)
	}
	if kept := ForClone(variables, true); kept[0].Value != "s3cr3t" || kept[2].Value != "prefix-${{db.url}}" {
		t.Fatalf("copy with secrets = %+v", kept)
	}
}
