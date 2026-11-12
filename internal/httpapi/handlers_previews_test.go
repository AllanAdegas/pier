package httpapi

import (
	"net/http"
	"net/http/httptest"
	"pier/internal/application"
	"pier/internal/identity"
	"strings"
	"testing"
)

func TestPreviewRoutesAbilitiesAndScopedVariables(t *testing.T) {
	a, _, mux, writer, id := engineHTTPFixture(t)
	a.Previews = application.NewPreviewService(a.Applications, a.Logger)
	a.registerPreviewRoutes(mux)
	user, err := a.Identity.CreateUser(t.Context(), "Reader", "preview-reader@example.invalid", "long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	_, reader, err := a.Identity.CreateToken(t.Context(), user.ID, "reader", []identity.Ability{identity.AbilityRead})
	if err != nil {
		t.Fatal(err)
	}
	_, deployer, err := a.Identity.CreateToken(t.Context(), user.ID, "deployer", []identity.Ability{identity.AbilityDeploy})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, body, token string
		want                      int
	}{
		{"GET", "", "", reader, 200},
		{"GET", "/settings", "", reader, 200},
		{"PATCH", "/settings", `{}`, reader, 403},
		{"DELETE", "/missing", "", reader, 403},
		{"DELETE", "/missing", "", deployer, 404},
		{"POST", "/envs", `{"key":"QA","value":"secret-preview"}`, reader, 403},
		{"POST", "/envs", `{"key":"QA","value":"secret-preview"}`, writer, 204},
		{"GET", "/envs", "", reader, 403},
		{"GET", "/envs", "", writer, 200},
	} {
		req := httptest.NewRequest(tc.method, "/api/v1/applications/"+id+"/previews"+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("GET", "/api/v1/applications/"+id+"/envs", nil)
	req.Header.Set("Authorization", "Bearer "+writer)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "secret-preview") {
		t.Fatal(w.Code, w.Body.String())
	}
}
