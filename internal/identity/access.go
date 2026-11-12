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
