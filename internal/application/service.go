package application

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/idgen"
	"pier/internal/platform/shell"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

const deploymentListLimit = 50

// CreateInput carries what is needed to register an application.
type CreateInput struct {
	EnvironmentID string `json:"environment_id"`
	ServerID      string `json:"server_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Config        Config `json:"config"`
}

// UpdateInput carries the editable fields of an application.
type UpdateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Config      Config `json:"config"`
}

// Service implements the application use cases.
type Service struct {
	store      *Store
	env        *envvar.Store
	connector  Connector
	dispatcher *Dispatcher
}

// NewService creates the service.
func NewService(store *Store, env *envvar.Store, connector Connector, dispatcher *Dispatcher) *Service {
	return &Service{store: store, env: env, connector: connector, dispatcher: dispatcher}
}

// List returns applications, optionally of one environment.
func (s *Service) List(ctx context.Context, environmentID string) ([]Application, error) {
	return s.store.List(ctx, environmentID)
}

// Get returns one application.
func (s *Service) Get(ctx context.Context, id string) (Application, error) {
	app, err := s.store.Get(ctx, id)
	return app, notFound(err, "Aplicação")
}

// Create registers an application; nothing runs until the first deployment.
func (s *Service) Create(ctx context.Context, input CreateInput) (Application, error) {
	now := time.Now()
	app := Application{
		ID: idgen.New(), EnvironmentID: input.EnvironmentID, ServerID: input.ServerID,
		Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description),
		Config: input.Config, Status: docker.StatusStopped, CreatedAt: now, UpdatedAt: now,
	}
	fields := apperr.Fields{}
	if app.Name == "" {
		fields.Add("name", "Informe o nome da aplicação.")
	}
	if app.EnvironmentID == "" {
		fields.Add("environment_id", "Selecione o ambiente.")
	}
	if app.ServerID == "" {
		fields.Add("server_id", "Selecione o servidor.")
	}
	if err := mergeFieldErrors(fields, app.Config.Validate()); err != nil {
		return Application{}, err
	}
	if err := s.store.insert(ctx, app, idgen.Token(24)); err != nil {
		return Application{}, err
	}
	return app, nil
}

// Update changes the definition; it takes effect on the next deployment.
func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Application, error) {
	app, err := s.Get(ctx, id)
	if err != nil {
		return Application{}, err
	}
	app.Name = strings.TrimSpace(input.Name)
	app.Description = strings.TrimSpace(input.Description)
	app.Config = input.Config
	fields := apperr.Fields{}
	if app.Name == "" {
		fields.Add("name", "Informe o nome da aplicação.")
	}
	if err := mergeFieldErrors(fields, app.Config.Validate()); err != nil {
		return Application{}, err
	}
	if err := s.store.update(ctx, app); err != nil {
		return Application{}, notFound(err, "Aplicação")
	}
	return s.Get(ctx, id)
}

// Delete stops the application and erases it. With force, an unreachable
// server does not block the removal of the record.
func (s *Service) Delete(ctx context.Context, id string, deleteVolumes, force bool) error {
	app, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.cancelActiveDeployments(ctx, id); err != nil {
		return err
	}
	if err := s.removeFromServer(ctx, app, deleteVolumes); err != nil && !force {
		return apperr.Unavailable("Não foi possível limpar o servidor (%s). Use a remoção forçada para "+
			"apagar apenas o registro.", err)
	}
	if err := s.env.DeleteAll(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: id}); err != nil {
		return err
	}
	return s.store.delete(ctx, id)
}

func (s *Service) cancelActiveDeployments(ctx context.Context, applicationID string) error {
	for _, status := range []DeploymentStatus{DeploymentQueued, DeploymentInProgress} {
		deployments, err := s.store.deploymentsByStatus(ctx, status)
		if err != nil {
			return err
		}
		for _, deployment := range deployments {
			if deployment.ApplicationID != applicationID {
				continue
			}
			if err := s.dispatcher.Cancel(ctx, deployment.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) removeFromServer(ctx context.Context, app Application, deleteVolumes bool) error {
	ex, target, err := s.connector.Executor(ctx, app.ServerID)
	if err != nil {
		return err
	}
	containers, err := docker.ListByResource(ctx, ex, app.ID)
	if err != nil {
		return err
	}
	if err := docker.Remove(ctx, ex, containers); err != nil {
		return err
	}
	server.LogEdgeSync(ctx, ex, target)
	if deleteVolumes {
		for _, storage := range app.Config.Storages {
			if storage.Kind == StorageVolume {
				_, _ = ex.Run(ctx, shell.Join("docker", "volume", "rm", VolumeName(app.ID, storage.Source)))
			}
		}
	}
	removeImages(ctx, ex, app.ID)
	project := compose.Project{Name: app.ID, Dir: target.ResourceDir(docker.TypeApplication, app.ID)}
	return project.RemoveDir(ctx, ex)
}

func removeImages(ctx context.Context, ex executor.Executor, applicationID string) {
	output, err := ex.Run(ctx, shell.Join("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", "pier/"+applicationID))
	if err != nil || output == "" {
		return
	}
	images := strings.Fields(output)
	_, _ = ex.Run(ctx, shell.Join(append([]string{"docker", "rmi", "--force"}, images...)...))
}

// Deploy queues a new deployment. A deployment already waiting in the queue
// is reused, so bursts of pushes do not pile up builds.
func (s *Service) Deploy(ctx context.Context, applicationID string, trigger Trigger) (Deployment, error) {
	if _, err := s.Get(ctx, applicationID); err != nil {
		return Deployment{}, err
	}
	if queued, err := s.store.queuedDeployment(ctx, applicationID); err == nil {
		return queued, nil
	} else if !errors.Is(err, sqlite.ErrNotFound) {
		return Deployment{}, err
	}
	return s.enqueue(ctx, Deployment{ApplicationID: applicationID, Trigger: trigger})
}

// Rollback queues a deployment that re-releases the image of a previous one.
func (s *Service) Rollback(ctx context.Context, applicationID, deploymentID string) (Deployment, error) {
	target, err := s.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		return Deployment{}, notFound(err, "Deploy")
	}
	if target.ApplicationID != applicationID {
		return Deployment{}, apperr.NotFound("Deploy")
	}
	if target.Status != DeploymentFinished || target.Image == "" {
		return Deployment{}, apperr.Conflict("Só é possível reverter para um deploy concluído com sucesso.")
	}
	return s.enqueue(ctx, Deployment{
		ApplicationID: applicationID, Trigger: TriggerRollback, RollbackImage: target.Image,
		CommitSHA: target.CommitSHA, CommitMessage: target.CommitMessage,
	})
}

func (s *Service) enqueue(ctx context.Context, deployment Deployment) (Deployment, error) {
	deployment.ID = idgen.New()
	deployment.Status = DeploymentQueued
	deployment.CreatedAt = time.Now()
	if err := s.store.insertDeployment(ctx, deployment); err != nil {
		return Deployment{}, err
	}
	if err := s.dispatcher.Enqueue(deployment.ID); err != nil {
		_ = s.store.finishDeployment(ctx, deployment.ID, DeploymentFailed, err.Error())
		return Deployment{}, err
	}
	return s.store.GetDeployment(ctx, deployment.ID)
}

// CancelDeployment stops a queued or running deployment.
func (s *Service) CancelDeployment(ctx context.Context, deploymentID string) error {
	deployment, err := s.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		return notFound(err, "Deploy")
	}
	if deployment.Status.Terminal() {
		return apperr.Conflict("Este deploy já foi encerrado.")
	}
	return s.dispatcher.Cancel(ctx, deploymentID)
}

// Deployment returns one deployment.
func (s *Service) Deployment(ctx context.Context, id string) (Deployment, error) {
	deployment, err := s.store.GetDeployment(ctx, id)
	return deployment, notFound(err, "Deploy")
}

// Deployments returns recent deployments, optionally of one application.
func (s *Service) Deployments(ctx context.Context, applicationID string) ([]Deployment, error) {
	return s.store.ListDeployments(ctx, applicationID, deploymentListLimit)
}

// DeploymentLogs returns log lines after the given id.
func (s *Service) DeploymentLogs(ctx context.Context, deploymentID string, afterID int64) ([]LogEntry, error) {
	return s.store.Logs(ctx, deploymentID, afterID)
}

// Containers lists the containers of the application on its server.
func (s *Service) Containers(ctx context.Context, id string) ([]docker.Container, error) {
	_, containers, err := s.reach(ctx, id)
	return containers, err
}

// Stop stops the running containers without removing them.
func (s *Service) Stop(ctx context.Context, id string) error {
	return s.operate(ctx, id, docker.StatusStopped, docker.Stop)
}

// Start starts containers previously stopped.
func (s *Service) Start(ctx context.Context, id string) error {
	return s.operate(ctx, id, docker.StatusRunning, docker.Start)
}

// Restart restarts the containers in place (no new build).
func (s *Service) Restart(ctx context.Context, id string) error {
	return s.operate(ctx, id, docker.StatusRunning, docker.Restart)
}

type containerOperation func(ctx context.Context, ex executor.Executor, containers []docker.Container) error

func (s *Service) operate(ctx context.Context, id, resultingStatus string, operation containerOperation) error {
	ex, containers, err := s.reach(ctx, id)
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return apperr.Conflict("A aplicação ainda não possui containers. Faça um deploy primeiro.")
	}
	if err := operation(ctx, ex, containers); err != nil {
		return apperr.Unavailable("A operação falhou: %s", err)
	}
	return s.store.SetStatus(ctx, id, resultingStatus)
}

func (s *Service) reach(ctx context.Context, id string) (executor.Executor, []docker.Container, error) {
	app, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	ex, _, err := s.connector.Executor(ctx, app.ServerID)
	if err != nil {
		return nil, nil, err
	}
	containers, err := docker.ListByResource(ctx, ex, id)
	if err != nil {
		return nil, nil, apperr.Unavailable("Não foi possível consultar o servidor: %s", err)
	}
	return ex, containers, nil
}

// WebhookSecret returns the secret that authenticates webhooks of the application.
func (s *Service) WebhookSecret(ctx context.Context, id string) (string, error) {
	secret, err := s.store.webhookSecret(ctx, id)
	return secret, notFound(err, "Aplicação")
}

// RotateWebhookSecret replaces the webhook secret, invalidating the old one.
func (s *Service) RotateWebhookSecret(ctx context.Context, id string) (string, error) {
	secret := idgen.Token(24)
	return secret, notFound(s.store.setWebhookSecret(ctx, id, secret), "Aplicação")
}

// HandleDeployHook deploys when the caller presents the application secret.
func (s *Service) HandleDeployHook(ctx context.Context, id, presentedSecret string) (WebhookResult, error) {
	secret, err := s.WebhookSecret(ctx, id)
	if err != nil || !constantTimeEqual(presentedSecret, secret) {
		return WebhookResult{}, apperr.Unauthorized("Segredo de webhook inválido.")
	}
	deployment, err := s.Deploy(ctx, id, TriggerWebhook)
	if err != nil {
		return WebhookResult{}, err
	}
	return WebhookResult{Deployed: true, DeploymentID: deployment.ID, Reason: "Deploy enfileirado."}, nil
}

// HandleGitPush processes a push webhook from a Git provider: it deploys only
// when the signature is valid, auto deploy is on and the branch matches.
func (s *Service) HandleGitPush(ctx context.Context, id string, provider GitProvider,
	header http.Header, body []byte) (WebhookResult, error) {
	secret, err := s.WebhookSecret(ctx, id)
	if err != nil || !verifyWebhook(provider, secret, header, body) {
		return WebhookResult{}, apperr.Unauthorized("Assinatura do webhook inválida.")
	}
	if !isPushEvent(provider, header) {
		return WebhookResult{Reason: "Evento ignorado: apenas push dispara deploy."}, nil
	}
	app, err := s.Get(ctx, id)
	if err != nil {
		return WebhookResult{}, err
	}
	if !app.Config.AutoDeploy {
		return WebhookResult{Reason: "Deploy automático desativado para esta aplicação."}, nil
	}
	branch, ok := pushedBranch(body)
	if !ok || branch != app.Config.Source.Branch {
		return WebhookResult{Reason: "Push em outra branch: nada a fazer."}, nil
	}
	deployment, err := s.Deploy(ctx, id, TriggerWebhook)
	if err != nil {
		return WebhookResult{}, err
	}
	return WebhookResult{Deployed: true, DeploymentID: deployment.ID, Reason: "Deploy enfileirado."}, nil
}

func mergeFieldErrors(fields apperr.Fields, err error) error {
	if err == nil {
		return fields.Err()
	}
	appErr, ok := apperr.As(err)
	if !ok {
		return err
	}
	for field, message := range appErr.Fields {
		fields.Add(field, "%s", message)
	}
	return fields.Err()
}

func notFound(err error, what string) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound(what)
	}
	return err
}
