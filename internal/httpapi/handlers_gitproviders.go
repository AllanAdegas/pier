package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"pier/internal/application"
	"pier/internal/gitprovider"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// gitProviderStatus is the GitHub App as the settings page sees it.
type gitProviderStatus struct {
	gitprovider.Status
	WebhookURL string `json:"webhook_url"`
}

// gitProviderLink is the GitHub App link of a resource.
type gitProviderLink struct {
	Selected bool `json:"selected"`
	gitprovider.Selection
}

// gitProviderEventResult is what one linked resource did with an App webhook.
type gitProviderEventResult struct {
	Kind       string `json:"kind"`
	ResourceID string `json:"resource_id"`
	application.WebhookResult
}

func (a *API) registerGitProviderRoutes(mux *http.ServeMux) {
	if a.GitProviders == nil {
		return
	}
	a.route(mux, "GET /api/v1/git-provider", canRead, a.gitProviderStatus)
	a.route(mux, "PUT /api/v1/git-provider", canWrite, a.configureGitProvider)
	a.route(mux, "POST /api/v1/git-provider/manifest", canWrite, a.startGitProviderManifest)
	a.route(mux, "POST /api/v1/git-provider/manifest/complete", canWrite, a.completeGitProviderManifest)
	a.route(mux, "GET /api/v1/git-provider/installations", canRead, func(w http.ResponseWriter, r *http.Request) error {
		rows, err := a.GitProviders.Installations(r.Context())
		if err != nil {
			return err
		}
		return ok(w, rows)
	})
	a.route(mux, "GET /api/v1/git-provider/installations/{installation}/repositories", canRead, a.gitProviderRepositories)
	for _, kind := range []string{gitprovider.KindApplication, gitprovider.KindStack} {
		base := "/api/v1/" + kind + "s/{id}/git-provider"
		a.route(mux, "GET "+base, canRead, a.getGitProviderLink(kind))
		a.route(mux, "PUT "+base, canWrite, a.putGitProviderLink(kind))
		a.route(mux, "DELETE "+base, canWrite, a.deleteGitProviderLink(kind))
		a.route(mux, "GET "+base+"/feedback", canRead, a.getGitProviderFeedback(kind))
		a.route(mux, "PUT "+base+"/feedback", canWrite, a.putGitProviderFeedback(kind))
	}
	a.public(mux, "POST "+gitprovider.WebhookPath, a.gitProviderWebhook)
}

func (a *API) gitProviderStatus(w http.ResponseWriter, r *http.Request) error {
	return ok(w, gitProviderStatus{Status: a.GitProviders.Status(r.Context()), WebhookURL: a.publicURL(r) + gitprovider.WebhookPath})
}

func (a *API) configureGitProvider(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[gitprovider.Config](w, r)
	if err != nil {
		return err
	}
	if err := a.GitProviders.Configure(r.Context(), input); err != nil {
		return err
	}
	return a.gitProviderStatus(w, r)
}

func (a *API) startGitProviderManifest(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[struct {
		Organization string `json:"organization"`
	}](w, r)
	if err != nil {
		return err
	}
	start, err := a.GitProviders.StartManifest(a.publicURL(r), input.Organization)
	if err != nil {
		return err
	}
	return ok(w, start)
}

func (a *API) completeGitProviderManifest(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}](w, r)
	if err != nil {
		return err
	}
	if _, err := a.GitProviders.CompleteManifest(r.Context(), input.Code, input.State); err != nil {
		return err
	}
	return a.gitProviderStatus(w, r)
}

func (a *API) gitProviderRepositories(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.PathValue("installation"), 10, 64)
	if err != nil || id <= 0 {
		return apperr.NotFound("Instalação")
	}
	rows, err := a.GitProviders.Repositories(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, rows)
}

func (a *API) getGitProviderLink(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		selection, err := a.GitProviders.Selection(r.Context(), kind, r.PathValue("id"))
		if errors.Is(err, gitprovider.ErrNoSelection) {
			return ok(w, gitProviderLink{})
		}
		if err != nil {
			return err
		}
		return ok(w, gitProviderLink{Selected: true, Selection: selection})
	}
}

func (a *API) putGitProviderLink(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[gitprovider.Selection](w, r)
		if err != nil {
			return err
		}
		if err := a.GitProviders.Select(r.Context(), kind, r.PathValue("id"), input); err != nil {
			return err
		}
		return a.getGitProviderLink(kind)(w, r)
	}
}

func (a *API) deleteGitProviderLink(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if err := a.GitProviders.ClearSelection(r.Context(), kind, r.PathValue("id")); err != nil {
			return err
		}
		return noContent(w)
	}
}

func (a *API) getGitProviderFeedback(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		settings, err := a.GitProviders.FeedbackSettings(r.Context(), kind, r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, settings)
	}
}

func (a *API) putGitProviderFeedback(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[gitprovider.FeedbackSettings](w, r)
		if err != nil {
			return err
		}
		repository, err := a.resourceRepository(r.Context(), kind, r.PathValue("id"))
		if err != nil {
			return err
		}
		settings, err := a.GitProviders.SaveFeedback(r.Context(), kind, r.PathValue("id"), repository, input)
		if err != nil {
			return err
		}
		return ok(w, settings)
	}
}

// resourceRepository is the repository the source of a resource uses now.
func (a *API) resourceRepository(ctx context.Context, kind, id string) (string, error) {
	if kind == gitprovider.KindStack {
		stack, err := a.Stacks.Get(ctx, id)
		if err != nil {
			return "", err
		}
		if stack.Git == nil {
			return "", apperr.Invalid("Esta stack não vem de um repositório Git.")
		}
		return stack.Git.Repository, nil
	}
	app, err := a.Applications.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return app.Config.Source.Repository, nil
}

// gitProviderWebhook receives the App webhook: GitHub signs it with the App
// secret, so no resource needs a webhook of its own. Each linked resource
// reacts on its own; one refusing (a frozen environment) does not stop the others.
func (a *API) gitProviderWebhook(w http.ResponseWriter, r *http.Request) error {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, gitprovider.MaxWebhookBytes))
	if err != nil {
		return apperr.Invalid("Webhook do GitHub inválido.")
	}
	event := r.Header.Get("X-GitHub-Event")
	delivery, err := a.GitProviders.Webhook(r.Context(), r.Header.Get("X-GitHub-Delivery"), event,
		r.Header.Get("X-Hub-Signature-256"), body)
	if err != nil {
		return err
	}
	language := i18n.FromContext(r.Context())
	results := []gitProviderEventResult{}
	for _, target := range delivery.Targets {
		result, err := a.dispatchGitProviderEvent(r.Context(), target, event, body)
		if err != nil {
			result = application.WebhookResult{Reason: i18n.Localize(err, language)}
		}
		results = append(results, gitProviderEventResult{Kind: target.Kind, ResourceID: target.ResourceID, WebhookResult: result})
	}
	return ok(w, map[string]any{"duplicate": delivery.Duplicate, "results": results})
}

func (a *API) dispatchGitProviderEvent(ctx context.Context, target gitprovider.Target, event string, body []byte) (application.WebhookResult, error) {
	if target.Kind == gitprovider.KindStack {
		return a.Stacks.HandleGitHubAppEvent(ctx, target.ResourceID, target.Repository, event, body)
	}
	return a.Applications.HandleGitHubAppEvent(ctx, target.ResourceID, target.Repository, event, body)
}
