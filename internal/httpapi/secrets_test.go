package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"pier/internal/application"
	"pier/internal/engine"
	"pier/internal/identity"
	"pier/internal/platform/apperr"
	"pier/internal/stack"
)

func requestAs(abilities ...identity.Ability) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	principal := identity.Principal{Abilities: abilities, ViaToken: true}
	return request.WithContext(context.WithValue(request.Context(), contextKey{}, principal))
}

func TestReadTokensDoNotSeeSecretsInsideOrdinaryResponses(t *testing.T) {
	reader := requestAs(identity.AbilityRead)
	secretStack := stack.Stack{Name: "db", Compose: "environment:\n  - DB_PASSWORD=dummy-secret\n"}
	secretApp := application.Application{Config: application.Config{
		Source: application.Source{Repository: "https://dummy-user:dummy-token@git.invalid/org/repo.git"},
	}}
	secretContainer := engine.Container{Name: "cache", Command: "redis-server --requirepass dummy-secret"}
	secretVariable := stack.VariableStatus{Variable: stack.Variable{Name: "DB_PASSWORD", Default: "dummy-secret"}}

	seen := strings.Join([]string{
		forCaller(reader, secretStack).Compose,
		allForCaller(reader, []stack.Stack{secretStack})[0].Compose,
		forCaller(reader, secretApp).Config.Source.Repository,
		allForCaller(reader, []engine.Container{secretContainer})[0].Command,
		allForCaller(reader, []stack.VariableStatus{secretVariable})[0].Default,
	}, "\n")
	for _, secret := range []string{"dummy-secret", "dummy-token", "dummy-user"} {
		if strings.Contains(seen, secret) {
			t.Errorf("a read token received %q:\n%s", secret, seen)
		}
	}
	if got := forCaller(reader, secretApp).Config.Source.Repository; got != "https://***@git.invalid/org/repo.git" {
		t.Errorf("masked repository = %q", got)
	}
}

func TestDeployTokensDoNotSeeSecretsEither(t *testing.T) {
	deployer := requestAs(identity.AbilityDeploy)
	app := application.Application{Config: application.Config{
		Source: application.Source{Repository: "https://ghp_dummytoken@github.com/org/repo"},
	}}
	if got := forCaller(deployer, app).Config.Source.Repository; strings.Contains(got, "dummytoken") {
		t.Errorf("a deploy token received the repository credential: %q", got)
	}
}

func TestWriteTokensAndSessionsSeeEverything(t *testing.T) {
	writer := requestAs(identity.AbilityWrite)
	secretStack := stack.Stack{Compose: "services: {}"}
	if got := forCaller(writer, secretStack).Compose; got != secretStack.Compose {
		t.Errorf("write token got compose %q", got)
	}
	repository := "https://user:token@git.invalid/repo.git"
	app := application.Application{Config: application.Config{Source: application.Source{Repository: repository}}}
	if got := forCaller(writer, app).Config.Source.Repository; got != repository {
		t.Errorf("write token got repository %q", got)
	}
}

func TestRepositoriesWithoutCredentialsAreUnchanged(t *testing.T) {
	reader := requestAs(identity.AbilityRead)
	for _, repository := range []string{
		"https://github.com/org/repo.git", "git@github.com:org/repo.git", "ssh://git@host/org/repo.git",
	} {
		app := application.Application{Config: application.Config{Source: application.Source{Repository: repository}}}
		if got := forCaller(reader, app).Config.Source.Repository; got != repository {
			t.Errorf("%q became %q", repository, got)
		}
	}
}

func TestManagedContainerLogsNeedTheWritePermission(t *testing.T) {
	api := &API{}
	// No connector is configured: reaching the server would panic, so the
	// refusal has to come first.
	err := api.streamContainerLogs(httptest.NewRecorder(), requestAs(identity.AbilityRead))
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.KindForbidden {
		t.Fatalf("err = %v, want forbidden", err)
	}
}

func trustOnly(prefixes ...string) proxyTrust {
	trust := proxyTrust{}
	for _, prefix := range prefixes {
		trust.prefixes = append(trust.prefixes, netip.MustParsePrefix(prefix))
	}
	return trust
}

func forwarded(peer, chain string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = peer
	if chain != "" {
		request.Header.Set("X-Forwarded-For", chain)
	}
	return request
}

func TestTheClientAddressIsTheFirstUntrustedHopFromTheRight(t *testing.T) {
	trust := trustOnly("10.0.0.0/8")
	cases := []struct{ peer, chain, want string }{
		// A client cannot pick its identity by writing the left of the chain:
		// the proxy appends the address it saw.
		{"10.0.0.2:4000", "1.1.1.1, 203.0.113.9", "203.0.113.9"},
		{"10.0.0.2:4000", "203.0.113.9, 10.0.0.7", "203.0.113.9"},
		{"10.0.0.2:4000", "", "10.0.0.2"},
		{"10.0.0.2:4000", "garbage", "10.0.0.2"},
		// Untrusted peers cannot forward anything.
		{"198.51.100.4:4000", "1.1.1.1", "198.51.100.4"},
		{"[::ffff:198.51.100.4]:4000", "1.1.1.1", "::ffff:198.51.100.4"},
	}
	for _, c := range cases {
		if got := trust.clientAddress(forwarded(c.peer, c.chain)); got != c.want {
			t.Errorf("peer %s chain %q: got %s, want %s", c.peer, c.chain, got, c.want)
		}
	}
}

func TestOnlyTrustedProxiesCanSayTheConnectionWasHTTPS(t *testing.T) {
	trust := trustOnly("127.0.0.0/8")
	direct := forwarded("198.51.100.4:4000", "")
	direct.Header.Set("X-Forwarded-Proto", "https")
	if trust.isHTTPS(direct) {
		t.Error("an untrusted peer made the connection count as HTTPS")
	}
	proxied := forwarded("127.0.0.1:4000", "")
	proxied.Header.Set("X-Forwarded-Proto", "https")
	if !trust.isHTTPS(proxied) {
		t.Error("the trusted proxy's X-Forwarded-Proto was ignored")
	}
}

func TestRotatingAddressesStillHitsThePerAccountLimit(t *testing.T) {
	api := New(Dependencies{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	allowed := 0
	for attempt := range 100 {
		// A workload next to Pier forwarding a different address each time.
		request := forwarded("10.0.0.9:5000", netip.AddrFrom4([4]byte{203, 0, 113, byte(attempt)}).String())
		if api.allowGuess(request, "login:admin@example.com") {
			allowed++
		}
	}
	if allowed > 20 {
		t.Errorf("%d guesses allowed against one account in a minute, want at most 20", allowed)
	}
}
