package identity

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

// accountFixture is a service with a sealed-secret box, a controllable
// clock and one user, Ana.
type accountFixture struct {
	service *Service
	db      *sql.DB
	user    User
	clock   time.Time
}

func newAccountFixture(t *testing.T) *accountFixture {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := secretbox.New("identity-test-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &accountFixture{db: db, clock: time.Unix(1_800_000_000, 0)}
	fixture.service = NewService(NewStore(db), time.Hour)
	fixture.service.SetSecretBox(box)
	fixture.service.now = func() time.Time { return fixture.clock }
	user, err := fixture.service.CreateUser(context.Background(), "Ana", "ana@example.com", strongPassword)
	if err != nil {
		t.Fatal(err)
	}
	fixture.user = user
	return fixture
}

func (f *accountFixture) advance(duration time.Duration) { f.clock = f.clock.Add(duration) }

// codeNow is what Ana's authenticator app shows at the fixture's clock.
func codeNow(t *testing.T, f *accountFixture, secret string) string {
	t.Helper()
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	return hotp(key, totpStep(f.clock), totpDigits)
}

// enable turns the second factor on and returns the secret and recovery codes.
func (f *accountFixture) enable(t *testing.T) (string, []string) {
	t.Helper()
	ctx := context.Background()
	enrollment, err := f.service.BeginTwoFactor(ctx, f.user.ID)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	codes, err := f.service.ConfirmTwoFactor(ctx, f.user.ID, codeNow(t, f, enrollment.Secret))
	if err != nil {
		t.Fatalf("ConfirmTwoFactor: %v", err)
	}
	f.advance(totpPeriod)
	return enrollment.Secret, codes
}

func (f *accountFixture) challenge(t *testing.T) string {
	t.Helper()
	outcome, err := f.service.Login(context.Background(), "ana@example.com", strongPassword, Client{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if outcome.SessionToken != "" || outcome.Challenge == "" {
		t.Fatalf("with two factors the password alone must not open a session: %+v", outcome)
	}
	return outcome.Challenge
}

func TestEnrollmentNeedsACodeBeforeItTakesEffect(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	enrollment, err := f.service.BeginTwoFactor(ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enrollment.URI, "otpauth://totp/") || !strings.HasPrefix(enrollment.QRSVG, "<svg") {
		t.Fatalf("unexpected enrollment %+v", enrollment)
	}
	if status, _ := f.service.TwoFactorStatus(ctx, f.user.ID); status.Enabled {
		t.Fatal("a pending enrollment must not be enabled")
	}
	if outcome, _ := f.service.Login(ctx, "ana@example.com", strongPassword, Client{}); outcome.SessionToken == "" {
		t.Fatal("a pending enrollment must not change the login")
	}
	if _, err := f.service.ConfirmTwoFactor(ctx, f.user.ID, "000000"); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("a wrong code must be rejected, got %v", err)
	}
	codes, err := f.service.ConfirmTwoFactor(ctx, f.user.ID, codeNow(t, f, enrollment.Secret))
	if err != nil || len(codes) != recoveryCodeCount {
		t.Fatalf("ConfirmTwoFactor: %d codes, %v", len(codes), err)
	}
	status, _ := f.service.TwoFactorStatus(ctx, f.user.ID)
	if !status.Enabled || status.RecoveryCodesLeft != recoveryCodeCount {
		t.Fatalf("unexpected status %+v", status)
	}
	if _, err := f.service.BeginTwoFactor(ctx, f.user.ID); kindOf(err) != apperr.KindConflict {
		t.Fatalf("enrolling over an enabled factor must conflict, got %v", err)
	}
}

func TestTheSecretAndRecoveryCodesAreNotStoredInClear(t *testing.T) {
	f := newAccountFixture(t)
	secret, codes := f.enable(t)
	var stored string
	if err := f.db.QueryRow(`SELECT secret FROM user_totp`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, secret) || !strings.HasPrefix(stored, "v1:") {
		t.Fatalf("the TOTP secret must be sealed, got %q", stored)
	}
	rows, err := f.db.Query(`SELECT code_hash FROM recovery_codes`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		_ = rows.Scan(&hash)
		for _, code := range codes {
			if strings.Contains(strings.ToLower(hash), strings.ReplaceAll(code, "-", "")) {
				t.Fatal("a recovery code is stored in clear")
			}
		}
	}
}

func TestTwoStepLoginOpensTheSessionOnlyWithTheCode(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	secret, _ := f.enable(t)

	challenge := f.challenge(t)
	if user, err := f.service.ChallengeUser(challenge); err != nil || user != f.user.ID {
		t.Fatalf("ChallengeUser: %q %v", user, err)
	}
	if _, err := f.service.CompleteLogin(ctx, challenge, "000000", Client{}); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("a wrong code must be rejected, got %v", err)
	}
	code := codeNow(t, f, secret)
	outcome, err := f.service.CompleteLogin(ctx, challenge, code, Client{IP: "203.0.113.7", UserAgent: "Firefox"})
	if err != nil || outcome.SessionToken == "" || outcome.User.ID != f.user.ID {
		t.Fatalf("CompleteLogin: %+v %v", outcome, err)
	}
	if _, err := f.service.AuthenticateSession(ctx, outcome.SessionToken); err != nil {
		t.Fatalf("the new session must work: %v", err)
	}
	if _, err := f.service.CompleteLogin(ctx, challenge, code, Client{}); kindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("a challenge must be single use, got %v", err)
	}
	// A new challenge with the same code: the step was already spent.
	if _, err := f.service.CompleteLogin(ctx, f.challenge(t), code, Client{}); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("a replayed code must be rejected, got %v", err)
	}
}

func TestRecoveryCodesWorkOnceEach(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	_, codes := f.enable(t)

	if _, err := f.service.CompleteLogin(ctx, f.challenge(t), strings.ToUpper(codes[0]), Client{}); err != nil {
		t.Fatalf("a recovery code must open the session: %v", err)
	}
	if _, err := f.service.CompleteLogin(ctx, f.challenge(t), codes[0], Client{}); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("a recovery code must not work twice, got %v", err)
	}
	if status, _ := f.service.TwoFactorStatus(ctx, f.user.ID); status.RecoveryCodesLeft != recoveryCodeCount-1 {
		t.Fatalf("one code must be spent, got %+v", status)
	}
}

func TestRegeneratingRecoveryCodesVoidsTheOldOnes(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	secret, old := f.enable(t)
	fresh, err := f.service.RegenerateRecoveryCodes(ctx, f.user.ID, codeNow(t, f, secret))
	if err != nil || len(fresh) != recoveryCodeCount {
		t.Fatalf("RegenerateRecoveryCodes: %v", err)
	}
	if _, err := f.service.CompleteLogin(ctx, f.challenge(t), old[1], Client{}); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("an old recovery code must stop working, got %v", err)
	}
	if _, err := f.service.CompleteLogin(ctx, f.challenge(t), fresh[1], Client{}); err != nil {
		t.Fatalf("a new recovery code must work: %v", err)
	}
}

func TestChallengesExpireAndCannotBeForged(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	secret, _ := f.enable(t)

	challenge := f.challenge(t)
	f.advance(challengeTTL + time.Second)
	if _, err := f.service.CompleteLogin(ctx, challenge, codeNow(t, f, secret), Client{}); kindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("an expired challenge must be rejected, got %v", err)
	}
	forged := f.challenge(t)
	forged = forged[:len(forged)-2] + "xx"
	if _, err := f.service.CompleteLogin(ctx, forged, codeNow(t, f, secret), Client{}); kindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("a tampered challenge must be rejected, got %v", err)
	}
	other := NewService(f.service.store, time.Hour)
	if _, err := other.ChallengeUser(f.challenge(t)); kindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("a challenge signed by another key must be rejected, got %v", err)
	}
}

func TestAChallengeAllowsOnlyAFewAttempts(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	secret, _ := f.enable(t)
	challenge := f.challenge(t)
	for range challengeAttempts {
		_, _ = f.service.CompleteLogin(ctx, challenge, "000000", Client{})
	}
	if _, err := f.service.CompleteLogin(ctx, challenge, codeNow(t, f, secret), Client{}); kindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("an exhausted challenge must be rejected even with the right code, got %v", err)
	}
}

func TestChangingThePasswordKillsPendingChallenges(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	secret, _ := f.enable(t)
	challenge := f.challenge(t)
	if err := f.service.ChangePassword(ctx, f.user.ID, strongPassword, "another-strong-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteLogin(ctx, challenge, codeNow(t, f, secret), Client{}); kindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("a challenge issued for the old password must die, got %v", err)
	}
}

func TestDisablingAsksForPasswordAndCode(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	secret, _ := f.enable(t)
	if err := f.service.DisableTwoFactor(ctx, f.user.ID, "wrong-password-xx", codeNow(t, f, secret)); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("a wrong password must be rejected, got %v", err)
	}
	if err := f.service.DisableTwoFactor(ctx, f.user.ID, strongPassword, "000000"); kindOf(err) != apperr.KindInvalid {
		t.Fatalf("a wrong code must be rejected, got %v", err)
	}
	if err := f.service.DisableTwoFactor(ctx, f.user.ID, strongPassword, codeNow(t, f, secret)); err != nil {
		t.Fatalf("DisableTwoFactor: %v", err)
	}
	if outcome, _ := f.service.Login(ctx, "ana@example.com", strongPassword, Client{}); outcome.SessionToken == "" {
		t.Fatal("without the second factor the password opens the session again")
	}
}

func TestTwoFactorNeedsTheSecretBox(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	user, _ := service.CreateUser(ctx, "Ana", "ana@example.com", strongPassword)
	if _, err := service.BeginTwoFactor(ctx, user.ID); kindOf(err) != apperr.KindUnavailable {
		t.Fatalf("without a secret box enrollment must be unavailable, got %v", err)
	}
}

func TestOfflineAdministrationResetsPasswordAndSecondFactor(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	f.enable(t)
	session, _ := f.service.OpenSession(ctx, f.user.ID)
	if err := f.service.SetPasswordByEmail(ctx, "nobody@example.com", "another-strong-pass"); kindOf(err) != apperr.KindNotFound {
		t.Fatalf("an unknown account must be reported, got %v", err)
	}
	if err := f.service.SetPasswordByEmail(ctx, "ANA@example.com", "another-strong-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AuthenticateSession(ctx, session); kindOf(err) != apperr.KindUnauthorized {
		t.Fatal("an offline reset must close every session")
	}
	if err := f.service.RemoveTwoFactorByEmail(ctx, "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	outcome, err := f.service.Login(ctx, "ana@example.com", "another-strong-pass", Client{})
	if err != nil || outcome.SessionToken == "" {
		t.Fatalf("after both resets the new password alone must log in: %+v %v", outcome, err)
	}
}

func TestConcurrentChallengeCompletionOpensOnlyOneSession(t *testing.T) {
	f := newAccountFixture(t)
	_, codes := f.enable(t)
	challenge := f.challenge(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, code := range codes[:2] {
		go func(code string) {
			<-start
			_, err := f.service.CompleteLogin(context.Background(), challenge, code, Client{})
			results <- err
		}(code)
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent challenge opened %d sessions", successes)
	}
	sessions, err := f.service.ListSessions(context.Background(), f.user.ID, "")
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions: %d, %v", len(sessions), err)
	}
}
