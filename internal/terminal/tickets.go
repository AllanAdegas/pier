package terminal

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"sync"
	"time"

	"pier/internal/platform/apperr"
)

// maxPendingTickets bounds the memory unredeemed tickets can take.
const maxPendingTickets = 1024

// tickets are single-use, short-lived and bound to the browser session that
// asked for them. The WebSocket upgrade is a GET a browser sends without the
// X-Pier-Client header, so redeeming a ticket obtained through an
// authenticated POST is what protects it from cross-site requests (on top of
// the Origin check).
type tickets struct {
	mu      sync.Mutex
	pending map[string]pendingTicket
	ttl     time.Duration
	now     func() time.Time
}

type pendingTicket struct {
	owner   Owner
	grant   Grant
	expires time.Time
}

func newTickets(ttl time.Duration) *tickets {
	return &tickets{pending: map[string]pendingTicket{}, ttl: ttl, now: time.Now}
}

func (t *tickets) issue(owner Owner, grant Grant) (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.purgeExpired()
	if len(t.pending) >= maxPendingTickets {
		return "", apperr.Conflict("Muitos terminais sendo abertos ao mesmo tempo. Tente de novo em alguns segundos.")
	}
	t.pending[token] = pendingTicket{owner: owner, grant: grant, expires: t.now().Add(t.ttl)}
	return token, nil
}

// redeem consumes the ticket. A ticket presented by anyone but its owner is
// burned too: it has leaked.
func (t *tickets) redeem(owner Owner, token string) (Grant, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ticket, found := t.pending[token]
	delete(t.pending, token)
	if !found || !t.now().Before(ticket.expires) || !sameOwner(ticket.owner, owner) {
		return Grant{}, apperr.Unauthorized("Terminal expirado ou inválido. Abra o terminal de novo.")
	}
	return ticket.grant, nil
}

func sameOwner(a, b Owner) bool {
	return a.UserID != "" && subtle.ConstantTimeCompare([]byte(a.UserID), []byte(b.UserID)) == 1 &&
		subtle.ConstantTimeCompare([]byte(a.SessionID), []byte(b.SessionID)) == 1
}

func (t *tickets) purgeExpired() {
	now := t.now()
	for token, ticket := range t.pending {
		if !now.Before(ticket.expires) {
			delete(t.pending, token)
		}
	}
}
