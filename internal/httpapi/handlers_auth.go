package httpapi

import (
	"net/http"

	"pier/internal/identity"
	"pier/internal/platform/apperr"
)

func (a *API) registerAuthRoutes(mux *http.ServeMux) {
	a.public(mux, "GET /api/v1/auth/status", a.authStatus)
	a.public(mux, "POST /api/v1/auth/setup", a.setup)
	a.public(mux, "POST /api/v1/auth/login", a.login)
	a.public(mux, "POST /api/v1/auth/logout", a.logout)
	a.route(mux, "PATCH /api/v1/me", sessionOnly, a.updateProfile)
	a.route(mux, "POST /api/v1/me/password", sessionOnly, a.changePassword)
	a.route(mux, "GET /api/v1/users", canRead, a.listUsers)
	a.route(mux, "POST /api/v1/users", sessionOnly, a.createUser)
	a.route(mux, "DELETE /api/v1/users/{id}", sessionOnly, a.deleteUser)
	a.route(mux, "GET /api/v1/tokens", sessionOnly, a.listTokens)
	a.route(mux, "POST /api/v1/tokens", sessionOnly, a.createToken)
	a.route(mux, "DELETE /api/v1/tokens/{id}", sessionOnly, a.deleteToken)
}

type authStatusResponse struct {
	NeedsSetup    bool           `json:"needs_setup"`
	Authenticated bool           `json:"authenticated"`
	User          *identity.User `json:"user"`
}

func (a *API) authStatus(w http.ResponseWriter, r *http.Request) error {
	needsSetup, err := a.Identity.NeedsSetup(r.Context())
	if err != nil {
		return err
	}
	response := authStatusResponse{NeedsSetup: needsSetup}
	if principal, err := a.resolvePrincipal(r); err == nil {
		response.Authenticated = true
		response.User = &principal.User
	}
	return ok(w, response)
}

type credentialsRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *API) setup(w http.ResponseWriter, r *http.Request) error {
	// Guessing the setup code is throttled like guessing a password.
	if !a.loginLimiter.allow(clientAddress(r)) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, errorBody{Error: errorDetail{
			Code: "rate_limited", Message: "Muitas tentativas. Aguarde um minuto.",
		}})
		return nil
	}
	request, err := decode[identity.SetupRequest](w, r)
	if err != nil {
		return err
	}
	user, err := a.Identity.Setup(r.Context(), request)
	if err != nil {
		return err
	}
	token, err := a.Identity.OpenSession(r.Context(), user.ID)
	if err != nil {
		return err
	}
	a.setSessionCookie(w, r, token)
	return created(w, user)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) error {
	if !a.loginLimiter.allow(clientAddress(r)) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, errorBody{Error: errorDetail{
			Code: "rate_limited", Message: "Muitas tentativas de login. Aguarde um minuto.",
		}})
		return nil
	}
	request, err := decode[credentialsRequest](w, r)
	if err != nil {
		return err
	}
	token, user, err := a.Identity.Login(r.Context(), request.Email, request.Password)
	if err != nil {
		return err
	}
	a.setSessionCookie(w, r, token)
	return ok(w, user)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) error {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_ = a.Identity.Logout(r.Context(), cookie.Value)
	}
	clearSessionCookie(w, r)
	return noContent(w)
}

func (a *API) updateProfile(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[credentialsRequest](w, r)
	if err != nil {
		return err
	}
	user, err := a.Identity.UpdateProfile(r.Context(), principalFrom(r.Context()).User.ID, request.Name, request.Email)
	if err != nil {
		return err
	}
	return ok(w, user)
}

type passwordRequest struct {
	Current string `json:"current"`
	Next    string `json:"next"`
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[passwordRequest](w, r)
	if err != nil {
		return err
	}
	userID := principalFrom(r.Context()).User.ID
	if err := a.Identity.ChangePassword(r.Context(), userID, request.Current, request.Next); err != nil {
		return err
	}
	// Every session was invalidated; open a new one for the current browser.
	token, err := a.Identity.OpenSession(r.Context(), userID)
	if err != nil {
		return err
	}
	a.setSessionCookie(w, r, token)
	return noContent(w)
}

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) error {
	users, err := a.Identity.ListUsers(r.Context())
	if err != nil {
		return err
	}
	return ok(w, users)
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[credentialsRequest](w, r)
	if err != nil {
		return err
	}
	user, err := a.Identity.CreateUser(r.Context(), request.Name, request.Email, request.Password)
	if err != nil {
		return err
	}
	return created(w, user)
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) error {
	actor := principalFrom(r.Context()).User.ID
	if err := a.Identity.DeleteUser(r.Context(), actor, r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) listTokens(w http.ResponseWriter, r *http.Request) error {
	tokens, err := a.Identity.ListTokens(r.Context())
	if err != nil {
		return err
	}
	return ok(w, tokens)
}

type tokenRequest struct {
	Name      string             `json:"name"`
	Abilities []identity.Ability `json:"abilities"`
}

type tokenResponse struct {
	identity.APIToken
	Secret string `json:"secret"`
}

func (a *API) createToken(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[tokenRequest](w, r)
	if err != nil {
		return err
	}
	userID := principalFrom(r.Context()).User.ID
	token, secret, err := a.Identity.CreateToken(r.Context(), userID, request.Name, request.Abilities)
	if err != nil {
		return err
	}
	return created(w, tokenResponse{APIToken: token, Secret: secret})
}

func (a *API) deleteToken(w http.ResponseWriter, r *http.Request) error {
	if err := a.Identity.DeleteToken(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

// requireSensitive guards secrets (credentials, variable values): read-only
// tokens may list resources but never see secret material.
func requireSensitive(r *http.Request) error {
	if !principalFrom(r.Context()).Can(identity.AbilityWrite) {
		return apperr.Forbidden("Dados sensíveis exigem um token com a permissão \"write\".")
	}
	return nil
}
