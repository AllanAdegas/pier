package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedirectsStayWithinTheOrigin(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the redirect to another origin was followed")
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/elsewhere":
			http.Redirect(w, r, other.URL+"/steal", http.StatusTemporaryRedirect)
		case "/moved":
			http.Redirect(w, r, "/here", http.StatusTemporaryRedirect)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer origin.Close()
	client := &http.Client{CheckRedirect: SameOrigin}

	if _, err := client.Post(origin.URL+"/elsewhere", "application/json", nil); err == nil {
		t.Error("a redirect to another origin was accepted")
	}
	response, err := client.Post(origin.URL+"/moved", "application/json", nil)
	if err != nil || response.StatusCode != http.StatusNoContent {
		t.Errorf("a redirect within the origin failed: %v", err)
	}
}
