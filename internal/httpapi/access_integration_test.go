package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rbac "pier/internal/access"
	"pier/internal/identity"
)

// accessWorld is two projects (A, B), each with an environment and an
// application, on one shared server, plus routes with stub handlers mounted
// on their real patterns: the stub answers 204 (or reports what the handler
// sees), so a status other than 204 is the authorization's decision.
type accessWorld struct {
	a       *API
	handler http.Handler
}

func newAccessWorld(t *testing.T) accessWorld {
	t.Helper()
	a := platformAPI(t)
	for _, statement := range []string{
		`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('srv','srv','local','/data','2026-01-01')`,
		`INSERT INTO projects(id,name,created_at) VALUES('pa','A','2026-01-01'),('pb','B','2026-01-01')`,
		`INSERT INTO environments(id,project_id,name,created_at) VALUES('ea','pa','prod','2026-01-01'),('eb','pb','prod','2026-01-01')`,
		`INSERT INTO applications(id,environment_id,server_id,name,config,webhook_secret,created_at,updated_at)
			VALUES('appa','ea','srv','a','{}','x','2026-01-01','2026-01-01'),('appb','eb','srv','b','{}','x','2026-01-01','2026-01-01')`,
	} {
		if _, err := a.Instance.DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	stub := func(w http.ResponseWriter, r *http.Request) error { return noContent(w) }
	for pattern, required := range map[string]access{
		"PUT /api/v1/applications/{id}/placement": canWrite,
		"GET /api/v1/applications/{id}":           canRead,
		"PATCH /api/v1/applications/{id}":         canWrite,
		"POST /api/v1/applications/{id}/deploy":   canDeploy,
		"POST /api/v1/applications/{id}/promote":  canDeploy,
		"POST /api/v1/applications":               canWrite,
		"DELETE /api/v1/projects/{id}":            canWrite,
		"GET /api/v1/servers":                     canRead,
		"PATCH /api/v1/servers/{id}":              canWrite,
		"POST /api/v1/users":                      sessionOnly,
		"GET /api/v1/dashboard":                   canRead,
		"POST /api/v1/projects/{id}/environments": canWrite,
	} {
		a.route(mux, pattern, required, stub)
	}
	a.route(mux, "GET /api/v1/projects", canRead, func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, []map[string]string{{"id": "pa"}, {"id": "pb"}})
	})
	a.route(mux, "GET /api/v1/applications/{id}/envs", canRead, func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, map[string]bool{"sees_secrets": seesSecrets(r)})
	})
	return accessWorld{a: a, handler: a.auditRequest(mux)}
}

func (w accessWorld) call(t *testing.T, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	w.handler.ServeHTTP(recorder, r)
	return recorder
}

type accessCase struct {
	method, path, body string
	want               map[rbac.Role]int
}

// every maps all project roles to the same status.
func every(status int) map[rbac.Role]int {
	return map[rbac.Role]int{rbac.Viewer: status, rbac.Developer: status, rbac.Admin: status, rbac.Owner: status}
}

func from(minimum rbac.Role) map[rbac.Role]int {
	want := map[rbac.Role]int{}
	for _, role := range []rbac.Role{rbac.Viewer, rbac.Developer, rbac.Admin, rbac.Owner} {
		want[role] = 403
		if role.AtLeast(minimum) {
			want[role] = 204
		}
	}
	return want
}

// TestProjectRoleMatrixAndIsolation: a member of project A only, for each
// role, against A and B. A developer on A can neither read nor deploy B.
func TestProjectRoleMatrixAndIsolation(t *testing.T) {
	world := newAccessWorld(t)
	cases := []accessCase{
		{"GET", "/api/v1/applications/appa", "", every(204)},
		{"GET", "/api/v1/applications/appb", "", every(403)},
		{"PUT", "/api/v1/applications/appa/placement", `{"swarm_cluster_id":"sw"}`, from(rbac.Admin)},
		{"PUT", "/api/v1/applications/appb/placement", `{"swarm_cluster_id":"sw"}`, every(403)},
		{"POST", "/api/v1/applications/appa/deploy", "", from(rbac.Developer)},
		{"POST", "/api/v1/applications/appb/deploy", "", every(403)},
		{"PATCH", "/api/v1/applications/appa", `{"name":"x"}`, from(rbac.Admin)},
		{"PATCH", "/api/v1/applications/appb", `{"name":"x"}`, every(403)},
		{"POST", "/api/v1/projects/pa/environments", `{"name":"dev"}`, from(rbac.Admin)},
		{"DELETE", "/api/v1/projects/pa", "", from(rbac.Owner)},
		{"DELETE", "/api/v1/projects/pb", "", every(403)},
		{"POST", "/api/v1/applications", `{"environment_id":"ea","server_id":"srv"}`, from(rbac.Admin)},
		{"POST", "/api/v1/applications", `{"environment_id":"eb","server_id":"srv"}`, every(403)},
		{"POST", "/api/v1/applications", `{"server_id":"srv"}`, every(403)},
		// Cross-project references in the body need the ability on B too.
		{"POST", "/api/v1/applications/appa/promote", `{"target_application_id":"appb"}`, every(403)},
		{"PATCH", "/api/v1/applications/appa", `{"Environment_ID":"eb"}`, every(403)},
		{"PATCH", "/api/v1/applications/appa", `{"nested":{"environment_id":"eb"}}`, every(403)},
		{"PATCH", "/api/v1/applications/appa", `{"private_key_id":"k"}`, every(403)},
		{"GET", "/api/v1/applications/missing", "", every(403)},
		{"GET", "/api/v1/servers", "", every(403)},
		{"PATCH", "/api/v1/servers/srv", `{}`, every(403)},
		{"GET", "/api/v1/dashboard", "", every(403)},
	}
	for _, role := range []rbac.Role{rbac.Viewer, rbac.Developer, rbac.Admin, rbac.Owner} {
		user, token := testIdentity(t, world.a, "member-"+string(role))
		grantMember(t, world.a, user.ID, "pa", role)
		for _, tc := range cases {
			if got := world.call(t, token, tc.method, tc.path, tc.body).Code; got != tc.want[role] {
				t.Errorf("project %s: %s %s %s = %d, want %d", role, tc.method, tc.path, tc.body, got, tc.want[role])
			}
		}
		listed := world.call(t, token, "GET", "/api/v1/projects", "")
		if listed.Code != 200 || strings.Contains(listed.Body.String(), "pb") || !strings.Contains(listed.Body.String(), "pa") {
			t.Errorf("project %s: list %d %s", role, listed.Code, listed.Body.String())
		}
	}
}

// TestInstanceRoleMatrix: instance roles cover every project and the
// instance-level routes; user management is owner-only.
func TestInstanceRoleMatrix(t *testing.T) {
	world := newAccessWorld(t)
	cases := []accessCase{
		{"GET", "/api/v1/applications/appb", "", every(204)},
		{"POST", "/api/v1/applications/appb/deploy", "", from(rbac.Developer)},
		{"PATCH", "/api/v1/applications/appb", `{"name":"x"}`, from(rbac.Admin)},
		{"POST", "/api/v1/applications/appa/promote", `{"target_application_id":"appb"}`, from(rbac.Developer)},
		{"GET", "/api/v1/servers", "", every(204)},
		{"GET", "/api/v1/dashboard", "", every(204)},
		{"PATCH", "/api/v1/servers/srv", `{}`, from(rbac.Admin)},
		{"POST", "/api/v1/applications", `{"server_id":"srv"}`, from(rbac.Admin)},
		{"DELETE", "/api/v1/projects/pb", "", from(rbac.Owner)},
	}
	for _, role := range []rbac.Role{rbac.Viewer, rbac.Developer, rbac.Admin, rbac.Owner} {
		user, token := testIdentity(t, world.a, "instance-"+string(role))
		grantRole(t, world.a, user.ID, role)
		for _, tc := range cases {
			if got := world.call(t, token, tc.method, tc.path, tc.body).Code; got != tc.want[role] {
				t.Errorf("instance %s: %s %s = %d, want %d", role, tc.method, tc.path, got, tc.want[role])
			}
		}
		listed := world.call(t, token, "GET", "/api/v1/projects", "")
		if !strings.Contains(listed.Body.String(), "pb") {
			t.Errorf("instance %s: list filtered %s", role, listed.Body.String())
		}
	}
}

// TestTokenAbilityAndRoleAreIntersected: neither the token nor the role can
// grant more than the other, and handlers see the reduced principal.
func TestTokenAbilityAndRoleAreIntersected(t *testing.T) {
	world := newAccessWorld(t)
	admin, readToken := testIdentity(t, world.a, "admin-read", identity.AbilityRead)
	grantMember(t, world.a, admin.ID, "pa", rbac.Admin)
	if got := world.call(t, readToken, "PATCH", "/api/v1/applications/appa", `{}`).Code; got != 403 {
		t.Fatalf("read token wrote: %d", got)
	}
	viewer, writeToken := testIdentity(t, world.a, "viewer-write")
	grantMember(t, world.a, viewer.ID, "pa", rbac.Viewer)
	response := world.call(t, writeToken, "GET", "/api/v1/applications/appa/envs", "")
	var seen map[string]bool
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &seen) != nil || seen["sees_secrets"] {
		t.Fatalf("viewer with a write token sees secrets: %d %s", response.Code, response.Body.String())
	}
	if got := world.call(t, writeToken, "PATCH", "/api/v1/applications/appa", `{}`).Code; got != 403 {
		t.Fatalf("viewer wrote with a write token: %d", got)
	}
	none, noRole := testIdentity(t, world.a, "nobody")
	_ = none
	listed := world.call(t, noRole, "GET", "/api/v1/projects", "")
	if listed.Code != 200 || strings.TrimSpace(listed.Body.String()) != "[]" {
		t.Fatalf("account without roles listed %s", listed.Body.String())
	}
}

// TestMembershipRemovalTakesEffectOnNextRequest: roles are never cached.
func TestMembershipRemovalTakesEffectOnNextRequest(t *testing.T) {
	world := newAccessWorld(t)
	owner, _ := testIdentity(t, world.a, "owner")
	grantRole(t, world.a, owner.ID, rbac.Owner)
	member, token := testIdentity(t, world.a, "dev")
	if err := world.a.Access.SetMember(t.Context(), owner.ID, "pa", member.ID, rbac.Developer); err != nil {
		t.Fatal(err)
	}
	if got := world.call(t, token, "POST", "/api/v1/applications/appa/deploy", "").Code; got != 204 {
		t.Fatalf("member deploy: %d", got)
	}
	if err := world.a.Access.SetMember(t.Context(), owner.ID, "pa", member.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := world.call(t, token, "GET", "/api/v1/applications/appa", "").Code; got != 403 {
		t.Fatalf("removed member read: %d", got)
	}
}

// TestMeAccessDescribesRolesAndCredential feeds the UI's can() helper.
func TestMeAccessDescribesRolesAndCredential(t *testing.T) {
	world := newAccessWorld(t)
	user, token := testIdentity(t, world.a, "reader", identity.AbilityRead)
	grantMember(t, world.a, user.ID, "pa", rbac.Developer)
	r := httptest.NewRequest("GET", "/api/v1/me/access", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	world.a.Handler().ServeHTTP(w, r)
	var body struct {
		InstanceRole string              `json:"instance_role"`
		Projects     []map[string]string `json:"projects"`
		Abilities    []string            `json:"abilities"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if body.InstanceRole != "" || len(body.Projects) != 1 || body.Projects[0]["role"] != "developer" ||
		len(body.Abilities) != 1 || body.Abilities[0] != "read" {
		t.Fatalf("summary %+v", body)
	}
}
