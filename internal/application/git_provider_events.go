package application

import (
	"context"
	"net/http"

	"pier/internal/platform/i18n"
)

// HandleGitHubAppEvent reacts to a push or pull request event the GitHub
// App webhook delivered (already authenticated with the App secret) for
// repository, which is linked to the application. It follows the same rules
// as the application webhook: auto deploy, branch, watch paths, previews.
func (s *Service) HandleGitHubAppEvent(ctx context.Context, id, repository, event string, body []byte) (WebhookResult, error) {
	app, err := s.Get(ctx, id)
	if err != nil {
		return WebhookResult{}, err
	}
	if !sameGitHubRepository(app.Config.Source.Repository, "https://github.com/"+repository+".git") {
		return WebhookResult{Reason: i18n.FromContext(ctx).Text(
			"A origem da aplicação não é mais o repositório vinculado ao GitHub App: nada a fazer.")}, nil
	}
	header := http.Header{}
	header.Set("X-GitHub-Event", event)
	return s.handleGitEvent(ctx, id, ProviderGitHub, header, body)
}
