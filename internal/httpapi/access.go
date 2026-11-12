package httpapi

import (
	"context"
	"net/http"

	"pier/internal/identity"
)

// ProjectScope resolves trusted resource ownership, including parent resources
// and all IDs in mutation bodies. Empty scope means instance-owner only.
// Never derive scope solely from a query parameter supplied by the caller.
type ProjectScope func(context.Context, *http.Request) (string, error)

// WithProjectAccess is the integration hook for authenticate. Call only after
// resolvePrincipal and existing CSRF/token checks. Unknown routes must pass an
// empty scope (instance-owner only), including global lists and raw engine APIs.
// MCP invokes these same HTTP routes and receives the same checks.
func WithProjectAccess(authorizer identity.AccessAuthorizer, scope ProjectScope, ability identity.Ability, principal identity.Principal, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authorizer == nil || scope == nil {
			writeJSON(w, http.StatusForbidden, errorBody{Error: errorDetail{Code: "forbidden", Message: "Acesso não autorizado."}})
			return
		}
		project, err := scope(r.Context(), r)
		if err == nil {
			err = principal.AuthorizeAccess(r.Context(), authorizer, project, ability)
		}
		if err != nil {
			writeJSON(w, http.StatusForbidden, errorBody{Error: errorDetail{Code: "forbidden", Message: "Acesso não autorizado."}})
			return
		}
		next.ServeHTTP(w, r)
	})
}
