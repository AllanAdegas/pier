package httpapi

import (
	"net/http"

	rbac "pier/internal/access"
	"pier/internal/application"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
)

// registerEngineRoutes exposes shared variables, the redeploy status, the
// automatic domain and the engine's instance settings.
func (a *API) registerEngineRoutes(mux *http.ServeMux) {
	a.registerSharedVariableRoutes(mux, "projects", envvar.SharedProject)
	a.registerSharedVariableRoutes(mux, "environments", envvar.SharedEnvironment)
	a.route(mux, "GET /api/v1/applications/{id}/redeploy-status", canRead, func(w http.ResponseWriter, r *http.Request) error {
		status, err := a.Applications.RedeployStatus(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, status)
	})
	a.route(mux, "GET /api/v1/applications/{id}/auto-domain", canRead, func(w http.ResponseWriter, r *http.Request) error {
		domain, err := a.Applications.AutoDomain(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, domain)
	})
	a.route(mux, "POST /api/v1/applications/{id}/auto-domain", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		app, err := a.Applications.GenerateAutoDomain(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, forCaller(r, app))
	})
	a.route(mux, "DELETE /api/v1/applications/{id}/auto-domain", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		app, err := a.Applications.RemoveAutoDomain(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, forCaller(r, app))
	})
	a.registerEngineSettingsRoutes(mux)
	a.registerCloneRoutes(mux)
}

func (a *API) registerSharedVariableRoutes(mux *http.ServeMux, collection string, scope envvar.SharedScope) {
	base := "/api/v1/" + collection + "/{id}/shared-variables"
	a.route(mux, "GET "+base, canRead, func(w http.ResponseWriter, r *http.Request) error {
		variables, err := a.Applications.ListSharedVariables(r.Context(), scope, r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, variables)
	})
	a.route(mux, "POST "+base, canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[envvar.SharedInput](w, r)
		if err != nil {
			return err
		}
		if err := a.requireSharedScope(r, scope); err != nil {
			return err
		}
		variable, err := a.Applications.SetSharedVariable(r.Context(), scope, r.PathValue("id"), input)
		if err != nil {
			return err
		}
		return ok(w, variable)
	})
	a.route(mux, "DELETE "+base+"/{key}", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		if err := a.Applications.DeleteSharedVariable(r.Context(), scope, r.PathValue("id"), r.PathValue("key")); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "GET "+base+"/{key}/dependents", canRead, func(w http.ResponseWriter, r *http.Request) error {
		apps, err := a.Applications.SharedVariableDependents(r.Context(), scope, r.PathValue("id"), r.PathValue("key"))
		if err != nil {
			return err
		}
		return ok(w, allForCaller(r, apps))
	})
}

// requireSharedScope answers 404 for an unknown project or environment
// instead of a storage error.
func (a *API) requireSharedScope(r *http.Request, scope envvar.SharedScope) error {
	if scope == envvar.SharedEnvironment {
		_, err := a.Projects.GetEnvironment(r.Context(), r.PathValue("id"))
		return err
	}
	_, err := a.Projects.Get(r.Context(), r.PathValue("id"))
	return err
}

func (a *API) registerEngineSettingsRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/settings/auto-domain", canRead, func(w http.ResponseWriter, r *http.Request) error {
		settings, err := a.Applications.AutoDomainSettings(r.Context())
		if err != nil {
			return err
		}
		return ok(w, settings)
	})
	a.route(mux, "PATCH /api/v1/settings/auto-domain", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[application.AutoDomainSettings](w, r)
		if err != nil {
			return err
		}
		settings, err := a.Applications.SetAutoDomainSettings(r.Context(), input)
		if err != nil {
			return err
		}
		return ok(w, settings)
	})
	a.route(mux, "GET /api/v1/settings/docker-options", canRead, func(w http.ResponseWriter, r *http.Request) error {
		policy, err := a.Applications.DockerOptionsPolicy(r.Context())
		if err != nil {
			return err
		}
		return ok(w, policy)
	})
	a.route(mux, "PATCH /api/v1/settings/docker-options", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[application.DockerOptionsPolicy](w, r)
		if err != nil {
			return err
		}
		if err := a.requireInstanceOwner(r); err != nil {
			return err
		}
		policy, err := a.Applications.SetDockerOptionsPolicy(r.Context(), input)
		if err != nil {
			return err
		}
		return ok(w, policy)
	})
}

// requireInstanceOwner is checked in the handler too, not only by the
// route's (empty) project scope: the dangerous-options switch must stay an
// owner decision even if global routes are ever opened to admins.
func (a *API) requireInstanceOwner(r *http.Request) error {
	if a.Access == nil {
		return apperr.Forbidden("Só um proprietário da instância pode mudar esta configuração.")
	}
	role, err := a.Access.Role(r.Context(), principalFrom(r.Context()).User.ID, "")
	if err != nil || role != rbac.Owner {
		return apperr.Forbidden("Só um proprietário da instância pode mudar esta configuração.")
	}
	return nil
}
