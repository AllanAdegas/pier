package httpapi

import (
	"net/http"
	"strings"

	"pier/internal/identity"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
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
	a.registerAccountRoutes(mux)
}

type authStatusResponse struct {
	NeedsSetup    bool           `json:"needs_setup"`
	Authenticated bool           `json:"authenticated"`
	User          *identity.User `json:"user"`
	// PasswordReset tells the login page whether to offer "forgot password".
	PasswordReset bool `json:"password_reset"`
}

func (a *API) authStatus(w http.ResponseWriter, r *http.Request) error {
	needsSetup, err := a.Identity.NeedsSetup(r.Context())
	if err != nil {
		return err
	}
	response := authStatusResponse{NeedsSetup: needsSetup,
		PasswordReset: a.Identity.PasswordResetAvailable(r.Context(), a.accountPublicURL(r))}
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
	request, err := decode[identity.SetupRequest](w, r)
	if err != nil {
		return err
	}
	// Guessing the setup code is throttled like guessing a password.
	if !a.allowGuess(r, "setup") {
		return tooManyAttempts(w, i18n.FromContext(r.Context()).Text("Muitas tentativas. Aguarde um minuto."))
	}
	user, err := a.Identity.Setup(r.Context(), request)
	if err != nil {
		return err
	}
	if a.Access != nil { if err := a.Access.Bootstrap(r.Context()); err != nil { return err } }
	if err := a.Settings.AdoptLanguage(r.Context(), i18n.FromContext(r.Context())); err != nil {
		return err
	}
	token, err := a.Identity.StartSession(r.Context(), user.ID, a.sessionClient(r))
	if err != nil {
		return err
	}
	a.setSessionCookie(w, r, token)
	auditActor(r, identity.Principal{User: user})
	return created(w, user)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[credentialsRequest](w, r)
	if err != nil {
		return err
	}
	if len(request.Email) > 320 {
		return apperr.Invalid("Informe um e-mail com até 320 caracteres.")
	}
	target := "login:" + strings.ToLower(strings.TrimSpace(request.Email))
	if !a.allowGuess(r, target) {
		return tooManyAttempts(w, i18n.FromContext(r.Context()).Text("Muitas tentativas de login. Aguarde um minuto."))
	}
	outcome, err := a.Identity.Login(r.Context(), request.Email, request.Password, a.sessionClient(r))
	if err != nil {
		return err
	}
	auditActor(r, identity.Principal{User: outcome.User})
	a.accountLimiter.reset(target)
	if outcome.Challenge != "" {
		return ok(w, challengeResponse{TwoFactorRequired: true, Challenge: outcome.Challenge,
			ExpiresIn: int(outcome.ChallengeTTL.Seconds())})
	}
	a.setSessionCookie(w, r, outcome.SessionToken)
	return ok(w, outcome.User)
}

// challengeResponse is the login answer for accounts with a second factor:
// no session yet, only the challenge to send with the code.
type challengeResponse struct {
	TwoFactorRequired bool   `json:"two_factor_required"`
	Challenge         string `json:"challenge"`
	ExpiresIn         int    `json:"expires_in"`
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) error {
	if principal, err := a.resolvePrincipal(r); err == nil {
		auditActor(r, principal)
	}
	a.clearSessionCookie(w, r)
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if err := a.Identity.Logout(r.Context(), cookie.Value); err != nil {
			return err
		}
	}
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
	if !a.accountLimiter.allow("password:" + userID) {
		return tooManyAttempts(w, i18n.FromContext(r.Context()).Text("Muitas tentativas. Aguarde um minuto."))
	}
	if err := a.Identity.ChangePassword(r.Context(), userID, request.Current, request.Next); err != nil {
		return err
	}
	// Every session was invalidated; open a new one for the current browser.
	token, err := a.Identity.StartSession(r.Context(), userID, a.sessionClient(r))
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
	own := tokens[:0]
 for _, token := range tokens { if token.UserID == principalFrom(r.Context()).User.ID { own = append(own,token) } }
 return ok(w, own)
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

// allowGuess throttles a credential guess both per client address and per
// target (an account, or the setup code). Rotating addresses — possible for
// anyone a trusted proxy forwards — still runs into the per-target limit.
func (a *API) allowGuess(r *http.Request, target string) bool {
	return a.addressLimiter.allow(a.proxies.clientAddress(r)) && a.accountLimiter.allow(target)
}

func tooManyAttempts(w http.ResponseWriter, translated string) error {
	w.Header().Set("Retry-After", "60")
	writeJSON(w, http.StatusTooManyRequests, errorBody{Error: errorDetail{Code: "rate_limited", Message: translated}})
	return nil
}
