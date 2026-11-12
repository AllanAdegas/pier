package httpapi

import (
	"net/http"
	"pier/internal/application"
	"pier/internal/envvar"
)

func (a *API) registerPreviewRoutes(mux *http.ServeMux) {
	base := "/api/v1/applications/{id}/previews"
	a.route(mux, "GET "+base, canRead, func(w http.ResponseWriter, r *http.Request) error {
		rows, err := a.Previews.List(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, rows)
	})
	a.route(mux, "GET "+base+"/settings", canRead, func(w http.ResponseWriter, r *http.Request) error {
		settings, err := a.Previews.Settings(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, settings)
	})
	a.route(mux, "PATCH "+base+"/settings", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[application.PreviewSettings](w, r)
		if err != nil {
			return err
		}
		settings, err := a.Previews.UpdateSettings(r.Context(), r.PathValue("id"), input)
		if err != nil {
			return err
		}
		return ok(w, settings)
	})
	a.route(mux, "POST "+base+"/{preview}/deploy", canDeploy, func(w http.ResponseWriter, r *http.Request) error {
		deployment, err := a.Previews.Redeploy(r.Context(), r.PathValue("id"), r.PathValue("preview"))
		if err != nil {
			return err
		}
		return accepted(w, deployment)
	})
	a.route(mux, "DELETE "+base+"/{preview}", canDeploy, func(w http.ResponseWriter, r *http.Request) error {
		if err := a.Previews.Destroy(r.Context(), r.PathValue("id"), r.PathValue("preview")); err != nil {
			return err
		}
		return noContent(w)
	})
	owner := func(r *http.Request) envvar.Owner {
		return envvar.Owner{Type: application.PreviewEnvOwner, ID: r.PathValue("id")}
	}
	a.route(mux, "GET "+base+"/envs", canRead, func(w http.ResponseWriter, r *http.Request) error {
		if err := requireSensitive(r); err != nil {
			return err
		}
		if _, err := a.Applications.Get(r.Context(), r.PathValue("id")); err != nil {
			return err
		}
		rows, err := a.Env.List(r.Context(), owner(r))
		if err != nil {
			return err
		}
		return ok(w, rows)
	})
	a.route(mux, "POST "+base+"/envs", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		if _, err := a.Applications.Get(r.Context(), r.PathValue("id")); err != nil {
			return err
		}
		v, err := decode[envvar.Variable](w, r)
		if err != nil {
			return err
		}
		if err = a.Env.Set(r.Context(), owner(r), v.Key, v.Value, v.BuildTime, v.SecretBuild); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "PUT "+base+"/envs", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		if _, err := a.Applications.Get(r.Context(), r.PathValue("id")); err != nil {
			return err
		}
		input, err := decode[bulkEnvRequest](w, r)
		if err != nil {
			return err
		}
		vars := input.Variables
		if vars == nil {
			vars, err = a.parseDotenv(r, owner(r), input.Dotenv)
			if err != nil {
				return err
			}
		}
		if err = a.Env.ReplaceAll(r.Context(), owner(r), vars); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "DELETE "+base+"/envs/{env}", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		if _, err := a.Applications.Get(r.Context(), r.PathValue("id")); err != nil {
			return err
		}
		if err := a.Env.Delete(r.Context(), owner(r), r.PathValue("env")); err != nil {
			return err
		}
		return noContent(w)
	})
}
