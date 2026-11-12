package httpapi

import (
	"net/http"

	"pier/internal/application"
	"pier/internal/registry"
	"pier/internal/server"
)

// registerMultiServerRoutes exposes container registries and application
// placements (build server, registry, destination servers).
func (a *API) registerMultiServerRoutes(mux *http.ServeMux) {
	if a.MultiServer == nil || a.Registries == nil {
		return
	}
	a.route(mux, "GET /api/v1/registries", canRead, func(w http.ResponseWriter, r *http.Request) error {
		registries, err := a.Registries.List(r.Context())
		if err != nil {
			return err
		}
		return ok(w, registries)
	})
	a.route(mux, "POST /api/v1/registries", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[registry.Input](w, r)
		if err != nil {
			return err
		}
		registryCreated, err := a.Registries.Create(r.Context(), input)
		if err != nil {
			return err
		}
		return created(w, registryCreated)
	})
	a.route(mux, "PATCH /api/v1/registries/{id}", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[registry.Input](w, r)
		if err != nil {
			return err
		}
		updated, err := a.Registries.Update(r.Context(), r.PathValue("id"), input)
		if err != nil {
			return err
		}
		return ok(w, updated)
	})
	a.route(mux, "DELETE /api/v1/registries/{id}", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		if err := a.Registries.Delete(r.Context(), r.PathValue("id")); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "POST /api/v1/registries/{id}/verify", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		if err := a.Registries.Verify(r.Context(), r.PathValue("id")); err != nil {
			return err
		}
		return noContent(w)
	})
	a.registerPlacementRoutes(mux)
}

func (a *API) registerPlacementRoutes(mux *http.ServeMux) {
	const base = "/api/v1/applications/{id}/placement"
	a.route(mux, "GET "+base, canRead, func(w http.ResponseWriter, r *http.Request) error {
		placement, err := a.MultiServer.Settings(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, placement)
	})
	a.route(mux, "PUT "+base, canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[application.Placement](w, r)
		if err != nil {
			return err
		}
		saved, err := a.MultiServer.Save(r.Context(), r.PathValue("id"), input)
		if err != nil {
			return err
		}
		return ok(w, saved)
	})
	a.route(mux, "GET "+base+"/servers", canRead, a.placementServers)
}

// placementServerView is one server of an application as the panel shows
// it: rollout state plus the live containers and their usage.
type placementServerView struct {
	application.PlacementServer
	Name       string                  `json:"name"`
	Reachable  bool                    `json:"reachable"`
	Containers []server.ContainerUsage `json:"containers"`
}

func (a *API) placementServers(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	servers, err := a.MultiServer.Servers(r.Context(), id)
	if err != nil {
		return err
	}
	views := make([]placementServerView, 0, len(servers))
	for _, item := range servers {
		view := placementServerView{PlacementServer: item, Name: item.ServerID, Containers: []server.ContainerUsage{}}
		if a.Servers != nil {
			if found, err := a.Servers.Get(r.Context(), item.ServerID); err == nil {
				view.Name = found.Name
			}
			// Metrics are shared (memoized) with the server pages and alerts.
			if metrics, err := a.Servers.Metrics(r.Context(), item.ServerID); err == nil {
				view.Reachable = true
				for _, container := range metrics.Containers {
					if container.ResourceID == id {
						view.Containers = append(view.Containers, container)
					}
				}
			}
		}
		views = append(views, view)
	}
	return ok(w, views)
}
