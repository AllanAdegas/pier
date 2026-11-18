package terminal

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/server"
)

type fakeConnector struct{ ex *executor.Fake }

func (c fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return c.ex, server.Server{ID: "srv"}, nil
}

func row(id, name, state, managed, kind, resource, service, process string) string {
	return strings.Join([]string{id, name, state, managed, kind, resource, service, process}, "\t")
}

// containersFixture lists containers of two applications, a stack with two
// services, an unmanaged look-alike and a stopped database.
func containersFixture() *executor.Fake {
	rows := []string{
		row("aaaaaaaaaaaa1111", "app-one-worker", "running", "true", "application", "app1", "", "worker"),
		row("aaaaaaaaaaaa2222", "app-one-web", "running", "true", "application", "app1", "", "web"),
		row("bbbbbbbbbbbb1111", "app-two-web", "running", "true", "application", "app2", "", "web"),
		row("cccccccccccc1111", "fake-app-one", "running", "false", "application", "app1", "", "web"),
		row("dddddddddddd1111", "stack-api-1", "running", "true", "stack", "st1", "api", ""),
		row("dddddddddddd2222", "stack-db-1", "running", "true", "stack", "st1", "db", ""),
		row("eeeeeeeeeeee1111", "db-one", "exited", "true", "database", "db1", "", ""),
	}
	return executor.NewFake().On("docker ps", strings.Join(rows, "\n"), 0)
}

func newTestService(ex *executor.Fake, limits Limits) *Service {
	return NewServiceWithLimits(fakeConnector{ex}, limits)
}

func testLimits() Limits {
	limits := DefaultLimits()
	limits.PerUser, limits.PerServer = 2, 3
	return limits
}

var alice = Owner{UserID: "alice", SessionID: "s1"}

func errorKind(err error) apperr.Kind {
	if e, ok := apperr.As(err); ok {
		return e.Kind
	}
	return ""
}

func TestIssueTargetsOnlyContainersOfTheResource(t *testing.T) {
	service := newTestService(containersFixture(), testLimits())
	ctx := context.Background()
	ticket, err := service.Issue(ctx, alice, Request{Kind: KindApplication, ServerID: "srv", ResourceID: "app1"})
	if err != nil || ticket.Container.ID != "aaaaaaaaaaaa2222" || ticket.Shell != ShellAuto {
		t.Fatalf("default container %+v err=%v", ticket, err)
	}
	for _, foreign := range []string{"app-two-web", "bbbbbbbbbbbb1111", "fake-app-one", "stack-api-1"} {
		_, err := service.Issue(ctx, alice, Request{Kind: KindApplication, ServerID: "srv", ResourceID: "app1", Container: foreign})
		if errorKind(err) != apperr.KindNotFound {
			t.Fatalf("container %s of another owner reachable: %v", foreign, err)
		}
	}
	ticket, err = service.Issue(ctx, alice, Request{Kind: KindApplication, ServerID: "srv", ResourceID: "app1", Container: "aaaaaaaaaaaa"})
	if err != nil || ticket.Container.Name != "app-one-worker" {
		t.Fatalf("id prefix %+v err=%v", ticket, err)
	}
}

func TestIssueStackNeedsAServiceAndDatabaseMustRun(t *testing.T) {
	service := newTestService(containersFixture(), testLimits())
	ctx := context.Background()
	if _, err := service.Issue(ctx, alice, Request{Kind: KindStack, ServerID: "srv", ResourceID: "st1"}); errorKind(err) != apperr.KindInvalid {
		t.Fatalf("stack without service: %v", err)
	}
	ticket, err := service.Issue(ctx, alice, Request{Kind: KindStack, ServerID: "srv", ResourceID: "st1", Service: "db"})
	if err != nil || ticket.Container.ID != "dddddddddddd2222" || ticket.Container.Service != "db" {
		t.Fatalf("service db %+v err=%v", ticket, err)
	}
	if _, err := service.Issue(ctx, alice, Request{Kind: KindStack, ServerID: "srv", ResourceID: "st1", Service: "cache"}); errorKind(err) != apperr.KindNotFound {
		t.Fatalf("unknown service: %v", err)
	}
	if _, err := service.Issue(ctx, alice, Request{Kind: KindDatabase, ServerID: "srv", ResourceID: "db1"}); errorKind(err) != apperr.KindConflict {
		t.Fatalf("stopped database: %v", err)
	}
}

func TestIssueRejectsHostileInputBeforeDocker(t *testing.T) {
	ex := containersFixture()
	service := newTestService(ex, testLimits())
	for _, request := range []Request{
		{Kind: KindApplication, ServerID: "srv", ResourceID: "app1", Shell: "bash; rm -rf /"},
		{Kind: KindApplication, ServerID: "srv", ResourceID: "app1", Container: "$(id)"},
		{Kind: KindStack, ServerID: "srv", ResourceID: "st1", Service: "--privileged"},
		{Kind: KindContainer, ServerID: "srv", ResourceID: "-it"},
		{Kind: "host", ServerID: "srv", ResourceID: "x"},
	} {
		if _, err := service.Issue(context.Background(), alice, request); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
	if len(ex.Commands) != 0 {
		t.Fatalf("hostile input reached the server: %v", ex.Commands)
	}
}

func TestServerContainerRouteReachesUnmanagedContainers(t *testing.T) {
	ex := containersFixture()
	service := newTestService(ex, testLimits())
	ticket, err := service.Issue(context.Background(), alice, Request{Kind: KindContainer, ServerID: "srv", ResourceID: "fake-app-one", Shell: ShellBash})
	if err != nil || ticket.Container.ID != "cccccccccccc1111" {
		t.Fatalf("%+v err=%v", ticket, err)
	}
	session, err := service.Open(context.Background(), alice, ticket.Ticket, executor.WindowSize{Cols: 999, Rows: 999}, allowTerminal)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !ex.Ran("docker exec --interactive --tty --env TERM=xterm-256color cccccccccccc1111 /bin/sh -c 'exec bash'") {
		t.Fatalf("commands %v", ex.Commands)
	}
	if size := ex.Terminals()[0].Resizes()[0]; size.Cols != maxCols || size.Rows != maxRows {
		t.Fatalf("size not clamped: %+v", size)
	}
}

func TestTicketsAreSingleUseBoundToTheSessionAndExpire(t *testing.T) {
	service := newTestService(containersFixture(), testLimits())
	ctx := context.Background()
	request := Request{Kind: KindApplication, ServerID: "srv", ResourceID: "app1"}
	issue := func() string {
		ticket, err := service.Issue(ctx, alice, request)
		if err != nil {
			t.Fatal(err)
		}
		return ticket.Ticket
	}
	token := issue()
	if _, err := service.Open(ctx, Owner{UserID: "alice", SessionID: "other"}, token, executor.WindowSize{}, allowTerminal); errorKind(err) != apperr.KindUnauthorized {
		t.Fatalf("other session redeemed: %v", err)
	}
	if _, err := service.Open(ctx, alice, token, executor.WindowSize{}, allowTerminal); errorKind(err) != apperr.KindUnauthorized {
		t.Fatalf("leaked ticket still valid: %v", err)
	}
	token = issue()
	session, err := service.Open(ctx, alice, token, executor.WindowSize{}, allowTerminal)
	if err != nil {
		t.Fatal(err)
	}
	session.Close()
	if _, err := service.Open(ctx, alice, token, executor.WindowSize{}, allowTerminal); errorKind(err) != apperr.KindUnauthorized {
		t.Fatalf("ticket reused: %v", err)
	}
	token = issue()
	service.tickets.now = func() time.Time { return time.Now().Add(time.Minute) }
	if _, err := service.Open(ctx, alice, token, executor.WindowSize{}, allowTerminal); errorKind(err) != apperr.KindUnauthorized {
		t.Fatalf("expired ticket: %v", err)
	}
}

func TestCapsPerUserAndPerServer(t *testing.T) {
	service := newTestService(containersFixture(), testLimits())
	ctx := context.Background()
	open := func(owner Owner) (*Session, error) {
		ticket, err := service.Issue(ctx, owner, Request{Kind: KindApplication, ServerID: "srv", ResourceID: "app1"})
		if err != nil {
			return nil, err
		}
		return service.Open(ctx, owner, ticket.Ticket, executor.WindowSize{}, allowTerminal)
	}
	var sessions []*Session
	for range 2 {
		session, err := open(alice)
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, session)
	}
	if _, err := open(alice); errorKind(err) != apperr.KindConflict {
		t.Fatalf("per-user cap: %v", err)
	}
	bob := Owner{UserID: "bob", SessionID: "s2"}
	session, err := open(bob)
	if err != nil {
		t.Fatal(err)
	}
	sessions = append(sessions, session)
	if _, err := open(Owner{UserID: "carol", SessionID: "s3"}); errorKind(err) != apperr.KindConflict {
		t.Fatalf("per-server cap: %v", err)
	}
	sessions[0].Close()
	sessions[0].Close()
	if service.OpenCount() != 2 {
		t.Fatalf("open=%d", service.OpenCount())
	}
	if session, err := open(alice); err != nil {
		t.Fatalf("freed slot not reusable: %v", err)
	} else {
		session.Close()
	}
	for _, session := range sessions[1:] {
		session.Close()
	}
	if service.OpenCount() != 0 {
		t.Fatalf("leaked slots: %d", service.OpenCount())
	}
}

// fakePeer is the browser end of a terminal in tests.
type fakePeer struct {
	incoming chan Frame
	mu       sync.Mutex
	sent     []Frame
	output   strings.Builder
	gone     chan struct{}
}

func newFakePeer() *fakePeer {
	return &fakePeer{incoming: make(chan Frame, 16), gone: make(chan struct{})}
}

func (p *fakePeer) Receive(ctx context.Context) (Frame, error) {
	select {
	case frame := <-p.incoming:
		return frame, nil
	case <-p.gone:
		return Frame{}, errors.New("peer closed")
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}
}

func (p *fakePeer) Send(_ context.Context, frame Frame) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, frame)
	if frame.Binary {
		p.output.Write(frame.Data)
	}
	return nil
}

func (p *fakePeer) controls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var controls []string
	for _, frame := range p.sent {
		if !frame.Binary {
			controls = append(controls, string(frame.Data))
		}
	}
	return controls
}

func (p *fakePeer) printed() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.output.String()
}

func openSession(t *testing.T, ex *executor.Fake, limits Limits) (*Service, *Session) {
	t.Helper()
	service := newTestService(ex, limits)
	ticket, err := service.Issue(context.Background(), alice, Request{Kind: KindApplication, ServerID: "srv", ResourceID: "app1"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Open(context.Background(), alice, ticket.Ticket, executor.WindowSize{Cols: 80, Rows: 24}, allowTerminal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return service, session
}

func serveInBackground(session *Session, peer Peer) <-chan Report {
	done := make(chan Report, 1)
	go func() { done <- session.Serve(peer) }()
	return done
}

func waitReport(t *testing.T, done <-chan Report) Report {
	t.Helper()
	select {
	case report := <-done:
		return report
	case <-time.After(5 * time.Second):
		t.Fatal("terminal did not end")
	}
	return Report{}
}

func TestServeForwardsKeystrokesResizeAndExitCode(t *testing.T) {
	ex := containersFixture().On("docker exec", "welcome", 3)
	_, session := openSession(t, ex, testLimits())
	peer := newFakePeer()
	done := serveInBackground(session, peer)
	peer.incoming <- Frame{Binary: true, Data: []byte("ls -la\r")}
	peer.incoming <- Frame{Data: []byte(`{"type":"resize","cols":132,"rows":43}`)}
	peer.incoming <- Frame{Data: []byte(`{"type":"bogus"}`)}
	deadline := time.Now().Add(5 * time.Second)
	for len(ex.Terminals()[0].Resizes()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	peer.incoming <- Frame{Binary: true, Data: []byte{executor.FakeEndOfInput}}
	report := waitReport(t, done)
	if report.Reason != ReasonExit || report.ExitCode == nil || *report.ExitCode != 3 {
		t.Fatalf("report %+v", report)
	}
	if got := ex.Terminals()[0].Resizes(); got[len(got)-1] != (executor.WindowSize{Cols: 132, Rows: 43}) {
		t.Fatalf("resizes %+v", got)
	}
	if !strings.Contains(peer.printed(), "welcome") || !strings.Contains(peer.printed(), "ls -la") {
		t.Fatalf("output %q", peer.printed())
	}
	controls := peer.controls()
	if len(controls) != 2 || !strings.Contains(controls[0], `"type":"ready"`) || controls[1] != `{"type":"exit","code":3}` {
		t.Fatalf("controls %v", controls)
	}
}

func TestServeClosesIdleTerminalsAndFreesTheSlot(t *testing.T) {
	limits := testLimits()
	limits.IdleTimeout = 50 * time.Millisecond
	ex := containersFixture()
	service, session := openSession(t, ex, limits)
	peer := newFakePeer()
	report := waitReport(t, serveInBackground(session, peer))
	if report.Reason != ReasonIdle || report.ExitCode != nil {
		t.Fatalf("report %+v", report)
	}
	if controls := peer.controls(); controls[len(controls)-1] != `{"type":"closed","reason":"idle"}` {
		t.Fatalf("controls %v", controls)
	}
	if !ex.Terminals()[0].Closed() {
		t.Fatal("idle shell left running")
	}
	session.Close()
	if service.OpenCount() != 0 {
		t.Fatal("slot not freed")
	}
}

func TestServeEnforcesMaxDurationDespiteActivity(t *testing.T) {
	limits := testLimits()
	limits.MaxDuration = 100 * time.Millisecond
	_, session := openSession(t, containersFixture(), limits)
	peer := newFakePeer()
	done := serveInBackground(session, peer)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case peer.incoming <- Frame{Binary: true, Data: []byte("x")}:
			case <-stop:
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	if report := waitReport(t, done); report.Reason != ReasonMaxDuration {
		t.Fatalf("report %+v", report)
	}
}

func TestServeEndsWhenTheBrowserLeaves(t *testing.T) {
	ex := containersFixture()
	_, session := openSession(t, ex, testLimits())
	peer := newFakePeer()
	done := serveInBackground(session, peer)
	close(peer.gone)
	if report := waitReport(t, done); report.Reason != ReasonClient {
		t.Fatalf("report %+v", report)
	}
	if !ex.Terminals()[0].Closed() {
		t.Fatal("shell outlived the browser")
	}
}

func TestShutdownClosesAllSessionsAndRefusesNewTickets(t *testing.T) {
	s := newTestService(containersFixture(), testLimits())
	ticket, err := s.Issue(context.Background(), alice, Request{Kind: KindApplication, ServerID: "srv", ResourceID: "app1"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Open(context.Background(), alice, ticket.Ticket, executor.WindowSize{}, allowTerminal)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	s.Close()
	if s.OpenCount() != 0 {
		t.Fatal("shutdown leaked session")
	}
	if _, err := s.Issue(context.Background(), alice, Request{Kind: KindApplication, ServerID: "srv", ResourceID: "app1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("issue after shutdown %v", err)
	}
	s.Close()
}

func TestOpenAuthorizedFailsClosedBeforeConnecting(t *testing.T) {
	service := NewService(nil)
	for _, check := range []func(context.Context, Grant) error{nil, func(context.Context, Grant) error { return apperr.Forbidden("Acesso não autorizado.") }} {
		token, err := service.tickets.issue(alice, Grant{ServerID: "srv"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Open(context.Background(), alice, token, executor.WindowSize{}, check); errorKind(err) != apperr.KindForbidden {
			t.Fatalf("expected forbidden, got %v", err)
		}
		if service.OpenCount() != 0 {
			t.Fatal("denial reserved capacity")
		}
		if _, err := service.Open(context.Background(), alice, token, executor.WindowSize{}, check); errorKind(err) != apperr.KindUnauthorized {
			t.Fatalf("denied ticket was reusable: %v", err)
		}
	}
}

func allowTerminal(context.Context, Grant) error { return nil }
