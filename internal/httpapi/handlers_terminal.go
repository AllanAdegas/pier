package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"pier/internal/audit"
	"pier/internal/identity"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/sqlite"
	"pier/internal/terminal"
)

const (
	terminalRoute        = "GET /api/v1/terminal"
	terminalPingInterval = 30 * time.Second
	terminalPingTimeout  = 10 * time.Second
)

// registerTerminalRoutes exposes interactive shells in containers.
//
// A terminal is a root shell on the server in practice, so every route is
// session-only (an API token, even with "write", never opens one: a leaked
// token must not become an interactive shell) and demands "write". The POST
// routes validate the resource and hand out a single-use ticket bound to the
// browser session; the WebSocket is opened with that ticket after a strict
// Origin check.
func (a *API) registerTerminalRoutes(mux *http.ServeMux) {
	a.route(mux, "POST /api/v1/applications/{id}/terminal", sessionOnly, a.terminalTicketFor(terminal.KindApplication))
	a.route(mux, "POST /api/v1/databases/{id}/terminal", sessionOnly, a.terminalTicketFor(terminal.KindDatabase))
	a.route(mux, "POST /api/v1/stacks/{id}/terminal", sessionOnly, a.terminalTicketFor(terminal.KindStack))
	a.route(mux, "POST /api/v1/servers/{id}/docker/containers/{container}/terminal", sessionOnly,
		a.terminalTicketFor(terminal.KindContainer))
	a.route(mux, terminalRoute, sessionOnly, a.terminalConnect)
}

type terminalTicketRequest struct {
	Service   string `json:"service"`
	Container string `json:"container"`
	Shell     string `json:"shell"`
}

func (a *API) terminalTicketFor(kind terminal.Kind) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		principal := principalFrom(r.Context())
		if err := requireInteractive(principal); err != nil {
			return err
		}
		if a.Terminal == nil {
			return apperr.Unavailable("O terminal não está disponível.")
		}
		body, err := decode[terminalTicketRequest](w, r)
		if err != nil {
			return err
		}
		request := terminal.Request{Kind: kind, ResourceID: r.PathValue("id"), Service: body.Service,
			Container: body.Container, Shell: body.Shell}
		if request.ServerID, err = a.terminalServer(r.Context(), kind, request.ResourceID); err != nil {
			return err
		}
		if kind == terminal.KindApplication {
			_, request.ServerID, err = a.Applications.ServerFor(r.Context(), request.ResourceID, r.URL.Query().Get("server_id"))
			if err != nil {
				return err
			}
		}
		if kind == terminal.KindContainer {
			request.ResourceID = r.PathValue("container")
		}
		ticket, err := a.Terminal.Issue(r.Context(), terminalOwner(principal), request)
		if err != nil {
			return err
		}
		return created(w, ticket)
	}
}

// terminalServer finds the server of the resource in the route; the body
// never chooses a server.
func (a *API) terminalServer(ctx context.Context, kind terminal.Kind, id string) (string, error) {
	switch kind {
	case terminal.KindApplication:
		app, err := a.Applications.Get(ctx, id)
		return app.ServerID, err
	case terminal.KindDatabase:
		database, err := a.Databases.Get(ctx, id)
		return database.ServerID, err
	case terminal.KindStack:
		stack, err := a.Stacks.Get(ctx, id)
		return stack.ServerID, err
	default:
		return id, nil
	}
}

// requireInteractive repeats what the route declares, so a change of the
// route table cannot quietly open terminals to tokens or read-only callers.
func requireInteractive(principal identity.Principal) error {
	if principal.ViaToken || principal.User.ID == "" {
		return apperr.Forbidden("Esta operação exige login no painel.")
	}
	if !principal.Can(identity.AbilityWrite) {
		return apperr.Forbidden("O token não tem a permissão %q.", string(identity.AbilityWrite))
	}
	return nil
}

func terminalOwner(principal identity.Principal) terminal.Owner {
	return terminal.Owner{UserID: principal.User.ID, SessionID: principal.SessionID}
}

// terminalConnect upgrades to a WebSocket and serves the terminal.
func (a *API) terminalConnect(w http.ResponseWriter, r *http.Request) error {
	principal := principalFrom(r.Context())
	if err := requireInteractive(principal); err != nil {
		return err
	}
	if a.Terminal == nil {
		return apperr.Unavailable("O terminal não está disponível.")
	}
	if err := a.verifyTerminalOrigin(r); err != nil {
		a.auditTerminal(r, principal, http.StatusForbidden, "", terminalEvent{Event: "terminal.denied", Reason: "origin"})
		return err
	}
	session, err := a.Terminal.Open(r.Context(), terminalOwner(principal), r.URL.Query().Get("ticket"), requestedSize(r))
	if err != nil {
		return err
	}
	defer session.Close()
	if err := a.authorizeTerminal(r.Context(), principal, session.Grant()); err != nil {
		return err
	}
	// The origin was verified above, scheme and host, against the public URL
	// and the request host: stricter than the library's host-only check.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return nil
	}
	conn.SetReadLimit(terminal.MaxFrameBytes)
	grant := session.Grant()
	defer a.guardTerminal(r, principal, grant, func() { _ = conn.CloseNow() })()
	a.auditTerminal(r, principal, http.StatusSwitchingProtocols, grant.ResourceID, openEvent(grant))
	stopPings := keepAlive(conn, func(ctx context.Context) bool {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || a.Identity == nil {
			return false
		}
		current, err := a.Identity.AuthenticateSession(ctx, cookie.Value)
		return err == nil && current.User.ID == principal.User.ID && current.Can(identity.AbilityWrite) &&
			a.authorizeTerminal(ctx, current, grant) == nil
	})
	report := session.Serve(websocketPeer{conn: conn})
	stopPings()
	session.Close()
	_ = conn.Close(websocket.StatusNormalClosure, report.Reason)
	a.auditTerminal(r, principal, http.StatusSwitchingProtocols, grant.ResourceID, closeEvent(report))
	return nil
}

// verifyTerminalOrigin demands an Origin equal to the panel's public URL or
// to the address this request reached. Browsers always send Origin on a
// WebSocket handshake, so its absence means the caller is not the panel.
func (a *API) verifyTerminalOrigin(r *http.Request) error {
	rawOrigin := r.Header.Get("Origin")
	parsed, parseErr := url.Parse(rawOrigin)
	if parseErr != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return apperr.Forbidden("Origem da conexão não permitida.")
	}
	origin := normalizeOrigin(rawOrigin)
	if origin == "" {
		return apperr.Forbidden("Origem da conexão não permitida.")
	}
	scheme := "http"
	if a.proxies.isHTTPS(r) {
		scheme = "https"
	}
	allowed := []string{normalizeOrigin(scheme + "://" + r.Host)}
	if a.Settings != nil {
		if current, err := a.Settings.Get(r.Context()); err == nil && current.PublicURL != "" {
			allowed = append(allowed, normalizeOrigin(current.PublicURL))
		}
	}
	for _, candidate := range allowed {
		if candidate != "" && candidate == origin {
			return nil
		}
	}
	return apperr.Forbidden("Origem da conexão não permitida.")
}

// normalizeOrigin reduces a URL to scheme://host[:port], lowercased and
// without the default port; anything unusable becomes "".
func normalizeOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return parsed.Scheme + "://" + host
}

func requestedSize(r *http.Request) executor.WindowSize {
	cols, _ := strconv.ParseUint(r.URL.Query().Get("cols"), 10, 16)
	rows, _ := strconv.ParseUint(r.URL.Query().Get("rows"), 10, 16)
	return executor.WindowSize{Cols: uint16(cols), Rows: uint16(rows)}
}

// keepAlive pings the browser so a vanished one ends the terminal.
func keepAlive(conn *websocket.Conn, authorized func(context.Context) bool) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(terminalPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), terminalPingTimeout)
				if !authorized(ctx) {
					cancel()
					_ = conn.CloseNow()
					return
				}
				err := conn.Ping(ctx)
				cancel()
				if err != nil {
					_ = conn.CloseNow()
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

// websocketPeer adapts the WebSocket to the terminal protocol.
type websocketPeer struct{ conn *websocket.Conn }

func (p websocketPeer) Receive(ctx context.Context) (terminal.Frame, error) {
	kind, data, err := p.conn.Read(ctx)
	if err != nil {
		return terminal.Frame{}, err
	}
	return terminal.Frame{Binary: kind == websocket.MessageBinary, Data: data}, nil
}

func (p websocketPeer) Send(ctx context.Context, frame terminal.Frame) error {
	kind := websocket.MessageText
	if frame.Binary {
		kind = websocket.MessageBinary
	}
	return p.conn.Write(ctx, kind, frame.Data)
}

// terminalEvent is the audit summary of a terminal. It names the container
// and the outcome; what was typed or printed is never recorded.
type terminalEvent struct {
	Event           string `json:"event"`
	Kind            string `json:"kind,omitempty"`
	ServerID        string `json:"server_id,omitempty"`
	Container       string `json:"container,omitempty"`
	ContainerID     string `json:"container_id,omitempty"`
	Service         string `json:"service,omitempty"`
	Shell           string `json:"shell,omitempty"`
	Reason          string `json:"reason,omitempty"`
	DurationSeconds *int64 `json:"duration_seconds,omitempty"`
	ExitCode        *int   `json:"exit_code,omitempty"`
}

func openEvent(grant terminal.Grant) terminalEvent {
	return terminalEvent{Event: "terminal.open", Kind: string(grant.Kind), ServerID: grant.ServerID,
		Container: grant.Container.Name, ContainerID: grant.Container.ID, Service: grant.Container.Service, Shell: grant.Shell}
}

func closeEvent(report terminal.Report) terminalEvent {
	event := openEvent(report.Grant)
	event.Event = "terminal.close"
	seconds := int64(report.Duration.Round(time.Second) / time.Second)
	event.DurationSeconds, event.Reason, event.ExitCode = &seconds, report.Reason, report.ExitCode
	return event
}

// auditTerminal records an entry directly: the upgrade is a GET, which the
// audit middleware skips, and one request yields two entries (open, close).
func (a *API) auditTerminal(r *http.Request, principal identity.Principal, status int, resourceID string, event terminalEvent) {
	if a.Audit == nil {
		return
	}
	summary, _ := json.Marshal(event)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	err := a.Audit.Record(ctx, audit.Entry{OccurredAt: sqlite.FormatTime(time.Now()), UserID: principal.User.ID,
		UserName: principal.User.Name, IP: a.proxies.clientAddress(r), Method: r.Method, Route: terminalRoute,
		ResourceID: resourceID, Status: status, Summary: string(summary)})
	if err != nil && a.Logger != nil {
		a.Logger.Error("audit record failed", "error", err)
	}
}
