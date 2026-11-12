package application

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pier/internal/gitprovider"
)

type providerTransport func(*http.Request) (*http.Response, error)

func (f providerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func integratedProvider(t *testing.T, f *deployFixture, handler http.HandlerFunc) *gitprovider.Service {
	t.Helper()
	client := &http.Client{Transport: providerTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" {
			t.Errorf("unexpected API host: %s", r.URL.Host)
		}
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Result(), nil
	})}
	s := gitprovider.NewWithHTTPClient(f.store.db, f.store.box, client)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Configure(t.Context(), gitprovider.Config{AppID: 42, PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), WebhookSecret: strings.Repeat("s", 32)})
	if err != nil {
		t.Fatal(err)
	}
	f.deployer.SetGitProvider(s)
	return s
}

const installationSecret = "integration-installation-token"

func providerCloneHandler(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	switch r.URL.Path {
	case "/app/installations/123/access_tokens":
		json.NewEncoder(w).Encode(map[string]any{"token": installationSecret, "expires_at": time.Now().Add(time.Hour)})
	case "/installation/repositories":
		if r.Header.Get("Authorization") != "Bearer "+installationSecret {
			t.Error("missing installation bearer")
		}
		json.NewEncoder(w).Encode(map[string]any{"repositories": []map[string]any{{"id": 1, "full_name": "acme/app", "private": true}}})
	default:
		t.Errorf("unexpected API request %s %s", r.Method, r.URL)
		http.Error(w, "unexpected", 400)
	}
}
func TestGitProviderDeployUsesInstallationCredentialsAndNoPersistentToken(t *testing.T) {
	config := gitConfig()
	config.Health.Enabled = true
	f := newDeployFixture(t, config)
	provider := integratedProvider(t, f, func(w http.ResponseWriter, r *http.Request) { providerCloneHandler(t, w, r) })
	if err := provider.Select(t.Context(), "application", f.app.ID, gitprovider.Selection{InstallationID: 123, Repository: "acme/app"}); err != nil {
		t.Fatal(err)
	}
	f.ex.On("git clone --depth", "PIER_COMMIT "+strings.Repeat("a", 40)+" private commit", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	deployment := f.deploy(t)
	if deployment.Status != DeploymentFinished {
		t.Fatal(deployment.Error, f.logText(t, deployment.ID))
	}
	env := string(f.ex.Files["/data/pier/builds/"+deployment.ID+"/clone.env"])
	if !strings.Contains(env, "PIER_REPOSITORY=https://github.com/acme/app.git") || !strings.Contains(env, "Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+installationSecret))) {
		t.Fatalf("private env did not carry scoped credentials")
	}
	if !f.ex.Ran("rm -f") {
		t.Fatal("credentials cleanup absent")
	}
	for _, command := range f.ex.Commands {
		if strings.Contains(command, installationSecret) {
			t.Fatal("token in argv")
		}
	}
	if strings.Contains(f.logText(t, deployment.ID), installationSecret) {
		t.Fatal("token in logs")
	}
	app, _ := f.store.Get(t.Context(), f.app.ID)
	if strings.Contains(app.Config.Source.Repository, installationSecret) {
		t.Fatal("token persisted in source")
	}
}

func TestGitProviderMissingSelectionFallsBackButRevokedInstallationDoesNot(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "unselected", true: "revoked"}[selected], func(t *testing.T) {
			f := newDeployFixture(t, gitConfig())
			provider := integratedProvider(t, f, func(w http.ResponseWriter, r *http.Request) { http.Error(w, installationSecret, 401) })
			if selected {
				_, err := f.store.db.Exec(`INSERT INTO git_provider_selections(kind,resource_id,installation_id,repository) VALUES('application',?,123,'acme/app')`, f.app.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			f.ex.On("git clone --depth", "PIER_COMMIT "+strings.Repeat("a", 40)+" cloned", 0)
			f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
			deployment := f.deploy(t)
			if selected {
				if deployment.Status != DeploymentFailed || f.ex.Ran("git clone --depth") {
					t.Fatal("revoked installation fell back")
				}
				if strings.Contains(deployment.Error, installationSecret) {
					t.Fatal("upstream token leaked")
				}
			} else if deployment.Status != DeploymentFinished {
				t.Fatal(deployment.Error)
			}
			_ = provider
		})
	}
}

func TestGitProviderPreviewUsesParentSelectionAndReportsInOrder(t *testing.T) {
	f, _, previews := previewFixture(t)
	var mu sync.Mutex
	var states []string
	var body string
	var commentCreates, commentUpdates int
	provider := integratedProvider(t, f, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/statuses/"):
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			mu.Lock()
			states = append(states, input["state"])
			mu.Unlock()
			w.WriteHeader(201)
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/comments"):
			mu.Lock()
			defer mu.Unlock()
			if body == "" {
				w.Write([]byte(`[]`))
			} else {
				json.NewEncoder(w).Encode([]any{map[string]any{"id": 8, "body": body, "user": map[string]string{"type": "Bot"}, "performed_via_github_app": map[string]int{"id": 42}}})
			}
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/comments"):
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			mu.Lock()
			body = input["body"]
			commentCreates++
			mu.Unlock()
			w.WriteHeader(201)
		case r.Method == "PATCH" && strings.Contains(r.URL.Path, "/comments/"):
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			mu.Lock()
			body = input["body"]
			commentUpdates++
			mu.Unlock()
			w.WriteHeader(200)
		default:
			providerCloneHandler(t, w, r)
		}
	})
	if err := provider.Select(t.Context(), "application", f.app.ID, gitprovider.Selection{InstallationID: 123, Repository: "acme/app"}); err != nil {
		t.Fatal(err)
	}
	previews.SetReporter(NewGitAppReporter(provider))
	result, err := previews.HandlePullRequest(t.Context(), f.app, ProviderGitHub, previewPayload("opened", false))
	if err != nil || !result.Deployed {
		t.Fatal(result, err)
	}
	rows, _ := previews.List(t.Context(), f.app.ID)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	deployments, err := f.store.ListDeployments(t.Context(), f.app.ID, 10)
	if err != nil || len(deployments) != 1 {
		t.Fatal(deployments, err)
	}
	d := deployments[0]
	f.ex.On("git clone --depth", "PIER_COMMIT "+strings.Repeat("a", 40)+" preview", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	if _, err = f.store.claimDeployment(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	f.deployer.Run(t.Context(), d.ID)
	finished, _ := f.store.GetDeployment(t.Context(), d.ID)
	if finished.Status != DeploymentFinished {
		t.Fatal(finished.Error)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := len(states) >= 2 && strings.Contains(body, "ready")
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(states) < 2 || states[0] != "pending" || states[len(states)-1] != "success" || commentCreates != 1 || commentUpdates < 1 {
		t.Fatalf("states=%v comments=%d/%d body=%s", states, commentCreates, commentUpdates, body)
	}
	env := string(f.ex.Files["/data/pier/builds/"+d.ID+"/clone.env"])
	if !strings.Contains(env, "GIT_CONFIG_VALUE_0=Authorization: Basic") {
		t.Fatal("preview did not use parent selection")
	}
	main, _ := f.store.Get(t.Context(), f.app.ID)
	if main.CurrentImage != "" {
		t.Fatal("preview changed main app")
	}
}

func TestGitProviderConfiguredSelectionNeverFallsBackOnMissingAppConfig(t *testing.T) {
	f := newDeployFixture(t, gitConfig())
	provider := gitprovider.New(f.store.db, f.store.box)
	f.deployer.SetGitProvider(provider)
	_, err := f.store.db.Exec(`INSERT INTO git_provider_selections(kind,resource_id,installation_id,repository) VALUES('application',?,123,'acme/app')`, f.app.ID)
	if err != nil {
		t.Fatal(err)
	}
	deployment := f.deploy(t)
	if deployment.Status != DeploymentFailed || f.ex.Ran("git clone --depth") {
		t.Fatal("missing provider configuration fell back")
	}
}

func TestGitProviderRejectsStaleRepositorySelection(t *testing.T) {
	for _, source := range []string{"https://github.com/another/repo.git", "https://evil.example/acme/app.git"} {
		t.Run(source, func(t *testing.T) {
			config := gitConfig()
			config.Source.Repository = source
			f := newDeployFixture(t, config)
			provider := integratedProvider(t, f, func(w http.ResponseWriter, r *http.Request) { providerCloneHandler(t, w, r) })
			if err := provider.Select(t.Context(), "application", f.app.ID, gitprovider.Selection{InstallationID: 123, Repository: "acme/app"}); err != nil {
				t.Fatal(err)
			}
			deployment := f.deploy(t)
			if deployment.Status != DeploymentFailed || f.ex.Ran("git clone --depth") {
				t.Fatal("stale repository selection accepted")
			}
			if strings.Contains(f.logText(t, deployment.ID), installationSecret) {
				t.Fatal("token leaked")
			}
		})
	}
}
