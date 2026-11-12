package httpapi

import (
	"net/http"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/monitor"
	"pier/internal/platform/apperr"
)

func (a *API) registerMetricsRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/servers/{id}/metrics/history", canRead, a.serverMetricsHistory)
	a.route(mux, "GET /api/v1/resources/{type}/{id}/metrics/history", canRead, a.resourceMetricsHistory)
}

func (a *API) serverMetricsHistory(w http.ResponseWriter, r *http.Request) error {
	window, err := historyRange(r)
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	if a.Servers != nil {
		if _, err := a.Servers.Get(r.Context(), id); err != nil {
			return err
		}
	}
	return a.writeMetricsHistory(w, r, monitor.SubjectKey("server", id), window)
}

func (a *API) resourceMetricsHistory(w http.ResponseWriter, r *http.Request) error {
	window, err := historyRange(r)
	if err != nil {
		return err
	}
	kind, known := resourceKind(r.PathValue("type"))
	if !known {
		return apperr.NotFound("Tipo de recurso")
	}
	id := r.PathValue("id")
	if err := a.requireResource(r, kind, id); err != nil {
		return err
	}
	return a.writeMetricsHistory(w, r, monitor.SubjectKey(kind, id), window)
}

// resourceKind accepts the singular and plural spelling of a resource type.
func resourceKind(name string) (string, bool) {
	switch strings.TrimSuffix(name, "s") {
	case docker.TypeApplication:
		return docker.TypeApplication, true
	case docker.TypeDatabase:
		return docker.TypeDatabase, true
	case docker.TypeStack:
		return docker.TypeStack, true
	}
	return "", false
}

func (a *API) requireResource(r *http.Request, kind, id string) error {
	var err error
	switch {
	case kind == docker.TypeApplication && a.Applications != nil:
		_, err = a.Applications.Get(r.Context(), id)
	case kind == docker.TypeDatabase && a.Databases != nil:
		_, err = a.Databases.Get(r.Context(), id)
	case kind == docker.TypeStack && a.Stacks != nil:
		_, err = a.Stacks.Get(r.Context(), id)
	}
	return err
}

func historyRange(r *http.Request) (monitor.Range, error) {
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "1h"
	}
	window, valid := monitor.ParseRange(name)
	if !valid {
		return monitor.Range{}, apperr.Invalid("Intervalo inválido. Use 1h, 6h, 24h, 7d ou 30d.")
	}
	return window, nil
}

func (a *API) writeMetricsHistory(w http.ResponseWriter, r *http.Request, subject string, window monitor.Range) error {
	if a.MetricsHistory == nil {
		return apperr.Unavailable("O histórico de métricas não está disponível.")
	}
	history, err := a.MetricsHistory.Query(r.Context(), subject, window, time.Now())
	if err != nil {
		return err
	}
	return ok(w, history)
}
