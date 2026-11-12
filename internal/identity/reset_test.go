package identity

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"pier/internal/platform/apperr"
)

const publicURL = "https://pier.example.com"

// fakeMailer records reset e-mails instead of sending them.
type fakeMailer struct {
	mu        sync.Mutex
	available bool
	sent      map[string]string // recipient → link
}

func (m *fakeMailer) Available(context.Context) bool { return m.available }

func (m *fakeMailer) SendPasswordReset(_ context.Context, to, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent[to] = link
	return nil
}

func (m *fakeMailer) tokenFor(t *testing.T, to string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	link, found := m.sent[to]
	if !found {
		t.Fatalf("no reset e-mail sent to %s", to)
	}
	token, found := strings.CutPrefix(link, publicURL+"/#reset=")
	if !found {
		t.Fatalf("unexpected link %q", link)
	}
	return token
}

func withMailer(f *accountFixture) *fakeMailer {
	mailer := &fakeMailer{available: true, sent: map[string]string{}}
	f.service.SetResetMailer(mailer, slog.New(slog.DiscardHandler))
	return mailer
}

// requestReset asks for a link and waits for the background delivery.
func requestReset(t *testing.T, f *accountFixture, email string) {
	t.Helper()
	if err := f.service.RequestPasswordReset(context.Background(), email, publicURL); err != nil {
		t.Fatalf("RequestPasswordReset(%s): %v", email, err)
	}
	f.service.resets.work.Wait()
}

func TestResetIsUnavailableWithoutMailerOrPublicURL(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	if f.service.PasswordResetAvailable(ctx, publicURL) {
		t.Fatal("without a mailer the reset must be unavailable")
	}
	if err := f.service.RequestPasswordReset(ctx, "ana@example.com", publicURL); kindOf(err) != apperr.KindUnavailable {
		t.Fatalf("got %v", err)
	}
	mailer := withMailer(f)
	if f.service.PasswordResetAvailable(ctx, "") {
		t.Fatal("without a public URL the link has nowhere to point")
	}
	mailer.available = false
	if f.service.PasswordResetAvailable(ctx, publicURL) {
		t.Fatal("without an enabled e-mail channel the reset must be unavailable")
	}
}

func TestUnknownAddressesGetTheSameAnswerAndNoEmail(t *testing.T) {
	f := newAccountFixture(t)
	mailer := withMailer(f)
	requestReset(t, f, "nobody@example.com")
	if len(mailer.sent) != 0 {
		t.Fatalf("no e-mail may go to an unknown address: %v", mailer.sent)
	}
	requestReset(t, f, "ANA@example.com")
	if _, found := mailer.sent["ana@example.com"]; !found {
		t.Fatal("the account owner must receive the link")
	}
}

func TestAResetLinkWorksOnceAndClosesEverySession(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	mailer := withMailer(f)
	session, _ := f.service.OpenSession(ctx, f.user.ID)
	requestReset(t, f, "ana@example.com")
	token := mailer.tokenFor(t, "ana@example.com")

	if err := f.service.ResetPassword(ctx, token, "short"); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("the password policy applies, got %v", err)
	}
	if err := f.service.ResetPassword(ctx, token, "another-strong-pass"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if _, err := f.service.AuthenticateSession(ctx, session); kindOf(err) != apperr.KindUnauthorized {
		t.Fatal("a reset must close every session")
	}
	if err := f.service.ResetPassword(ctx, token, "yet-another-strong-pass"); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("a reset link must work once, got %v", err)
	}
	if outcome, err := f.service.Login(ctx, "ana@example.com", "another-strong-pass", Client{}); err != nil || outcome.SessionToken == "" {
		t.Fatalf("the new password must log in: %v", err)
	}
}

func TestAResetLinkExpiresAndANewRequestVoidsTheOldLink(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	mailer := withMailer(f)

	requestReset(t, f, "ana@example.com")
	first := mailer.tokenFor(t, "ana@example.com")
	requestReset(t, f, "ana@example.com")
	second := mailer.tokenFor(t, "ana@example.com")
	if err := f.service.ResetPassword(ctx, first, "another-strong-pass"); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("an older link must stop working, got %v", err)
	}
	f.advance(resetTTL + time.Second)
	if err := f.service.ResetPassword(ctx, second, "another-strong-pass"); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("an expired link must be rejected, got %v", err)
	}
	if err := f.service.ResetPassword(ctx, "not-a-token", "another-strong-pass"); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("an unknown token must be rejected, got %v", err)
	}
}

func TestAResetKeepsTheSecondFactor(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	f.enable(t)
	mailer := withMailer(f)
	requestReset(t, f, "ana@example.com")
	if err := f.service.ResetPassword(ctx, mailer.tokenFor(t, "ana@example.com"), "another-strong-pass"); err != nil {
		t.Fatal(err)
	}
	outcome, err := f.service.Login(ctx, "ana@example.com", "another-strong-pass", Client{})
	if err != nil || outcome.Challenge == "" || outcome.SessionToken != "" {
		t.Fatalf("e-mail access alone must not bypass the second factor: %+v %v", outcome, err)
	}
}
