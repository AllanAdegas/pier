package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"pier/internal/application"
	"pier/internal/notify"
	"pier/internal/server"
	"pier/internal/settings"
)

func (a *API) registerMiscRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/dashboard", canRead, a.dashboard)
	a.route(mux, "GET /api/v1/resources", canRead, a.allResources)
	a.route(mux, "GET /api/v1/settings", canRead, a.getSettings)
	a.route(mux, "PATCH /api/v1/settings", canWrite, a.updateSettings)
	a.route(mux, "GET /api/v1/notification-events", canRead, func(w http.ResponseWriter, _ *http.Request) error {
		return ok(w, notify.AllEvents)
	})
	a.route(mux, "GET /api/v1/notifications", canRead, a.listChannels)
	a.route(mux, "POST /api/v1/notifications", canWrite, a.createChannel)
	a.route(mux, "PATCH /api/v1/notifications/{id}", canWrite, a.updateChannel)
	a.route(mux, "DELETE /api/v1/notifications/{id}", canWrite, a.deleteChannel)
	a.route(mux, "POST /api/v1/notifications/{id}/test", canWrite, a.testChannel)
}

type dashboardResponse struct {
	Servers      []server.Server          `json:"servers"`
	Projects     int                      `json:"projects"`
	Applications int                      `json:"applications"`
	Databases    int                      `json:"databases"`
	Stacks       int                      `json:"stacks"`
	Running      int                      `json:"running"`
	Unhealthy    int                      `json:"unhealthy"`
	Deployments  []application.Deployment `json:"deployments"`
}

func (a *API) dashboard(w http.ResponseWriter, r *http.Request) error {
	var response dashboardResponse
	var err error
	if response.Servers, err = a.Servers.List(r.Context()); err != nil {
		return err
	}
	projects, err := a.Projects.List(r.Context())
	if err != nil {
		return err
	}
	resources, err := a.resources(r, "")
	if err != nil {
		return err
	}
	if response.Deployments, err = a.Applications.Deployments(r.Context(), ""); err != nil {
		return err
	}
	response.Deployments = response.Deployments[:min(len(response.Deployments), 8)]
	response.Projects = len(projects)
	response.Applications = len(resources.Applications)
	response.Databases = len(resources.Databases)
	response.Stacks = len(resources.Stacks)
	statuses := []string{}
	for _, item := range resources.Applications {
		statuses = append(statuses, item.Status)
	}
	for _, item := range resources.Databases {
		statuses = append(statuses, item.Status)
	}
	for _, item := range resources.Stacks {
		statuses = append(statuses, item.Status)
	}
	for _, status := range statuses {
		switch status {
		case "running":
			response.Running++
		case "unhealthy", "restarting", "degraded":
			response.Unhealthy++
		}
	}
	return ok(w, response)
}

func (a *API) allResources(w http.ResponseWriter, r *http.Request) error {
	resources, err := a.resources(r, "")
	if err != nil {
		return err
	}
	return ok(w, resources)
}

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) error {
	current, err := a.Settings.Get(r.Context())
	if err != nil {
		return err
	}
	return ok(w, current)
}

func (a *API) updateSettings(w http.ResponseWriter, r *http.Request) error {
	next, err := decode[settings.Settings](w, r)
	if err != nil {
		return err
	}
	saved, err := a.Settings.Update(r.Context(), next)
	if err != nil {
		return err
	}
	return ok(w, saved)
}

func (a *API) listChannels(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	channels, err := a.Notifications.List(r.Context())
	if err != nil {
		return err
	}
	return ok(w, channels)
}

func (a *API) createChannel(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[notify.Input](w, r)
	if err != nil {
		return err
	}
	channel, err := a.Notifications.Create(r.Context(), input)
	if err != nil {
		return err
	}
	return created(w, channel)
}

func (a *API) updateChannel(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[notify.Input](w, r)
	if err != nil {
		return err
	}
	channel, err := a.Notifications.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return ok(w, channel)
}

func (a *API) deleteChannel(w http.ResponseWriter, r *http.Request) error {
	if err := a.Notifications.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) testChannel(w http.ResponseWriter, r *http.Request) error {
	if err := a.Notifications.Test(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

// webUIHandler serves the embedded single-page app: real files are served
// as-is and every other path falls back to index.html for client routing.
func (a *API) webUIHandler() http.Handler {
	files := http.FileServerFS(a.WebUI)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(a.WebUI, name); err != nil {
			name = "index.html"
		}
		if _, err := fs.Stat(a.WebUI, "index.html"); err != nil {
			http.Error(w, "A interface web não foi compilada. Rode `make web` e reinicie o Pier.", http.StatusServiceUnavailable)
			return
		}
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, a.WebUI, "index.html")
			return
		}
		// Vite fingerprints asset names, so they can be cached forever.
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}
