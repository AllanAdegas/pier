package access

import (
	"context"
	"path/filepath"
	"pier/internal/platform/sqlite"
	"sync"
	"testing"
	"time"
)

func TestIsolationAndInvitations(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []string{"first", "alice", "bob"} {
		if _, err = db.Exec(`INSERT INTO users(id,email,name,password_hash,created_at) VALUES(?,?,?,'unused',?)`, id, id+"@example.com", id, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"one", "two"} {
		if _, err = db.Exec(`INSERT INTO projects(id,name,description,created_at) VALUES(?,?,'',?)`, id, id, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	s := New(db)
	if err = s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	// Lexicographic tiebreak is deterministic, so alice is the oldest owner.
	if err = s.Check(ctx, "alice", "", "write"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetMember(ctx, "alice", "one", "bob", Developer); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(ctx, "bob", "one", "deploy"); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"two", ""} {
		if s.Check(ctx, "bob", scope, "read") == nil {
			t.Fatal("cross-project/global leak")
		}
	}
	if s.Check(ctx, "bob", "one", "write") == nil {
		t.Fatal("developer escalated")
	}
	if s.SetMember(ctx, "bob", "one", "first", Owner) == nil {
		t.Fatal("developer granted owner")
	}
	_, token, err := s.Invite(ctx, "alice", "two", "bob@example.com", Viewer)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = db.QueryRow(`SELECT token_hash FROM access_invites`).Scan(&stored); err != nil || stored == token {
		t.Fatal("plaintext token persisted", err)
	}
	if s.Accept(ctx, "first", token) == nil {
		t.Fatal("wrong identity accepted")
	}
	if err = s.Accept(ctx, "bob", token); err != nil {
		t.Fatal(err)
	}
	if s.Accept(ctx, "bob", token) == nil {
		t.Fatal("replay accepted")
	}
	if s.Check(ctx, "bob", "two", "deploy") == nil {
		t.Fatal("viewer deployed")
	}
	item, token, err := s.Invite(ctx, "alice", "two", "first@example.com", Viewer)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(ctx, "alice", "two", item.ID); err != nil {
		t.Fatal(err)
	}
	if s.Accept(ctx, "first", token) == nil {
		t.Fatal("revoked invite accepted")
	}
	_, concurrentToken, concurrentErr := s.Invite(ctx, "alice", "two", "first@example.com", Viewer)
	if concurrentErr != nil {
		t.Fatal(concurrentErr)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Accept(ctx, "first", concurrentToken) }()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("concurrent accepts: %d", accepted)
	}
	_, token, err = s.Invite(ctx, "alice", "two", "first@example.com", Viewer)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if s.Accept(ctx, "first", token) == nil {
		t.Fatal("expired invite accepted")
	}
	if err = s.SetMember(ctx, "alice", "one", "first", Owner); err != nil {
		t.Fatal(err)
	}
	if s.SetMember(ctx, "alice", "one", "first", Viewer) == nil {
		t.Fatal("last owner removed")
	}
	if s.SetMember(ctx, "first", "one", "first", Viewer) == nil {
		t.Fatal("self lockout")
	}
}
func TestRoleMatrix(t *testing.T) {
	for _, r := range []Role{Owner, Admin, Developer, Viewer, "invalid"} {
		for _, a := range []string{"read", "deploy", "write", "unknown"} {
			want := (a == "read" && r.Valid()) || (a == "deploy" && (r == Owner || r == Admin || r == Developer)) || (a == "write" && (r == Owner || r == Admin))
			if r.Allows(a) != want {
				t.Fatalf("%s %s", r, a)
			}
		}
	}
}
