package identity

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"pier/internal/platform/apperr"
)

func TestSessionsAreListedAndRevokedByTheirOwner(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	laptop, _ := f.service.StartSession(ctx, f.user.ID, Client{IP: "203.0.113.7", UserAgent: "Firefox"})
	phone, _ := f.service.StartSession(ctx, f.user.ID, Client{IP: "198.51.100.2", UserAgent: strings.Repeat("é", 300)})
	current, err := f.service.AuthenticateSession(ctx, laptop)
	if err != nil || current.SessionID == "" {
		t.Fatalf("AuthenticateSession: %+v %v", current, err)
	}
	sessions, err := f.service.ListSessions(ctx, f.user.ID, current.SessionID)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("ListSessions: %+v %v", sessions, err)
	}
	marked := 0
	for _, session := range sessions {
		if session.Current {
			marked++
			if session.IP != "203.0.113.7" || session.UserAgent != "Firefox" {
				t.Errorf("the current session lost its client: %+v", session)
			}
		}
		if len(session.UserAgent) > maxUserAgentLength || !utf8.ValidString(session.UserAgent) {
			t.Errorf("the user agent must be clipped on a character boundary: %d bytes", len(session.UserAgent))
		}
	}
	if marked != 1 {
		t.Fatalf("exactly one session is the caller's, got %d", marked)
	}

	other, _ := f.service.CreateUser(ctx, "Eve", "eve@example.com", strongPassword)
	if err := f.service.RevokeSession(ctx, other.ID, current.SessionID); kindOf(err) != apperr.KindNotFound {
		t.Fatalf("a user must not revoke someone else's session, got %v", err)
	}
	if revoked, err := f.service.RevokeOtherSessions(ctx, f.user.ID, current.SessionID); err != nil || revoked != 1 {
		t.Fatalf("RevokeOtherSessions: %d %v", revoked, err)
	}
	if _, err := f.service.AuthenticateSession(ctx, phone); kindOf(err) != apperr.KindUnauthorized {
		t.Fatal("the other session must be closed")
	}
	if err := f.service.RevokeSession(ctx, f.user.ID, current.SessionID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := f.service.AuthenticateSession(ctx, laptop); kindOf(err) != apperr.KindUnauthorized {
		t.Fatal("a revoked session must stop working")
	}
}

func TestLastSeenMovesAtMostOncePerInterval(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	token, _ := f.service.StartSession(ctx, f.user.ID, Client{})
	opened := f.clock
	f.advance(10 * time.Second)
	principal, _ := f.service.AuthenticateSession(ctx, token)
	sessions, _ := f.service.ListSessions(ctx, f.user.ID, principal.SessionID)
	if !sessions[0].LastSeenAt.Equal(opened) {
		t.Fatalf("last seen must not move within the interval: %v", sessions[0].LastSeenAt)
	}
	f.advance(sessionTouchInterval)
	_, _ = f.service.AuthenticateSession(ctx, token)
	sessions, _ = f.service.ListSessions(ctx, f.user.ID, principal.SessionID)
	if !sessions[0].LastSeenAt.Equal(f.clock) {
		t.Fatalf("last seen must follow use: %v, want %v", sessions[0].LastSeenAt, f.clock)
	}
}
