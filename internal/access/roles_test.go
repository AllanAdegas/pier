package access

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"pier/internal/platform/sqlite"
)

func rolesFixture(t *testing.T, users ...string) (*Service, *sql.DB) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "roles.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, id := range users {
		if _, err := db.Exec(`INSERT INTO users(id,email,name,password_hash,created_at) VALUES(?,?,?,'unused','2026-01-01')`,
			id, id+"@example.com", id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO projects(id,name,description,created_at) VALUES('p','p','','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}

func TestRoleAbilities(t *testing.T) {
	for role, abilities := range map[Role][3]bool{
		Viewer: {true, false, false}, Developer: {true, true, false}, Admin: {true, true, true}, Owner: {true, true, true}, "": {},
	} {
		for index, ability := range []string{"read", "deploy", "write"} {
			if role.Allows(ability) != abilities[index] {
				t.Errorf("%q allows %s: %v", role, ability, !abilities[index])
			}
		}
	}
}

func TestInvitesNeverEscalateBeyondTheInviter(t *testing.T) {
	ctx := context.Background()
	s, _ := rolesFixture(t, "root", "admin", "dev", "guest", "guest2")
	if err := s.Assign(ctx, "root", Owner); err != nil {
		t.Fatal(err)
	}
	for user, role := range map[string]Role{"admin": Admin, "dev": Developer} {
		if err := s.SetMember(ctx, "root", "p", user, role); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Invite(ctx, "dev", "p", "guest@example.com", Viewer); err == nil {
		t.Fatal("developer invited")
	}
	if _, _, err := s.Invite(ctx, "admin", "p", "guest@example.com", Owner); err == nil {
		t.Fatal("invite granted owner")
	}
	if s.SetMember(ctx, "admin", "p", "guest", Owner) == nil {
		t.Fatal("admin granted owner")
	}
	_, token, err := s.Invite(ctx, "admin", "p", "guest@example.com", Admin)
	if err != nil {
		t.Fatal(err)
	}
	// The inviter is demoted before the invite is used: it no longer works.
	if err := s.SetMember(ctx, "root", "p", "admin", Developer); err != nil {
		t.Fatal(err)
	}
	if s.Accept(ctx, "guest", token) == nil {
		t.Fatal("invite of a demoted inviter accepted")
	}
	_, token, err = s.Invite(ctx, "root", "p", "guest2@example.com", Developer)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Accept(ctx, "guest2", token); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.Role(ctx, "guest2", "p"); role != Developer {
		t.Fatalf("accepted role %q", role)
	}
}

func TestLastOwnerCannotBeRemovedOrDemoted(t *testing.T) {
	ctx := context.Background()
	s, db := rolesFixture(t, "first", "second", "member")
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInstanceRole(ctx, "first", "first", Viewer); err == nil {
		t.Fatal("self demotion")
	}
	if err := s.Assign(ctx, "first", Viewer); err == nil {
		t.Fatal("last instance owner demoted")
	}
	if err := s.GuardAccountRemoval(ctx, "first"); err == nil {
		t.Fatal("last owner removal allowed")
	}
	// The database refuses it on every other path too (account deletion).
	if _, err := db.Exec(`DELETE FROM users WHERE id='first'`); err == nil {
		t.Fatal("trigger let the last owner account be deleted")
	}
	if _, err := db.Exec(`UPDATE access_instance_roles SET role='admin' WHERE user_id='first'`); err == nil {
		t.Fatal("trigger let the last owner be demoted")
	}
	if err := s.SetInstanceRole(ctx, "first", "second", Owner); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInstanceRole(ctx, "second", "first", Admin); err != nil {
		t.Fatalf("demoting one of two owners: %v", err)
	}
	if err := s.SetInstanceRole(ctx, "first", "second", ""); err == nil {
		t.Fatal("instance admin removed the owner")
	}
	// Project owners: the project keeps one.
	if err := s.SetMember(ctx, "second", "p", "member", Owner); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMember(ctx, "second", "p", "member", Admin); err == nil {
		t.Fatal("last project owner demoted")
	}
	if err := s.SetMember(ctx, "second", "p", "member", ""); err == nil {
		t.Fatal("last project owner removed")
	}
}

func TestSSORoleNeverTouchesOwners(t *testing.T) {
	ctx := context.Background()
	s, _ := rolesFixture(t, "owner", "sso")
	if err := s.Assign(ctx, "owner", Owner); err != nil {
		t.Fatal(err)
	}
	changed, cancel := s.Subscribe("sso")
	defer cancel()
	if err := s.ApplySSORole(ctx, "sso", Developer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	default:
		t.Fatal("role change not signalled")
	}
	if role, _ := s.Role(ctx, "sso", ""); role != Developer {
		t.Fatalf("mapped role %q", role)
	}
	if err := s.ApplySSORole(ctx, "sso", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Role(ctx, "sso", ""); err == nil {
		t.Fatal("unmapped login kept its instance role")
	}
	if s.ApplySSORole(ctx, "sso", Owner) == nil {
		t.Fatal("SSO granted owner")
	}
	if err := s.ApplySSORole(ctx, "owner", Viewer); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.Role(ctx, "owner", ""); role != Owner {
		t.Fatalf("SSO demoted an owner to %q", role)
	}
}
