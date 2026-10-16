package httpapi

import (
	"net/http"
	"strconv"

	"pier/internal/engine"
)

// registerDockerRoutes exposes the plain Docker objects of a server.
//
// Listings need "read". Anything that reveals what runs inside a container
// (inspect shows its environment; logs show its output) is sensitive, since
// here the container may belong to something Pier knows nothing about.
func (a *API) registerDockerRoutes(mux *http.ServeMux) {
	const base = "/api/v1/servers/{id}/docker"
	a.route(mux, "GET "+base+"/containers", canRead, a.dockerContainers)
	a.route(mux, "POST "+base+"/containers", canWrite, a.dockerRun)
	a.route(mux, "GET "+base+"/containers/{container}", canRead, a.dockerInspect)
	a.route(mux, "DELETE "+base+"/containers/{container}", canWrite, a.dockerRemoveContainer)
	a.route(mux, "POST "+base+"/containers/{container}/actions/{action}", canWrite, a.dockerAct)
	a.route(mux, "POST "+base+"/containers/{container}/exec", canWrite, a.dockerExec)
	a.route(mux, "GET "+base+"/containers/{container}/logs", canRead, a.dockerLogs)
	a.route(mux, "GET "+base+"/images", canRead, a.dockerImages)
	a.route(mux, "POST "+base+"/images/pull", canWrite, a.dockerPull)
	a.route(mux, "POST "+base+"/images/prune", canWrite, a.dockerPrune)
	a.route(mux, "DELETE "+base+"/images", canWrite, a.dockerRemoveImage)
	a.route(mux, "GET "+base+"/volumes", canRead, a.dockerVolumes)
	a.route(mux, "POST "+base+"/volumes", canWrite, a.dockerCreateVolume)
	a.route(mux, "DELETE "+base+"/volumes/{name}", canWrite, a.dockerRemoveVolume)
	a.route(mux, "GET "+base+"/networks", canRead, a.dockerNetworks)
	a.route(mux, "POST "+base+"/networks", canWrite, a.dockerCreateNetwork)
	a.route(mux, "DELETE "+base+"/networks/{name}", canWrite, a.dockerRemoveNetwork)
}

func (a *API) dockerContainers(w http.ResponseWriter, r *http.Request) error {
	containers, err := a.Engine.Containers(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, containers)
}

func (a *API) dockerRun(w http.ResponseWriter, r *http.Request) error {
	spec, err := decode[engine.RunSpec](w, r)
	if err != nil {
		return err
	}
	containerID, err := a.Engine.Run(r.Context(), r.PathValue("id"), spec)
	if err != nil {
		return err
	}
	return created(w, map[string]string{"id": containerID})
}

func (a *API) dockerInspect(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	document, err := a.Engine.Inspect(r.Context(), r.PathValue("id"), r.PathValue("container"))
	if err != nil {
		return err
	}
	return ok(w, document)
}

func (a *API) dockerRemoveContainer(w http.ResponseWriter, r *http.Request) error {
	err := a.Engine.Remove(r.Context(), r.PathValue("id"), r.PathValue("container"), queryFlag(r, "force"))
	if err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) dockerAct(w http.ResponseWriter, r *http.Request) error {
	err := a.Engine.Act(r.Context(), r.PathValue("id"), r.PathValue("container"), r.PathValue("action"))
	if err != nil {
		return err
	}
	return noContent(w)
}

type execRequest struct {
	Command string `json:"command"`
}

func (a *API) dockerExec(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[execRequest](w, r)
	if err != nil {
		return err
	}
	output, err := a.Engine.Exec(r.Context(), r.PathValue("id"), r.PathValue("container"), request.Command)
	if err != nil {
		return err
	}
	return ok(w, map[string]string{"output": output})
}

func (a *API) dockerLogs(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	tail := defaultLogTail
	if requested, err := strconv.Atoi(r.URL.Query().Get("tail")); err == nil && requested > 0 {
		tail = min(requested, maxLogTail)
	}
	ex, command, err := a.Engine.LogsCommand(r.Context(), r.PathValue("id"), r.PathValue("container"), tail)
	if err != nil {
		return err
	}
	stream := newEventStream(w)
	var sequence int64
	_ = ex.Stream(r.Context(), command, func(line string) {
		sequence++
		_ = stream.send("log", sequence, map[string]string{"line": line})
	})
	_ = stream.send("done", 0, map[string]bool{"done": true})
	return nil
}

func (a *API) dockerImages(w http.ResponseWriter, r *http.Request) error {
	images, err := a.Engine.Images(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, images)
}

type imageRequest struct {
	Image string `json:"image"`
}

func (a *API) dockerPull(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[imageRequest](w, r)
	if err != nil {
		return err
	}
	result, err := a.Engine.PullImage(r.Context(), r.PathValue("id"), request.Image)
	if err != nil {
		return err
	}
	return ok(w, map[string]string{"result": result})
}

func (a *API) dockerPrune(w http.ResponseWriter, r *http.Request) error {
	report, err := a.Engine.PruneImages(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, map[string]string{"report": report})
}

// dockerRemoveImage takes the reference in the query string, because image
// names contain slashes.
func (a *API) dockerRemoveImage(w http.ResponseWriter, r *http.Request) error {
	err := a.Engine.RemoveImage(r.Context(), r.PathValue("id"), r.URL.Query().Get("ref"), queryFlag(r, "force"))
	if err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) dockerVolumes(w http.ResponseWriter, r *http.Request) error {
	volumes, err := a.Engine.Volumes(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, volumes)
}

type nameRequest struct {
	Name string `json:"name"`
}

func (a *API) dockerCreateVolume(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[nameRequest](w, r)
	if err != nil {
		return err
	}
	if err := a.Engine.CreateVolume(r.Context(), r.PathValue("id"), request.Name); err != nil {
		return err
	}
	return created(w, request)
}

func (a *API) dockerRemoveVolume(w http.ResponseWriter, r *http.Request) error {
	if err := a.Engine.RemoveVolume(r.Context(), r.PathValue("id"), r.PathValue("name")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) dockerNetworks(w http.ResponseWriter, r *http.Request) error {
	networks, err := a.Engine.Networks(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, networks)
}

func (a *API) dockerCreateNetwork(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[nameRequest](w, r)
	if err != nil {
		return err
	}
	if err := a.Engine.CreateNetwork(r.Context(), r.PathValue("id"), request.Name); err != nil {
		return err
	}
	return created(w, request)
}

func (a *API) dockerRemoveNetwork(w http.ResponseWriter, r *http.Request) error {
	if err := a.Engine.RemoveNetwork(r.Context(), r.PathValue("id"), r.PathValue("name")); err != nil {
		return err
	}
	return noContent(w)
}
