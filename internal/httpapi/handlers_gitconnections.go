package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"pier/internal/gitprovider"
	"pier/internal/platform/apperr"
)

func (a *API) registerGitConnectionRoutes(mux *http.ServeMux) {
	if a.GitConnections == nil {
		return
	}
	c := a.GitConnections
	a.route(mux, "GET /api/v1/git-connections", canRead, func(w http.ResponseWriter, r *http.Request) error {
		rows, err := c.List(r.Context())
		if err != nil {
			return err
		}
		return ok(w, rows)
	})
	a.route(mux, "POST /api/v1/git-connections", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[gitprovider.ConnectionInput](w, r)
		if err != nil {
			return err
		}
		var result gitprovider.ConnectionResult
		if r.Method == "POST" {
			result, err = c.Create(r.Context(), input)
		} else {
			result, err = c.Update(r.Context(), r.PathValue("connection"), input)
		}
		if err != nil {
			return err
		}
		return ok(w, result)
	})
	a.route(mux, "PUT /api/v1/git-connections/{connection}", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[gitprovider.ConnectionInput](w, r)
		if err != nil {
			return err
		}
		var result gitprovider.ConnectionResult
		if r.Method == "POST" {
			result, err = c.Create(r.Context(), input)
		} else {
			result, err = c.Update(r.Context(), r.PathValue("connection"), input)
		}
		if err != nil {
			return err
		}
		return ok(w, result)
	})

	a.route(mux, "DELETE /api/v1/git-connections/{connection}", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		if err := c.Delete(r.Context(), r.PathValue("connection")); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "GET /api/v1/git-connections/{connection}/repositories", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		q := r.URL.Query()
		page, _ := strconv.Atoi(q.Get("page"))
		rows, err := c.Repositories(r.Context(), r.PathValue("connection"), q.Get("q"), page)
		if err != nil {
			return err
		}
		return ok(w, rows)
	})
	a.route(mux, "GET /api/v1/git-connections/{connection}/branches", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		q := r.URL.Query()
		page, _ := strconv.Atoi(q.Get("page"))
		rows, err := c.Branches(r.Context(), r.PathValue("connection"), q.Get("repository"), q.Get("q"), page)
		if err != nil {
			return err
		}
		return ok(w, rows)
	})

	for _, kind := range []string{gitprovider.KindApplication, gitprovider.KindStack} {
		base := "/api/v1/" + kind + "s/{id}/git-connection"
		a.route(mux, "GET "+base, canRead, func(w http.ResponseWriter, r *http.Request) error {
			link, err := c.Link(r.Context(), kind, r.PathValue("id"))
			if err != nil {
				return err
			}
			return ok(w, link)
		})
		a.route(mux, "DELETE "+base, canWrite, func(w http.ResponseWriter, r *http.Request) error {
			if err := c.Disconnect(r.Context(), kind, r.PathValue("id")); err != nil {
				return err
			}
			return noContent(w)
		})
		a.route(mux, "PUT "+base, canWrite, func(w http.ResponseWriter, r *http.Request) error {
			input, err := decode[struct {
				ConnectionID string `json:"connection_id"`
				Repository   string `json:"repository"`
			}](w, r)
			if err != nil {
				return err
			}
			baseURL, err := a.managedWebhookBaseURL(r)
			if err != nil {
				return err
			}
			source, err := a.resourceRepository(r.Context(), kind, r.PathValue("id"))
			if err != nil {
				return err
			}
			connection, err := c.Get(r.Context(), input.ConnectionID)
			if err != nil {
				return err
			}
			var secret string
			if kind == gitprovider.KindStack {
				secret, err = a.Stacks.WebhookSecret(r.Context(), r.PathValue("id"))
			} else {
				secret, err = a.Applications.WebhookSecret(r.Context(), r.PathValue("id"))
			}
			if err != nil {
				return err
			}
			link, err := c.Connect(r.Context(), gitprovider.LinkRequest{Kind: kind, ResourceID: r.PathValue("id"), ConnectionID: input.ConnectionID, Repository: input.Repository, Source: source, Hook: gitprovider.HookTarget{URL: baseURL + "/webhooks/" + kind + "s/" + r.PathValue("id") + "/" + connection.Provider, Secret: secret}})
			if err != nil {
				return err
			}
			return ok(w, link)
		})
	}
}

// Managed callbacks are sent to a Git provider and must use the address
// configured by an instance administrator.
func (a *API) managedWebhookBaseURL(r *http.Request) (string, error) {
	if a.Settings != nil {
		current, err := a.Settings.Get(r.Context())
		if err != nil {
			return "", err
		}
		parsed, err := url.Parse(current.PublicURL)
		if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
			parsed.Hostname() != "" && parsed.User == nil && !parsed.ForceQuery && parsed.RawQuery == "" && parsed.Fragment == "" {
			return current.PublicURL, nil
		}
	}
	return "", apperr.Invalid("Endereço de webhook inválido. Confira a URL pública do Pier em Configurações.")
}

func (a *API) linkedWebhookURL(r *http.Request, kind string) (string, error) {
	if a.GitConnections == nil {
		return "", nil
	}
	id := r.PathValue("id")
	link, err := a.GitConnections.Link(r.Context(), kind, id)
	if err != nil || !link.Linked {
		return "", err
	}
	baseURL, err := a.managedWebhookBaseURL(r)
	if err != nil {
		return "", err
	}
	return baseURL + "/webhooks/" + kind + "s/" + id + "/" + link.Provider, nil
}
