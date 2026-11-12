package gitprovider

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"
	"time"

	"pier/internal/platform/apperr"
)

func sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookSignatureFailuresAreRejectedBeforeParsing(t *testing.T) {
	s := fixture(t)
	body := []byte(`{"action":"deleted","installation":{"id":123}}`)
	for name, signature := range map[string]string{
		"missing":   "",
		"no prefix": hex.EncodeToString(body),
		"wrong":     "sha256=" + hex.EncodeToString(make([]byte, 32)),
		"short":     "sha256=abcd",
		"sha1":      "sha1=" + hex.EncodeToString(make([]byte, 20)),
	} {
		_, err := s.Webhook(t.Context(), "delivery-"+name, "installation", signature, body)
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != apperr.KindUnauthorized {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM git_provider_deliveries`).Scan(&count)
	if count != 0 {
		t.Fatal("unauthenticated deliveries recorded")
	}
	// Tampering with a signed body invalidates it.
	signature := sign(body)
	if _, err := s.Webhook(t.Context(), "tampered", "installation", signature, []byte(`{"action":"created","installation":{"id":123}}`)); err == nil {
		t.Fatal("tampered body accepted")
	}
}

func TestWebhookReplayAndInstallationRevocation(t *testing.T) {
	s := fixture(t)
	body := []byte(`{"action":"deleted","installation":{"id":123}}`)
	key := tokenKey{installation: 123, repository: "acme/app", purpose: purposeClone}
	s.tokens[key] = cachedToken{"stale", time.Now().Add(time.Hour)}
	for range 2 {
		if _, err := s.Webhook(t.Context(), "one", "installation", sign(body), body); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM git_provider_deliveries`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	if _, err := s.installationToken(t.Context(), key); err == nil {
		t.Fatal("deleted installation still mints or reuses tokens")
	}
	restored := []byte(`{"action":"unsuspend","installation":{"id":123}}`)
	if _, err := s.Webhook(t.Context(), "two", "installation", sign(restored), restored); err != nil {
		t.Fatal(err)
	}
	if err := s.checkInstallation(t.Context(), 123); err != nil {
		t.Fatal("unsuspended installation still blocked", err)
	}
}

func TestWebhookPushTargetsLinkedResourcesOnce(t *testing.T) {
	s := fixture(t)
	for _, statement := range []string{
		`INSERT INTO git_provider_selections VALUES('application','app',123,'Acme/App')`,
		`INSERT INTO git_provider_selections VALUES('stack','stack',123,'acme/app')`,
		`INSERT INTO git_provider_selections VALUES('application','other-installation',999,'acme/app')`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	mock(s, func(w http.ResponseWriter, r *http.Request) { t.Fatal("a webhook needs no API call") })
	body := []byte(`{"ref":"refs/heads/main","installation":{"id":123},"repository":{"full_name":"acme/APP"}}`)
	delivery, err := s.Webhook(t.Context(), "push-1", EventPush, sign(body), body)
	if err != nil || len(delivery.Targets) != 2 || delivery.Targets[0].Kind != KindApplication || delivery.Targets[1].Kind != KindStack {
		t.Fatal(delivery, err)
	}
	again, err := s.Webhook(t.Context(), "push-1", EventPush, sign(body), body)
	if err != nil || !again.Duplicate || len(again.Targets) != 0 {
		t.Fatal("redelivery deployed again", again, err)
	}
	ignored := []byte(`{"action":"created","installation":{"id":123}}`)
	other, err := s.Webhook(t.Context(), "issue-1", "issues", sign(ignored), ignored)
	if err != nil || len(other.Targets) != 0 {
		t.Fatal(other, err)
	}
}
