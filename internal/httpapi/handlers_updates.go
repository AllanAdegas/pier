package httpapi

import "net/http"

func (a *API) registerUpdateRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/updates", canRead, a.updateStatus)
}

// updateStatus answers from the Checker's cache; it is the only code path
// that may cause an outbound request, and at most once per interval.
func (a *API) updateStatus(w http.ResponseWriter, r *http.Request) error {
	return ok(w, a.Updates.Status(r.Context()))
}
