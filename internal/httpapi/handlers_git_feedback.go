package httpapi

import (
	"net/http"
	"strconv"

	"pier/internal/gitprovider"
	"pier/internal/platform/apperr"
)

// registerGitFeedbackRoutes exposes the reports a resource could not deliver
// to its Git provider: read-only, plus "resend" with the deploy ability
// (sending a status is part of deploying, not of changing the resource).
func (a *API) registerGitFeedbackRoutes(mux *http.ServeMux) {
	if a.GitFeedback == nil {
		return
	}
	for _, kind := range []string{gitprovider.KindApplication, gitprovider.KindStack} {
		base := "/api/v1/" + kind + "s/{id}/git-provider/reports"
		a.route(mux, "GET "+base, canRead, a.listGitFeedbackReports(kind))
		a.route(mux, "POST "+base+"/{report}/resend", canDeploy, a.resendGitFeedbackReport(kind))
	}
}

func (a *API) listGitFeedbackReports(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		reports, err := a.GitFeedback.FailedReports(r.Context(), kind, r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, reports)
	}
}

func (a *API) resendGitFeedbackReport(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		report, err := strconv.ParseInt(r.PathValue("report"), 10, 64)
		if err != nil || report <= 0 {
			return apperr.NotFound("Envio")
		}
		if err := a.GitFeedback.Resend(r.Context(), kind, r.PathValue("id"), report); err != nil {
			return err
		}
		return noContent(w)
	}
}
