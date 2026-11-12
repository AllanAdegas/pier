package httpapi

import (
	"net/http"
	"strings"

	"pier/internal/identity"
	"pier/internal/platform/i18n"
)

// registerAccountRoutes adds the account-security endpoints: second factor,
// password reset by e-mail and the caller's own sessions.
func (a *API) registerAccountRoutes(mux *http.ServeMux) {
	a.public(mux, "POST /api/v1/auth/login/2fa", a.loginSecondFactor)
	a.public(mux, "POST /api/v1/auth/password-reset", a.requestPasswordReset)
	a.public(mux, "POST /api/v1/auth/password-reset/confirm", a.confirmPasswordReset)
	a.route(mux, "GET /api/v1/me/2fa", sessionOnly, a.twoFactorStatus)
	a.route(mux, "POST /api/v1/me/2fa/enroll", sessionOnly, a.beginTwoFactor)
	a.route(mux, "POST /api/v1/me/2fa/confirm", sessionOnly, a.confirmTwoFactor)
	a.route(mux, "POST /api/v1/me/2fa/disable", sessionOnly, a.disableTwoFactor)
	a.route(mux, "POST /api/v1/me/2fa/recovery-codes", sessionOnly, a.regenerateRecoveryCodes)
	a.route(mux, "GET /api/v1/me/sessions", sessionOnly, a.listSessions)
	a.route(mux, "POST /api/v1/me/sessions/revoke-others", sessionOnly, a.revokeOtherSessions)
	a.route(mux, "DELETE /api/v1/me/sessions/{id}", sessionOnly, a.revokeSession)
}

type secondFactorRequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

func (a *API) loginSecondFactor(w http.ResponseWriter, r *http.Request) error {
	hideCodesFromAudit(r)
	request, err := decode[secondFactorRequest](w, r)
	if err != nil {
		return err
	}
	language := i18n.FromContext(r.Context())
	if !a.addressLimiter.allow(a.proxies.clientAddress(r)) {
		return tooManyAttempts(w, language.Text("Muitas tentativas de login. Aguarde um minuto."))
	}
	userID, err := a.Identity.ChallengeUser(request.Challenge)
	if err != nil {
		return err
	}
	target := "2fa:" + userID
	if !a.accountLimiter.allow(target) {
		return tooManyAttempts(w, language.Text("Muitas tentativas de login. Aguarde um minuto."))
	}
	outcome, err := a.Identity.CompleteLogin(r.Context(), request.Challenge, request.Code, a.sessionClient(r))
	if err != nil {
		return err
	}
	auditActor(r, identity.Principal{User: outcome.User})
	a.accountLimiter.reset(target)
	a.setSessionCookie(w, r, outcome.SessionToken)
	return ok(w, outcome.User)
}

type resetRequest struct {
	Email string `json:"email"`
}

// requestPasswordReset always answers 202 for a well-formed request, so the
// answer never says whether the e-mail has an account.
func (a *API) requestPasswordReset(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[resetRequest](w, r)
	if err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(request.Email))
	if !a.addressLimiter.allow(a.proxies.clientAddress(r)) || !a.resetLimiter.allow(email) {
		return tooManyAttempts(w, i18n.FromContext(r.Context()).Text("Muitas tentativas. Aguarde um minuto."))
	}
	if err := a.Identity.RequestPasswordReset(r.Context(), email, a.accountPublicURL(r)); err != nil {
		return err
	}
	return accepted(w, map[string]string{"status": "sent"})
}

type resetConfirmRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (a *API) confirmPasswordReset(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[resetConfirmRequest](w, r)
	if err != nil {
		return err
	}
	if !a.addressLimiter.allow(a.proxies.clientAddress(r)) {
		return tooManyAttempts(w, i18n.FromContext(r.Context()).Text("Muitas tentativas. Aguarde um minuto."))
	}
	if err := a.Identity.ResetPassword(r.Context(), request.Token, request.Password); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) twoFactorStatus(w http.ResponseWriter, r *http.Request) error {
	status, err := a.Identity.TwoFactorStatus(r.Context(), principalFrom(r.Context()).User.ID)
	if err != nil {
		return err
	}
	return ok(w, status)
}

func (a *API) beginTwoFactor(w http.ResponseWriter, r *http.Request) error {
	enrollment, err := a.Identity.BeginTwoFactor(r.Context(), principalFrom(r.Context()).User.ID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, enrollment)
}

type codeRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func (a *API) confirmTwoFactor(w http.ResponseWriter, r *http.Request) error {
	request, userID, err := a.decodeCodeRequest(w, r)
	if err != nil {
		return err
	}
	codes, err := a.Identity.ConfirmTwoFactor(r.Context(), userID, request.Code)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, recoveryCodesResponse{RecoveryCodes: codes})
}

func (a *API) disableTwoFactor(w http.ResponseWriter, r *http.Request) error {
	request, userID, err := a.decodeCodeRequest(w, r)
	if err != nil {
		return err
	}
	if err := a.Identity.DisableTwoFactor(r.Context(), userID, request.Password, request.Code); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) error {
	request, userID, err := a.decodeCodeRequest(w, r)
	if err != nil {
		return err
	}
	codes, err := a.Identity.RegenerateRecoveryCodes(r.Context(), userID, request.Code)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, recoveryCodesResponse{RecoveryCodes: codes})
}

// decodeCodeRequest reads a form that carries a code (and maybe the
// password), throttling guesses per account.
func (a *API) decodeCodeRequest(w http.ResponseWriter, r *http.Request) (codeRequest, string, error) {
	hideCodesFromAudit(r)
	request, err := decode[codeRequest](w, r)
	if err != nil {
		return codeRequest{}, "", err
	}
	userID := principalFrom(r.Context()).User.ID
	if !a.accountLimiter.allow("2fa-manage:" + userID) {
		return codeRequest{}, "", tooManyAttempts(w, i18n.FromContext(r.Context()).Text("Muitas tentativas. Aguarde um minuto."))
	}
	return request, userID, nil
}

func (a *API) listSessions(w http.ResponseWriter, r *http.Request) error {
	principal := principalFrom(r.Context())
	sessions, err := a.Identity.ListSessions(r.Context(), principal.User.ID, principal.SessionID)
	if err != nil {
		return err
	}
	return ok(w, sessions)
}

func (a *API) revokeSession(w http.ResponseWriter, r *http.Request) error {
	principal := principalFrom(r.Context())
	if err := a.Identity.RevokeSession(r.Context(), principal.User.ID, r.PathValue("id")); err != nil {
		return err
	}
	if r.PathValue("id") == principal.SessionID {
		a.clearSessionCookie(w, r)
	}
	return noContent(w)
}

func (a *API) revokeOtherSessions(w http.ResponseWriter, r *http.Request) error {
	principal := principalFrom(r.Context())
	revoked, err := a.Identity.RevokeOtherSessions(r.Context(), principal.User.ID, principal.SessionID)
	if err != nil {
		return err
	}
	return ok(w, map[string]int64{"revoked": revoked})
}

// sessionClient describes the browser opening a session, for the list.
func (a *API) sessionClient(r *http.Request) identity.Client {
	return identity.Client{IP: a.proxies.clientAddress(r), UserAgent: r.UserAgent()}
}

// publicURL is where reset links point; empty when not configured.
func (a *API) accountPublicURL(r *http.Request) string {
	if a.Settings == nil {
		return ""
	}
	current, err := a.Settings.Get(r.Context())
	if err != nil {
		return ""
	}
	return current.PublicURL
}

// hideCodesFromAudit keeps one-time codes and recovery codes out of the
// audit log, whose redaction does not know the "code" field.
func hideCodesFromAudit(r *http.Request) {
	auditSummary(r, map[string]string{"code": "[redacted]"})
}
