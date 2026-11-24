package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/indexnow"
	"pier/internal/platform/executor"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

type siteResources struct{}

func (siteResources) Resource(context.Context, string, string) (indexnow.Resource, error) {
	return indexnow.Resource{Domains: []string{"https://asigas.com"}, ServerIDs: []string{"local"}, Proxied: true}, nil
}

func (siteResources) List(context.Context) ([]indexnow.ResourceRef, error) {
	return []indexnow.ResourceRef{{Kind: indexnow.KindApplication, ID: "app", Name: "site"}}, nil
}

type localProxy struct{ ex *executor.Fake }

func (p localProxy) Get(context.Context, string) (server.Server, error) {
	return server.Server{ID: "local", Kind: server.KindLocal, DataDir: "/data/pier"}, nil
}

func (p localProxy) Executor(ctx context.Context, id string) (executor.Executor, server.Server, error) {
	srv, _ := p.Get(ctx, id)
	return p.ex, srv, nil
}

// Search engines read the key without a session, and an unknown key is a
// plain 404 that tells nothing about which keys exist.
func TestTheIndexNowKeyIsPublicAndUnknownKeysAreNotFound(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	service := indexnow.NewService(indexnow.NewStore(db), siteResources{}, localProxy{executor.NewFake()},
		func(context.Context) string { return "" }, http.DefaultClient)
	overview, err := service.SetEnabled(context.Background(), indexnow.KindApplication, "app", "asigas.com", true)
	if err != nil {
		t.Fatal(err)
	}
	location := overview.Hosts[0].KeyLocation
	file := location[strings.LastIndex(location, "/")+1:]
	handler := New(Dependencies{IndexNow: service}).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/indexnow/"+file, nil))
	body, _ := io.ReadAll(response.Body)
	if response.Code != http.StatusOK || string(body) != strings.TrimSuffix(file, ".txt") ||
		!strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("status %d, body %q, type %q", response.Code, body, response.Header().Get("Content-Type"))
	}
	for _, unknown := range []string{"00000000000000000000000000000000.txt", "..%2Fsecret.txt", "x.txt", "ABCDEF0123456789ABCDEF0123456789.txt"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/indexnow/"+unknown, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("%s: status %d", unknown, response.Code)
		}
	}
}

// The panel is never for search engines: every answer says so, pages, API
// and static files alike.
func TestThePanelIsNeverIndexed(t *testing.T) {
	// securityHeaders wraps the whole mux (API.Handler), so it covers every route.
	handler := (&API{}).securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/", "/api/v1/version", "/assets/index.js", "/indexnow/x.txt"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if got := response.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
			t.Errorf("%s: X-Robots-Tag = %q", path, got)
		}
	}
}

// The list of every domain belongs to the instance; changes stay on the
// project-scoped routes of each resource.
func TestTheListOfAllSitesNeedsTheInstanceRole(t *testing.T) {
	if policy, _ := policyFor("GET /api/v1/indexnow/sites"); policy.scope != scopeInstance {
		t.Errorf("scope = %v", policy.scope)
	}
	if policy, _ := policyFor("PUT /api/v1/stacks/{id}/indexnow"); policy.scope != scopeProject || policy.kind != "stacks" {
		t.Errorf("changes must be project-scoped: %+v", policy)
	}
}
