package application

import (
	"context"
	"errors"

	"pier/internal/platform/i18n"
	"pier/internal/platform/sqlite"
)

// handlePullRequest hands a pull request webhook, already authenticated by
// HandleGitPush, to the preview service.
func (s *Service) handlePullRequest(ctx context.Context, id string, provider GitProvider, body []byte) (WebhookResult, error) {
	if s.previews == nil {
		return WebhookResult{Reason: i18n.FromContext(ctx).Text("Previews não estão disponíveis nesta instância.")}, nil
	}
	app, err := s.Get(ctx, id)
	if err != nil {
		return WebhookResult{}, err
	}
	return s.previews.HandlePullRequest(ctx, app, provider, body)
}

// HandlePullRequest reacts to a pull/merge request event: opening, reopening
// or pushing deploys the preview; closing or merging destroys it.
func (p *PreviewService) HandlePullRequest(ctx context.Context, app Application, provider GitProvider,
	body []byte) (WebhookResult, error) {
	event, err := parsePullRequest(provider, body)
	if err != nil {
		return WebhookResult{}, err
	}
	switch event.Action {
	case pullRequestIgnored:
		return WebhookResult{Reason: i18n.FromContext(ctx).Text("Ação de pull request ignorada pelos previews.")}, nil
	case pullRequestClosed:
		return p.closePullRequest(ctx, app, event.Number)
	}
	return p.deployPullRequest(ctx, app, provider, event)
}

func (p *PreviewService) deployPullRequest(ctx context.Context, app Application, provider GitProvider,
	event pullRequestEvent) (WebhookResult, error) {
	settings, err := p.store.previewSettings(ctx, app.ID)
	if err != nil {
		return WebhookResult{}, err
	}
	if reason := p.refusal(ctx, app, settings, event); reason != "" {
		return WebhookResult{Reason: reason}, nil
	}
	if err := p.apps.checkDeploy(ctx, app); err != nil {
		return WebhookResult{}, err
	}
	request := previewRequest{
		Number: event.Number, Provider: provider, Branch: event.Branch, CommitSHA: event.CommitSHA,
		Title: event.Title, Author: event.Author, Fork: event.Fork, SkipSameCommit: event.Action == pullRequestUpdated,
	}
	deployment, reason, err := p.admit(ctx, app, settings, request)
	if err != nil || reason != "" {
		return WebhookResult{Reason: reason}, err
	}
	return WebhookResult{
		Deployed: true, DeploymentID: deployment.ID,
		Reason: i18n.FromContext(ctx).Sprintf("Deploy do preview #%d enfileirado.", event.Number),
	}, nil
}

// refusal explains why a pull request gets no preview, or is empty.
func (p *PreviewService) refusal(ctx context.Context, app Application, settings PreviewSettings, event pullRequestEvent) string {
	language := i18n.FromContext(ctx)
	switch {
	case !settings.Enabled:
		return language.Text("Previews desativados para esta aplicação.")
	case app.Config.Build.Pack == PackImage:
		return language.Text("Aplicações de imagem Docker não têm previews.")
	case event.BaseBranch != app.Config.Source.Branch:
		return language.Sprintf("Pull request para a branch %s; esta aplicação acompanha %s: nada a fazer.",
			event.BaseBranch, app.Config.Source.Branch)
	case event.Fork && !settings.AllowForks:
		p.logger.Warn("preview refused: pull request from a fork", "application", app.ID, "pull_request", event.Number)
		return language.Sprintf("O pull request #%d vem de um fork: previews de forks estão desativados, "+
			"porque rodariam código de terceiros no servidor.", event.Number)
	}
	return ""
}

// closePullRequest destroys the preview of a closed or merged pull request,
// whatever the settings say now: a preview never outlives its pull request.
func (p *PreviewService) closePullRequest(ctx context.Context, app Application, number int) (WebhookResult, error) {
	language := i18n.FromContext(ctx)
	preview, err := p.store.previewByNumber(ctx, app.ID, number)
	if errors.Is(err, sqlite.ErrNotFound) {
		return WebhookResult{Reason: language.Sprintf("O pull request #%d não tem preview: nada a remover.", number)}, nil
	}
	if err != nil {
		return WebhookResult{}, err
	}
	if err := p.destroy(ctx, app, preview); err != nil {
		return WebhookResult{}, err
	}
	return WebhookResult{Reason: language.Sprintf("Preview #%d removido.", number)}, nil
}
