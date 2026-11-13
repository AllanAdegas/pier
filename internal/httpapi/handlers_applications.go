package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pier/internal/application"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/schedule"
)

const deploymentPollInterval = 1500 * time.Millisecond

func (a *API) registerApplicationRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/applications", canRead, a.listApplications)
	a.route(mux, "POST /api/v1/applications", canWrite, a.createApplication)
	a.route(mux, "GET /api/v1/applications/defaults", canRead, a.applicationDefaults)
	a.route(mux, "GET /api/v1/applications/{id}", canRead, a.getApplication)
	a.route(mux, "PATCH /api/v1/applications/{id}", canWrite, a.updateApplication)
	a.route(mux, "DELETE /api/v1/applications/{id}", canWrite, a.deleteApplication)
	a.route(mux, "POST /api/v1/applications/{id}/deploy", canDeploy, a.deployApplication)
	a.route(mux, "POST /api/v1/applications/{id}/rollback", canDeploy, a.rollbackApplication)
	a.route(mux, "POST /api/v1/applications/{id}/deploy-archive", canDeploy, a.deployArchive)
	a.route(mux, "POST /api/v1/applications/{id}/promote", canDeploy, a.promoteApplication)
	a.route(mux, "POST /api/v1/applications/{id}/actions/{action}", canDeploy, a.operateApplication)
	a.route(mux, "GET /api/v1/applications/{id}/deployments", canRead, a.listApplicationDeployments)
	a.route(mux, "GET /api/v1/applications/{id}/containers", canRead, a.applicationContainers)
	a.route(mux, "GET /api/v1/applications/{id}/webhook", canRead, a.getWebhook)
	a.route(mux, "POST /api/v1/applications/{id}/webhook/rotate", canWrite, a.rotateWebhook)
	a.route(mux, "POST /api/v1/applications/{id}/git-host-key/reset", canWrite, a.resetGitHostKey)
	a.route(mux, "POST /api/v1/applications/image-ports", canWrite, a.imagePorts)
	a.route(mux, "GET /api/v1/applications/{id}/tasks", canRead, a.listTasks)
	a.route(mux, "POST /api/v1/applications/{id}/tasks", canWrite, a.createTask)
	a.route(mux, "PATCH /api/v1/tasks/{id}", canWrite, a.updateTask)
	a.route(mux, "DELETE /api/v1/tasks/{id}", canWrite, a.deleteTask)
	a.route(mux, "POST /api/v1/tasks/{id}/run", canDeploy, a.runTask)
	a.route(mux, "GET /api/v1/tasks/{id}/executions", canRead, a.taskExecutions)
	a.route(mux, "GET /api/v1/deployments", canRead, a.listDeployments)
	a.route(mux, "GET /api/v1/deployments/{id}", canRead, a.getDeployment)
	a.route(mux, "POST /api/v1/deployments/{id}/cancel", canDeploy, a.cancelDeployment)
	a.route(mux, "GET /api/v1/deployments/{id}/logs", canRead, a.deploymentLogs)
	a.route(mux, "GET /api/v1/deployments/{id}/logs/stream", canRead, a.streamDeploymentLogs)
	a.registerEnvRoutes(mux, "applications", envvar.OwnerApplication)
}

func (a *API) listApplications(w http.ResponseWriter, r *http.Request) error {
	apps, err := a.Applications.List(r.Context(), r.URL.Query().Get("environment_id"))
	if err != nil {
		return err
	}
	return ok(w, allForCaller(r, apps))
}

// applicationDefaults lets the UI prefill the creation form for a build pack.
func (a *API) applicationDefaults(w http.ResponseWriter, r *http.Request) error {
	pack := application.BuildPack(r.URL.Query().Get("pack"))
	if pack == "" {
		pack = application.PackDockerfile
	}
	return ok(w, application.DefaultConfig(pack))
}

func (a *API) getApplication(w http.ResponseWriter, r *http.Request) error {
	app, err := a.Applications.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, forCaller(r, app))
}

func (a *API) createApplication(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[application.CreateInput](w, r)
	if err != nil {
		return err
	}
	if err := a.authorizeHostOptions(r, input.Config); err != nil {
		return err
	}
	if _, err := a.Projects.GetEnvironment(r.Context(), input.EnvironmentID); err != nil && input.EnvironmentID != "" {
		return err
	}
	if _, err := a.Servers.Get(r.Context(), input.ServerID); err != nil && input.ServerID != "" {
		return err
	}
	app, err := a.Applications.Create(r.Context(), input)
	if err != nil {
		return err
	}
	return created(w, app)
}

func (a *API) updateApplication(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[application.UpdateInput](w, r)
	if err != nil {
		return err
	}
	if err := a.authorizeHostOptions(r, input.Config); err != nil {
		return err
	}
	app, err := a.Applications.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return ok(w, app)
}

func (a *API) deleteApplication(w http.ResponseWriter, r *http.Request) error {
	app, err := a.Applications.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	if err := a.checkDestroy(r, app.EnvironmentID); err != nil {
		return err
	}
	err = a.Applications.Delete(r.Context(), r.PathValue("id"), queryFlag(r, "volumes"), queryFlag(r, "force"))
	if err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) deployApplication(w http.ResponseWriter, r *http.Request) error {
	request, err := decodeOptional[deployRequest](w, r)
	if err != nil {
		return err
	}
	deployment, err := a.Applications.DeployRef(r.Context(), r.PathValue("id"), deployTrigger(r), request.Ref)
	if err != nil {
		return err
	}
	return accepted(w, deployment)
}

type rollbackRequest struct {
	DeploymentID string `json:"deployment_id"`
}

func (a *API) rollbackApplication(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[rollbackRequest](w, r)
	if err != nil {
		return err
	}
	deployment, err := a.Applications.Rollback(r.Context(), r.PathValue("id"), request.DeploymentID)
	if err != nil {
		return err
	}
	return accepted(w, deployment)
}

func (a *API) operateApplication(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "start":
		err = a.Applications.Start(r.Context(), id)
	case "stop":
		err = a.Applications.Stop(r.Context(), id)
	case "restart":
		err = a.Applications.Restart(r.Context(), id)
	default:
		err = apperr.NotFound("Ação")
	}
	if err != nil {
		return err
	}
	return a.getApplication(w, r)
}

func (a *API) listApplicationDeployments(w http.ResponseWriter, r *http.Request) error {
	deployments, err := a.Applications.Deployments(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, deployments)
}

func (a *API) applicationContainers(w http.ResponseWriter, r *http.Request) error {
	containers, err := a.Applications.Containers(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, containers)
}

type webhookResponse struct {
	Secret    string `json:"secret"`
	DeployURL string `json:"deploy_url"`
	GitHubURL string `json:"github_url"`
	GitLabURL string `json:"gitlab_url"`
	GiteaURL  string `json:"gitea_url"`
	// BitbucketURL is the Bitbucket Cloud webhook (repo:push, pull requests).
	BitbucketURL string `json:"bitbucket_url"`
}

func (a *API) getWebhook(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	secret, err := a.Applications.WebhookSecret(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, a.webhookResponse(r, secret))
}

func (a *API) rotateWebhook(w http.ResponseWriter, r *http.Request) error {
	secret, err := a.Applications.RotateWebhookSecret(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, a.webhookResponse(r, secret))
}

func (a *API) webhookResponse(r *http.Request, secret string) webhookResponse {
	base := a.publicURL(r) + "/webhooks/applications/" + r.PathValue("id") + "/"
	return webhookResponse{
		Secret: secret, DeployURL: base + "deploy", GitHubURL: base + "github",
		GitLabURL: base + "gitlab", GiteaURL: base + "gitea", BitbucketURL: base + "bitbucket",
	}
}

// publicURL prefers the configured address and falls back to the request's.
func (a *API) publicURL(r *http.Request) string {
	if current, err := a.Settings.Get(r.Context()); err == nil && current.PublicURL != "" {
		return current.PublicURL
	}
	scheme := "http"
	if a.proxies.isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (a *API) listTasks(w http.ResponseWriter, r *http.Request) error {
	tasks, err := a.Tasks.List(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, tasks)
}

func (a *API) createTask(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[schedule.TaskInput](w, r)
	if err != nil {
		return err
	}
	task, err := a.Tasks.Create(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return created(w, task)
}

func (a *API) updateTask(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[schedule.TaskInput](w, r)
	if err != nil {
		return err
	}
	task, err := a.Tasks.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return ok(w, task)
}

func (a *API) deleteTask(w http.ResponseWriter, r *http.Request) error {
	if err := a.Tasks.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) runTask(w http.ResponseWriter, r *http.Request) error {
	execution, err := a.Tasks.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, execution)
}

func (a *API) taskExecutions(w http.ResponseWriter, r *http.Request) error {
	raw := queryFlag(r, "raw")
	if raw {
		if err := requireSensitive(r); err != nil {
			return err
		}
	}
	executions, err := a.Tasks.Executions(r.Context(), r.PathValue("id"), raw)
	if err != nil {
		return err
	}
	return ok(w, executions)
}

func (a *API) listDeployments(w http.ResponseWriter, r *http.Request) error {
	deployments, err := a.Applications.Deployments(r.Context(), "")
	if err != nil {
		return err
	}
	return ok(w, deployments)
}

func (a *API) getDeployment(w http.ResponseWriter, r *http.Request) error {
	deployment, err := a.Applications.Deployment(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, deployment)
}

func (a *API) cancelDeployment(w http.ResponseWriter, r *http.Request) error {
	if err := a.Applications.CancelDeployment(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) deploymentLogs(w http.ResponseWriter, r *http.Request) error {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if _, err := a.Applications.Deployment(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	entries, err := a.Applications.DeploymentLogs(r.Context(), r.PathValue("id"), after)
	if err != nil {
		return err
	}
	return ok(w, entries)
}

// streamDeploymentLogs replays the persisted log and then follows it live.
// Subscribing before reading history guarantees no line is lost in between;
// ids deduplicate the overlap.
func (a *API) streamDeploymentLogs(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	deploymentID := r.PathValue("id")
	if _, err := a.Applications.Deployment(ctx, deploymentID); err != nil {
		return err
	}
	lastID, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	live, unsubscribe := a.LogHub.Subscribe(deploymentID)
	defer unsubscribe()
	stream := newEventStream(w)

	flushPersisted := func() error {
		entries, err := a.Applications.DeploymentLogs(ctx, deploymentID, lastID)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := stream.send("log", entry.ID, entry); err != nil {
				return err
			}
			lastID = entry.ID
		}
		return nil
	}
	if err := flushPersisted(); err != nil {
		return nil
	}
	poll := time.NewTicker(deploymentPollInterval)
	defer poll.Stop()
	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case entry := <-live:
			if entry.ID <= lastID {
				continue
			}
			if err := stream.send("log", entry.ID, entry); err != nil {
				return nil
			}
			lastID = entry.ID
		case <-heartbeat.C:
			if err := stream.heartbeat(); err != nil {
				return nil
			}
		case <-poll.C:
			deployment, err := a.Applications.Deployment(ctx, deploymentID)
			if err != nil {
				return nil
			}
			// The poll also recovers lines a slow subscriber may have dropped.
			if err := flushPersisted(); err != nil {
				return nil
			}
			if deployment.Status.Terminal() {
				_ = stream.send("done", 0, deployment)
				return nil
			}
		}
	}
}

// registerEnvRoutes exposes the variables of a resource type under
// /api/v1/<collection>/{id}/envs.
func (a *API) registerEnvRoutes(mux *http.ServeMux, collection string, ownerType envvar.OwnerType) {
	owner := func(r *http.Request) envvar.Owner { return envvar.Owner{Type: ownerType, ID: r.PathValue("id")} }
	base := "/api/v1/" + collection + "/{id}/envs"

	a.route(mux, "GET "+base, canRead, func(w http.ResponseWriter, r *http.Request) error {
		if err := requireSensitive(r); err != nil {
			return err
		}
		variables, err := a.Env.List(r.Context(), owner(r))
		if err != nil {
			return err
		}
		return ok(w, variables)
	})
	a.route(mux, "POST "+base, canWrite, func(w http.ResponseWriter, r *http.Request) error {
		variable, err := decode[envvar.Variable](w, r)
		if err != nil {
			return err
		}
		if err := a.Env.Set(r.Context(), owner(r), variable.Key, variable.Value, variable.BuildTime, variable.SecretBuild); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "PUT "+base, canWrite, func(w http.ResponseWriter, r *http.Request) error {
		request, err := decode[bulkEnvRequest](w, r)
		if err != nil {
			return err
		}
		variables := request.Variables
		if request.Variables == nil {
			if variables, err = a.parseDotenv(r, owner(r), request.Dotenv); err != nil {
				return err
			}
		}
		if err := a.Env.ReplaceAll(r.Context(), owner(r), variables); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "DELETE "+base+"/{env}", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		if err := a.Env.Delete(r.Context(), owner(r), r.PathValue("env")); err != nil {
			return err
		}
		return noContent(w)
	})
}

type bulkEnvRequest struct {
	Variables []envvar.Variable `json:"variables"`
	// Dotenv is used when Variables is absent: the .env text is parsed here.
	Dotenv string `json:"dotenv"`
}

// parseDotenv reads .env text. The format cannot express the build-time
// flag, so keys that already exist keep the flag they had.
func (a *API) parseDotenv(r *http.Request, owner envvar.Owner, text string) ([]envvar.Variable, error) {
	variables, err := envvar.ParseDotenv(text)
	if err != nil {
		return nil, err
	}
	existing, err := a.Env.List(r.Context(), owner)
	if err != nil {
		return nil, err
	}
	flags := map[string]envvar.Variable{}
	for _, variable := range existing {
		flags[variable.Key] = variable
	}
	for index := range variables {
		variables[index].BuildTime = flags[variables[index].Key].BuildTime
		variables[index].SecretBuild = flags[variables[index].Key].SecretBuild
	}
	return variables, nil
}

func (a *API) registerWebhookRoutes(mux *http.ServeMux) {
	a.public(mux, "POST /webhooks/applications/{id}/deploy", a.deployHook)
	a.public(mux, "POST /webhooks/applications/{id}/{provider}", a.gitPushHook)
}

func (a *API) deployHook(w http.ResponseWriter, r *http.Request) error {
	secret := r.Header.Get("X-Pier-Token")
	if bearer, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found {
		secret = bearer
	}
	result, err := a.Applications.HandleDeployHook(r.Context(), r.PathValue("id"), strings.TrimSpace(secret))
	if err != nil {
		return err
	}
	return accepted(w, result)
}

func (a *API) gitPushHook(w http.ResponseWriter, r *http.Request) error {
	provider := application.GitProvider(r.PathValue("provider"))
	switch provider {
	case application.ProviderGitHub, application.ProviderGitLab, application.ProviderGitea, application.ProviderBitbucket:
	default:
		return apperr.NotFound("Provedor de webhook")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return err
	}
	result, err := a.Applications.HandleGitPush(r.Context(), r.PathValue("id"), provider, r.Header, body)
	if err != nil {
		return err
	}
	return ok(w, result)
}

func (a *API) resetGitHostKey(w http.ResponseWriter, r *http.Request) error {
	if err := a.Applications.ResetGitHostKey(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}
func (a *API) imagePorts(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[struct {
		ServerID string `json:"server_id"`
		Image    string `json:"image"`
	}](w, r)
	if err != nil {
		return err
	}
	ports, err := a.Applications.ImagePorts(r.Context(), input.ServerID, input.Image)
	if err != nil {
		return err
	}
	return ok(w, ports)
}
