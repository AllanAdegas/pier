// Package identity is the bounded context for users, sessions and API tokens.
package identity

import (
	"slices"
	"time"
)

// Ability is a permission carried by an API token.
type Ability string

const (
	AbilityRead   Ability = "read"
	AbilityWrite  Ability = "write"
	AbilityDeploy Ability = "deploy"
)

// AllAbilities lists every ability a token can carry.
var AllAbilities = []Ability{AbilityRead, AbilityWrite, AbilityDeploy}

// User is a person allowed to operate the panel.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// APIToken is a long-lived credential for automation. The secret itself is
// only shown once, at creation.
type APIToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Abilities  []Ability  `json:"abilities"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Principal is the authenticated caller of a request.
type Principal struct {
	TokenID   string `json:"-"`
	TokenName string `json:"-"`
	// SessionID identifies the browser session (empty for API tokens).
	SessionID string `json:"-"`
	User      User
	Abilities []Ability
	// ViaToken is true for API-token calls, false for browser sessions.
	ViaToken bool
}

// Can reports whether the caller holds the ability. Write implies deploy and
// read; browser sessions hold every ability.
func (p Principal) Can(ability Ability) bool {
	if slices.Contains(p.Abilities, ability) {
		return true
	}
	if slices.Contains(p.Abilities, AbilityWrite) {
		return true
	}
	return false
}
