package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"pier/internal/identity"
	"strings"
	"testing"
	"time"
)

func TestAccountRoutesRequireBrowserSessionEvenForWriteTokens(t *testing.T) {
	a := platformAPI(t)
	ctx := context.Background()
	user, err := a.Identity.CreateUser(ctx, "Owner", "owner@example.com", "long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := a.Identity.CreateToken(ctx, user.ID, "test", identity.AllAbilities)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.registerAccountRoutes(mux)
	for _, path := range []string{"/api/v1/me/2fa", "/api/v1/me/sessions"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("token %s: %d", path, w.Code)
		}
		session, err := a.Identity.OpenSession(ctx, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("session %s: %d", path, w.Code)
		}
	}
}

func TestTwoFactorHTTPLoginHasNoCookieUntilRecoveryCode(t *testing.T) {
	a := platformAPI(t)
	ctx := context.Background()
	a.Identity.SetSecretBox(a.Instance.Box)
	user, err := a.Identity.CreateUser(ctx, "Owner", "owner@example.com", "long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := a.Identity.BeginTwoFactor(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(raw[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	code := fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000)
	recovery, err := a.Identity.ConfirmTwoFactor(ctx, user.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.registerAuthRoutes(mux)
	send := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := send("/api/v1/auth/login", `{"email":"owner@example.com","password":"long-test-password"}`)
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("password created session: %d %s", w.Code, w.Body)
	}
	var challenge challengeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &challenge); err != nil || !challenge.TwoFactorRequired || challenge.Challenge == "" {
		t.Fatal("missing challenge")
	}
	body, _ := json.Marshal(secondFactorRequest{Challenge: challenge.Challenge, Code: recovery[0]})
	w = send("/api/v1/auth/login/2fa", string(body))
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("2FA failed: %d %s", w.Code, w.Body)
	}
	if _, err := a.Identity.AuthenticateSession(ctx, w.Result().Cookies()[0].Value); err != nil {
		t.Fatal(err)
	}
	w = send("/api/v1/auth/login/2fa", string(body))
	if w.Code < 400 {
		t.Fatal("replayed challenge accepted")
	}
}
