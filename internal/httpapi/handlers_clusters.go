package httpapi

import (
	"bufio"
	"net/http"
	"strconv"

	"pier/internal/cluster"
)

func (a *API) registerClusterRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/clusters", canRead, a.listClusters)
	a.route(mux, "POST /api/v1/clusters", canWrite, a.importCluster)
	a.route(mux, "POST /api/v1/clusters/k3s", canWrite, a.createK3sCluster)
	a.route(mux, "GET /api/v1/clusters/{id}", canRead, a.getCluster)
	a.route(mux, "PATCH /api/v1/clusters/{id}", canWrite, a.updateCluster)
	a.route(mux, "DELETE /api/v1/clusters/{id}", canWrite, a.deleteCluster)
	a.route(mux, "POST /api/v1/clusters/{id}/check", canRead, a.checkCluster)
	a.route(mux, "GET /api/v1/clusters/{id}/operation", canRead, a.clusterOperation)
	a.route(mux, "GET /api/v1/clusters/{id}/kubeconfig", canRead, a.clusterKubeconfig)
	a.route(mux, "GET /api/v1/clusters/{id}/overview", canRead, a.clusterOverview)
	a.route(mux, "GET /api/v1/clusters/{id}/api-resources", canRead, a.clusterAPIResources)
	a.route(mux, "GET /api/v1/clusters/{id}/objects", canRead, a.clusterObjects)
	a.route(mux, "GET /api/v1/clusters/{id}/object", canRead, a.clusterObject)
	a.route(mux, "DELETE /api/v1/clusters/{id}/object", canWrite, a.deleteClusterObject)
	a.route(mux, "POST /api/v1/clusters/{id}/apply", canWrite, a.applyToCluster)
	a.route(mux, "POST /api/v1/clusters/{id}/workloads", canWrite, a.createWorkload)
	a.route(mux, "POST /api/v1/clusters/{id}/scale", canDeploy, a.scaleWorkload)
	a.route(mux, "POST /api/v1/clusters/{id}/restart", canDeploy, a.restartWorkload)
	a.route(mux, "GET /api/v1/clusters/{id}/logs", canRead, a.clusterPodLogs)
}

func (a *API) listClusters(w http.ResponseWriter, r *http.Request) error {
	clusters, err := a.Clusters.List(r.Context())
	if err != nil {
		return err
	}
	return ok(w, clusters)
}

func (a *API) getCluster(w http.ResponseWriter, r *http.Request) error {
	item, err := a.Clusters.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, item)
}

func (a *API) importCluster(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[cluster.ImportInput](w, r)
	if err != nil {
		return err
	}
	item, err := a.Clusters.Import(r.Context(), input)
	if err != nil {
		return err
	}
	return created(w, item)
}

func (a *API) createK3sCluster(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[cluster.K3sInput](w, r)
	if err != nil {
		return err
	}
	item, err := a.Clusters.CreateK3s(r.Context(), input)
	if err != nil {
		return err
	}
	return accepted(w, item)
}

func (a *API) updateCluster(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[projectRequest](w, r)
	if err != nil {
		return err
	}
	item, err := a.Clusters.Update(r.Context(), r.PathValue("id"), request.Name, request.Description)
	if err != nil {
		return err
	}
	return ok(w, item)
}

func (a *API) deleteCluster(w http.ResponseWriter, r *http.Request) error {
	if err := a.Clusters.Delete(r.Context(), r.PathValue("id"), queryFlag(r, "force")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) checkCluster(w http.ResponseWriter, r *http.Request) error {
	item, err := a.Clusters.Check(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, item)
}

func (a *API) clusterOperation(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.Clusters.Get(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return ok(w, a.Clusters.Operation(r.PathValue("id")))
}

// clusterKubeconfig hands out the cluster credentials: sensitive by definition.
func (a *API) clusterKubeconfig(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	kubeconfig, err := a.Clusters.Kubeconfig(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, map[string]string{"kubeconfig": kubeconfig})
}

func (a *API) clusterOverview(w http.ResponseWriter, r *http.Request) error {
	overview, err := a.Clusters.Overview(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, overview)
}

func (a *API) clusterAPIResources(w http.ResponseWriter, r *http.Request) error {
	resources, err := a.Clusters.Resources(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, resources)
}

func resourceRef(r *http.Request) cluster.ResourceRef {
	query := r.URL.Query()
	return cluster.ResourceRef{Group: query.Get("group"), Resource: query.Get("resource")}
}

func objectRef(r *http.Request) cluster.ObjectRef {
	query := r.URL.Query()
	return cluster.ObjectRef{ResourceRef: resourceRef(r), Namespace: query.Get("namespace"), Name: query.Get("name")}
}

// clusterObjects lists summaries, which never include object contents.
func (a *API) clusterObjects(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.Clusters.Rows(r.Context(), r.PathValue("id"), resourceRef(r), r.URL.Query().Get("namespace"))
	if err != nil {
		return err
	}
	return ok(w, rows)
}

// clusterObject returns a full manifest. Any object may embed credentials
// (Secrets, but also env vars in a pod template), so it is sensitive.
func (a *API) clusterObject(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	manifest, err := a.Clusters.Manifest(r.Context(), r.PathValue("id"), objectRef(r))
	if err != nil {
		return err
	}
	return ok(w, map[string]string{"manifest": manifest})
}

func (a *API) deleteClusterObject(w http.ResponseWriter, r *http.Request) error {
	if err := a.Clusters.DeleteObject(r.Context(), r.PathValue("id"), objectRef(r)); err != nil {
		return err
	}
	return noContent(w)
}

type applyRequest struct {
	Manifest  string `json:"manifest"`
	Namespace string `json:"namespace"`
}

func (a *API) applyToCluster(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[applyRequest](w, r)
	if err != nil {
		return err
	}
	applied, err := a.Clusters.Apply(r.Context(), r.PathValue("id"), request.Manifest, request.Namespace)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"applied": applied})
}

func (a *API) createWorkload(w http.ResponseWriter, r *http.Request) error {
	spec, err := decode[cluster.WorkloadSpec](w, r)
	if err != nil {
		return err
	}
	applied, manifest, err := a.Clusters.CreateWorkload(r.Context(), r.PathValue("id"), spec)
	if err != nil {
		return err
	}
	return created(w, map[string]any{"applied": applied, "manifest": manifest})
}

type scaleRequest struct {
	cluster.ObjectRef
	Replicas int `json:"replicas"`
}

func (a *API) scaleWorkload(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[scaleRequest](w, r)
	if err != nil {
		return err
	}
	if err := a.Clusters.Scale(r.Context(), r.PathValue("id"), request.ObjectRef, request.Replicas); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) restartWorkload(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[cluster.ObjectRef](w, r)
	if err != nil {
		return err
	}
	if err := a.Clusters.Restart(r.Context(), r.PathValue("id"), request); err != nil {
		return err
	}
	return noContent(w)
}

// clusterPodLogs follows the log of a pod as SSE.
func (a *API) clusterPodLogs(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	query := r.URL.Query()
	tail := defaultLogTail
	if requested, err := strconv.Atoi(query.Get("tail")); err == nil && requested > 0 {
		tail = min(requested, maxLogTail)
	}
	logs, err := a.Clusters.PodLogs(r.Context(), r.PathValue("id"), query.Get("namespace"), query.Get("pod"),
		query.Get("container"), tail, true)
	if err != nil {
		return err
	}
	defer logs.Close()
	stream := newEventStream(w)
	scanner := bufio.NewScanner(logs)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var sequence int64
	for scanner.Scan() {
		sequence++
		if err := stream.send("log", sequence, map[string]string{"line": scanner.Text()}); err != nil {
			return nil
		}
	}
	_ = stream.send("done", 0, map[string]bool{"done": true})
	return nil
}
