package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/notify"
	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
	"pier/internal/server"
)

const (
	retainedImages = 5
	cleanupTimeout = 60 * time.Second
)

// Connector reaches the server an application runs on.
type Connector interface {
	Executor(ctx context.Context, serverID string) (executor.Executor, server.Server, error)
}

// KeyVault hands out deploy keys for private repositories.
type KeyVault interface {
	Material(ctx context.Context, id string) (string, error)
}

// job is the working state of one deployment being executed.
type job struct {
	app        Application
	deployment Deployment
	server     server.Server
	ex         executor.Executor
	log        *deployLog
	env        []envvar.Variable
}

// Deployer executes deployments: fetch source, build image, release.
type Deployer struct {
	store     *Store
	env       *envvar.Store
	connector Connector
	keys      KeyVault
	events    notify.Publisher
	hub       *LogHub
	logger    *slog.Logger
}

// NewDeployer creates the deployer.
func NewDeployer(store *Store, env *envvar.Store, connector Connector, keys KeyVault,
	events notify.Publisher, hub *LogHub, logger *slog.Logger) *Deployer {
	return &Deployer{store: store, env: env, connector: connector, keys: keys, events: events, hub: hub, logger: logger}
}

// Run executes a claimed deployment to completion and records the outcome.
func (d *Deployer) Run(ctx context.Context, deploymentID string) {
	log := &deployLog{store: d.store, hub: d.hub, deploymentID: deploymentID}
	deployment, err := d.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		d.logger.Error("load deployment", "deployment", deploymentID, "error", err)
		return
	}
	startedAt := time.Now()
	err = d.execute(ctx, deployment, log)
	background := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		log.Success("Deploy concluído em %s.", time.Since(startedAt).Round(time.Second))
		d.finish(background, deployment, DeploymentFinished, "")
	case errors.Is(ctx.Err(), context.Canceled):
		log.Error("Deploy cancelado.")
		d.finish(background, deployment, DeploymentCancelled, "Cancelado pelo usuário.")
	default:
		reason := describeFailure(err)
		log.Error("Deploy falhou: %s", reason)
		d.finish(background, deployment, DeploymentFailed, reason)
	}
}

// describeFailure turns infrastructure errors the operator can act on into
// instructions; anything else is reported as is.
func describeFailure(err error) string {
	if errors.Is(err, executor.ErrHostKeyMismatch) {
		return "A chave de host SSH do servidor mudou desde o primeiro acesso, então a conexão foi recusada. " +
			"Se a máquina foi reinstalada, redefina a chave em Servidores › Manutenção e valide a conexão."
	}
	return err.Error()
}

func (d *Deployer) finish(ctx context.Context, deployment Deployment, status DeploymentStatus, failure string) {
	if err := d.store.finishDeployment(ctx, deployment.ID, status, failure); err != nil {
		d.logger.Error("finish deployment", "deployment", deployment.ID, "error", err)
	}
	switch status {
	case DeploymentFinished:
		d.events.Publish(notify.Event{
			Type: notify.DeploymentSucceeded, Resource: deployment.ApplicationName,
			Title:   "Deploy concluído: " + deployment.ApplicationName,
			Message: "A aplicação " + deployment.ApplicationName + " foi implantada com sucesso.",
		})
	case DeploymentFailed:
		d.events.Publish(notify.Event{
			Type: notify.DeploymentFailed, Resource: deployment.ApplicationName,
			Title:   "Deploy falhou: " + deployment.ApplicationName,
			Message: failure,
		})
	}
}

func (d *Deployer) execute(ctx context.Context, deployment Deployment, log *deployLog) error {
	app, err := d.store.Get(ctx, deployment.ApplicationID)
	if err != nil {
		return fmt.Errorf("carregar aplicação: %w", err)
	}
	variables, err := d.env.List(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: app.ID})
	if err != nil {
		return fmt.Errorf("carregar variáveis de ambiente: %w", err)
	}
	for _, variable := range variables {
		log.redact(variable.Value)
	}
	log.Info("Iniciando deploy de %q (%s).", app.Name, deployment.Trigger)
	ex, target, err := d.connector.Executor(ctx, app.ServerID)
	if err != nil {
		return fmt.Errorf("conectar ao servidor: %w", err)
	}
	log.Info("Servidor de destino: %s.", target.Name)
	if err := docker.EnsureNetwork(ctx, ex); err != nil {
		return err
	}
	j := &job{app: app, deployment: deployment, server: target, ex: ex, log: log, env: variables}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		j.cleanupBuildDir(cleanupCtx)
	}()

	image, err := d.produceImage(ctx, j)
	if err != nil {
		return err
	}
	if err := d.store.setDeploymentImage(ctx, deployment.ID, image); err != nil {
		return err
	}
	env := make(map[string]string, len(variables))
	for _, variable := range variables {
		env[variable.Key] = variable.Value
	}
	if err := j.release(ctx, image, env); err != nil {
		return err
	}
	if err := d.store.setCurrentImage(ctx, app.ID, image); err != nil {
		return err
	}
	d.pruneImages(context.WithoutCancel(ctx), j)
	return nil
}

// produceImage returns the image to run: the one being rolled back to, or a
// freshly built (or pulled) one.
func (d *Deployer) produceImage(ctx context.Context, j *job) (string, error) {
	if image := j.deployment.RollbackImage; image != "" {
		j.log.Info("Rollback para a imagem %s (sem novo build).", image)
		if !docker.ImageExists(ctx, j.ex, image) {
			return "", fmt.Errorf("a imagem %s não está mais disponível no servidor", image)
		}
		return image, nil
	}
	if j.app.Config.Build.Pack != PackImage {
		if err := d.checkout(ctx, j); err != nil {
			return "", err
		}
	}
	strategy, err := builderFor(j.app.Config.Build.Pack)
	if err != nil {
		return "", err
	}
	return strategy.build(ctx, j)
}

func (d *Deployer) checkout(ctx context.Context, j *job) error {
	privateKey := ""
	if keyID := j.app.Config.Source.PrivateKeyID; keyID != "" {
		material, err := d.keys.Material(ctx, keyID)
		if err != nil {
			return fmt.Errorf("carregar chave de deploy: %w", err)
		}
		privateKey = material
	}
	sha, message, err := j.fetchSource(ctx, privateKey)
	if err != nil {
		return err
	}
	j.deployment.CommitSHA, j.deployment.CommitMessage = sha, message
	j.log.Info("Commit %s — %s", shortSHA(sha), message)
	return d.store.setDeploymentCommit(ctx, j.deployment.ID, sha, message)
}

// pruneImages keeps only the images of the most recent successful
// deployments, which are the ones a rollback can target.
func (d *Deployer) pruneImages(ctx context.Context, j *job) {
	if j.app.Config.Build.Pack == PackImage {
		return
	}
	_, expired, err := d.store.retainedImages(ctx, j.app.ID, retainedImages)
	if err != nil || len(expired) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()
	// Without --force, docker refuses to remove an image still in use.
	_, _ = j.ex.Run(ctx, shell.Join(append([]string{"docker", "rmi"}, expired...)...)+" 2>/dev/null || true")
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
