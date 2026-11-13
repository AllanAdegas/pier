package httpapi

import (
	"net/http"
	"strings"

	"pier/internal/platform/apperr"
	"pier/internal/server"
)

// Server operations: maintenance mode, safe cleanup and Docker install.
func (a *API) registerServerOperationsRoutes(mux *http.ServeMux) {
	if a.ServerOperations != nil {
		a.route(mux, "GET /api/v1/servers/{id}/operations-policy", canRead, a.serverOperationsPolicy)
		a.route(mux, "PUT /api/v1/servers/{id}/operations-policy", canWrite, a.setServerOperationsPolicy)
		a.route(mux, "GET /api/v1/servers/{id}/safe-cleanup", canRead, a.lastSafeCleanup)
		a.route(mux, "POST /api/v1/servers/{id}/safe-cleanup", canWrite, a.safeServerCleanup)
	}
	if a.DockerInstaller != nil {
		a.route(mux, "GET /api/v1/servers/{id}/docker-install", canRead, a.dockerInstallStatus)
		a.route(mux, "POST /api/v1/servers/{id}/docker-install", canWrite, a.startDockerInstall)
	}
}

func (a *API) serverOperationsPolicy(w http.ResponseWriter, r *http.Request) error {
	policy, err := a.ServerOperations.Policy(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, policy)
}

func (a *API) setServerOperationsPolicy(w http.ResponseWriter, r *http.Request) error {
	policy, err := decode[server.OperationsPolicy](w, r)
	if err != nil {
		return err
	}
	saved, err := a.ServerOperations.SetPolicy(r.Context(), r.PathValue("id"), policy)
	if err != nil {
		return err
	}
	return ok(w, saved)
}

func (a *API) lastSafeCleanup(w http.ResponseWriter, r *http.Request) error {
	report, found, err := a.ServerOperations.LastCleanup(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"found": found, "report": report})
}

// safeServerCleanup lists by default; removing needs dry_run=false and the
// server name typed in confirmation.
func (a *API) safeServerCleanup(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[struct {
		DryRun       *bool  `json:"dry_run"`
		Confirmation string `json:"confirmation"`
	}](w, r)
	if err != nil {
		return err
	}
	dryRun := input.DryRun == nil || *input.DryRun
	if !dryRun {
		target, err := a.Servers.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		if strings.TrimSpace(input.Confirmation) != target.Name {
			return apperr.Invalid("Digite o nome do servidor para confirmar a limpeza.")
		}
	}
	report, err := a.ServerOperations.Cleanup(r.Context(), r.PathValue("id"), dryRun)
	if err != nil {
		return err
	}
	return ok(w, report)
}

func (a *API) dockerInstallStatus(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.Servers.Get(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return ok(w, a.DockerInstaller.Status(r.PathValue("id")))
}

func (a *API) startDockerInstall(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[server.DockerInstallRequest](w, r)
	if err != nil {
		return err
	}
	snapshot, err := a.DockerInstaller.Start(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return accepted(w, snapshot)
}
