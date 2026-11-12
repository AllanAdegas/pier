package gitprovider

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) *Service {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := secretbox.New("test-passphrase-long-enough")
	s := New(db, box)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	c := Config{AppID: 42, PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), WebhookSecret: strings.Repeat("s", 32)}
	if err = s.Configure(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return s
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mock(s *Service, handler http.HandlerFunc) {
	s.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Result(), nil
	})
}
func TestEncryptionJWTAndValidation(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	var raw string
	s.db.QueryRow(`SELECT encrypted FROM git_provider_config`).Scan(&raw)
	if strings.Contains(raw, "PRIVATE KEY") || strings.Contains(raw, strings.Repeat("s", 32)) {
		t.Fatal("plaintext secrets")
	}
	jwt, err := s.jwt(ctx)
	if err != nil || len(strings.Split(jwt, ".")) != 3 {
		t.Fatal(jwt, err)
	}
	if err = s.Configure(ctx, Config{AppID: 42}); err == nil {
		t.Fatal("invalid accepted")
	}
	if validRepo("x/../y") || validRepo("x/y?token=secret") {
		t.Fatal("invalid repo")
	}
}
func TestTokenCachingExpiryAndSanitizedFailure(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	calls := 0
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Host != "api.github.com" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Fatal("wrong target/auth")
		}
		json.NewEncoder(w).Encode(map[string]any{"token": "sensitive-token", "expires_at": time.Now().Add(time.Hour)})
	})
	first, err := s.token(ctx, 123)
	if err != nil || first != "sensitive-token" {
		t.Fatal(first, err)
	}
	s.token(ctx, 123)
	if calls != 1 {
		t.Fatal(calls)
	}
	s.tokens[123] = cachedToken{first, time.Now()}
	s.token(ctx, 123)
	if calls != 2 {
		t.Fatal(calls)
	}
	mock(s, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401); io.WriteString(w, "sensitive-token") })
	delete(s.tokens, 123)
	_, err = s.token(ctx, 123)
	if err == nil || strings.Contains(err.Error(), "sensitive-token") {
		t.Fatal(err)
	}
}
func TestWebhookSignatureReplayAndRevocation(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	body := []byte(`{"action":"deleted","installation":{"id":123}}`)
	if s.Webhook(ctx, "one", "installation", "", body) == nil {
		t.Fatal("unsigned accepted")
	}
	mac := hmac.New(sha256.New, []byte(strings.Repeat("s", 32)))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	s.tokens[123] = cachedToken{"stale", time.Now().Add(time.Hour)}
	for range 2 {
		if err := s.Webhook(ctx, "one", "installation", signature, body); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM git_provider_deliveries`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	if _, err := s.token(ctx, 123); err == nil {
		t.Fatal("deleted installation usable")
	}
}
func TestReporterUpdatesExistingComment(t *testing.T) {
	s := fixture(t)
	s.scopedTokens = map[string]cachedToken{"123/owner/repo/true": {"token", time.Now().Add(time.Hour)}}
	patches, posts := 0, 0
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/statuses/"):
			posts++
			io.WriteString(w, `{}`)
		case r.Method == "GET":
			io.WriteString(w, `[{"id":5,"body":"<!-- pier:preview -->\nold","user":{"type":"Bot"},"performed_via_github_app":{"id":42}}]`)
		case r.Method == "PATCH":
			patches++
			io.WriteString(w, `{}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL)
		}
	})
	err := s.Report(context.Background(), Selection{123, "owner/repo"}, strings.Repeat("a", 40), 3, "success", "ready")
	if err != nil || patches != 1 || posts != 1 {
		t.Fatal(patches, posts, err)
	}
}
func TestRepositoryPaginationAndRateLimit(t *testing.T) {
	s := fixture(t)
	s.tokens[12] = cachedToken{"token", time.Now().Add(time.Hour)}
	pages := 0
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		pages++
		rows := []Repository{}
		if pages == 1 {
			for i := range 100 {
				rows = append(rows, Repository{ID: int64(i), FullName: "owner/repo", Private: true})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"repositories": rows})
	})
	rows, err := s.Repositories(context.Background(), 12)
	if err != nil || len(rows) != 100 || pages != 2 {
		t.Fatal(len(rows), pages, err)
	}
	mock(s, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) })
	_, err = s.Repositories(context.Background(), 12)
	if err == nil {
		t.Fatal("rate limit ignored")
	}
	mock(s, func(w http.ResponseWriter, r *http.Request) { t.Fatal("request during cooldown") })
	if _, err = s.Repositories(context.Background(), 12); err == nil {
		t.Fatal("cooldown ignored")
	}
}
