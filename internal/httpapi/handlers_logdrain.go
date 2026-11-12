package httpapi

import (
	"net/http"

	"pier/internal/logdrain"
)

// Log drains: forwarding of application and stack logs to external sinks.
// The sink token is write-only: responses carry has_token, never the value.
func (a *API) registerLogDrainRoutes(mux *http.ServeMux) {
	if a.LogDrains == nil {
		return
	}
	a.route(mux, "GET /api/v1/log-drains", canRead, a.listLogDrains)
	a.route(mux, "POST /api/v1/log-drains", canWrite, a.createLogDrain)
	a.route(mux, "PUT /api/v1/log-drains/{id}", canWrite, a.updateLogDrain)
	a.route(mux, "DELETE /api/v1/log-drains/{id}", canWrite, a.deleteLogDrain)
	a.route(mux, "POST /api/v1/log-drains/{id}/test", canWrite, a.testLogDrain)
}

func (a *API) listLogDrains(w http.ResponseWriter, r *http.Request) error {
	drains, err := a.LogDrains.List(r.Context())
	if err != nil {
		return err
	}
	return ok(w, drains)
}

func (a *API) createLogDrain(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[logdrain.Input](w, r)
	if err != nil {
		return err
	}
	drain, err := a.LogDrains.Create(r.Context(), input)
	if err != nil {
		return err
	}
	return created(w, drain)
}

func (a *API) updateLogDrain(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[logdrain.Input](w, r)
	if err != nil {
		return err
	}
	drain, err := a.LogDrains.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return ok(w, drain)
}

func (a *API) deleteLogDrain(w http.ResponseWriter, r *http.Request) error {
	if err := a.LogDrains.Delete(r.Context(), r.PathValue("id"), r.URL.Query().Get("confirmation")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) testLogDrain(w http.ResponseWriter, r *http.Request) error {
	if err := a.LogDrains.Test(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}
