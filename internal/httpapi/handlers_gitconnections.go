package httpapi

import (
	"net/http"
	"pier/internal/gitprovider"
	"strconv"
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
			link, err := c.Connect(r.Context(), gitprovider.LinkRequest{Kind: kind, ResourceID: r.PathValue("id"), ConnectionID: input.ConnectionID, Repository: input.Repository, Source: source, Hook: gitprovider.HookTarget{URL: a.publicURL(r) + "/webhooks/" + kind + "s/" + r.PathValue("id") + "/" + connection.Provider, Secret: secret}})
			if err != nil {
				return err
			}
			return ok(w, link)
		})
	}
}
