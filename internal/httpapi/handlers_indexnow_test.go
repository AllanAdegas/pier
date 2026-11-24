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
