package httpapi

import (
	"net/http"

	"pier/internal/application"
	"pier/internal/resourceclone"
)

// registerCloneRoutes copies a resource's definition into another
// environment. The body's target_environment_id is authorized like the path
// id: the caller needs write access to both projects.
func (a *API) registerCloneRoutes(mux *http.ServeMux) {
	a.route(mux, "POST /api/v1/applications/{id}/clone", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		options, err := decode[application.CloneOptions](w, r)
		if err != nil {
			return err
		}
		app, err := a.Applications.CloneWithConfigCheck(r.Context(), r.PathValue("id"), options, func(config application.Config) error {
			return a.authorizeHostOptions(r, config)
		})
		if err != nil {
			return err
		}
		return created(w, forCaller(r, app))
	})
	a.route(mux, "POST /api/v1/databases/{id}/clone", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		options, err := decode[application.CloneOptions](w, r)
		if err != nil {
			return err
		}
		item, err := a.cloner().CloneDatabase(r.Context(), r.PathValue("id"), options)
		if err != nil {
			return err
		}
		return created(w, item)
	})
	a.route(mux, "POST /api/v1/stacks/{id}/clone", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		options, err := decode[application.CloneOptions](w, r)
		if err != nil {
			return err
		}
		item, err := a.cloner().CloneStack(r.Context(), r.PathValue("id"), options)
		if err != nil {
			return err
		}
		return created(w, forCaller(r, item))
	})
}

func (a *API) cloner() *resourceclone.Service {
	return &resourceclone.Service{Databases: a.Databases, Stacks: a.Stacks, Env: a.Env}
}
