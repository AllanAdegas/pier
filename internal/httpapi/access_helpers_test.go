package httpapi

import (
	"context"
	"database/sql"
	"testing"

	rbac "pier/internal/access"
	"pier/internal/identity"
)

// testAccess builds the access service of a fixture. Bootstrap mirrors the
// setup of a real instance: the oldest existing account becomes owner (when a
// fixture creates no account first, nobody is owner until grantRole).
func testAccess(t *testing.T, db *sql.DB) *rbac.Service {
	t.Helper()
	service := rbac.New(db)
	if err := service.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	return service
}

// grantRole gives a test identity its instance role, as an owner would.
func grantRole(t *testing.T, a *API, userID string, role rbac.Role) {
	t.Helper()
	_, err := a.Instance.DB.Exec(`INSERT INTO access_instance_roles(user_id,role) VALUES(?,?)
		ON CONFLICT(user_id) DO UPDATE SET role=excluded.role`, userID, role)
	if err != nil {
		t.Fatal(err)
	}
}

// grantMember gives a test identity a role on one project.
func grantMember(t *testing.T, a *API, userID, project string, role rbac.Role) {
	t.Helper()
	_, err := a.Instance.DB.Exec(`INSERT INTO access_memberships(project_id,user_id,role) VALUES(?,?,?)
		ON CONFLICT(project_id,user_id) DO UPDATE SET role=excluded.role`, project, userID, role)
	if err != nil {
		t.Fatal(err)
	}
}

// testIdentity creates an account and an API token with the abilities.
func testIdentity(t *testing.T, a *API, name string, abilities ...identity.Ability) (identity.User, string) {
	t.Helper()
	ctx := context.Background()
	user, err := a.Identity.CreateUser(ctx, name, name+"@example.invalid", "a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if len(abilities) == 0 {
		abilities = []identity.Ability{identity.AbilityWrite}
	}
	_, token, err := a.Identity.CreateToken(ctx, user.ID, name, abilities)
	if err != nil {
		t.Fatal(err)
	}
	return user, token
}
