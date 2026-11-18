package httpapi

import (
	"net/http"
	"net/http/httptest"
	rbac "pier/internal/access"
	"pier/internal/identity"
	"strings"
	"testing"
)

func TestSwarmRoutesDeclareInstanceAuthorization(t *testing.T) {
	a := New(Dependencies{})
	a.Handler()
	count := 0
	for _, r := range a.routes {
		p, ok := policyFor(r.pattern)
		if !strings.Contains(r.pattern, "/api/v1/swarm") {
			continue
		}
		count++
		if !ok || p.scope != scopeInstance {
			t.Fatalf("undeclared Swarm route %s", r.pattern)
		}
		if r.required != canRead && r.required != canWrite && r.required != canDeploy {
			t.Fatal(r.pattern)
		}
	}
	if count != 13 {
		t.Fatalf("Swarm routes: %d", count)
	}
}

func TestSwarmRejectsAnonymous(t *testing.T) {
	a := platformAPI(t)
	h := a.Handler()
	for _, path := range []string{"/api/v1/swarm", "/api/v1/swarm/test/services", "/api/v1/swarm/test/nodes"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestSwarmRoleAndTokenRestrictions(t *testing.T) {
	a := platformAPI(t)
	mux := http.NewServeMux()
	for pattern, ability := range map[string]access{"GET /api/v1/swarm": canRead, "POST /api/v1/swarm": canWrite, "POST /api/v1/swarm/{id}/services": canDeploy} {
		a.route(mux, pattern, ability, func(w http.ResponseWriter, r *http.Request) error { return noContent(w) })
	}
	world := accessWorld{a: a, handler: a.auditRequest(mux)}
	viewer, token := testIdentity(t, a, "swarm-viewer")
	grantRole(t, a, viewer.ID, rbac.Viewer)
	if got := world.call(t, token, "GET", "/api/v1/swarm", "").Code; got != 204 {
		t.Fatal(got)
	}
	if got := world.call(t, token, "POST", "/api/v1/swarm/test/services", "{}").Code; got != 403 {
		t.Fatal(got)
	}
	admin, readToken := testIdentity(t, a, "swarm-admin-read", identity.AbilityRead)
	grantRole(t, a, admin.ID, rbac.Admin)
	if got := world.call(t, readToken, "POST", "/api/v1/swarm", "{}").Code; got != 403 {
		t.Fatal(got)
	}
	_, noRole := testIdentity(t, a, "swarm-no-role")
	if got := world.call(t, noRole, "GET", "/api/v1/swarm", "").Code; got != 403 {
		t.Fatal(got)
	}
}
