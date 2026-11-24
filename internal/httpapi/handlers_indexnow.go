package httpapi

import (
	"net/http"

	"pier/internal/indexnow"
)

// indexNowInput turns notification on or off for a host of a resource.
type indexNowInput struct {
	Host    string `json:"host"`
	Enabled bool   `json:"enabled"`
}

// indexNowSubmit names the host to submit now.
type indexNowSubmit struct {
	Host string `json:"host"`
}

func (a *API) registerIndexNowRoutes(mux *http.ServeMux) {
	for _, kind := range []string{indexnow.KindApplication, indexnow.KindStack} {
		base := "/api/v1/" + kind + "/{id}/indexnow"
		a.route(mux, "GET "+base, canRead, a.indexNowOverview(kind))
		a.route(mux, "PUT "+base, canWrite, a.setIndexNow(kind))
		a.route(mux, "POST "+base+"/submit", canDeploy, a.submitIndexNow(kind))
	}
	a.route(mux, "GET /api/v1/indexnow/sites", canRead, a.allIndexNowSites)
	// Search engines read the key here, through the proxy of the site. The
	// key is public by design and the answer never says why one is unknown.
	a.public(mux, "GET "+indexnow.KeyPath+"{file}", a.indexNowKey)
}

func (a *API) indexNowOverview(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		overview, err := a.IndexNow.Overview(r.Context(), kind, r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, overview)
	}
}

func (a *API) setIndexNow(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[indexNowInput](w, r)
		if err != nil {
			return err
		}
		overview, err := a.IndexNow.SetEnabled(r.Context(), kind, r.PathValue("id"), input.Host, input.Enabled)
		if err != nil {
			return err
		}
		return ok(w, overview)
	}
}

func (a *API) submitIndexNow(kind string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[indexNowSubmit](w, r)
		if err != nil {
			return err
		}
		state, err := a.IndexNow.SubmitNow(r.Context(), kind, r.PathValue("id"), input.Host)
		if err != nil {
			return err
		}
		return ok(w, state)
	}
}

func (a *API) indexNowKey(w http.ResponseWriter, r *http.Request) error {
	key, err := a.IndexNow.Key(r.Context(), r.PathValue("file"))
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(key))
	return err
}

func (a *API) allIndexNowSites(w http.ResponseWriter, r *http.Request) error {
	all, err := a.IndexNow.All(r.Context())
	if err != nil {
		return err
	}
	return ok(w, all)
}
