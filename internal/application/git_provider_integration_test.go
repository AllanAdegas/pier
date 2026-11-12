package application

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"log/slog"
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

const installationSecret = "integration-installation-token"

var headCommit = strings.Repeat("a", 40)

// integratedProvider is a configured GitHub App whose API is handler.
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
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err := s.Configure(t.Context(), gitprovider.Config{AppID: 42, PrivateKey: pemKey, WebhookSecret: strings.Repeat("s", 32)}); err != nil {
		t.Fatal(err)
	}
	f.deployer.SetGitProvider(s)
	return s
}

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

// githubAPIFake records commit statuses and keeps one pull request thread.
type githubAPIFake struct {
	t        *testing.T
	mu       sync.Mutex
	states   []string
	comment  string
	creates  int
	updates  int
	failWith int
}

func (g *githubAPIFake) handle(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/access_tokens") || r.URL.Path == "/installation/repositories":
		providerCloneHandler(g.t, w, r)
	case g.failWith != 0:
		w.WriteHeader(g.failWith)
		w.Write([]byte(installationSecret))
	case strings.Contains(r.URL.Path, "/statuses/"):
		var input map[string]string
		json.NewDecoder(r.Body).Decode(&input)
		g.states = append(g.states, input["context"]+"="+input["state"])
		w.WriteHeader(http.StatusCreated)
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/comments"):
		if g.comment == "" {
			w.Write([]byte(`[]`))
			return
		}
		json.NewEncoder(w).Encode([]any{map[string]any{"id": 8, "body": g.comment, "performed_via_github_app": map[string]int{"id": 42}}})
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/comments"):
		g.saveComment(r)
		g.creates++
	case r.Method == "PATCH" && strings.HasSuffix(r.URL.Path, "/comments/8"):
		g.saveComment(r)
		g.updates++
	default:
		g.t.Errorf("unexpected API request %s %s", r.Method, r.URL)
	}
}

func (g *githubAPIFake) saveComment(r *http.Request) {
	var input map[string]string
	json.NewDecoder(r.Body).Decode(&input)
	g.comment = input["body"]
}

func (g *githubAPIFake) snapshot() (states []string, comment string, creates, updates int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.states...), g.comment, g.creates, g.updates
}

func linkApp(t *testing.T, f *deployFixture, provider *gitprovider.Service) {
	t.Helper()
	if err := provider.Select(t.Context(), gitprovider.KindApplication, f.app.ID, gitprovider.Selection{InstallationID: 123, Repository: "acme/app"}); err != nil {
		t.Fatal(err)
	}
}

func feedbackFor(provider *gitprovider.Service, logs *bytes.Buffer) *GitFeedback {
	logger := slog.New(slog.NewTextHandler(logs, nil))
	return NewGitFeedback(provider, func(context.Context) string { return "https://pier.example.com" }, logger)
}

func TestGitProviderDeployUsesInstallationCredentialsAndNoPersistentToken(t *testing.T) {
	config := gitConfig()
	config.Health.Enabled = true
	f := newDeployFixture(t, config)
	provider := integratedProvider(t, f, func(w http.ResponseWriter, r *http.Request) { providerCloneHandler(t, w, r) })
	linkApp(t, f, provider)
	f.ex.On("git clone --depth", "PIER_COMMIT "+headCommit+" private commit", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	deployment := f.deploy(t)
	if deployment.Status != DeploymentFinished {
		t.Fatal(deployment.Error, f.logText(t, deployment.ID))
	}
	env := string(f.ex.Files["/data/pier/builds/"+deployment.ID+"/clone.env"])
	if !strings.Contains(env, "PIER_REPOSITORY=https://github.com/acme/app.git") ||
		!strings.Contains(env, "Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+installationSecret))) {
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
	encoded, _ := json.Marshal(app)
	if strings.Contains(string(encoded), installationSecret) {
		t.Fatal("token persisted in or returned with the application")
	}
}

func TestGitProviderMissingSelectionFallsBackButRevokedInstallationDoesNot(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "unselected", true: "revoked"}[selected], func(t *testing.T) {
			f := newDeployFixture(t, gitConfig())
			// GitHub answers 404 for a deleted installation, with a body that must never surface.
			integratedProvider(t, f, func(w http.ResponseWriter, r *http.Request) { http.Error(w, installationSecret, 404) })
			if selected {
				_, err := f.store.db.Exec(`INSERT INTO git_provider_selections(kind,resource_id,installation_id,repository) VALUES('application',?,123,'acme/app')`, f.app.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			f.ex.On("git clone --depth", "PIER_COMMIT "+headCommit+" cloned", 0)
			f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
			deployment := f.deploy(t)
			if !selected {
				if deployment.Status != DeploymentFinished {
					t.Fatal(deployment.Error)
				}
				return
			}
			if deployment.Status != DeploymentFailed || f.ex.Ran("git clone --depth") {
				t.Fatal("revoked installation fell back")
			}
			if strings.Contains(deployment.Error, installationSecret) || !strings.Contains(deployment.Error, "instalação do GitHub App") {
				t.Fatal("unexpected failure reason", deployment.Error)
			}
		})
	}
}

func TestGitProviderConfiguredSelectionNeverFallsBackOnMissingAppConfig(t *testing.T) {
	f := newDeployFixture(t, gitConfig())
	f.deployer.SetGitProvider(gitprovider.New(f.store.db, f.store.box))
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
			linkApp(t, f, provider)
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

func TestDeployCommitStatusesAreReportedInOrder(t *testing.T) {
	config := gitConfig()
	config.Health.Enabled = true
	f := newDeployFixture(t, config)
	api := &githubAPIFake{t: t}
	provider := integratedProvider(t, f, api.handle)
	linkApp(t, f, provider)
	var logs bytes.Buffer
	feedback := feedbackFor(provider, &logs)
	f.deployer.SetDeploymentReporter(feedback)
	f.ex.On("git clone --depth", "PIER_COMMIT "+headCommit+" ok", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	if deployment := f.deploy(t); deployment.Status != DeploymentFinished {
		t.Fatal(deployment.Error)
	}
	feedback.wait()
	states, _, _, _ := api.snapshot()
	if strings.Join(states, ",") != "pier/demo=pending,pier/demo=success" {
		t.Fatal(states, logs.String())
	}
	// Turning commit statuses off is honoured.
	if _, err := provider.SaveFeedback(t.Context(), gitprovider.KindApplication, f.app.ID, "", gitprovider.FeedbackSettings{Provider: gitprovider.ProviderNone}); err != nil {
		t.Fatal(err)
	}
	f.deploy(t)
	feedback.wait()
	if again, _, _, _ := api.snapshot(); len(again) != 2 {
		t.Fatal("statuses sent with feedback off", again)
	}
}

func TestFailingProviderAPINeverBlocksOrFailsTheDeploy(t *testing.T) {
	config := gitConfig()
	config.Health.Enabled = true
	f := newDeployFixture(t, config)
	api := &githubAPIFake{t: t, failWith: http.StatusInternalServerError}
	provider := integratedProvider(t, f, api.handle)
	linkApp(t, f, provider)
	var logs bytes.Buffer
	feedback := feedbackFor(provider, &logs)
	f.deployer.SetDeploymentReporter(feedback)
	f.ex.On("git clone --depth", "PIER_COMMIT "+headCommit+" ok", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	deployment := f.deploy(t)
	if deployment.Status != DeploymentFinished {
		t.Fatal("provider failure failed the deploy:", deployment.Error)
	}
	feedback.wait()
	if !strings.Contains(logs.String(), "git feedback failed") || !strings.Contains(logs.String(), "HTTP 500") {
		t.Fatal("failure not logged", logs.String())
	}
	if strings.Contains(logs.String(), installationSecret) || strings.Contains(f.logText(t, deployment.ID), installationSecret) {
		t.Fatal("token or upstream body in logs")
	}
}

func TestFeedbackNeverBlocksWhenTheQueueIsFull(t *testing.T) {
	release := make(chan struct{})
	source := blockingSource{release: release}
	var logs bytes.Buffer
	feedback := NewGitFeedback(source, func(context.Context) string { return "" }, slog.New(slog.NewTextHandler(&logs, nil)))
	report := DeploymentReport{Application: Application{ID: "same"}, Deployment: Deployment{CommitSHA: headCommit}}
	done := make(chan struct{})
	go func() {
		for range feedbackLaneBacklog * 3 {
			feedback.ReportDeployment(t.Context(), report)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reporting blocked the caller")
	}
	close(release)
	feedback.wait()
	if !strings.Contains(logs.String(), "queue full") {
		t.Fatal("dropped reports not logged")
	}
}

type blockingSource struct{ release chan struct{} }

func (b blockingSource) Feedback(ctx context.Context, _, _, _ string) (gitprovider.Feedback, error) {
	<-b.release
	return gitprovider.Feedback{}, gitprovider.ErrNoFeedback
}

func TestGitProviderPreviewUsesParentSelectionAndKeepsOneComment(t *testing.T) {
	f, _, previews := previewFixture(t)
	api := &githubAPIFake{t: t}
	provider := integratedProvider(t, f, api.handle)
	linkApp(t, f, provider)
	var logs bytes.Buffer
	feedback := feedbackFor(provider, &logs)
	previews.SetReporter(feedback)
	result, err := previews.HandlePullRequest(t.Context(), f.app, ProviderGitHub, previewPayload("opened", false))
	if err != nil || !result.Deployed {
		t.Fatal(result, err)
	}
	d, err := f.store.GetDeployment(t.Context(), result.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	f.ex.On("git clone --depth", "PIER_COMMIT "+headCommit+" preview", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	if _, err = f.store.claimDeployment(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	f.deployer.Run(t.Context(), d.ID)
	if finished, _ := f.store.GetDeployment(t.Context(), d.ID); finished.Status != DeploymentFinished {
		t.Fatal(finished.Error)
	}
	feedback.wait()
	states, comment, creates, updates := api.snapshot()
	if strings.Join(states, ",") != "pier/preview: demo=pending,pier/preview: demo=success" {
		t.Fatal(states, logs.String())
	}
	if creates != 1 || updates != 1 || !strings.Contains(comment, "https://pr-7.preview.example.com") ||
		!strings.HasPrefix(comment, "<!-- pier:preview:"+f.app.ID+" -->") {
		t.Fatalf("comments %d/%d: %s", creates, updates, comment)
	}
	env := string(f.ex.Files["/data/pier/builds/"+d.ID+"/clone.env"])
	if !strings.Contains(env, "GIT_CONFIG_VALUE_0=Authorization: Basic") {
		t.Fatal("preview did not use parent selection")
	}
	if main, _ := f.store.Get(t.Context(), f.app.ID); main.CurrentImage != "" {
		t.Fatal("preview changed main app")
	}
}

func TestGitHubAppEventDeploysOnlyTheLinkedSource(t *testing.T) {
	f, s, _ := previewFixture(t)
	f.app.Config.AutoDeploy = true
	if _, err := s.Update(t.Context(), f.app.ID, UpdateInput{Name: f.app.Name, Config: f.app.Config}); err != nil {
		t.Fatal(err)
	}
	push := []byte(`{"ref":"refs/heads/main","after":"` + headCommit + `"}`)
	result, err := s.HandleGitHubAppEvent(t.Context(), f.app.ID, "acme/app", "push", push)
	if err != nil || !result.Deployed {
		t.Fatal(result, err)
	}
	other, err := s.HandleGitHubAppEvent(t.Context(), f.app.ID, "acme/other", "push", push)
	if err != nil || other.Deployed {
		t.Fatal("event of another repository deployed", other, err)
	}
	branch, _ := s.HandleGitHubAppEvent(t.Context(), f.app.ID, "acme/app", "push", []byte(`{"ref":"refs/heads/dev"}`))
	if branch.Deployed {
		t.Fatal("push on another branch deployed")
	}
}

func bitbucketSignature(secret string, body []byte) http.Header {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	header := http.Header{}
	header.Set("X-Hub-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	header.Set("X-Event-Key", "repo:push")
	return header
}

func TestBitbucketPushWebhookIsVerifiedAndDeploysTheBranch(t *testing.T) {
	f, s, _ := previewFixture(t)
	f.app.Config.AutoDeploy = true
	f.app.Config.Source.Repository = "https://bitbucket.org/ws/app.git"
	if _, err := s.Update(t.Context(), f.app.ID, UpdateInput{Name: f.app.Name, Config: f.app.Config}); err != nil {
		t.Fatal(err)
	}
	secret, err := s.WebhookSecret(t.Context(), f.app.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"push":{"changes":[{"new":{"type":"branch","name":"main","target":{"hash":"` + headCommit + `"}},"old":null,"closed":false}]}}`)
	if _, err := s.HandleGitPush(t.Context(), f.app.ID, ProviderBitbucket, bitbucketSignature("wrong-secret", body), body); err == nil {
		t.Fatal("wrong signature accepted")
	}
	unsigned := http.Header{"X-Event-Key": {"repo:push"}}
	if _, err := s.HandleGitPush(t.Context(), f.app.ID, ProviderBitbucket, unsigned, body); err == nil {
		t.Fatal("unsigned webhook accepted")
	}
	result, err := s.HandleGitPush(t.Context(), f.app.ID, ProviderBitbucket, bitbucketSignature(secret, body), body)
	if err != nil || !result.Deployed {
		t.Fatal(result, err)
	}
	deleted := []byte(`{"push":{"changes":[{"new":null,"old":{"type":"branch","name":"main"},"closed":true}]}}`)
	push, ok := ParsePush(deleted)
	if !ok || push.Branch != "main" || !push.Removed {
		t.Fatal(push)
	}
}

type resolverFake struct{ calls int }

func (r *resolverFake) ResolveBitbucketCommit(_ context.Context, kind, id, repository, commit string) (string, error) {
	r.calls++
	if kind != gitprovider.KindApplication || repository != "ws/app" || commit != "aaaaaaaaaaaa" {
		return "", gitprovider.ErrNoFeedback
	}
	return headCommit, nil
}

func TestBitbucketPullRequestBecomesAPreviewWithTheFullCommit(t *testing.T) {
	f, s, previews := previewFixture(t)
	s.previews = previews
	resolver := &resolverFake{}
	s.SetCommitResolver(resolver)
	secret, _ := s.WebhookSecret(t.Context(), f.app.ID)
	body := []byte(`{"pullrequest":{"id":9,"title":"Feature","state":"OPEN","author":{"nickname":"dev"},
		"source":{"branch":{"name":"feature"},"commit":{"hash":"aaaaaaaaaaaa"},"repository":{"full_name":"ws/app","uuid":"{1}"}},
		"destination":{"branch":{"name":"main"},"repository":{"full_name":"ws/app","uuid":"{1}"}}}}`)
	header := bitbucketSignature(secret, body)
	header.Set("X-Event-Key", "pullrequest:created")
	result, err := s.HandleGitPush(t.Context(), f.app.ID, ProviderBitbucket, header, body)
	if err != nil || !result.Deployed || resolver.calls != 1 {
		t.Fatal(result, err, resolver.calls)
	}
	rows, _ := previews.List(t.Context(), f.app.ID)
	if len(rows) != 1 || rows[0].CommitSHA != headCommit || rows[0].Fork || rows[0].Provider != ProviderBitbucket {
		t.Fatal(rows)
	}
	forked := bytes.Replace(body, []byte(`"uuid":"{1}"}},
		"destination"`), []byte(`"uuid":"{2}"}},
		"destination"`), 1)
	header = bitbucketSignature(secret, forked)
	header.Set("X-Event-Key", "pullrequest:updated")
	result, err = s.HandleGitPush(t.Context(), f.app.ID, ProviderBitbucket, header, forked)
	if err != nil || result.Deployed {
		t.Fatal("fork pull request deployed without opting in", result, err)
	}
}

func TestBitbucketCloneUsesTheTokenOnlyInTheCredentialFile(t *testing.T) {
	const token = "bitbucket-access-token"
	config := gitConfig()
	config.Health.Enabled = true
	config.Source.Repository = "https://x-token-auth:" + token + "@bitbucket.org/ws/app.git"
	f := newDeployFixture(t, config)
	f.ex.On("git clone --depth", "PIER_COMMIT "+headCommit+" ok", 0)
	f.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	deployment := f.deploy(t)
	if deployment.Status != DeploymentFinished {
		t.Fatal(deployment.Error)
	}
	env := string(f.ex.Files["/data/pier/builds/"+deployment.ID+"/clone.env"])
	if !strings.Contains(env, "PIER_REPOSITORY=https://bitbucket.org/ws/app.git") ||
		!strings.Contains(env, "Basic "+base64.StdEncoding.EncodeToString([]byte("x-token-auth:"+token))) {
		t.Fatal("Bitbucket token not passed as a header")
	}
	for _, command := range f.ex.Commands {
		if strings.Contains(command, token) {
			t.Fatal("token in argv")
		}
	}
	if strings.Contains(f.logText(t, deployment.ID), token) {
		t.Fatal("token in deploy log")
	}
}
