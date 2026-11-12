package httpapi

import (
	"net/http"
	"strconv"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/server"
)

const (
	defaultLogTail = 200
	maxLogTail     = 5000
)

func (a *API) registerServerRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/servers", canRead, a.listServers)
	a.route(mux, "POST /api/v1/servers", canWrite, a.createServer)
	a.route(mux, "GET /api/v1/servers/{id}", canRead, a.getServer)
	a.route(mux, "PATCH /api/v1/servers/{id}", canWrite, a.updateServer)
	a.route(mux, "DELETE /api/v1/servers/{id}", canWrite, a.deleteServer)
	a.route(mux, "POST /api/v1/servers/{id}/validate", canWrite, a.validateServer)
	a.route(mux, "POST /api/v1/servers/{id}/check-domain", canRead, a.checkServerDomain)
	a.route(mux, "POST /api/v1/servers/{id}/reset-host-key", canWrite, a.resetHostKey)
	a.route(mux, "GET /api/v1/servers/{id}/metrics", canRead, a.serverMetrics)
	a.route(mux, "GET /api/v1/servers/{id}/files", canWrite, a.browseServer)
	a.route(mux, "POST /api/v1/servers/{id}/cleanup", canWrite, a.cleanupServer)
	a.route(mux, "GET /api/v1/servers/{id}/proxy", canRead, a.getProxy)
	a.route(mux, "POST /api/v1/servers/{id}/proxy/{action}", canWrite, a.operateProxy)
	a.route(mux, "GET /api/v1/servers/{id}/proxy/logs", canRead, a.proxyLogs)
	a.route(mux, "POST /api/v1/servers/{id}/proxy/edge/sync", canWrite, a.syncEdge)
	a.route(mux, "GET /api/v1/servers/{id}/containers/{container}/logs", canRead, a.streamContainerLogs)
	a.route(mux, "GET /api/v1/keys", canRead, a.listKeys)
	a.route(mux, "POST /api/v1/keys", canWrite, a.createKey)
	a.route(mux, "DELETE /api/v1/keys/{id}", canWrite, a.deleteKey)
}

func (a *API) listServers(w http.ResponseWriter, r *http.Request) error {
	servers, err := a.Servers.List(r.Context())
	if err != nil {
		return err
	}
	return ok(w, servers)
}

func (a *API) getServer(w http.ResponseWriter, r *http.Request) error {
	target, err := a.Servers.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, target)
}

func (a *API) createServer(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[server.Input](w, r)
	if err != nil {
		return err
	}
	target, err := a.Servers.Create(r.Context(), input)
	if err != nil {
		return err
	}
	return created(w, target)
}

func (a *API) updateServer(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[server.Input](w, r)
	if err != nil {
		return err
	}
	target, err := a.Servers.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return ok(w, target)
}

func (a *API) deleteServer(w http.ResponseWriter, r *http.Request) error {
	if err := a.Servers.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) validateServer(w http.ResponseWriter, r *http.Request) error {
	target, err := a.Servers.Validate(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, target)
}

func (a *API) resetHostKey(w http.ResponseWriter, r *http.Request) error {
	if err := a.Servers.ResetHostKey(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) serverMetrics(w http.ResponseWriter, r *http.Request) error {
	metrics, err := a.Servers.Metrics(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, metrics)
}

func (a *API) cleanupServer(w http.ResponseWriter, r *http.Request) error {
	report, err := a.Servers.Cleanup(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, map[string]string{"report": report})
}

type proxyResponse struct {
	State         server.ProxyState `json:"state"`
	Configuration string            `json:"configuration"`
	// EdgeCandidates are the Caddy containers running on the server.
	EdgeCandidates []string `json:"edge_candidates"`
}

func (a *API) getProxy(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	configuration, err := a.Proxy.Configuration(r.Context(), id)
	if err != nil {
		return err
	}
	state, err := a.Proxy.State(r.Context(), id)
	if err != nil {
		return err
	}
	candidates := a.Proxy.EdgeCandidates(r.Context(), id)
	return ok(w, proxyResponse{State: state, Configuration: configuration, EdgeCandidates: candidates})
}

func (a *API) operateProxy(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "start", "restart":
		err = a.Proxy.Start(r.Context(), id)
	case "stop":
		err = a.Proxy.Stop(r.Context(), id)
	default:
		err = apperr.NotFound("Ação")
	}
	if err != nil {
		return err
	}
	state, err := a.Proxy.State(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, state)
}

func (a *API) syncEdge(w http.ResponseWriter, r *http.Request) error {
	result, err := a.Proxy.SyncEdge(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, result)
}

func (a *API) proxyLogs(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	logs, err := a.Proxy.Logs(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, map[string]string{"logs": logs})
}

// streamContainerLogs follows the logs of a managed container as SSE. It
// serves applications, databases, stacks and the proxy alike. Like the logs
// of plain Docker containers and pods, the output is sensitive: services
// print tokens and connection strings.
func (a *API) streamContainerLogs(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	ex, _, err := a.Connector.Executor(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	managed, err := docker.ListManaged(r.Context(), ex)
	if err != nil {
		return apperr.Unavailable("Não foi possível consultar o servidor: %s", err)
	}
	containerID := ""
	for _, container := range managed {
		if container.ID == r.PathValue("container") || container.Name == r.PathValue("container") {
			containerID = container.ID
		}
	}
	if containerID == "" {
		return apperr.NotFound("Container")
	}
	tail := defaultLogTail
	if requested, err := strconv.Atoi(r.URL.Query().Get("tail")); err == nil && requested > 0 {
		tail = min(requested, maxLogTail)
	}
	stream := newEventStream(w)
	var sequence int64
	sink := func(line string) {
		sequence++
		_ = stream.send("log", sequence, map[string]string{"line": line})
	}
	// The stream ends when the client disconnects (context cancelled) or the
	// container stops.
	_ = ex.Stream(r.Context(), docker.LogsCommand(containerID, tail, true), sink)
	_ = stream.send("done", 0, map[string]bool{"done": true})
	return nil
}

func (a *API) listKeys(w http.ResponseWriter, r *http.Request) error {
	keys, err := a.Keys.List(r.Context())
	if err != nil {
		return err
	}
	return ok(w, keys)
}

type keyRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	PrivateKey  string `json:"private_key"`
}

func (a *API) createKey(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[keyRequest](w, r)
	if err != nil {
		return err
	}
	var key server.PrivateKey
	if request.PrivateKey == "" {
		key, err = a.Keys.Generate(r.Context(), request.Name, request.Description)
	} else {
		key, err = a.Keys.Import(r.Context(), request.Name, request.Description, request.PrivateKey)
	}
	if err != nil {
		return err
	}
	return created(w, key)
}

func (a *API) deleteKey(w http.ResponseWriter, r *http.Request) error {
	if err := a.Keys.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

// browseServer lists a folder of the server, for choosing where a compose
// project lives. It reveals the layout of the disk, so it takes "write".
func (a *API) browseServer(w http.ResponseWriter, r *http.Request) error {
	listing, err := a.Servers.Browse(r.Context(), r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	return ok(w, listing)
}

func (a *API) checkServerDomain(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[struct {
		Domain string `json:"domain"`
	}](w, r)
	if err != nil {
		return err
	}
	result, err := a.Servers.CheckDomain(r.Context(), r.PathValue("id"), input.Domain)
	if err != nil {
		return err
	}
	return ok(w, result)
}
