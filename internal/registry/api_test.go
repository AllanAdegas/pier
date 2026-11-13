package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"pier/internal/platform/apperr"
)

// fakeRegistry speaks the Docker registry handshake: /v2/ challenges with a
// Bearer realm and the token endpoint checks Basic credentials.
type fakeRegistry struct {
	server    *httptest.Server
	mu        sync.Mutex
	realm     string
	scopes    []string
	manifests map[string]bool
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	fake := &fakeRegistry{manifests: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+fake.realm+`",service="registry.test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v2/" {
			return
		}
		if r.Method != http.MethodHead || !strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if !fake.manifests[r.URL.Path] {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		fake.mu.Lock()
		fake.scopes = append(fake.scopes, r.URL.Query().Get("scope"))
		fake.mu.Unlock()
		if r.URL.Query().Get("service") != "registry.test" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if ok && (user != "deploy" || password != testPassword) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"token":"good-token"}`))
	})
	fake.server = httptest.NewTLSServer(mux)
	fake.realm = fake.server.URL + "/token"
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeRegistry) host() string { return strings.TrimPrefix(f.server.URL, "https://") }

func registryWith(t *testing.T, fake *fakeRegistry, password string) (*Service, string) {
	t.Helper()
	s := newTestService(t)
	s.SetHTTPClient(fake.server.Client())
	created, err := s.Create(context.Background(), Input{Name: "Teste", Host: fake.host(), Username: "deploy", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	return s, created.ID
}

func TestVerifyFollowsTheBearerTokenFlow(t *testing.T) {
	fake := newFakeRegistry(t)
	s, id := registryWith(t, fake, testPassword)
	if err := s.Verify(context.Background(), id); err != nil {
		t.Fatalf("valid credential refused: %v", err)
	}
}

func TestVerifyReportsRejectedCredentialsWithoutTheirValue(t *testing.T) {
	fake := newFakeRegistry(t)
	s, id := registryWith(t, fake, "wrong-password-value")
	err := s.Verify(context.Background(), id)
	appErr, ok := apperr.As(err)
	if !ok || appErr.Kind != apperr.KindInvalid {
		t.Fatalf("expected a validation error, got %v", err)
	}
	if strings.Contains(err.Error(), "wrong-password-value") {
		t.Fatal("password in the error")
	}
}

func TestVerifyRefusesATokenRealmOutsideHTTPS(t *testing.T) {
	fake := newFakeRegistry(t)
	fake.realm = "http://" + fake.host() + "/token"
	s, id := registryWith(t, fake, testPassword)
	err := s.Verify(context.Background(), id)
	if appErr, ok := apperr.As(err); !ok || appErr.Kind != apperr.KindUnavailable {
		t.Fatalf("credential sent to a plain HTTP realm: %v", err)
	}
	for _, scope := range fake.scopes {
		if scope != "" {
			t.Fatal("token endpoint reached")
		}
	}
	if len(fake.scopes) != 0 {
		t.Fatal("token endpoint reached over HTTP")
	}
}

func TestManifestExistsAsksForTheDigestWithAPullScope(t *testing.T) {
	ctx := context.Background()
	fake := newFakeRegistry(t)
	s, _ := registryWith(t, fake, testPassword)
	digest := "sha256:" + strings.Repeat("d", 64)
	fake.manifests["/v2/acme/api/manifests/"+digest] = true
	exists, err := s.ManifestExists(ctx, fake.host()+"/acme/api@"+digest)
	if err != nil || !exists {
		t.Fatalf("existing manifest: %v %v", exists, err)
	}
	missing, err := s.ManifestExists(ctx, fake.host()+"/acme/api@sha256:"+strings.Repeat("e", 64))
	if err != nil || missing {
		t.Fatalf("missing manifest: %v %v", missing, err)
	}
	if fake.scopes[len(fake.scopes)-1] != "repository:acme/api:pull" {
		t.Fatalf("scope: %v", fake.scopes)
	}
}

func TestReferencesFollowDockerRules(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("a", 64)
	cases := map[string]string{"nginx:1.27": DockerHub, "acme/app": DockerHub, "ghcr.io/acme/app": "ghcr.io",
		"localhost:5000/app": "localhost:5000", "index.docker.io/acme/app": DockerHub}
	for image, host := range cases {
		if got := HostOf(image); got != host {
			t.Errorf("HostOf(%q) = %q, want %q", image, got, host)
		}
	}
	if got := Canonical("nginx" + digest); got != "docker.io/library/nginx"+digest {
		t.Errorf("Canonical: %q", got)
	}
	for image, want := range map[string]bool{
		"ghcr.io/acme/app" + digest: true, "docker.io/acme/app" + digest: true, "acme/app" + digest: false,
		"ghcr.io/acme/app:1.0": false, "ghcr.io/../app" + digest: false, "ghcr.io/Acme/app" + digest: false,
	} {
		if Immutable(image) != want {
			t.Errorf("Immutable(%q) != %v", image, want)
		}
	}
}
