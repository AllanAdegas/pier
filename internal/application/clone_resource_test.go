package application

import (
	"context"
	"fmt"
	"testing"

	"pier/internal/envvar"
)

func cloneFixture(t *testing.T) (*deployFixture, *Service, Application) {
	t.Helper()
	config := gitConfig()
	config.Source.Repository = "https://user:repo-token@github.com/acme/app.git"
	config.Network.Domains = []string{"https://app.example.com", "http://demo-production-abc123.203.0.113.7.sslip.io"}
	config.Limits.Memory = "512m"
	fixture := newDeployFixture(t, config)
	for _, statement := range []string{
		`INSERT INTO environments (id, project_id, name, created_at) VALUES ('env-staging', 'prj1', 'staging', '2026-01-01T00:00:00Z')`,
		`INSERT INTO projects (id, name, created_at) VALUES ('prj2', 'other', '2026-01-01T00:00:00Z')`,
		`INSERT INTO environments (id, project_id, name, created_at) VALUES ('env-other', 'prj2', 'production', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := fixture.store.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.store.saveAutoDomain(context.Background(), fixture.app.ID, "demo-production-abc123.203.0.113.7.sslip.io"); err != nil {
		t.Fatal(err)
	}
	owner := envvar.Owner{Type: envvar.OwnerApplication, ID: fixture.app.ID}
	if err := fixture.env.ReplaceAll(context.Background(), owner, []envvar.Variable{
		{Key: "API_KEY", Value: "live-secret", SecretBuild: true},
		{Key: "DATABASE_URL", Value: "${{db.url}}"},
	}); err != nil {
		t.Fatal(err)
	}
	return fixture, NewService(fixture.store, fixture.env, fakeConnector{fixture.ex}, nil), fixture.app
}

// TestCloneApplicationMatrix covers every combination of target (same
// environment, same project, other project) and the three options.
func TestCloneApplicationMatrix(t *testing.T) {
	for _, target := range []string{"env1", "env-staging", "env-other"} {
		for _, variables := range []bool{false, true} {
			for _, domains := range []bool{false, true} {
				for _, secrets := range []bool{false, true} {
					name := fmt.Sprintf("%s/vars=%v/domains=%v/secrets=%v", target, variables, domains, secrets)
					t.Run(name, func(t *testing.T) {
						checkClone(t, target, CloneOptions{TargetEnvironmentID: target, CopyVariables: variables, CopyDomains: domains, CopySecrets: secrets})
					})
				}
			}
		}
	}
}

func checkClone(t *testing.T, target string, options CloneOptions) {
	fixture, service, source := cloneFixture(t)
	ctx := context.Background()
	clone, err := service.Clone(ctx, source.ID, options)
	if err != nil {
		t.Fatal(err)
	}
	sameProject := target != "env-other"
	keepSecrets := sameProject || options.CopySecrets
	if clone.ID == source.ID || clone.EnvironmentID != target || clone.ServerID != source.ServerID || clone.CurrentImage != "" {
		t.Fatalf("clone placement = %+v", clone)
	}
	if expected := map[bool]string{true: "demo (cópia)", false: "demo"}[target == "env1"]; clone.Name != expected {
		t.Errorf("name = %q, want %q", clone.Name, expected)
	}
	if clone.Config.Limits.Memory != "512m" {
		t.Error("configuration not copied")
	}
	wantRepository := map[bool]string{true: source.Config.Source.Repository, false: "https://github.com/acme/app.git"}[keepSecrets]
	if clone.Config.Source.Repository != wantRepository {
		t.Errorf("repository = %s, want %s", clone.Config.Source.Repository, wantRepository)
	}
	wantDomains := 0
	if options.CopyDomains {
		wantDomains = 1 // the automatic domain belongs to the source
	}
	if len(clone.Config.Network.Domains) != wantDomains || (wantDomains == 1 && clone.Config.Network.Domains[0] != "https://app.example.com") {
		t.Errorf("domains = %v", clone.Config.Network.Domains)
	}
	values, _ := fixture.env.Map(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: clone.ID})
	if !options.CopyVariables {
		if len(values) != 0 {
			t.Errorf("variables copied without the option: %v", values)
		}
		return
	}
	wantSecret := map[bool]string{true: "live-secret", false: ""}[keepSecrets]
	if values["API_KEY"] != wantSecret || values["DATABASE_URL"] != "${{db.url}}" || len(values) != 2 {
		t.Errorf("variables = %v (want API_KEY=%q)", values, wantSecret)
	}
}

func TestCloneRejectsUnknownTarget(t *testing.T) {
	_, service, source := cloneFixture(t)
	if _, err := service.Clone(context.Background(), source.ID, CloneOptions{}); err == nil {
		t.Error("a clone needs a target environment")
	}
	if _, err := service.Clone(context.Background(), source.ID, CloneOptions{TargetEnvironmentID: "nope"}); err == nil {
		t.Error("unknown target environment accepted")
	}
}
