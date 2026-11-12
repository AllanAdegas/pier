package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"pier/internal/monitor"
	"pier/internal/platform/apperr"
)

func (a *API) registerMonitoringRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/monitoring/options", canRead, a.monitoringOptions)
	a.route(mux, "PATCH /api/v1/monitoring/options", canWrite, a.setMonitoringOptions)
	a.route(mux, "POST /api/v1/monitoring/silences", canWrite, a.monitoringSilence)
	a.route(mux, "GET /api/v1/monitoring/uptime", canRead, a.uptimeChecks)
	a.route(mux, "POST /api/v1/monitoring/uptime", canWrite, a.saveUptime)
	a.route(mux, "DELETE /api/v1/monitoring/uptime/{id}", canWrite, a.deleteUptime)
	a.route(mux, "GET /api/v1/monitoring/uptime/{id}/history", canRead, a.uptimeHistory)
}
func (a *API) monitoringOptions(w http.ResponseWriter, r *http.Request) error {
	v, err := a.Monitoring.Options(r.Context())
	if err != nil {
		return err
	}
	return ok(w, v)
}
func (a *API) setMonitoringOptions(w http.ResponseWriter, r *http.Request) error {
	v, err := decode[monitor.Options](w, r)
	if err != nil {
		return err
	}
	if err = a.Monitoring.SetOptions(r.Context(), v); err != nil {
		return err
	}
	return ok(w, v)
}
func (a *API) monitoringSilence(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[struct {
		Resource string `json:"resource"`
		UntilAt  string `json:"until_at"`
	}](w, r)
	if err != nil {
		return err
	}
	until, err := time.Parse(time.RFC3339, input.UntilAt)
	now := time.Now()
	if err != nil || until.Before(now.Add(-time.Minute)) || until.After(now.Add(30*24*time.Hour)) {
		return apperr.Invalid("Informe um término válido para a manutenção, em até 30 dias.")
	}
	if input.Resource != "" {
		if err = a.monitoringResource(r, input.Resource); err != nil {
			return err
		}
	}
	if err = a.Monitoring.Silence(r.Context(), input.Resource, until); err != nil {
		return err
	}
	return noContent(w)
}
func (a *API) monitoringResource(r *http.Request, id string) error {
	if a.Applications != nil {
		if _, err := a.Applications.Get(r.Context(), id); err == nil {
			return nil
		} else if _, ok := apperr.As(err); !ok {
			return err
		}
	}
	if a.Stacks != nil {
		if _, err := a.Stacks.Get(r.Context(), id); err == nil {
			return nil
		} else if _, ok := apperr.As(err); !ok {
			return err
		}
	}
	if a.Databases != nil {
		if _, err := a.Databases.Get(r.Context(), id); err == nil {
			return nil
		} else if _, ok := apperr.As(err); !ok {
			return err
		}
	}
	if a.Servers != nil {
		if _, err := a.Servers.Get(r.Context(), id); err == nil {
			return nil
		} else if _, ok := apperr.As(err); !ok {
			return err
		}
	}
	return apperr.NotFound("Recurso")
}
func (a *API) uptimeChecks(w http.ResponseWriter, r *http.Request) error {
	v, err := a.Monitoring.UptimeChecks(r.Context())
	if err != nil {
		return err
	}
	return ok(w, v)
}
func (a *API) saveUptime(w http.ResponseWriter, r *http.Request) error {
	v, err := decode[monitor.UptimeCheck](w, r)
	if err != nil {
		return err
	}
	routes := []string{}
	found := false
	if a.Applications != nil {
		app, e := a.Applications.Get(r.Context(), v.Resource)
		if e == nil {
			found = true
			routes = app.Config.Network.Domains
		} else if _, ok := apperr.As(e); !ok {
			return e
		}
	}
	if !found && a.Stacks != nil {
		st, e := a.Stacks.Get(r.Context(), v.Resource)
		if e == nil {
			found = true
			for _, route := range st.Routes {
				routes = append(routes, route.Domain)
			}
		} else if _, ok := apperr.As(e); !ok {
			return e
		}
	}
	if !found {
		return apperr.NotFound("Aplicação ou stack")
	}
	if !publishedURL(v.URL, routes) {
		return apperr.Invalid("A URL deve corresponder a uma rota publicada da aplicação ou stack.")
	}
	v.LastChecked = ""
	v, err = a.Monitoring.SaveUptime(r.Context(), v)
	if err != nil {
		return err
	}
	return created(w, v)
}

// A health path may be below a published path, but cannot change its origin,
// escape that path, or redirect the monitor to credentials or another port.
func publishedURL(raw string, routes []string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	for _, rawRoute := range routes {
		route, err := url.Parse(rawRoute)
		if err != nil || route.Host == "" {
			continue
		}
		if !strings.EqualFold(route.Scheme, u.Scheme) || !strings.EqualFold(route.Host, u.Host) {
			continue
		}
		base := strings.TrimRight(route.EscapedPath(), "/")
		p := u.EscapedPath()
		if strings.Contains(u.Path, "..") || strings.Contains(strings.ToLower(p), "%2f") {
			continue
		}
		if base == "" || p == base || strings.HasPrefix(p, base+"/") {
			return true
		}
	}
	return false
}
func (a *API) deleteUptime(w http.ResponseWriter, r *http.Request) error {
	if err := a.Monitoring.DeleteUptime(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}
func (a *API) uptimeHistory(w http.ResponseWriter, r *http.Request) error {
	v, err := a.Monitoring.UptimeHistory(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, v)
}
