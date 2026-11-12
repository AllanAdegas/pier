package httpapi

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"pier/internal/identity"
	"strings"
	"testing"

	rbac "pier/internal/access"
)

func testAccess(t *testing.T, db *sql.DB) *rbac.Service {
	t.Helper()
	service := rbac.New(db)
	if err := service.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	return service
}

func TestAccessFullHandlerRolesLiveAndGlobal(t *testing.T) {
	a := platformAPI(t)
	ctx := context.Background()
	owner, err := a.Identity.CreateUser(ctx, "Owner", "owner@example.com", "testing-password-long")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Access.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	db := a.Instance.DB
	for _, query := range []string{
		`INSERT INTO projects(id,name,created_at) VALUES('p','Project','2026-01-01'),('foreign','Foreign','2026-01-01')`,
		`INSERT INTO environments(id,project_id,name,created_at) VALUES('e','p','Env','2026-01-01'),('other','foreign','Other','2026-01-01')`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	_ = owner
	handler := a.Handler()
	for _, role := range []string{"viewer", "developer", "admin", "owner"} {
		t.Run(role, func(t *testing.T) {
			user, err := a.Identity.CreateUser(ctx, role, role+"@example.com", "testing-password-long")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`INSERT INTO access_memberships(project_id,user_id,role) VALUES('p',?,?)`, user.ID, role); err != nil {
				t.Fatal(err)
			}
			_, token, err := a.Identity.CreateToken(ctx, user.ID, "test", []identity.Ability{identity.AbilityWrite})
			if err != nil {
				t.Fatal(err)
			}
			call := func(method, path, body string) int {
				r := httptest.NewRequest(method, path, strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w.Code
			}
			if got := call("GET", "/api/v1/projects/p", ""); got != 200 {
				t.Fatalf("read: %d", got)
			}
			for _, path := range []string{"/api/v1/projects/foreign", "/api/v1/projects", "/api/v1/servers", "/api/v1/users"} {
				if got := call("GET", path, ""); got != 403 {
					t.Fatalf("%s: %d", path, got)
				}
			}
			got := call("PATCH", "/api/v1/projects/p", `{"name":"Updated"}`)
			if role == "viewer" || role == "developer" {
				if got != 403 {
					t.Fatalf("write: %d", got)
				}
			} else if got != 200 {
				t.Fatalf("write: %d", got)
			}
			if got := call("PATCH", "/api/v1/projects/p", `{"name":"Updated","environment_id":"other"}`); got != 403 {
				t.Fatalf("foreign reference: %d", got)
			}
			if _, err = db.Exec(`DELETE FROM access_memberships WHERE user_id=?`, user.ID); err != nil {
				t.Fatal(err)
			}
			if got := call("GET", "/api/v1/projects/p", ""); got != 403 {
				t.Fatalf("revoked: %d", got)
			}
		})
	}
}
