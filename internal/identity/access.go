package identity

import (
	"context"

	"pier/internal/platform/apperr"
)

// AccessAuthorizer intersects current membership with the credential's ability.
// Implementations must resolve membership on each request, never cache it in sessions.
type AccessAuthorizer interface {
	Check(context.Context, string, string, string) error
}

func (p Principal) AuthorizeAccess(ctx context.Context, authorizer AccessAuthorizer, projectID string, ability Ability) error {
	if !p.Can(ability) {
		return apperr.Forbidden("Acesso não autorizado.")
	}
	return authorizer.Check(ctx, p.User.ID, projectID, string(ability))
}

// Within returns the principal reduced to the abilities both its credential
// and its role grant. Handlers that test Can (secrets, terminals) then see
// what the caller may really do, not only what the token carries.
func (p Principal) Within(roleAllows func(Ability) bool) Principal {
	granted := make([]Ability, 0, len(AllAbilities))
	for _, ability := range AllAbilities {
		if p.Can(ability) && roleAllows(ability) {
			granted = append(granted, ability)
		}
	}
	p.Abilities = granted
	return p
}
