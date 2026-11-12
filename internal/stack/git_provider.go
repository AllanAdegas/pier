package stack

import (
	"context"
	"net/http"

	"pier/internal/application"
	"pier/internal/platform/i18n"
)

// SetGitProvider resolves installation credentials for selected Git stacks.
func (s *Service) SetGitProvider(provider application.GitCredentials) { s.gitProvider = provider }

// HandleGitHubAppEvent deploys a Git stack on a push the GitHub App webhook
// delivered (already authenticated with the App secret) for repository,
// with the same rules as the stack webhook. Pull requests are ignored.
func (s *Service) HandleGitHubAppEvent(ctx context.Context, id, repository, event string, body []byte) (application.WebhookResult, error) {
	language := i18n.FromContext(ctx)
	header := http.Header{}
	header.Set("X-GitHub-Event", event)
	if !application.IsPushEvent(application.ProviderGitHub, header) {
		return application.WebhookResult{Reason: language.Text("Evento ignorado: apenas push dispara deploy.")}, nil
	}
	stack, err := s.Get(ctx, id)
	if err != nil {
		return application.WebhookResult{}, err
	}
	if stack.Git == nil || !application.SameGitHubRepository(stack.Git.Repository, repository) {
		return application.WebhookResult{Reason: language.Text(
			"A origem da stack não é mais o repositório vinculado ao GitHub App: nada a fazer.")}, nil
	}
	if reason := pushIgnoredReason(language, *stack.Git, body); reason != "" {
		return application.WebhookResult{Reason: reason}, nil
	}
	if err := s.Deploy(ctx, id); err != nil {
		return application.WebhookResult{}, err
	}
	return application.WebhookResult{Deployed: true, Reason: language.Text("Deploy enfileirado.")}, nil
}
