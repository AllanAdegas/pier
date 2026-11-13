package application

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"pier/internal/compose"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/sqlite"
)

// PreviewService runs pull request previews: settings, the reaction to pull
// request webhooks, manual redeploy/destroy and the janitor.
type PreviewService struct {
	apps     *Service
	store    *Store
	deployer *Deployer
	logger   *slog.Logger
	now      func() time.Time
	// admission serializes the decisions that must not race: the limit
	// count, creating a preview and claiming its destruction.
	admission sync.Mutex
}

// NewPreviewService creates the service and plugs it into the application
// service, which hands it the pull request webhooks.
func NewPreviewService(apps *Service, logger *slog.Logger) *PreviewService {
	previews := &PreviewService{apps: apps, store: apps.store, logger: logger, now: time.Now}
	if apps.dispatcher != nil {
		previews.deployer = apps.dispatcher.deployer
	}
	apps.previews = previews
	return previews
}

// SetReporter installs who tells the Git provider about previews (a pull
// request comment, a commit status). Without one, nothing is reported.
func (p *PreviewService) SetReporter(reporter PreviewReporter) {
	if p.deployer != nil {
		p.deployer.SetPreviewReporter(reporter)
	}
}

func (p *PreviewService) report(ctx context.Context, report PreviewReport) {
	if p.deployer != nil {
		p.deployer.reportPreview(ctx, report)
	}
}

// Settings returns the preview settings of an application.
func (p *PreviewService) Settings(ctx context.Context, applicationID string) (PreviewSettings, error) {
	if _, err := p.apps.Get(ctx, applicationID); err != nil {
		return PreviewSettings{}, err
	}
	return p.store.previewSettings(ctx, applicationID)
}

// UpdateSettings validates and saves the preview settings. Existing
// previews keep running; a lower limit or TTL is applied by the janitor.
func (p *PreviewService) UpdateSettings(ctx context.Context, applicationID string, settings PreviewSettings) (PreviewSettings, error) {
	app, err := p.apps.Get(ctx, applicationID)
	if err != nil {
		return PreviewSettings{}, err
	}
	if err := settings.Validate(); err != nil {
		return PreviewSettings{}, err
	}
	if settings.Enabled && app.Config.Build.Pack == PackImage {
		return PreviewSettings{}, apperr.Conflict("Aplicações de imagem Docker não têm previews: um preview constrói o código do pull request.")
	}
	if err := p.store.savePreviewSettings(ctx, applicationID, settings); err != nil {
		return PreviewSettings{}, err
	}
	return settings, nil
}

// List returns the previews of an application.
func (p *PreviewService) List(ctx context.Context, applicationID string) ([]Preview, error) {
	if _, err := p.apps.Get(ctx, applicationID); err != nil {
		return nil, err
	}
	previews, err := p.store.previews(ctx, applicationID)
	if err != nil {
		return nil, err
	}
	for index := range previews {
		previews[index].Status = effectiveStatus(previews[index])
	}
	return previews, nil
}

// Redeploy builds the recorded head commit of a preview again.
func (p *PreviewService) Redeploy(ctx context.Context, applicationID, previewID string) (Deployment, error) {
	app, preview, err := p.load(ctx, applicationID, previewID)
	if err != nil {
		return Deployment{}, err
	}
	settings, err := p.store.previewSettings(ctx, app.ID)
	if err != nil {
		return Deployment{}, err
	}
	switch {
	case !settings.Enabled:
		return Deployment{}, apperr.Conflict("Os previews estão desativados nesta aplicação.")
	case preview.Fork && !settings.AllowForks:
		return Deployment{}, apperr.Conflict("Previews de pull requests de forks estão desativados nesta aplicação.")
	}
	if err := p.apps.checkDeploy(ctx, app); err != nil {
		return Deployment{}, err
	}
	deployment, _, err := p.admit(ctx, app, settings, requestOf(preview))
	return deployment, err
}

// Destroy removes a preview and everything it runs. Like closing the pull
// request, it is allowed in a frozen environment: nothing is deployed.
func (p *PreviewService) Destroy(ctx context.Context, applicationID, previewID string) error {
	app, preview, err := p.load(ctx, applicationID, previewID)
	if err != nil {
		return err
	}
	return p.destroy(ctx, app, preview)
}

func (p *PreviewService) load(ctx context.Context, applicationID, previewID string) (Application, Preview, error) {
	app, err := p.apps.Get(ctx, applicationID)
	if err != nil {
		return Application{}, Preview{}, err
	}
	preview, err := p.store.preview(ctx, applicationID, previewID)
	if err != nil {
		return Application{}, Preview{}, notFound(err, "Preview")
	}
	return app, preview, nil
}

// previewRequest asks for the preview of one pull request head.
type previewRequest struct {
	Number    int
	Provider  GitProvider
	Branch    string
	CommitSHA string
	Title     string
	Author    string
	Fork      bool
	// SkipSameCommit ignores an update whose commit is already deployed.
	SkipSameCommit bool
}

func requestOf(preview Preview) previewRequest {
	return previewRequest{
		Number: preview.Number, Provider: preview.Provider, Branch: preview.Branch, CommitSHA: preview.CommitSHA,
		Title: preview.Title, Author: preview.Author, Fork: preview.Fork,
	}
}

// admit creates or updates the preview of a pull request and queues its
// deployment. A non-empty reason means it was refused without error (the
// limit, an unchanged commit), which webhooks report as is.
func (p *PreviewService) admit(ctx context.Context, app Application, settings PreviewSettings,
	request previewRequest) (Deployment, string, error) {
	language := i18n.FromContext(ctx)
	p.admission.Lock()
	defer p.admission.Unlock()
	preview, err := p.store.previewByNumber(ctx, app.ID, request.Number)
	exists := err == nil
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return Deployment{}, "", err
	}
	switch {
	case exists && preview.Status == PreviewDestroying:
		return Deployment{}, "", apperr.Conflict("O preview #%d está sendo removido. Tente novamente em instantes.", request.Number)
	case exists && request.SkipSameCommit && preview.CommitSHA == request.CommitSHA && preview.Status != PreviewFailed:
		return Deployment{}, language.Sprintf("O commit %s já está no preview #%d: nada a fazer.", shortSHA(request.CommitSHA), request.Number), nil
	case !exists:
		if reason, err := p.overLimit(ctx, app, settings, request.Number); reason != "" || err != nil {
			return Deployment{}, reason, err
		}
	}
	route, err := p.previewRoute(ctx, app, settings, request)
	if err != nil {
		return Deployment{}, "", err
	}
	preview, err = p.record(ctx, app, preview, exists, request, route)
	if err != nil {
		return Deployment{}, "", err
	}
	deployment, err := p.enqueue(ctx, app, preview)
	if err != nil {
		return Deployment{}, "", err
	}
	if err := p.store.setPreviewDeployment(ctx, preview.ID, deployment.ID); err != nil {
		return Deployment{}, "", err
	}
	p.report(ctx, PreviewReport{Event: PreviewEventDeploying, Application: app, Preview: preview, DeploymentID: deployment.ID})
	return deployment, "", nil
}

// overLimit refuses a new preview beyond the maximum. Refusing, instead of
// evicting the oldest, keeps anyone able to open pull requests from
// destroying the previews other people are reviewing.
func (p *PreviewService) overLimit(ctx context.Context, app Application, settings PreviewSettings, number int) (string, error) {
	count, err := p.store.countPreviews(ctx, app.ID)
	if err != nil || count < settings.MaxPreviews {
		return "", err
	}
	p.logger.Warn("preview refused: limit reached", "application", app.ID, "pull_request", number, "limit", settings.MaxPreviews)
	return i18n.FromContext(ctx).Sprintf("Limite de %d previews atingido: o pull request #%d ficou sem preview. "+
		"Feche outro pull request ou aumente o limite.", settings.MaxPreviews, number), nil
}

// previewRoute renders the preview URL and refuses a host already served
// by any application or by another preview, so a pull request can never
// take over traffic that is not its own.
func (p *PreviewService) previewRoute(ctx context.Context, app Application, settings PreviewSettings,
	request previewRequest) (compose.Route, error) {
	route, err := renderPreviewDomain(settings.DomainTemplate, request.Number, request.Branch)
	if err != nil {
		return compose.Route{}, apperr.Conflict("%s", err)
	}
	applications, err := p.store.List(ctx, "")
	if err != nil {
		return compose.Route{}, err
	}
	for _, other := range applications {
		for _, domain := range other.Config.Network.Domains {
			if existing, err := compose.ParseRoute(domain); err == nil && existing.Host == route.Host {
				return compose.Route{}, apperr.Conflict("O domínio %s do preview já é usado pela aplicação %s.", route.Host, other.Name)
			}
		}
	}
	previews, err := p.store.previews(ctx, "")
	if err != nil {
		return compose.Route{}, err
	}
	for _, other := range previews {
		existing, err := compose.ParseRoute(other.URL)
		sameRequest := other.ApplicationID == app.ID && other.Number == request.Number
		if err == nil && existing.Host == route.Host && !sameRequest {
			return compose.Route{}, apperr.Conflict("O domínio %s já é usado pelo preview #%d.", route.Host, other.Number)
		}
	}
	return route, nil
}

func (p *PreviewService) record(ctx context.Context, app Application, preview Preview, exists bool,
	request previewRequest, route compose.Route) (Preview, error) {
	now := p.now()
	if !exists {
		preview = Preview{ID: idgen.New(), ApplicationID: app.ID, Number: request.Number, Provider: request.Provider, CreatedAt: now}
	}
	preview.Branch, preview.CommitSHA = request.Branch, request.CommitSHA
	preview.Title, preview.Author, preview.Fork = request.Title, request.Author, request.Fork
	preview.URL, preview.Status = route.String(), PreviewDeploying
	preview.UpdatedAt, preview.LastActivityAt = now, now
	if exists {
		return preview, p.store.updatePreviewHead(ctx, preview)
	}
	return preview, p.store.insertPreview(ctx, preview)
}

// enqueue queues a deployment of the preview, joining one already waiting:
// it reads the head commit when it starts, so it builds the latest push.
func (p *PreviewService) enqueue(ctx context.Context, app Application, preview Preview) (Deployment, error) {
	queued, err := p.store.queuedDeployment(ctx, app.ID, preview.ref())
	if err == nil {
		return queued, nil
	}
	if !errors.Is(err, sqlite.ErrNotFound) {
		return Deployment{}, err
	}
	return p.apps.enqueue(ctx, Deployment{
		ApplicationID: app.ID, Trigger: TriggerPreview, Ref: preview.ref(),
		Source: i18n.Instance().Sprintf("Preview #%d (%s)", preview.Number, preview.Branch),
	})
}

// destroy stops the preview's deployments and erases it from the server and
// from the database. It never touches the application's own resources.
func (p *PreviewService) destroy(ctx context.Context, app Application, preview Preview) error {
	p.admission.Lock()
	claimed, err := p.store.claimPreviewDestruction(ctx, preview.ID)
	p.admission.Unlock()
	if err != nil {
		return err
	}
	if !claimed {
		return apperr.Conflict("O preview #%d já está sendo removido.", preview.Number)
	}
	if err := p.stopDeployments(ctx, app, preview); err != nil {
		p.releaseDestruction(ctx, preview)
		return err
	}
	if err := removePreviewFromServer(ctx, p.apps.connector, app, preview); err != nil {
		p.releaseDestruction(ctx, preview)
		return apperr.Unavailable("Não foi possível limpar o preview #%d no servidor: %s", preview.Number, err)
	}
	if err := p.store.deletePreview(ctx, preview.ID); err != nil {
		return err
	}
	p.report(ctx, PreviewReport{Event: PreviewEventDestroyed, Application: app, Preview: preview})
	return nil
}

func (p *PreviewService) stopDeployments(ctx context.Context, app Application, preview Preview) error {
	active, err := p.store.activePreviewDeployments(ctx, app.ID, preview.ref())
	if err != nil {
		return err
	}
	for _, deploymentID := range active {
		if p.apps.dispatcher == nil {
			break
		}
		if err := p.apps.dispatcher.Cancel(ctx, deploymentID); err != nil {
			return err
		}
	}
	if err := waitForPreviewDeployments(ctx, p.store, app.ID, preview.ref()); err != nil {
		return apperr.Conflict("O deploy do preview #%d ainda está encerrando. Tente remover novamente em instantes.", preview.Number)
	}
	return nil
}

// releaseDestruction lets a failed destruction be retried.
func (p *PreviewService) releaseDestruction(ctx context.Context, preview Preview) {
	if err := p.store.abandonPreviewDestruction(context.WithoutCancel(ctx), preview.ID); err != nil {
		p.logger.Error("release preview destruction", "preview", preview.ID, "error", err)
	}
}
