package httpapi

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/identity"
	"pier/internal/platform/sqlite"
)

// preRBACStatements turn a current database back into a 0.5 one: no access
// tables, migration 0019 not applied.
var preRBACStatements = []string{
	`DROP TRIGGER access_keep_last_owner_update`, `DROP TRIGGER access_keep_last_owner_delete`,
	`DROP TABLE access_oidc_states`, `DROP TABLE access_oidc_links`, `DROP TABLE access_oidc_config`,
	`DROP TABLE access_invites`, `DROP TABLE access_memberships`, `DROP TABLE access_instance_roles`,
	`DELETE FROM schema_migrations WHERE name LIKE '%0019_rbac.sql'`,
}

// TestUpgradeKeepsExistingAdminsAndTokens: on a 0.5 instance every account
// was a full administrator. After migrating, each one is instance owner and
// its tokens keep exactly their abilities, on every project and the instance.
func TestUpgradeKeepsExistingAdminsAndTokens(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "pier.db")
	old, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range preRBACStatements {
		if _, err := old.Exec(statement); err != nil {
			t.Fatal(statement, err)
		}
	}
	accounts := identity.NewService(identity.NewStore(old), time.Hour)
	tokens := map[string]string{}
	for _, name := range []string{"admin", "colleague"} {
		user, err := accounts.CreateUser(ctx, name, name+"@example.invalid", "a-long-test-password")
		if err != nil {
			t.Fatal(err)
		}
		for _, ability := range []identity.Ability{identity.AbilityRead, identity.AbilityWrite} {
			_, secret, err := accounts.CreateToken(ctx, user.ID, string(ability), []identity.Ability{ability})
			if err != nil {
				t.Fatal(err)
			}
			tokens[name+"-"+string(ability)] = secret
		}
	}
	old.Close()

	db, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := platformAPIWith(t, db, dir)
	handler := a.Handler()
	call := func(token, method, path, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	for _, name := range []string{"admin", "colleague"} {
		if got := call(tokens[name+"-write"], "POST", "/api/v1/projects", `{"name":"`+name+`"}`); got != 201 {
			t.Fatalf("%s write token after upgrade: %d", name, got)
		}
		if got := call(tokens[name+"-read"], "GET", "/api/v1/projects", ""); got != 200 {
			t.Fatalf("%s read token after upgrade: %d", name, got)
		}
		if got := call(tokens[name+"-read"], "POST", "/api/v1/projects", `{"name":"no"}`); got != 403 {
			t.Fatalf("%s read token gained write: %d", name, got)
		}
		if got := call(tokens[name+"-read"], "GET", "/api/v1/users", ""); got != 200 {
			t.Fatalf("%s instance read after upgrade: %d", name, got)
		}
	}
	var owners int
	if err := db.QueryRow(`SELECT COUNT(*) FROM access_instance_roles WHERE role='owner'`).Scan(&owners); err != nil || owners != 2 {
		t.Fatalf("owners after upgrade %d %v", owners, err)
	}
	newcomer, token := testIdentity(t, a, "newcomer")
	_ = newcomer
	if got := call(token, "GET", "/api/v1/users", ""); got != 403 {
		t.Fatalf("account created after the upgrade got an implicit role: %d", got)
	}
}

// TestFreshInstanceSetupMakesFirstAccountOwner covers the other entry: no
// account at migration time, the setup endpoint creates the owner.
func TestFreshInstanceSetupMakesFirstAccountOwner(t *testing.T) {
	a := platformAPI(t)
	handler := a.Handler()
	code, err := a.Identity.SetupCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/auth/setup", strings.NewReader(
		`{"name":"Admin","email":"admin@example.invalid","password":"a-long-test-password","setup_code":"`+code+`"}`))
	r.Header.Set(clientHeader, "web")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("setup %d %s", w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	r = httptest.NewRequest("GET", "/api/v1/users", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("first account is not owner: %d %s", w.Code, w.Body.String())
	}
}
