package httpapi

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"pier/internal/gitprovider"
	"strconv"
)

func (a *API) registerGitProviderRoutes(mux *http.ServeMux) {
	if a.GitProviders == nil {
		return
	}
	a.route(mux, "GET /api/v1/git-provider", canRead, func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, map[string]bool{"configured": a.GitProviders.Configured(r.Context())})
	})
	a.route(mux, "PUT /api/v1/git-provider", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		v, err := decode[gitprovider.Config](w, r)
		if err != nil {
			return err
		}
		if err = a.GitProviders.Configure(r.Context(), v); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "GET /api/v1/git-provider/installations", canRead, func(w http.ResponseWriter, r *http.Request) error {
		rows, err := a.GitProviders.Installations(r.Context())
		if err != nil {
			return err
		}
		return ok(w, rows)
	})
	a.route(mux, "GET /api/v1/git-provider/installations/{installation}/repositories", canRead, func(w http.ResponseWriter, r *http.Request) error {
		id, err := strconv.ParseInt(r.PathValue("installation"), 10, 64)
		if err != nil {
			return err
		}
		rows, err := a.GitProviders.Repositories(r.Context(), id)
		if err != nil {
			return err
		}
		return ok(w, rows)
	})
	a.route(mux, "GET /api/v1/git-provider/{kind}/{id}", canRead, func(w http.ResponseWriter, r *http.Request) error {
		selection, err := a.GitProviders.Selection(r.Context(), r.PathValue("kind"), r.PathValue("id"))
		if errors.Is(err, sql.ErrNoRows) {
			return ok(w, map[string]any{"selected": false})
		}
		if err != nil {
			return err
		}
		return ok(w, selection)
	})
	a.route(mux, "DELETE /api/v1/git-provider/{kind}/{id}", canDeploy, func(w http.ResponseWriter, r *http.Request) error {
		if err := a.GitProviders.ClearSelection(r.Context(), r.PathValue("kind"), r.PathValue("id")); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "PUT /api/v1/git-provider/{kind}/{id}", canDeploy, func(w http.ResponseWriter, r *http.Request) error {
		v, err := decode[gitprovider.Selection](w, r)
		if err != nil {
			return err
		}
		if err = a.GitProviders.Select(r.Context(), r.PathValue("kind"), r.PathValue("id"), v); err != nil {
			return err
		}
		return noContent(w)
	})
	mux.HandleFunc("POST /webhooks/git-provider/github", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err == nil {
			err = a.GitProviders.Webhook(r.Context(), r.Header.Get("X-GitHub-Delivery"), r.Header.Get("X-GitHub-Event"), r.Header.Get("X-Hub-Signature-256"), body)
		}
		if err != nil {
			http.Error(w, "Webhook recusado.", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
