package identity

import (
	"context"
	"time"
)

// Session is a signed-in browser, as its owner sees it in the list.
type Session struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"user_agent"`
	Current    bool      `json:"current"`
}

// Client describes who opens a session, for the session list.
type Client struct {
	IP        string
	UserAgent string
}

// TwoFactorStatus is what the account page shows about the second factor.
type TwoFactorStatus struct {
	Enabled           bool `json:"enabled"`
	RecoveryCodesLeft int  `json:"recovery_codes_left"`
}

// Enrollment is a TOTP secret waiting for its first code. The secret is
// shown only while enrolling.
type Enrollment struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
	QRSVG  string `json:"qr_svg"`
}

// LoginOutcome is either an open session or, for accounts with a second
// factor, the challenge the code must be sent with.
type LoginOutcome struct {
	SessionToken string
	User         User
	Challenge    string
	ChallengeTTL time.Duration
}

// ResetMailer delivers the password reset link. It exists only when the
// instance has an e-mail channel to send it through.
type ResetMailer interface {
	// Available reports whether a reset e-mail can be sent right now.
	Available(ctx context.Context) bool
	SendPasswordReset(ctx context.Context, to, link string) error
}
