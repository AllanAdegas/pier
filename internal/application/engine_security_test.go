package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"pier/internal/platform/apperr"
	"pier/internal/server"
)

func TestEngineMalformedRepositoryCredentialsFailClosed(t *testing.T) {
	for _, password := range []string{"dummy^token", "dummy%zztoken", "dummy#token"} {
		repository := "https://dummy-user:" + password + "@git.invalid/org/repo.git"
		if got := withoutCredentials(repository); got != "https://***@git.invalid/org/repo.git" {
			t.Errorf("masked repository: %s", got)
		}
		config := gitConfig()
		config.Source.Repository = repository
		if fieldsOf(t, config.Validate())["source.repository"] == "" {
			t.Errorf("malformed URL accepted: %s", repository)
		}
		var redactor OutputRedactor
		redactor.repository(repository)
		if got := redactor.Mask("rejected " + repository + " password=" + password); strings.Contains(got, password) {
			t.Errorf("error leaked: %s", got)
		}
	}
	if got := withoutCredentials("ssh://user:dummy-pass@git.invalid/repo.git"); got != "ssh://***@git.invalid/repo.git" {
		t.Fatal(got)
	}
	config := gitConfig()
	config.Source.Repository = "https://user:dummy%23token@git.invalid/repo.git"
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEngineCommitPinsRequireFullObjectIDs(t *testing.T) {
	for _, length := range []int{7, 39, 40, 41, 63, 64, 65} {
		config := gitConfig()
		config.Source.Commit = strings.Repeat("a", length)
		fields := fieldsOf(t, config.Validate())
		invalid := fields["source.commit"] != ""
		if invalid == (length == 40 || length == 64) {
			t.Errorf("length %d invalid=%v", length, invalid)
		}
	}
}

func TestEngineEmptyHMACSecretRejected(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, nil)
	mac.Write(body)
	if validHMAC("", body, hex.EncodeToString(mac.Sum(nil))) {
		t.Fatal("empty secret accepted")
	}
}

func TestEngineTrimmedAndOverlappingSecrets(t *testing.T) {
	var r OutputRedactor
	r.Redact(" secret-one \n", "secret-one-plus", "  secret-two  ")
	output := r.Mask("secret-one-plus / secret-one / secret-two")
	if strings.Contains(output, "secret-") || strings.Contains(output, "plus") {
		t.Fatal(output)
	}
}

func TestEngineRejectsInheritedRoutingAndOwnershipLabels(t *testing.T) {
	for _, labels := range []string{`{"traefik.http.routers.evil.rule":"HostRegexp(...)"}`, `{"pier.proxy":"forged"}`, `{"pier.managed":"true"}`, `{"pier.id":"other"}`, `{"Traefik.enable":"true"}`} {
		t.Run(labels, func(t *testing.T) {
			f := newDeployFixture(t, imageConfig())
			f.ex.On("docker image inspect --format '{{json .Config.Labels}}'", labels, 0)
			dep := f.deploy(t)
			if dep.Status != DeploymentFailed || !strings.Contains(dep.Error, "etiquetas reservadas") {
				t.Fatalf("%s: %s", dep.Status, dep.Error)
			}
			if f.ex.Ran("docker compose") {
				t.Fatal("untrusted image was authorized for routing")
			}
		})
	}
	f := newDeployFixture(t, gitConfig())
	j := sourceTestJob(f)
	f.ex.On("{{json .Config.Labels}}", fmt.Sprintf(`{"pier.managed":"true","pier.id":%q}`, j.app.ID), 0)
	if err := j.checkImageLabels(context.Background(), j.imageTag()); err != nil {
		t.Fatal(err)
	}
	f.ex.On("{{json .Config.Labels}}", `garbage`, 0)
	if err := j.checkImageLabels(context.Background(), j.imageTag()); err == nil {
		t.Fatal("invalid inspection accepted")
	}
}

func TestEngineComposeRequiresPersistedRouteTokenAndRotatesLogs(t *testing.T) {
	spec := releaseSpec{App: Application{ID: "app", Config: imageConfig()}, Image: "nginx", ContainerName: "app-new", Server: server.Server{ProxyToken: "persisted-token"}}
	file, err := composeFor(spec)
	if err != nil {
		t.Fatal(err)
	}
	if file.Services["app-new"].Labels[server.ProxyLabel] != "persisted-token" {
		t.Fatal("missing token")
	}
	output, err := file.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"logging:", "driver: local", "max-size: 10m", `max-file: "3"`} {
		if !strings.Contains(string(output), fragment) {
			t.Errorf("missing %s: %s", fragment, output)
		}
	}
	spec.App.Config.Network.Domains = nil
	file, err = composeFor(spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := file.Services["app-new"].Labels[server.ProxyLabel]; ok {
		t.Fatal("unrouted service authorized")
	}
}

func TestEngineRepositoryCredentialsMaskToolErrors(t *testing.T) {
	config := gitConfig()
	config.Source.Repository = "https://user:dummy-repo-token@git.invalid/app"
	f := newDeployFixture(t, config)
	f.ex.On("git clone --depth", "fatal: dummy-repo-token denied", 1)
	dep := f.deploy(t)
	if dep.Status != DeploymentFailed {
		t.Fatal(dep.Status)
	}
	if strings.Contains(f.logText(t, dep.ID)+dep.Error, "dummy-repo-token") {
		t.Fatal("credential leaked")
	}
}

func requireConflict(t *testing.T, err error) {
	t.Helper()
	e, ok := apperr.As(err)
	if !ok || e.Kind != apperr.KindConflict {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestEngineMissingDeploymentStopsBeforeRelease(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	if err := f.store.setDeploymentImage(context.Background(), "missing", "nginx"); err == nil {
		t.Fatal("missing row updated")
	}
}

func TestEngineRollbackAllowsItsOriginalBuildLabelsAfterPackChange(t *testing.T) {
	f := newDeployFixture(t, imageConfig())
	j := sourceTestJob(f)
	image := "pier/" + j.app.ID + ":previous-build"
	j.deployment.RollbackImage = image
	f.ex.On("{{json .Config.Labels}}", fmt.Sprintf(`{"pier.managed":"true","pier.id":%q}`, j.app.ID), 0)
	if err := j.checkImageLabels(context.Background(), image); err != nil {
		t.Fatal("valid rollback denied after pack change", err)
	}
	f.ex.On("{{json .Config.Labels}}", fmt.Sprintf(`{"pier.managed":"true","pier.id":%q,"traefik.enable":"true"}`, j.app.ID), 0)
	if err := j.checkImageLabels(context.Background(), image); err == nil {
		t.Fatal("rollback authorized inherited routing labels")
	}
}
