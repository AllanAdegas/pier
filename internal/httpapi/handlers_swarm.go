package httpapi

import (
	"net/http"
	"pier/internal/swarm"
)

func (a *API) registerSwarmRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/swarm", canRead, func(w http.ResponseWriter, r *http.Request) error {
		v, e := a.Swarm.List(r.Context())
		if e != nil {
			return e
		}
		return ok(w, v)
	})
	a.route(mux, "POST /api/v1/swarm", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		i, e := decode[swarm.CreateInput](w, r)
		if e != nil {
			return e
		}
		v, e := a.Swarm.Create(r.Context(), i)
		if e != nil {
			return e
		}
		return created(w, v)
	})
	a.route(mux, "GET /api/v1/swarm/{id}", canRead, func(w http.ResponseWriter, r *http.Request) error {
		v, e := a.Swarm.Get(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		return ok(w, v)
	})
	a.route(mux, "POST /api/v1/swarm/{id}/delete", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		i, e := decode[swarm.Confirmation](w, r)
		if e != nil {
			return e
		}
		if e = a.Swarm.Delete(r.Context(), r.PathValue("id"), i.Confirmation); e != nil {
			return e
		}
		return ok(w, nil)
	})
	a.route(mux, "GET /api/v1/swarm/{id}/nodes", canRead, func(w http.ResponseWriter, r *http.Request) error {
		v, e := a.Swarm.Nodes(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		return ok(w, v)
	})
	a.route(mux, "POST /api/v1/swarm/{id}/nodes", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		i, e := decode[swarm.JoinInput](w, r)
		if e != nil {
			return e
		}
		if e = a.Swarm.Join(r.Context(), r.PathValue("id"), i); e != nil {
			return e
		}
		return ok(w, nil)
	})
	a.route(mux, "POST /api/v1/swarm/{id}/nodes/{node}/availability", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		i, e := decode[struct {
			Availability string `json:"availability"`
		}](w, r)
		if e != nil {
			return e
		}
		if e = a.Swarm.Availability(r.Context(), r.PathValue("id"), r.PathValue("node"), i.Availability); e != nil {
			return e
		}
		return ok(w, nil)
	})
	a.route(mux, "POST /api/v1/swarm/{id}/nodes/{node}/remove", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		i, e := decode[swarm.Confirmation](w, r)
		if e != nil {
			return e
		}
		if e = a.Swarm.RemoveNode(r.Context(), r.PathValue("id"), r.PathValue("node"), i.Confirmation); e != nil {
			return e
		}
		return ok(w, nil)
	})
	a.route(mux, "GET /api/v1/swarm/{id}/services", canRead, func(w http.ResponseWriter, r *http.Request) error {
		v, e := a.Swarm.Services(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		return ok(w, v)
	})
	a.route(mux, "POST /api/v1/swarm/{id}/services", canDeploy, func(w http.ResponseWriter, r *http.Request) error {
		i, e := decode[swarm.DeployInput](w, r)
		if e != nil {
			return e
		}
		if e = a.Swarm.Deploy(r.Context(), r.PathValue("id"), i); e != nil {
			return e
		}
		return ok(w, nil)
	})
	a.route(mux, "POST /api/v1/swarm/{id}/services/{service}", canDeploy, func(w http.ResponseWriter, r *http.Request) error {
		i, e := decode[struct {
			Action   string `json:"action"`
			Replicas int    `json:"replicas"`
		}](w, r)
		if e != nil {
			return e
		}
		if e = a.Swarm.Action(r.Context(), r.PathValue("id"), r.PathValue("service"), i.Action, i.Replicas); e != nil {
			return e
		}
		return ok(w, nil)
	})
	a.route(mux, "GET /api/v1/swarm/{id}/services/{service}/logs", canRead, func(w http.ResponseWriter, r *http.Request) error {
		v, e := a.Swarm.Logs(r.Context(), r.PathValue("id"), r.PathValue("service"))
		if e != nil {
			return e
		}
		return ok(w, map[string]string{"logs": v})
	})
	a.route(mux, "GET /api/v1/swarm/{id}/services/{service}/tasks", canRead, func(w http.ResponseWriter, r *http.Request) error {
		v, e := a.Swarm.Tasks(r.Context(), r.PathValue("id"), r.PathValue("service"))
		if e != nil {
			return e
		}
		return ok(w, v)
	})
}
