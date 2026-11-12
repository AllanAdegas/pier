package httpapi

import (
	"net/http"
	rbac "pier/internal/access"
	"pier/internal/identity"
	"pier/internal/platform/apperr"
)

// RegisterAccessRoutes keeps construction independent of auth's active changes.
// The coordinator calls this on the same API mux so audit/CSRF apply normally.
func (a *API) RegisterAccessRoutes(mux *http.ServeMux, service *rbac.Service) {
	a.route(mux, "GET /api/v1/projects/{id}/members", canRead, func(w http.ResponseWriter, r *http.Request) error {
		items, err := service.Members(r.Context(), principalFrom(r.Context()).User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, items)
	})
	a.route(mux, "PUT /api/v1/projects/{id}/members/{user}", sessionOnly, func(w http.ResponseWriter, r *http.Request) error {
		body, err := decode[struct {
			Role rbac.Role `json:"role"`
		}](w, r)
		if err != nil {
			return err
		}
		if err = service.SetMember(r.Context(), principalFrom(r.Context()).User.ID, r.PathValue("id"), r.PathValue("user"), body.Role); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "GET /api/v1/projects/{id}/invites", sessionOnly, func(w http.ResponseWriter, r *http.Request) error {
		items, err := service.Invites(r.Context(), principalFrom(r.Context()).User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, items)
	})
	a.route(mux, "POST /api/v1/projects/{id}/invites", sessionOnly, func(w http.ResponseWriter, r *http.Request) error {
		body, err := decode[struct {
			Email string    `json:"email"`
			Role  rbac.Role `json:"role"`
		}](w, r)
		if err != nil {
			return err
		}
		item, token, err := service.Invite(r.Context(), principalFrom(r.Context()).User.ID, r.PathValue("id"), body.Email, body.Role)
		if err != nil {
			return err
		}
		return created(w, struct {
			Invite rbac.Invite `json:"invite"`
			Token  string      `json:"token"`
		}{item, token})
	})
	a.route(mux, "DELETE /api/v1/projects/{id}/invites/{invite}", sessionOnly, func(w http.ResponseWriter, r *http.Request) error {
		if err := service.Revoke(r.Context(), principalFrom(r.Context()).User.ID, r.PathValue("id"), r.PathValue("invite")); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "POST /api/v1/access/accept", sessionOnly, func(w http.ResponseWriter, r *http.Request) error {
		body, err := decode[struct {
			Token string `json:"token"`
		}](w, r)
		if err != nil {
			return err
		}
		if err = service.Accept(r.Context(), principalFrom(r.Context()).User.ID, body.Token); err != nil {
			return err
		}
		return noContent(w)
	})
}

// RegisterOIDCRoutes must be called only when an allowlisted provider is enabled.
func (a *API) RegisterOIDCRoutes(mux *http.ServeMux, provider *identity.OIDCService) {
	a.route(mux, "DELETE /api/v1/access/oidc", sessionOnly, func(w http.ResponseWriter, r *http.Request) error {
		if err := provider.Disable(r.Context(), principalFrom(r.Context()).User.ID); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "POST /api/v1/access/oidc/unlink", sessionOnly, func(w http.ResponseWriter, r *http.Request) error {
		body, err := decode[struct {
			Issuer  string `json:"issuer"`
			Subject string `json:"subject"`
		}](w, r)
		if err != nil {
			return err
		}
		if err = provider.Unlink(r.Context(), principalFrom(r.Context()).User.ID, body.Issuer, body.Subject); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "PUT /api/v1/access/oidc", sessionOnly, func(w http.ResponseWriter, r *http.Request) error {
		body, err := decode[identity.OIDCConfig](w, r)
		if err != nil {
			return err
		}
		if err = provider.Configure(r.Context(), principalFrom(r.Context()).User.ID, body); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "POST /api/v1/access/oidc/links", sessionOnly, func(w http.ResponseWriter, r *http.Request) error {
		body, err := decode[struct {
			Issuer  string `json:"issuer"`
			Subject string `json:"subject"`
			UserID  string `json:"user_id"`
		}](w, r)
		if err != nil {
			return err
		}
		if err = provider.Link(r.Context(), principalFrom(r.Context()).User.ID, body.Issuer, body.Subject, body.UserID); err != nil {
			return err
		}
		return noContent(w)
	})
	a.public(mux, "GET /api/v1/access/oidc/start", func(w http.ResponseWriter, r *http.Request) error {
		if !a.allowGuess(r, "oidc") {
			return tooManyAttempts(w, "Muitas tentativas. Aguarde um minuto.")
		}
		target, binding, err := provider.Begin(r.Context())
		if err != nil {
			return err
		}
		http.SetCookie(w, &http.Cookie{Name: "__Host-pier_oidc", Value: binding, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusFound)
		return nil
	})
	a.public(mux, "GET /api/v1/access/oidc/callback", func(w http.ResponseWriter, r *http.Request) error {
		binding, err := r.Cookie("__Host-pier_oidc")
		if err != nil {
			return apperr.Forbidden("SSO não autorizado.")
		}
		http.SetCookie(w, &http.Cookie{Name: "__Host-pier_oidc", Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		token, err := a.Identity.OpenOIDCSession(r.Context(), provider, r.URL.Query().Get("state"), binding.Value, r.URL.Query().Get("code"), a.sessionClient(r))
		if err != nil {
			return err
		}
		a.setSessionCookie(w, r, token)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
	})
}
