package httpapi

import (
	"net/http"
	"pier/internal/application"
	"pier/internal/registry"
)

func (a *API) registerMultiServerRoutes(mux *http.ServeMux) {
	if a.MultiServer == nil || a.Registries == nil {
		return
	}
	a.route(mux, "GET /api/v1/registries", canRead, func(w http.ResponseWriter, r *http.Request) error {
		rows, err := a.Registries.List(r.Context())
		if err != nil {
			return err
		}
		return ok(w, rows)
	})
	a.route(mux, "PUT /api/v1/registries", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		in, err := decode[registry.Input](w, r)
		if err != nil {
			return err
		}
		if err = a.Registries.Save(r.Context(), in); err != nil {
			return err
		}
		return noContent(w)
	})
	base := "/api/v1/applications/{id}/placement"
	a.route(mux, "GET "+base, canRead, func(w http.ResponseWriter, r *http.Request) error {
		p, err := a.MultiServer.Settings(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, p)
	})
	a.route(mux, "PUT "+base, canWrite, func(w http.ResponseWriter, r *http.Request) error {
		p, err := decode[application.Placement](w, r)
		if err != nil {
			return err
		}
		if err = a.MultiServer.Save(r.Context(), r.PathValue("id"), p); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "GET "+base+"/status", canRead, func(w http.ResponseWriter, r *http.Request) error {
		p, err := a.MultiServer.Status(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, p)
	})
	a.route(mux, "POST "+base+"/rollout", canDeploy, func(w http.ResponseWriter, r *http.Request) error {
		in, err := decode[struct {
			Image string `json:"image"`
		}](w, r)
		if err != nil {
			return err
		}
		if err = a.MultiServer.Rollout(r.Context(), r.PathValue("id"), in.Image); err != nil {
			return err
		}
		return noContent(w)
	})
}
