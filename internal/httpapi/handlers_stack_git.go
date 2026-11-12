package httpapi

import (
	"io"
	"net/http"
	"strings"

	"pier/internal/application"
	"pier/internal/platform/apperr"
)

// registerStackGitRoutes registers what only stacks from a Git repository
// have: the webhook (secret management and the public receiver) and the
// pinned SSH host keys.
func (a *API) registerStackGitRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/stacks/{id}/webhook", canRead, a.getStackWebhook)
	a.route(mux, "POST /api/v1/stacks/{id}/webhook/rotate", canWrite, a.rotateStackWebhook)
	a.route(mux, "POST /api/v1/stacks/{id}/git/reset-host-key", canWrite, a.resetStackGitHostKey)
	a.public(mux, "POST /webhooks/stacks/{id}/deploy", a.stackDeployHook)
	a.public(mux, "POST /webhooks/stacks/{id}/{provider}", a.stackGitPushHook)
}

func (a *API) getStackWebhook(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	secret, err := a.Stacks.WebhookSecret(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, a.stackWebhookResponse(r, secret))
}

func (a *API) rotateStackWebhook(w http.ResponseWriter, r *http.Request) error {
	secret, err := a.Stacks.RotateWebhookSecret(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, a.stackWebhookResponse(r, secret))
}

func (a *API) stackWebhookResponse(r *http.Request, secret string) webhookResponse {
	base := a.publicURL(r) + "/webhooks/stacks/" + r.PathValue("id") + "/"
	return webhookResponse{
		Secret: secret, DeployURL: base + "deploy", GitHubURL: base + "github",
		GitLabURL: base + "gitlab", GiteaURL: base + "gitea", BitbucketURL: base + "bitbucket",
	}
}

func (a *API) resetStackGitHostKey(w http.ResponseWriter, r *http.Request) error {
	if err := a.Stacks.ResetGitHostKey(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) stackDeployHook(w http.ResponseWriter, r *http.Request) error {
	secret := r.Header.Get("X-Pier-Token")
	if bearer, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found {
		secret = bearer
	}
	result, err := a.Stacks.HandleDeployHook(r.Context(), r.PathValue("id"), strings.TrimSpace(secret))
	if err != nil {
		return err
	}
	return accepted(w, result)
}

func (a *API) stackGitPushHook(w http.ResponseWriter, r *http.Request) error {
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
	result, err := a.Stacks.HandleGitPush(r.Context(), r.PathValue("id"), provider, r.Header, body)
	if err != nil {
		return err
	}
	return ok(w, result)
}
