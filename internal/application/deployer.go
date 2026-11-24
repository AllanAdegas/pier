package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/notify"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
	"pier/internal/registry"
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
	app         Application
	deployment  Deployment
	server      server.Server
	ex          executor.Executor
	log         *deployLog
	env         []envvar.Variable
	domains     DomainChecker
	gitProvider GitCredentials
	registries  *registry.Service
}

// Deployer executes deployments: fetch source, build image, release.
type Deployer struct {
	multi     *MultiServer
	images    *registry.Service
	store     *Store
	env       *envvar.Store
	connector Connector
	keys      KeyVault
	events    notify.Publisher
	hub       *LogHub
	logger    *slog.Logger
	archives  archiveStore
	domains   DomainChecker
	// previewReporter tells the Git provider about previews; nil reports nothing.
	previewReporter PreviewReporter
	gitProvider     GitCredentials
	// deploymentReporter sets commit statuses; nil reports nothing.
	deploymentReporter DeploymentReporter
	// search tells search engines about the pages; nil notifies nobody.
	search SearchNotifier
}

// SetArchiveDir sets where uploaded source archives wait for their deployment.
func (d *Deployer) SetArchiveDir(dir string) { d.archives = archiveStore{dir: dir} }

// NewDeployer creates the deployer.
func NewDeployer(store *Store, env *envvar.Store, connector Connector, keys KeyVault,
	events notify.Publisher, hub *LogHub, logger *slog.Logger) *Deployer {
	return &Deployer{store: store, env: env, connector: connector, keys: keys, events: events, hub: hub, logger: logger}
}

// Run executes a claimed deployment to completion and records the outcome.
func (d *Deployer) Run(ctx context.Context, deploymentID string) {
	defer d.archives.remove(deploymentID)
	log := &deployLog{store: d.store, hub: d.hub, deploymentID: deploymentID}
	// Loaded even when ctx is already done (cancelled or out of time before
	// it started), so the outcome is still recorded.
	deployment, err := d.store.GetDeployment(context.WithoutCancel(ctx), deploymentID)
	if err != nil {
		d.logger.Error("load deployment", "deployment", deploymentID, "error", err)
		return
	}
	startedAt := time.Now()
	err = d.execute(ctx, deployment, log)
	background := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		d.notifySearchEngines(background, deployment, log)
		log.Success("Deploy concluído em %s.", time.Since(startedAt).Round(time.Second))
		d.finish(background, deployment, DeploymentFinished, "")
	case errors.Is(ctx.Err(), context.Canceled):
		reason := i18n.Instance().Text("Interrompido pela reinicialização do Pier.")
		if errors.Is(context.Cause(ctx), errUserCancelled) {
			reason = i18n.Instance().Text("Cancelado pelo usuário.")
			log.Error("Deploy cancelado.")
		}
		if errors.Is(context.Cause(ctx), errApplicationDeleting) {
			reason = i18n.Instance().Text("Aplicação em remoção.")
		}
		log.Error("%s", reason)
		d.finish(background, deployment, DeploymentCancelled, reason)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		reason := i18n.Instance().Text("O deploy passou do tempo limite e foi interrompido (PIER_DEPLOY_TIMEOUT).")
		log.Error("Deploy falhou: %s", reason)
		d.explainFailure(background, deployment.ID, log, err)
		d.finish(background, deployment, DeploymentFailed, reason)
	default:
		// Command output in the error is masked like the log it came from.
		reason := log.mask(describeFailure(err))
		log.Error("Deploy falhou: %s", reason)
		d.explainFailure(background, deployment.ID, log, err)
		d.finish(background, deployment, DeploymentFailed, reason)
	}
}

// describeFailure turns infrastructure errors the operator can act on into
// instructions; anything else is reported as is. The reason is kept with the
// deployment, in the language of the instance.
func describeFailure(err error) string {
	language := i18n.Instance()
	if errors.Is(err, executor.ErrHostKeyMismatch) {
		return language.Text("A chave de host SSH do servidor mudou desde o primeiro acesso, então a conexão foi recusada. " +
			"Se a máquina foi reinstalada, redefina a chave em Servidores › Manutenção e valide a conexão.")
	}
	return i18n.Localize(err, language)
}

func (d *Deployer) finish(ctx context.Context, deployment Deployment, status DeploymentStatus, failure string) {
	if err := d.store.finishDeployment(ctx, deployment.ID, status, failure); err != nil {
		d.logger.Error("finish deployment", "deployment", deployment.ID, "error", err)
	}
	d.reportDeploymentFinished(ctx, deployment, status)
	if deployment.Trigger == TriggerPreview {
		d.finishPreview(ctx, deployment, status, failure)
		return
	}
	switch status {
	case DeploymentFinished:
		d.events.Publish(notify.Event{
			Type: notify.DeploymentSucceeded, Resource: deployment.ApplicationName,
			Title:   i18n.Instance().Sprintf("Deploy concluído: %s", deployment.ApplicationName),
			Message: i18n.Instance().Sprintf("A aplicação %s foi implantada com sucesso.", deployment.ApplicationName),
		})
	case DeploymentFailed:
		d.events.Publish(notify.Event{
			Type: notify.DeploymentFailed, Resource: deployment.ApplicationName,
			Title:   i18n.Instance().Sprintf("Deploy falhou: %s", deployment.ApplicationName),
			Message: failure,
		})
	}
}

func (d *Deployer) execute(ctx context.Context, deployment Deployment, log *deployLog) error {
	if deployment.Trigger == TriggerPreview {
		return d.executePreview(ctx, deployment, log)
	}
	app, err := d.store.Get(ctx, deployment.ApplicationID)
	if err != nil {
		return i18n.Errorf("carregar aplicação: %w", err)
	}
	if err := d.checkDeployPolicy(ctx, app); err != nil {
		return err
	}
	if handled, err := d.placementHook(ctx, deployment, app, log); handled {
		return err
	}
	log.repository(app.Config.Source.Repository)
	variables, err := d.env.ListForDeploy(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: app.ID}, app.EnvironmentID, app.ServerID)
	if err != nil {
		return i18n.Errorf("carregar variáveis de ambiente: %w", err)
	}
	for _, variable := range variables {
		log.redact(variable.Value)
	}
	log.Info("Iniciando deploy de %q (%s).", app.Name, deployment.Trigger)
	if err := server.CheckConnectorDeployment(ctx, d.connector, app.ServerID); err != nil {
		return err
	}
	ex, target, err := d.connector.Executor(ctx, app.ServerID)
	if err != nil {
		return i18n.Errorf("conectar ao servidor: %w", err)
	}
	log.redact(target.ProxyToken)
	log.Info("Servidor de destino: %s.", target.Name)
	if err := docker.EnsureNetwork(ctx, ex); err != nil {
		return err
	}
	if err := d.engineDeployPreflight(ctx, &app, target, ex, log); err != nil {
		return err
	}
	j := &job{app: app, deployment: deployment, server: target, ex: ex, log: log, env: variables, domains: d.domains, gitProvider: d.gitProvider, registries: d.images}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		j.cleanupBuildDir(cleanupCtx)
	}()

	image, err := d.produceImage(ctx, j)
	if err != nil {
		return err
	}
	if err := j.checkImageLabels(ctx, image); err != nil {
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
	if err := d.ensureRegistryImage(ctx, j); err != nil {
		return "", err
	}
	if j.deployment.Trigger == TriggerPromotion {
		return j.promotedImage(ctx)
	}
	if image := j.deployment.RollbackImage; image != "" {
		j.log.Info("Rollback para a imagem %s (sem novo build).", image)
		if !docker.ImageExists(ctx, j.ex, image) {
			return "", i18n.Errorf("a imagem %s não está mais disponível no servidor", image)
		}
		return image, nil
	}
	if err := d.fetchCode(ctx, j); err != nil {
		return "", err
	}
	if j.app.Config.Build.Pack != PackImage {
		if err := j.applyRepoConfig(ctx); err != nil {
			return "", err
		}
	}
	strategy, err := builderFor(j.app.Config.Build.Pack)
	if err != nil {
		return "", err
	}
	d.reportDeploymentBuilding(ctx, j)
	return strategy.build(ctx, j)
}

// fetchCode puts the source in the build directory: the uploaded archive or
// a Git checkout. Image applications have no source.
func (d *Deployer) fetchCode(ctx context.Context, j *job) error {
	archive := j.deployment.Trigger == TriggerArchive
	if j.app.Config.Build.Pack == PackImage {
		if archive {
			return i18n.Errorf("a aplicação passou a usar uma imagem Docker; o código enviado foi descartado")
		}
		return nil
	}
	if archive {
		j.log.Info("Origem: %s.", j.deployment.Source)
		return j.unpackArchive(ctx, d.archives.path(j.deployment.ID))
	}
	return d.checkout(ctx, j)
}

func (d *Deployer) checkout(ctx context.Context, j *job) error {
	privateKey := ""
	if keyID := j.app.Config.Source.PrivateKeyID; keyID != "" {
		material, err := d.keys.Material(ctx, keyID)
		if err != nil {
			return i18n.Errorf("carregar chave de deploy: %w", err)
		}
		privateKey = material
	}
	sha, message, err := j.fetchSource(ctx, privateKey)
	if err != nil {
		return err
	}
	if err := j.ensureInsideSource(ctx); err != nil {
		return err
	}
	j.deployment.CommitSHA, j.deployment.CommitMessage = sha, message
	j.log.Info("Commit %s — %s", shortSHA(sha), message)
	if err := d.store.setDeploymentCommit(ctx, j.deployment.ID, sha, message); err != nil {
		return err
	}
	d.reportDeploymentStarted(ctx, j)
	return nil
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
