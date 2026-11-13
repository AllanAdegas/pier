package identity

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/sqlite"
)

const strongPassword = "correct-horse-battery"

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(NewStore(db), time.Hour)
}

func kindOf(err error) apperr.Kind {
	if appErr, ok := apperr.As(err); ok {
		return appErr.Kind
	}
	return ""
}

func TestSetupCreatesTheOwnerExactlyOnce(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	if needs, _ := service.NeedsSetup(ctx); !needs {
		t.Fatal("a fresh instance must need setup")
	}
	if _, err := service.Setup(ctx, claim(ctx, service, "Ana", "ANA@Example.com ")); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if needs, _ := service.NeedsSetup(ctx); needs {
		t.Error("setup must be closed after the first user exists")
	}
	// A second visitor must not be able to claim the instance.
	if _, err := service.Setup(ctx, claim(ctx, service, "Eve", "eve@example.com")); kindOf(err) != apperr.KindConflict {
		t.Errorf("second setup must conflict, got %v", err)
	}
}

func TestPasswordPolicy(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	for _, password := range []string{"short", string(make([]byte, 73))} {
		if _, err := service.CreateUser(ctx, "Ana", "ana@example.com", password); kindOf(err) != apperr.KindInvalid {
			t.Errorf("password of length %d must be rejected, got %v", len(password), err)
		}
	}
	if _, err := service.CreateUser(ctx, "Ana", "not-an-email", strongPassword); kindOf(err) != apperr.KindInvalid {
		t.Errorf("invalid e-mail must be rejected, got %v", err)
	}
}

func TestLoginAndSessionLifecycle(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	user, _ := service.Setup(ctx, claim(ctx, service, "Ana", "ana@example.com"))

	if _, err := service.Login(ctx, "ana@example.com", "wrong-password-xx", Client{}); kindOf(err) != apperr.KindUnauthorized {
		t.Errorf("wrong password must be unauthorized, got %v", err)
	}
	if _, err := service.Login(ctx, "nobody@example.com", strongPassword, Client{}); kindOf(err) != apperr.KindUnauthorized {
		t.Errorf("unknown account must be unauthorized, got %v", err)
	}
	outcome, err := service.Login(ctx, "Ana@Example.com", strongPassword, Client{})
	token := outcome.SessionToken
	if err != nil || outcome.User.ID != user.ID || token == "" || outcome.Challenge != "" {
		t.Fatalf("Login: %v", err)
	}
	principal, err := service.AuthenticateSession(ctx, token)
	if err != nil || principal.User.ID != user.ID || principal.ViaToken {
		t.Fatalf("AuthenticateSession: %+v %v", principal, err)
	}
	for _, ability := range AllAbilities {
		if !principal.Can(ability) {
			t.Errorf("a browser session must hold %s", ability)
		}
	}
	if err := service.Logout(ctx, token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := service.AuthenticateSession(ctx, token); kindOf(err) != apperr.KindUnauthorized {
		t.Errorf("a closed session must be rejected, got %v", err)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	user, _ := service.Setup(ctx, claim(ctx, service, "Ana", "ana@example.com"))
	token, _ := service.OpenSession(ctx, user.ID)

	service.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := service.AuthenticateSession(ctx, token); kindOf(err) != apperr.KindUnauthorized {
		t.Errorf("an expired session must be rejected, got %v", err)
	}
}

func TestChangingThePasswordInvalidatesOtherSessions(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	user, _ := service.Setup(ctx, claim(ctx, service, "Ana", "ana@example.com"))
	stolen, _ := service.OpenSession(ctx, user.ID)

	if err := service.ChangePassword(ctx, user.ID, "not-the-current", "another-strong-pass"); kindOf(err) != apperr.KindInvalid {
		t.Errorf("the current password must be verified, got %v", err)
	}
	if err := service.ChangePassword(ctx, user.ID, strongPassword, "another-strong-pass"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, err := service.AuthenticateSession(ctx, stolen); kindOf(err) != apperr.KindUnauthorized {
		t.Error("sessions opened before the password change must stop working")
	}
	if _, err := service.Login(ctx, "ana@example.com", "another-strong-pass", Client{}); err != nil {
		t.Errorf("login with the new password failed: %v", err)
	}
}

func TestAPITokenAbilities(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	user, _ := service.Setup(ctx, claim(ctx, service, "Ana", "ana@example.com"))

	cases := []struct {
		abilities []Ability
		can       map[Ability]bool
	}{
		{[]Ability{AbilityRead}, map[Ability]bool{AbilityRead: true, AbilityDeploy: false, AbilityWrite: false}},
		{[]Ability{AbilityDeploy}, map[Ability]bool{AbilityRead: false, AbilityDeploy: true, AbilityWrite: false}},
		{[]Ability{AbilityRead, AbilityDeploy}, map[Ability]bool{AbilityRead: true, AbilityDeploy: true, AbilityWrite: false}},
		{[]Ability{AbilityWrite}, map[Ability]bool{AbilityRead: true, AbilityDeploy: true, AbilityWrite: true}},
	}
	for _, tc := range cases {
		token, secret, err := service.CreateToken(ctx, user.ID, "ci", tc.abilities)
		if err != nil {
			t.Fatalf("CreateToken(%v): %v", tc.abilities, err)
		}
		principal, err := service.AuthenticateToken(ctx, secret)
		if err != nil || !principal.ViaToken {
			t.Fatalf("AuthenticateToken: %+v %v", principal, err)
		}
		for ability, want := range tc.can {
			if got := principal.Can(ability); got != want {
				t.Errorf("token %v: Can(%s) = %v, want %v", tc.abilities, ability, got, want)
			}
		}
		if err := service.DeleteToken(ctx, token.ID); err != nil {
			t.Fatalf("DeleteToken: %v", err)
		}
		if _, err := service.AuthenticateToken(ctx, secret); kindOf(err) != apperr.KindUnauthorized {
			t.Errorf("a revoked token must be rejected, got %v", err)
		}
	}
	if _, _, err := service.CreateToken(ctx, user.ID, "x", []Ability{"root"}); kindOf(err) != apperr.KindInvalid {
		t.Errorf("unknown abilities must be rejected, got %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, "pier_"+"0000000000000000"); kindOf(err) != apperr.KindUnauthorized {
		t.Errorf("an unknown token must be rejected, got %v", err)
	}
}

func TestTheLastUserCannotBeRemoved(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	owner, _ := service.Setup(ctx, claim(ctx, service, "Ana", "ana@example.com"))
	other, _ := service.CreateUser(ctx, "Bia", "bia@example.com", strongPassword)

	if err := service.DeleteUser(ctx, owner.ID, owner.ID); kindOf(err) != apperr.KindConflict {
		t.Errorf("self removal must conflict, got %v", err)
	}
	if err := service.DeleteUser(ctx, owner.ID, other.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if err := service.DeleteUser(ctx, other.ID, owner.ID); kindOf(err) != apperr.KindConflict {
		t.Errorf("removing the last user must conflict, got %v", err)
	}
}

// claim builds a setup request with the code the instance would log.
func claim(ctx context.Context, service *Service, name, email string) SetupRequest {
	code, _ := service.SetupCode(ctx)
	return SetupRequest{Code: code, Name: name, Email: email, Password: strongPassword}
}

func TestSetupNeedsTheCodeFromTheLog(t *testing.T) {
	ctx := context.Background()
	service := newService(t)
	code, err := service.SetupCode(ctx)
	if err != nil || len(code) != 14 {
		t.Fatalf("code %q (%v): 12 characters in groups of four", code, err)
	}
	if again, _ := service.SetupCode(ctx); again != code {
		t.Error("the code stays the same until the instance is claimed")
	}
	for _, wrong := range []string{"", "AAAA-BBBB-CCCC", code[:13]} {
		request := SetupRequest{Code: wrong, Name: "Eve", Email: "eve@example.com", Password: strongPassword}
		if _, err := service.Setup(ctx, request); kindOf(err) != apperr.KindInvalid {
			t.Errorf("code %q must be refused, got %v", wrong, err)
		}
	}
	relaxed := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	if _, err := service.Setup(ctx, SetupRequest{Code: relaxed, Name: "Ana", Email: "ana@example.com", Password: strongPassword}); err != nil {
		t.Fatalf("case, spaces and dashes do not matter: %v", err)
	}
	if code, _ := service.SetupCode(ctx); code != "" {
		t.Error("a configured instance has no setup code")
	}
}

func TestOnlyOneConcurrentSetupWins(t *testing.T) {
	ctx := context.Background()
	service := newService(t)
	code, _ := service.SetupCode(ctx)
	var wins atomic.Int32
	var group sync.WaitGroup
	for index := range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			request := SetupRequest{Code: code, Name: "User", Email: fmt.Sprintf("u%d@example.com", index), Password: strongPassword}
			if _, err := service.Setup(ctx, request); err == nil {
				wins.Add(1)
			}
		}()
	}
	group.Wait()
	if wins.Load() != 1 {
		t.Errorf("%d setups succeeded; only the first may", wins.Load())
	}
}

func TestConcurrentCrossDeletionKeepsOneOperator(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	a, err := s.CreateUser(ctx, "Alice", "alice@example.com", strongPassword)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateUser(ctx, "Bob", "bob@example.com", strongPassword)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { defer wg.Done(); <-start; results <- s.DeleteUser(ctx, a.ID, b.ID) }()
	go func() { defer wg.Done(); <-start; results <- s.DeleteUser(ctx, b.ID, a.ID) }()
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if kindOf(err) != apperr.KindConflict {
			t.Fatalf("unexpected deletion failure: %v", err)
		}
	}
	users, err := s.ListUsers(ctx)
	if err != nil || len(users) != 1 || successes != 1 {
		t.Fatalf("users %d successes %d err %v", len(users), successes, err)
	}
}
