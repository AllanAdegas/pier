package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pier/internal/settings"
)

func TestManagedWebhookRequiresConfiguredPublicURL(t *testing.T) {
	a := platformAPI(t)
	r := httptest.NewRequest("PUT", "https://request.example.invalid", nil)

	if _, err := a.managedWebhookBaseURL(r); err == nil {
		t.Fatal("managed webhook requires a configured URL")
	}

	for _, baseURL := range []string{"https://pier.example.invalid?query=value", "https://pier.example.invalid/pier"} {
		if _, err := a.Settings.Update(t.Context(), settings.Settings{InstanceName: "Pier", PublicURL: baseURL}); err != nil {
			t.Fatal(err)
		}
		got, err := a.managedWebhookBaseURL(r)
		if baseURL == "https://pier.example.invalid/pier" {
			if err != nil || got != baseURL {
				t.Fatalf("managed base URL = %q, %v", got, err)
			}
		} else if err == nil {
			t.Fatal("managed webhook base URL must not contain a query")
		}
	}
}

// A request's Host header must never choose where a managed provider webhook
// points: rotation uses only the configured public URL, and without one it
// refuses before rotating the secret or calling the provider.
func TestRotatingAManagedWebhookIgnoresTheRequestHost(t *testing.T) {
	f := newGitProviderFixture(t)
	f.api.route(f.mux, "POST /api/v1/applications/{id}/webhook/rotate", canWrite, f.api.rotateWebhook)
	for _, statement := range []string{
		`DELETE FROM git_provider_selections`,
		`INSERT INTO git_connection_links(kind,resource_id,connection_id,repository,clone_url,webhook_id,webhook_status,hook_url)
			VALUES('application','` + f.app + `','valid-instance-connection','acme/app','https://forge.example/acme/app.git','7','active','https://pier.example.invalid/webhooks/applications/` + f.app + `/gitlab')`,
	} {
		if _, err := f.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	forged := http.Header{"X-Pier-Client": {"web"}}
	rotate := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/api/v1/applications/"+f.app+"/webhook/rotate", nil)
		request.Host = "attacker.example"
		request.Header = forged.Clone()
		request.Header.Set("Authorization", "Bearer "+f.writer)
		response := httptest.NewRecorder()
		f.mux.ServeHTTP(response, request)
		return response
	}
	secretBefore := webhookSecretOf(t, f)

	if response := rotate(); response.Code < 400 || *f.remoteCalls != 0 {
		t.Fatalf("without a public URL: %d, %d provider calls", response.Code, *f.remoteCalls)
	}
	if webhookSecretOf(t, f) != secretBefore {
		t.Fatal("a refused rotation changed the webhook secret")
	}

	if _, err := f.api.Settings.Update(t.Context(), settings.Settings{InstanceName: "Pier", PublicURL: "https://pier.example.invalid"}); err != nil {
		t.Fatal(err)
	}
	rotate()
	var hookURL string
	if err := f.db.QueryRow(`SELECT hook_url FROM git_connection_links WHERE resource_id=?`, f.app).Scan(&hookURL); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hookURL, "attacker") || !strings.HasPrefix(hookURL, "https://pier.example.invalid/webhooks/applications/") {
		t.Fatalf("hook URL follows the request host: %q", hookURL)
	}
}

func webhookSecretOf(t *testing.T, f gitProviderFixture) string {
	t.Helper()
	var sealed string
	if err := f.db.QueryRow(`SELECT webhook_secret FROM applications WHERE id=?`, f.app).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	return sealed
}
