package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	rbac "pier/internal/access"
	"pier/internal/platform/executor"
	"pier/internal/terminal"
)

// TestTerminalTicketNeedsWriteOnTheResourceProject: a developer cannot open
// a terminal, and an admin of A cannot open one on B or on a server.
func TestTerminalTicketNeedsWriteOnTheResourceProject(t *testing.T) {
	world := newAccessWorld(t)
	handler := world.a.Handler()
	open := func(cookie, path string) int {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		r.Header.Set(clientHeader, "web")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	for role, want := range map[rbac.Role]map[string]bool{
		rbac.Developer: {"/api/v1/applications/appa/terminal": false},
		rbac.Admin: {"/api/v1/applications/appa/terminal": true, "/api/v1/applications/appb/terminal": false,
			"/api/v1/servers/srv/docker/containers/c/terminal": false},
	} {
		user, _ := testIdentity(t, world.a, "terminal-"+string(role))
		grantMember(t, world.a, user.ID, "pa", role)
		cookie, err := world.a.Identity.OpenSession(context.Background(), user.ID)
		if err != nil {
			t.Fatal(err)
		}
		for path, allowed := range want {
			// Without a terminal service an authorized request ends in 502.
			if got := open(cookie, path); (got != 403) != allowed {
				t.Errorf("%s %s: %d (allowed %v)", role, path, got, allowed)
			}
		}
	}
}

// TestOpenTerminalClosesWhenTheRoleIsRemoved: the open WebSocket ends as
// soon as the owner takes the caller's role away, without waiting for a ping.
func TestOpenTerminalClosesWhenTheRoleIsRemoved(t *testing.T) {
	fake := executor.NewFake().On("docker ps", "aaaaaaaaaaaa\ttest\trunning\tfalse\t\t\t\t", 0).On("docker exec", "hello-terminal", 0)
	svc := terminal.NewService(terminalConnector{fake})
	t.Cleanup(svc.Close)
	a := platformAPI(t)
	a.Terminal = svc
	owner, _ := testIdentity(t, a, "owner")
	grantRole(t, a, owner.ID, rbac.Owner)
	operator, _ := testIdentity(t, a, "operator")
	grantRole(t, a, operator.ID, rbac.Admin)
	ctx := context.Background()
	cookie, err := a.Identity.OpenSession(ctx, operator.ID)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := a.Identity.AuthenticateSession(ctx, cookie)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := svc.Issue(ctx, terminalOwner(principal), terminal.Request{Kind: terminal.KindContainer, ServerID: "s", ResourceID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/terminal?ticket="+ticket.Ticket,
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{srv.URL}, "Cookie": []string{sessionCookie + "=" + cookie}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, _, err := conn.Read(dialCtx); err != nil {
		t.Fatal("no ready frame", err)
	}
	if svc.OpenCount() != 1 {
		t.Fatal("terminal not open before the revocation")
	}
	if err := a.Access.SetInstanceRole(ctx, owner.ID, operator.ID, rbac.Viewer); err != nil {
		t.Fatal(err)
	}
	readCtx, cancelRead := context.WithTimeout(ctx, 2*time.Second)
	defer cancelRead()
	for {
		if _, _, err := conn.Read(readCtx); err != nil {
			if readCtx.Err() != nil {
				t.Fatal("terminal still open after the role was removed")
			}
			break
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for svc.OpenCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if svc.OpenCount() != 0 {
		t.Fatal("terminal slot not released after revocation")
	}
}

// Revoking write after issuance must prevent even a transient server shell.
func TestTerminalRevokedTicketNeverExecutes(t *testing.T) {
	fake := executor.NewFake().On("docker ps", "aaaaaaaaaaaa\ttest\trunning\tfalse\t\t\t\t", 0)
	svc := terminal.NewService(terminalConnector{fake})
	t.Cleanup(svc.Close)
	a := platformAPI(t)
	a.Terminal = svc
	owner, _ := testIdentity(t, a, "owner")
	grantRole(t, a, owner.ID, rbac.Owner)
	operator, _ := testIdentity(t, a, "operator")
	grantRole(t, a, operator.ID, rbac.Admin)
	ctx := context.Background()
	cookie, err := a.Identity.OpenSession(ctx, operator.ID)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := a.Identity.AuthenticateSession(ctx, cookie)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := svc.Issue(ctx, terminalOwner(principal), terminal.Request{Kind: terminal.KindContainer, ServerID: "s", ResourceID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	before := len(fake.Commands)
	if err := a.Access.SetInstanceRole(ctx, owner.ID, operator.ID, rbac.Viewer); err != nil {
		t.Fatal(err)
	}
	handler := a.Handler()
	for attempt := 0; attempt < 2; attempt++ {
		r := httptest.NewRequest("GET", "http://panel.test/api/v1/terminal?ticket="+ticket.Ticket, nil)
		r.Header.Set("Origin", "http://panel.test")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := http.StatusForbidden
		if attempt == 1 {
			want = http.StatusUnauthorized
		}
		if w.Code != want {
			t.Fatalf("attempt %d: status %d: %s", attempt, w.Code, w.Body.String())
		}
		if len(fake.Commands) != before || svc.OpenCount() != 0 {
			t.Fatal("denied ticket executed a command or reserved capacity")
		}
	}
}
