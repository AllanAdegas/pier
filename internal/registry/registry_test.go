package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
)

const testPassword = "glpat-super-secret-token"

func newTestService(t *testing.T) *Service {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box := testbox.Box(t)
	return New(db, box)
}

func mustCreate(t *testing.T, s *Service, host string) Registry {
	t.Helper()
	created, err := s.Create(context.Background(), Input{Name: "Registro", Host: host, Username: "deploy", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func TestCredentialsAreSealedAndNeverListed(t *testing.T) {
	s := newTestService(t)
	created := mustCreate(t, s, "https://GHCR.io/")
	if created.Host != "ghcr.io" {
		t.Fatalf("host not normalized: %q", created.Host)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT credential FROM registries`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, testPassword) {
		t.Fatal("password stored in plaintext")
	}
	listed, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(listed)
	if strings.Contains(string(encoded), testPassword) || strings.Contains(string(encoded), "credential") {
		t.Fatalf("list leaks the credential: %s", encoded)
	}
}

func TestUpdateKeepsPasswordWhenOmittedAndRefusesDuplicateHosts(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	created := mustCreate(t, s, "registry.gitlab.com")
	if _, err := s.Update(ctx, created.ID, Input{Name: "GitLab", Host: "registry.gitlab.com", Username: "bot"}); err != nil {
		t.Fatal(err)
	}
	found, err := s.credentialByID(ctx, created.ID)
	if err != nil || found.password != testPassword || found.username != "bot" {
		t.Fatalf("credential after update: %+v %v", found, err)
	}
	_, err = s.Create(ctx, Input{Name: "Outro", Host: "registry.gitlab.com", Username: "x", Password: "y"})
	if appErr, ok := apperr.As(err); !ok || appErr.Kind != apperr.KindConflict {
		t.Fatalf("duplicate host accepted: %v", err)
	}
}

func TestCreateValidatesEveryField(t *testing.T) {
	s := newTestService(t)
	_, err := s.Create(context.Background(), Input{Name: "", Host: "bad host", Username: "a:b", Password: ""})
	appErr, ok := apperr.As(err)
	if !ok || len(appErr.Fields) != 4 {
		t.Fatalf("expected four field errors, got %v", err)
	}
}

func TestDeleteRefusesARegistryInUse(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	created := mustCreate(t, s, "ghcr.io")
	for _, statement := range []string{
		`INSERT INTO servers (id, name, kind, data_dir, created_at) VALUES ('srv1', 's', 'local', '/data', '2026-01-01T00:00:00Z')`,
		`INSERT INTO projects (id, name, created_at) VALUES ('prj', 'p', '2026-01-01T00:00:00Z')`,
		`INSERT INTO environments (id, project_id, name, created_at) VALUES ('env', 'prj', 'production', '2026-01-01T00:00:00Z')`,
		`INSERT INTO applications (id, environment_id, server_id, name, config, webhook_secret, status, created_at, updated_at)
		 VALUES ('app', 'env', 'srv1', 'a', '{}', 'secret', 'stopped', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO application_placements (application_id, registry_id, updated_at) VALUES ('app', '` + created.ID + `', '2026')`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Delete(ctx, created.ID); err == nil {
		t.Fatal("deleted a registry in use")
	}
	if _, err := s.db.Exec(`DELETE FROM application_placements`); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPublishKeepsTheSecretOutOfCommandsAndReturnsTheDigest(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	created := mustCreate(t, s, "ghcr.io")
	digest := "ghcr.io/acme/api@sha256:" + strings.Repeat("a", 64)
	ex := executor.NewFake().
		On("mktemp -d", "/tmp/pier-registry-abc123\n", 0).
		On("push", "pushed with "+testPassword, 0).
		On("RepoDigests", `["ghcr.io/acme/other@sha256:`+strings.Repeat("b", 64)+`","`+digest+`"]`, 0)
	lines := []string{}
	got, err := s.Publish(ctx, created.ID, ex, "pier/app:dep1", "acme/api", "pier-dep1", func(line string) { lines = append(lines, line) })
	if err != nil || got != digest {
		t.Fatalf("publish: %q %v", got, err)
	}
	for _, command := range ex.Commands {
		if strings.Contains(command, testPassword) {
			t.Fatalf("secret in command: %s", command)
		}
	}
	if strings.Contains(strings.Join(lines, "\n"), testPassword) {
		t.Fatal("secret echoed to the log")
	}
	config := string(ex.Files["/tmp/pier-registry-abc123/config.json"])
	token := base64.StdEncoding.EncodeToString([]byte("deploy:" + testPassword))
	if !strings.Contains(config, token) || !strings.Contains(config, `"ghcr.io"`) {
		t.Fatalf("config.json: %s", config)
	}
	if !ex.Ran("docker --config /tmp/pier-registry-abc123 push ghcr.io/acme/api:pier-dep1") {
		t.Fatalf("push not scoped to the private config: %v", ex.Commands)
	}
	if !ex.Ran("rm -rf -- /tmp/pier-registry-abc123") {
		t.Fatal("temporary configuration left behind")
	}
}

func TestPublishFailureStillRemovesTheConfiguration(t *testing.T) {
	s := newTestService(t)
	created := mustCreate(t, s, "ghcr.io")
	ex := executor.NewFake().On("mktemp -d", "/tmp/pier-registry-abc123", 0).On("push", "denied", 1)
	if _, err := s.Publish(context.Background(), created.ID, ex, "pier/app:dep1", "acme/api", "t", nil); err == nil {
		t.Fatal("push failure ignored")
	}
	if !ex.Ran("rm -rf -- /tmp/pier-registry-abc123") {
		t.Fatal("temporary configuration left behind")
	}
}

func TestPullUsesTheCredentialOfTheHostOrAPlainPull(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreate(t, s, DockerHub)
	ex := executor.NewFake().On("mktemp -d", "/tmp/pier-registry-abc123", 0)
	if err := s.Pull(ctx, ex, "acme/private:1.0", nil); err != nil {
		t.Fatal(err)
	}
	if !ex.Ran("docker --config /tmp/pier-registry-abc123 pull acme/private:1.0") {
		t.Fatalf("private pull not authenticated: %v", ex.Commands)
	}
	if !strings.Contains(string(ex.Files["/tmp/pier-registry-abc123/config.json"]), "https://index.docker.io/v1/") {
		t.Fatal("Docker Hub key missing from config.json")
	}
	plain := executor.NewFake()
	if err := s.Pull(ctx, plain, "ghcr.io/acme/public:1.0", nil); err != nil {
		t.Fatal(err)
	}
	if len(plain.Commands) != 1 || plain.Commands[0] != "docker pull ghcr.io/acme/public:1.0" {
		t.Fatalf("public pull changed: %v", plain.Commands)
	}
}

func TestDigestCanonicalizesDockerHubAndReportsLocalImages(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	hub := executor.NewFake().On("RepoDigests", `["acme/app@sha256:`+strings.Repeat("c", 64)+`"]`, 0)
	got, err := s.Digest(ctx, hub, "acme/app:1.0")
	if err != nil || got != "docker.io/acme/app@sha256:"+strings.Repeat("c", 64) {
		t.Fatalf("digest: %q %v", got, err)
	}
	local := executor.NewFake().On("RepoDigests", `[]`, 0)
	if _, err := s.Digest(ctx, local, "pier/app:dep"); !IsNoDigest(err) {
		t.Fatalf("local image: %v", err)
	}
}
