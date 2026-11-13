package gitprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"pier/internal/platform/apperr"
)

// manifestLifetime is how long a "create the App on GitHub" round trip may take.
const manifestLifetime = 30 * time.Minute

var (
	manifestCode = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)
	githubLogin  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
)

// WebhookPath is where GitHub delivers the App webhook.
const WebhookPath = "/webhooks/git-provider/github"

// CallbackPath is the page GitHub sends the browser back to with the code.
const CallbackPath = "/settings/git-provider"

// ManifestStart is what the browser posts to GitHub to create the App: the
// form action and the "manifest" field.
type ManifestStart struct {
	ActionURL string `json:"action_url"`
	Manifest  string `json:"manifest"`
}

// StartManifest prepares the GitHub App manifest flow: GitHub creates the
// App with the webhook already pointing at publicURL and returns its
// credentials once, so nobody copies a secret by hand. organization is
// optional (empty: the App belongs to the user who confirms it).
func (s *Service) StartManifest(publicURL, organization string) (ManifestStart, error) {
	base, err := validPublicURL(publicURL)
	if err != nil {
		return ManifestStart{}, err
	}
	if organization != "" && !githubLogin.MatchString(organization) {
		return ManifestStart{}, apperr.Invalid("Organização do GitHub inválida.")
	}
	state, err := s.newManifestState()
	if err != nil {
		return ManifestStart{}, err
	}
	manifest, err := json.Marshal(map[string]any{
		"name":            appName(base),
		"url":             base.String(),
		"hook_attributes": map[string]any{"url": base.String() + WebhookPath, "active": true},
		"redirect_url":    base.String() + CallbackPath,
		"public":          false,
		"default_permissions": map[string]string{
			"contents": "read", "metadata": "read", "statuses": "write", "pull_requests": "write",
		},
		"default_events": []string{EventPush, EventPullRequest},
	})
	if err != nil {
		return ManifestStart{}, err
	}
	action := "https://github.com/settings/apps/new"
	if organization != "" {
		action = "https://github.com/organizations/" + organization + "/settings/apps/new"
	}
	return ManifestStart{ActionURL: action + "?state=" + state, Manifest: string(manifest)}, nil
}

func validPublicURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(value), "/"))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, apperr.Invalid("Defina a URL pública do Pier em Configurações antes de criar o GitHub App.")
	}
	return parsed, nil
}

// appName is unique enough per instance; GitHub lets the person rename it.
func appName(base *url.URL) string {
	name := "Pier " + base.Hostname()
	if len(name) > 34 {
		name = name[:34]
	}
	return name
}

func (s *Service) newManifestState() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := hex.EncodeToString(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for existing, expires := range s.manifests {
		if now.After(expires) {
			delete(s.manifests, existing)
		}
	}
	s.manifests[state] = now.Add(manifestLifetime)
	return state, nil
}

// consumeManifestState accepts each state once, before it expires.
func (s *Service) consumeManifestState(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expires, found := s.manifests[state]
	delete(s.manifests, state)
	return found && s.now().Before(expires)
}

// CompleteManifest exchanges the one-time code GitHub returned for the App
// credentials and stores them sealed. The state must be one this instance issued.
func (s *Service) CompleteManifest(ctx context.Context, code, state string) (Status, error) {
	if !manifestCode.MatchString(code) || !s.consumeManifestState(state) {
		return Status{}, apperr.Invalid("O pedido de criação do GitHub App expirou ou não foi feito por este painel. Comece de novo.")
	}
	var created struct {
		ID            int64  `json:"id"`
		Slug          string `json:"slug"`
		PEM           string `json:"pem"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if err := s.github(ctx, http.MethodPost, "/app-manifests/"+code+"/conversions", "", nil, &created); err != nil {
		return Status{}, err
	}
	config := Config{AppID: created.ID, Slug: created.Slug, PrivateKey: created.PEM, WebhookSecret: created.WebhookSecret}
	if err := s.store(ctx, config); err != nil {
		return Status{}, err
	}
	return s.Status(ctx), nil
}
