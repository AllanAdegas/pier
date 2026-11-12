package stack

import (
	"context"
	"net/http"

	"pier/internal/application"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// HandleGitPush processes a push webhook of a Git provider. It deploys only
// when the signature is valid, automatic deployment is on, the pushed branch
// is the one the stack follows and the push touched the watched paths.
// Frozen environments refuse the deployment like any other.
func (s *Service) HandleGitPush(ctx context.Context, id string, provider application.GitProvider,
	header http.Header, body []byte) (application.WebhookResult, error) {
	secret, err := s.WebhookSecret(ctx, id)
	if err != nil || !application.VerifyWebhook(provider, secret, header, body) {
		return application.WebhookResult{}, apperr.Unauthorized("Assinatura do webhook inválida.")
	}
	language := i18n.FromContext(ctx)
	if !application.IsPushEvent(provider, header) {
		return application.WebhookResult{Reason: language.Text("Evento ignorado: apenas push dispara deploy.")}, nil
	}
	stack, err := s.Get(ctx, id)
	if err != nil {
		return application.WebhookResult{}, err
	}
	if reason := pushIgnoredReason(language, *stack.Git, body); reason != "" {
		return application.WebhookResult{Reason: reason}, nil
	}
	if err := s.Deploy(ctx, id); err != nil {
		return application.WebhookResult{}, err
	}
	return application.WebhookResult{Deployed: true, Reason: language.Text("Deploy enfileirado.")}, nil
}

// pushIgnoredReason explains why a push does not deploy; empty means it does.
func pushIgnoredReason(language i18n.Language, git GitSource, body []byte) string {
	if !git.AutoDeploy {
		return language.Text("Deploy automático desativado para esta stack.")
	}
	if git.Tag != "" {
		return language.Sprintf("Esta stack usa a tag fixa %s: webhooks de push são ignorados.", git.Tag)
	}
	push, ok := application.ParsePush(body)
	if !ok || push.Branch != git.Branch {
		return language.Text("Push em outra branch: nada a fazer.")
	}
	if push.Removed {
		return language.Text("Branch removida: nada a fazer.")
	}
	if !application.WatchPathsChanged(git.WatchPaths, body) {
		return language.Text("Push fora dos caminhos monitorados: nada a fazer.")
	}
	return ""
}

// HandleDeployHook deploys when the caller presents the webhook secret: the
// hook for pipelines that are not a Git provider.
func (s *Service) HandleDeployHook(ctx context.Context, id, presentedSecret string) (application.WebhookResult, error) {
	secret, err := s.WebhookSecret(ctx, id)
	if err != nil || !application.ConstantTimeEqual(presentedSecret, secret) {
		return application.WebhookResult{}, apperr.Unauthorized("Segredo de webhook inválido.")
	}
	if err := s.Deploy(ctx, id); err != nil {
		return application.WebhookResult{}, err
	}
	return application.WebhookResult{Deployed: true, Reason: i18n.FromContext(ctx).Text("Deploy enfileirado.")}, nil
}
