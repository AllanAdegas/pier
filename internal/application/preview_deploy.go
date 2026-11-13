package application

import (
 "pier/internal/server"
	"context"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// SetPreviewReporter installs who tells the Git provider about previews.
func (d *Deployer) SetPreviewReporter(reporter PreviewReporter) { d.previewReporter = reporter }

func (d *Deployer) reportPreview(ctx context.Context, report PreviewReport) {
	if d.previewReporter == nil {
		return
	}
	d.previewReporter.ReportPreview(context.WithoutCancel(ctx), report)
}

// executePreview builds the pull request head and releases it under the
// preview's own docker identity: the main application's containers, images,
// volumes, compose project and current image are never read or written.
func (d *Deployer) executePreview(ctx context.Context, deployment Deployment, log *deployLog) error {
	app, err := d.store.Get(ctx, deployment.ApplicationID)
	if err != nil {
		return i18n.Errorf("carregar aplicação: %w", err)
	}
	if err := d.checkDeployPolicy(ctx, app); err != nil {
		return err
	}
	preview, err := d.previewOf(ctx, deployment)
	if err != nil {
		return err
	}
	settings, err := d.store.previewSettings(ctx, app.ID)
	if err != nil {
		return err
	}
	if preview.Fork && !settings.AllowForks {
		return i18n.Errorf("previews de pull requests de forks estão desativados nesta aplicação")
	}
	view := previewApplication(app, preview)
	log.repository(view.Config.Source.Repository)
	variables, err := d.previewVariables(ctx, app, preview)
	if err != nil {
		return i18n.Errorf("carregar variáveis de ambiente: %w", err)
	}
	for _, variable := range variables {
		log.redact(variable.Value)
	}
	log.Info("Iniciando o preview do pull request #%d de %q (branch %s).", preview.Number, app.Name, preview.Branch)
	reportDroppedMounts(log, app.Config, preview)
	if err := server.CheckConnectorDeployment(ctx, d.connector, app.ServerID); err != nil { return err }
	ex, target, err := d.connector.Executor(ctx, app.ServerID)
	if err != nil {
		return i18n.Errorf("conectar ao servidor: %w", err)
	}
	log.redact(target.ProxyToken)
	if err := docker.EnsureNetwork(ctx, ex); err != nil {
		return err
	}
	j := &job{app: view, deployment: deployment, server: target, ex: ex, log: log, env: variables, domains: d.domains, gitProvider: d.gitProvider}
	// The ref only names the preview; the checkout is the pinned head commit.
	j.deployment.Ref = ""
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		j.cleanupBuildDir(cleanupCtx)
	}()
	seedPreviewGitTrust(ctx, j, app.ID)
	image, err := d.produceImage(ctx, j)
	if err != nil {
		return err
	}
	// pier.yaml was applied on top of the configuration: isolate it again.
	isolatePreview(&j.app.Config, preview)
	if err := j.checkImageLabels(ctx, image); err != nil {
		return err
	}
	if err := j.release(ctx, image, previewRuntimeEnv(variables, preview)); err != nil {
		return err
	}
	if err := d.store.setPreviewImage(ctx, preview.ID, image); err != nil {
		return err
	}
	prunePreviewImages(context.WithoutCancel(ctx), j, image)
	return nil
}

// previewOf finds the preview a deployment builds, through its ref.
func (d *Deployer) previewOf(ctx context.Context, deployment Deployment) (Preview, error) {
	number, err := strconv.Atoi(strings.TrimPrefix(deployment.Ref, "#"))
	if err != nil || !strings.HasPrefix(deployment.Ref, "#") {
		return Preview{}, i18n.Errorf("deploy de preview sem pull request associado")
	}
	preview, err := d.store.previewByNumber(ctx, deployment.ApplicationID, number)
	if err != nil {
		return Preview{}, i18n.Errorf("o preview #%d foi removido", number)
	}
	if preview.Status == PreviewDestroying {
		return Preview{}, i18n.Errorf("o preview #%d está sendo removido", number)
	}
	return preview, nil
}

// previewApplication is the application as the preview runs it: its own
// identity, the pull request head and nothing published on the host.
func previewApplication(app Application, preview Preview) Application {
	view := app
	view.ID = preview.ID
	view.Name = app.Name + "-pr-" + strconv.Itoa(preview.Number)
	view.CurrentImage = preview.Image
	config := app.Config
	// The base branch exists in the base repository; the head commit is
	// fetched from it by SHA, so a fork's URL is never contacted.
	config.Source.Commit = preview.CommitSHA
	config.Source.Tag = ""
	config.Source.TagPattern = ""
	config.Source.WatchPaths = slices.Clone(config.Source.WatchPaths)
	config.Processes = slices.Clone(config.Processes)
	config.AutoDeploy = false
	isolatePreview(&config, preview)
	view.Config = config
	return view
}

// isolatePreview keeps the preview off what the main application owns on
// the host: its domains, its host ports and its host directories. Named
// volumes stay, under the preview's own names.
func isolatePreview(config *Config, preview Preview) {
	stripPreviewHostOptions(config)
	config.Network.Domains = []string{}
	if preview.URL != "" {
		config.Network.Domains = []string{preview.URL}
	}
	config.Network.PortMappings = []string{}
	storages := []Storage{}
	for _, storage := range config.Storages {
		if storage.Kind == StorageVolume {
			storages = append(storages, storage)
		}
	}
	config.Storages = storages
}

func reportDroppedMounts(log *deployLog, config Config, preview Preview) {
	if len(config.Network.PortMappings) > 0 {
		log.Info("Portas publicadas no host não valem para previews: o preview só responde em %s.", preview.URL)
	}
	for _, storage := range config.Storages {
		if storage.Kind == StorageBind {
			log.Info("O diretório do host %s não é montado no preview, para não alterar dados da aplicação.", storage.Source)
		}
	}
}

// previewVariables are the application's variables with the preview-only
// ones over them. A fork's code gets only the preview-only variables: opting
// in to build forks never hands the application's secrets to a stranger.
func (d *Deployer) previewVariables(ctx context.Context, app Application, preview Preview) ([]envvar.Variable, error) {
	overrides, err := d.env.ListForDeploy(ctx, envvar.Owner{Type: PreviewEnvOwner, ID: app.ID}, app.EnvironmentID, app.ServerID)
	if err != nil || preview.Fork {
		return overrides, err
	}
	base, err := d.env.ListForDeploy(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: app.ID}, app.EnvironmentID, app.ServerID)
	if err != nil {
		return nil, err
	}
	return mergeVariables(base, overrides), nil
}

func mergeVariables(base, overrides []envvar.Variable) []envvar.Variable {
	byKey := map[string]envvar.Variable{}
	for _, variable := range append(slices.Clone(base), overrides...) {
		byKey[variable.Key] = variable
	}
	merged := make([]envvar.Variable, 0, len(byKey))
	for _, variable := range byKey {
		merged = append(merged, variable)
	}
	sort.Slice(merged, func(a, b int) bool { return merged[a].Key < merged[b].Key })
	return merged
}

// previewRuntimeEnv tells the code it runs as a preview; user variables win.
func previewRuntimeEnv(variables []envvar.Variable, preview Preview) map[string]string {
	env := map[string]string{
		"PIER_PREVIEW": "true", "PIER_PULL_REQUEST": strconv.Itoa(preview.Number), "PIER_BRANCH": preview.Branch,
	}
	for _, variable := range variables {
		env[variable.Key] = variable.Value
	}
	return env
}

// seedPreviewGitTrust copies the Git host keys the application already
// trusts, so a preview clone checks them instead of trusting on first use.
func seedPreviewGitTrust(ctx context.Context, j *job, applicationID string) {
	if j.app.Config.Source.PrivateKeyID == "" {
		return
	}
	trusted := path.Join(j.server.ResourceDir(docker.TypeApplication, applicationID), "git", "known_hosts")
	dir := path.Join(j.server.ResourceDir(docker.TypeApplication, j.app.ID), "git")
	script := `mkdir -p "$2" && if [ -s "$1" ] && [ ! -L "$1" ]; then rm -f "$2/known_hosts" && cp -- "$1" "$2/known_hosts"; fi`
	_, _ = j.ex.Run(ctx, shell.Join("sh", "-c", script, "sh", trusted, dir))
}

// prunePreviewImages keeps only the image the preview runs now.
func prunePreviewImages(ctx context.Context, j *job, current string) {
	ctx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()
	output, err := j.ex.Run(ctx, shell.Join("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", "pier/"+j.app.ID))
	if err != nil {
		return
	}
	expired := []string{}
	for _, image := range strings.Fields(output) {
		if image != current && strings.HasPrefix(image, "pier/"+j.app.ID+":") {
			expired = append(expired, image)
		}
	}
	if len(expired) > 0 {
		_, _ = j.ex.Run(ctx, shell.Join(append([]string{"docker", "rmi"}, expired...)...)+" 2>/dev/null || true")
	}
}

// finishPreview records the outcome of a preview deployment on the preview
// and tells people about it, marked as a preview.
func (d *Deployer) finishPreview(ctx context.Context, deployment Deployment, status DeploymentStatus, failure string) {
	preview, err := d.previewOf(ctx, deployment)
	if err != nil {
		return
	}
	app, err := d.store.Get(ctx, deployment.ApplicationID)
	if err != nil {
		return
	}
	report := PreviewReport{Application: app, Preview: preview, DeploymentID: deployment.ID, Failure: failure}
	switch status {
	case DeploymentFinished:
		report.Event, preview.Status = PreviewEventReady, PreviewRunning
	case DeploymentFailed:
		report.Event, preview.Status = PreviewEventFailed, PreviewFailed
	default:
		_ = d.store.setPreviewStatus(ctx, preview.ID, settledStatus(preview))
		return
	}
	if err := d.store.setPreviewStatus(ctx, preview.ID, preview.Status); err != nil {
		d.logger.Error("record preview status", "preview", preview.ID, "error", err)
	}
	report.Preview = preview
	d.events.Publish(previewEvent(app.Name, preview, status, failure))
	d.reportPreview(ctx, report)
}

// settledStatus is the status of a preview whose last deployment ended
// without a verdict (cancelled): it runs what it ran before, if anything.
func settledStatus(preview Preview) PreviewStatus {
	if preview.Image != "" {
		return PreviewRunning
	}
	return PreviewFailed
}

// effectiveStatus corrects a preview left "deploying" by a deployment that
// ended without reaching the deployer (cancelled while queued).
func effectiveStatus(preview Preview) PreviewStatus {
	last := preview.LastDeployment
	if preview.Status != PreviewDeploying || last == nil || !last.Status.Terminal() {
		return preview.Status
	}
	if last.Status == DeploymentFinished {
		return PreviewRunning
	}
	return settledStatus(preview)
}
