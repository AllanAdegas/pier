package httpapi

import "net/http"

// checkDestroy enforces the protection of an environment before one of its
// resources is removed: a protected environment requires its name in the
// "confirmation" query parameter, typed by the operator.
func (a *API) checkDestroy(r *http.Request, environmentID string) error {
	if a.Projects == nil {
		return nil
	}
	return a.Projects.CheckDestroy(r.Context(), environmentID, r.URL.Query().Get("confirmation"))
}
