package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"pier/internal/application"
	"pier/internal/gitprovider"
	"pier/internal/identity"
	"pier/internal/indexnow"
	"pier/internal/registry"
	"pier/internal/server"
)

// fullAPI registers every optional route group (each guarded by its service
// being configured), so the coverage test sees the whole route table.
func fullAPI(t *testing.T) *API {
	t.Helper()
	a := platformAPI(t)
	a.Previews = &application.PreviewService{}
	a.MultiServer = &application.MultiServer{}
	a.Registries = &registry.Service{}
	a.GitProviders = &gitprovider.Service{}
	a.GitFeedback = &gitprovider.Queue{}
	a.GitConnections = gitprovider.NewConnections(a.GitProviders)
	a.ServerOperations = &server.Operations{}
	a.OIDC = &identity.OIDCService{}
	a.IndexNow = &indexnow.Service{}
	a.Handler()
	return a
}

// TestEveryRouteDeclaresAbilityAndScope walks every registered route. Each
// must declare an ability and resolve to a declared authorization policy,
// whose wildcards exist in the pattern. A new route outside the declared
// prefixes fails here instead of shipping (at runtime it is owner-only).
func TestEveryRouteDeclaresAbilityAndScope(t *testing.T) {
	a := fullAPI(t)
	if len(a.routes) < 150 {
		t.Fatalf("only %d routes registered: the fixture lost route groups", len(a.routes))
	}
	for _, route := range a.routes {
		policy, declared := policyFor(route.pattern)
		if !declared {
			t.Errorf("%s: no authorization policy (add it to access_policy.go)", route.pattern)
			continue
		}
		if route.public != (policy.scope == scopePublic) {
			t.Errorf("%s: public registration and policy disagree", route.pattern)
		}
		if !route.public && route.required.ability == "" {
			t.Errorf("%s: no ability declared", route.pattern)
		}
		checkPolicyWildcards(t, route.pattern, policy)
	}
}

func checkPolicyWildcards(t *testing.T, pattern string, policy routePolicy) {
	t.Helper()
	if policy.scope != scopeProject {
		return
	}
	if !strings.Contains(pattern, "{"+policy.idParam()+"}") {
		t.Errorf("%s: project scope without {%s}", pattern, policy.idParam())
	}
	if policy.kindParam != "" && !strings.Contains(pattern, "{"+policy.kindParam+"}") {
		t.Errorf("%s: kind wildcard {%s} missing", pattern, policy.kindParam)
	}
	if policy.kindParam == "" && policy.kind == "" {
		t.Errorf("%s: project scope without a resource kind", pattern)
	}
}

// TestNoHandlerBypassesTheRouteTable finds mux registrations that skip
// a.route/a.public (and so the authorization middleware). Only the listed,
// deliberately unauthenticated or self-protected endpoints may do that.
func TestNoHandlerBypassesTheRouteTable(t *testing.T) {
	allowed := map[string]bool{
		`"GET /healthz"`: true, `"GET /api/v1/version"`: true, `"/api/"`: true, `"/"`: true,
		`mcp.Path`: true, `"POST /webhooks/git-provider/github"`: true,
	}
	direct := regexp.MustCompile(`mux\.Handle(?:Func)?\(([^,]+),`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range direct.FindAllStringSubmatch(string(source), -1) {
			if target := strings.TrimSpace(match[1]); !allowed[target] && target != "pattern" {
				t.Errorf("%s registers %s outside a.route/a.public", file, target)
			}
		}
	}
}

func TestUndeclaredRouteIsOwnerOnly(t *testing.T) {
	policy, declared := policyFor("GET /api/v1/something-new/{id}")
	if declared || policy.scope != scopeUndeclared || policy.minimum != "owner" {
		t.Fatalf("undeclared route policy %+v", policy)
	}
}
