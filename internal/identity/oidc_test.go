package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"testing"
	"time"
)

func TestOIDCFlow(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "oidc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, user := range []string{"owner", "member"} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash,created_at) VALUES(?,?,?,'unused','2026-01-01T00:00:00Z')`, user, user+"@example.com", user)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO access_instance_roles VALUES('owner','owner')`); err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer, nonce, challenge, subject string
	badNonce := false
	encode := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
				t.Error("PKCE verifier mismatch")
			}
			claimNonce := nonce
			if badNonce {
				claimNonce = "wrong"
			}
			payload := encode(map[string]string{"alg": "RS256", "kid": "test"}) + "." + encode(map[string]any{"iss": issuer, "sub": subject, "aud": "client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": claimNonce, "email": "member@example.com", "email_verified": true})
			digest := sha256.Sum256([]byte(payload))
			signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
			if err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test", "token_type": "Bearer", "id_token": payload + "." + base64.RawURLEncoding.EncodeToString(signature)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL
	box, _ := secretbox.New("test-key-32-characters-long-secret")
	s := NewOIDC(db, box, []string{issuer}, "https://pier.example/api/v1/access/oidc/callback")
	s.client = server.Client()
	config := OIDCConfig{Issuer: issuer, ClientID: "client", ClientSecret: "secret", Callback: s.callback}
	if s.Configure(ctx, "member", config) == nil {
		t.Fatal("non-admin configured SSO")
	}
	if err = s.Configure(ctx, "owner", config); err != nil {
		t.Fatal(err)
	}
	var sealed string
	_ = db.QueryRow(`SELECT sealed FROM access_oidc_config`).Scan(&sealed)
	if sealed == "" || sealed[0:3] != "v1:" {
		t.Fatal("config not encrypted")
	}
	start := func() (string, string) {
		target, browser, err := s.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(target)
		nonce = u.Query().Get("nonce")
		challenge = u.Query().Get("code_challenge")
		if u.Query().Get("code_challenge_method") != "S256" {
			t.Fatal("missing PKCE")
		}
		return u.Query().Get("state"), browser
	}
	subject = "authorized"
	if err = s.Link(ctx, "owner", issuer, subject, "member"); err != nil {
		t.Fatal(err)
	}
	state, browser := start()
	if _, err = s.Complete(ctx, state, "wrong-browser", "code"); err == nil {
		t.Fatal("browser binding bypassed")
	}
	user, err := s.Complete(ctx, state, browser, "code")
	if err != nil || user != "member" {
		t.Fatalf("callback: %s %v", user, err)
	}
	if _, err = s.Complete(ctx, state, browser, "code"); err == nil {
		t.Fatal("state replay")
	}
	subject = "unlinked"
	state, browser = start()
	if _, err = s.Complete(ctx, state, browser, "code"); err == nil {
		t.Fatal("email claim linked unauthorized identity")
	}
	subject = "authorized"
	badNonce = true
	state, browser = start()
	if _, err = s.Complete(ctx, state, browser, "code"); err == nil {
		t.Fatal("nonce bypassed")
	}
	state, browser = start()
	_, _ = db.Exec(`UPDATE access_oidc_states SET expires_at=0`)
	if _, err = s.Complete(ctx, state, browser, "code"); err == nil {
		t.Fatal("expired state accepted")
	}
	config.Issuer = "https://untrusted.example"
	if s.Configure(ctx, "owner", config) == nil {
		t.Fatal("issuer allowlist bypassed")
	}
}
