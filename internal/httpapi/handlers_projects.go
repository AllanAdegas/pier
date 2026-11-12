package httpapi

import (
	"net/http"

	"pier/internal/application"
	"pier/internal/database"
	"pier/internal/stack"
)

func (a *API) registerProjectRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/projects", canRead, a.listProjects)
	a.route(mux, "POST /api/v1/projects", canWrite, a.createProject)
	a.route(mux, "GET /api/v1/projects/{id}", canRead, a.getProject)
	a.route(mux, "PATCH /api/v1/projects/{id}", canWrite, a.updateProject)
	a.route(mux, "DELETE /api/v1/projects/{id}", canWrite, a.deleteProject)
	a.route(mux, "POST /api/v1/projects/{id}/environments", canWrite, a.createEnvironment)
	a.route(mux, "DELETE /api/v1/environments/{id}", canWrite, a.deleteEnvironment)
	a.route(mux, "PATCH /api/v1/environments/{id}/protection", canWrite, a.environmentProtection)
	a.route(mux, "GET /api/v1/environments/{id}/resources", canRead, a.environmentResources)
}

type projectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) error {
	projects, err := a.Projects.List(r.Context())
	if err != nil {
		return err
	}
	return ok(w, projects)
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) error {
	item, err := a.Projects.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, item)
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[projectRequest](w, r)
	if err != nil {
		return err
	}
	item, err := a.Projects.Create(r.Context(), request.Name, request.Description)
	if err != nil {
		return err
	}
	return created(w, item)
}

func (a *API) updateProject(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[projectRequest](w, r)
	if err != nil {
		return err
	}
	item, err := a.Projects.Update(r.Context(), r.PathValue("id"), request.Name, request.Description)
	if err != nil {
		return err
	}
	return ok(w, item)
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) error {
	item, err := a.Projects.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	for _, e := range item.Environments {
		if err = a.Projects.CheckDestroy(r.Context(), e.ID, r.URL.Query().Get("confirmation")); err != nil {
			return err
		}
	}
	if err := a.Projects.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) createEnvironment(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[projectRequest](w, r)
	if err != nil {
		return err
	}
	environment, err := a.Projects.CreateEnvironment(r.Context(), r.PathValue("id"), request.Name)
	if err != nil {
		return err
	}
	return created(w, environment)
}

func (a *API) deleteEnvironment(w http.ResponseWriter, r *http.Request) error {
	if err := a.Projects.CheckDestroy(r.Context(), r.PathValue("id"), r.URL.Query().Get("confirmation")); err != nil {
		return err
	}
	if err := a.Projects.DeleteEnvironment(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

type resourcesResponse struct {
	Applications []application.Application `json:"applications"`
	Databases    []database.Database       `json:"databases"`
	Stacks       []stack.Stack             `json:"stacks"`
}

func (a *API) environmentResources(w http.ResponseWriter, r *http.Request) error {
	environment, err := a.Projects.GetEnvironment(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	resources, err := a.resources(r, environment.ID)
	if err != nil {
		return err
	}
	return ok(w, resources)
}

// resources loads every resource, optionally of one environment.
func (a *API) resources(r *http.Request, environmentID string) (resourcesResponse, error) {
	var response resourcesResponse
	var err error
	if response.Applications, err = a.Applications.List(r.Context(), environmentID); err != nil {
		return response, err
	}
	if response.Databases, err = a.Databases.List(r.Context(), environmentID); err != nil {
		return response, err
	}
	stacks, err := a.Stacks.List(r.Context(), environmentID)
	response.Applications = allForCaller(r, response.Applications)
	response.Stacks = allForCaller(r, stacks)
	return response, err
}

func (a *API) environmentProtection(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[struct {
		Protected bool   `json:"protected"`
		Frozen    bool   `json:"frozen"`
		Reason    string `json:"reason"`
	}](w, r)
	if err != nil {
		return err
	}
	current, err := a.Projects.GetEnvironment(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	auditSummary(r, map[string]any{"protected": input.Protected, "frozen": input.Frozen, "reason": input.Reason})
	e, err := a.Projects.SetProtection(r.Context(), current.ID, input.Protected, input.Frozen, input.Reason)
	if err != nil {
		return err
	}
	return ok(w, e)
}
