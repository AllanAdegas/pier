package httpapi

import (
	"net/http/httptest"
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
