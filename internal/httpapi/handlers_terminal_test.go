package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pier/internal/identity"
	"pier/internal/platform/executor"
	"pier/internal/server"
	"pier/internal/terminal"
)

type terminalConnector struct{ ex *executor.Fake }

func (c terminalConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return c.ex, server.Server{ID: "s"}, nil
}

func TestTerminalOriginAndPermissions(t *testing.T) {
	a := New(Dependencies{})
	for _, origin := range []string{"", "https://evil.invalid", "null", "http://panel.invalid.evil", "http://panel.invalid/path", "http://panel.invalid?query=1", "http://panel.invalid#fragment"} {
		r := httptest.NewRequest("GET", "http://panel.invalid/api/v1/terminal", nil)
		r.Header.Set("Origin", origin)
		if a.verifyTerminalOrigin(r) == nil {
			t.Fatalf("accepted %q", origin)
		}
	}
	r := httptest.NewRequest("GET", "http://panel.invalid/api/v1/terminal", nil)
	r.Header.Set("Origin", "http://panel.invalid:80")
	if err := a.verifyTerminalOrigin(r); err != nil {
		t.Fatal(err)
	}
	if requireInteractive(identity.Principal{ViaToken: true, User: identity.User{ID: "u"}}) == nil {
		t.Fatal("token allowed")
	}
	if requireInteractive(identity.Principal{}) == nil {
		t.Fatal("anonymous allowed")
	}
}

func TestTerminalWebSocketMovesBytesAndClosesShell(t *testing.T) {
	fake := executor.NewFake().On("docker ps", "aaaaaaaaaaaa\ttest\trunning\tfalse\t\t\t\t", 0).On("docker exec", "hello-terminal", 0)
	svc := terminal.NewService(terminalConnector{fake})
	a := platformAPI(t)
	a.Terminal = svc
	user, err := a.Identity.CreateUser(context.Background(), "Terminal", "websocket@example.invalid", "a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := a.Identity.OpenSession(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := a.Identity.AuthenticateSession(context.Background(), cookie)
	if err != nil {
		t.Fatal(err)
	}
	owner := terminalOwner(principal)
	ticket, err := svc.Issue(context.Background(), owner, terminal.Request{Kind: terminal.KindContainer, ServerID: "s", ResourceID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/terminal?ticket="+ticket.Ticket, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{srv.URL}, "Cookie": []string{sessionCookie + "=" + cookie}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var control map[string]any
	if json.Unmarshal(data, &control) != nil || control["type"] != "ready" {
		t.Fatalf("ready %s", data)
	}
	_, data, err = conn.Read(ctx)
	if err != nil || !strings.Contains(string(data), "hello-terminal") {
		t.Fatalf("output %s %v", data, err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte{'x', executor.FakeEndOfInput}); err != nil {
		t.Fatal(err)
	}
	for {
		_, data, err = conn.Read(ctx)
		if err != nil {
			break
		}
		if strings.Contains(string(data), `"type":"exit"`) {
			break
		}
	}
	deadline := time.Now().Add(time.Second)
	for svc.OpenCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if svc.OpenCount() != 0 {
		t.Fatal("terminal slot leaked")
	}
}

func TestTerminalRoutesRejectAPITokensAndMissingCSRF(t *testing.T) {
	a := platformAPI(t)
	ctx := context.Background()
	user, err := a.Identity.CreateUser(ctx, "Terminal user", "terminal@example.invalid", "a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := a.Identity.CreateToken(ctx, user.ID, "terminal-test", []identity.Ability{identity.AbilityWrite})
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler()
	for _, path := range []string{"/api/v1/terminal?ticket=secret", "/api/v1/servers/s/docker/containers/c/terminal"} {
		method := "POST"
		if strings.Contains(path, "?") {
			method = "GET"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("token %s status %d", path, w.Code)
		}
	}
	session, err := a.Identity.OpenSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/servers/s/docker/containers/c/terminal", strings.NewReader(`{}`))
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("missing CSRF status %d", w.Code)
	}
}
