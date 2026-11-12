package httpapi

import (
	"context"
	"net/http"
	"time"

	"pier/internal/identity"
	"pier/internal/terminal"
)

// terminalRecheckInterval bounds how long an open terminal survives the end
// of its browser session. Role changes close it at once (Subscribe).
const terminalRecheckInterval = 5 * time.Second

// authorizeTerminal demands write on the project of the terminal's resource;
// a container of a server (or an unresolvable resource) needs the instance.
func (a *API) authorizeTerminal(ctx context.Context, p identity.Principal, grant terminal.Grant) error {
	if a.Access == nil {
		return accessDenied()
	}
	project := ""
	if kind := kindAliases[string(grant.Kind)]; kind != "" {
		resolved, err := a.resolveProject(ctx, kind, grant.ResourceID)
		if err != nil {
			return err
		}
		project = resolved
	}
	_, err := a.authorizeScope(ctx, p, project, identity.AbilityWrite, "")
	return err
}

// guardTerminal closes an open terminal as soon as its caller loses the
// right to it: a role change re-checks at once, and a periodic re-check
// catches an ended (logged out, revoked, expired) session. stop releases it.
func (a *API) guardTerminal(r *http.Request, p identity.Principal, grant terminal.Grant, closeTerminal func()) (stop func()) {
	if a.Access == nil {
		closeTerminal()
		return func() {}
	}
	changed, unsubscribe := a.Access.Subscribe(p.User.ID)
	done := make(chan struct{})
	go func() {
		defer unsubscribe()
		ticker := time.NewTicker(terminalRecheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-changed:
			case <-ticker.C:
			}
			if !a.terminalStillAllowed(r, p, grant) {
				closeTerminal()
				return
			}
		}
	}()
	return func() { close(done) }
}

func (a *API) terminalStillAllowed(r *http.Request, p identity.Principal, grant terminal.Grant) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), terminalPingTimeout)
	defer cancel()
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || a.Identity == nil {
		return false
	}
	current, err := a.Identity.AuthenticateSession(ctx, cookie.Value)
	if err != nil || current.User.ID != p.User.ID || current.SessionID != p.SessionID {
		return false
	}
	return a.authorizeTerminal(ctx, current, grant) == nil
}
