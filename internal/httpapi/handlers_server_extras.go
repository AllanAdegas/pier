package httpapi

import (
	"net/http"
	"pier/internal/platform/apperr"
	"pier/internal/server"
)

func (a *API) registerServerOperationsRoutes(mux *http.ServeMux) {
	if a.ServerOperations == nil {
		return
	}
	a.route(mux, "GET /api/v1/servers/{id}/operations-policy", canRead, a.serverOperationsPolicy)
	a.route(mux, "PUT /api/v1/servers/{id}/operations-policy", canWrite, a.setServerOperationsPolicy)
	a.route(mux, "POST /api/v1/servers/{id}/safe-cleanup", canWrite, a.safeServerCleanup)
}
func (a *API) serverOperationsPolicy(w http.ResponseWriter, r *http.Request) error {
	p, err := a.ServerOperations.Policy(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, p)
}
func (a *API) setServerOperationsPolicy(w http.ResponseWriter, r *http.Request) error {
	p, err := decode[server.OperationsPolicy](w, r)
	if err != nil {
		return err
	}
	saved, err := a.ServerOperations.SetPolicy(r.Context(), r.PathValue("id"), p)
	if err != nil {
		return err
	}
	return ok(w, saved)
}
func (a *API) safeServerCleanup(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[struct {
		DryRun       *bool  `json:"dry_run"`
		Confirmation string `json:"confirmation"`
	}](w, r)
	if err != nil {
		return err
	}
	dry := input.DryRun == nil || *input.DryRun
	if !dry {
		target, err := a.Servers.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		if target.Name != input.Confirmation {
			return apperr.Invalid("Digite o nome do servidor para confirmar a limpeza.")
		}
	}
	report, err := a.ServerOperations.Cleanup(r.Context(), r.PathValue("id"), dry)
	if err != nil {
		return err
	}
	return ok(w, report)
}
