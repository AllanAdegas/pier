package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

const maxBodyBytes = 2 << 20

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func noContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func ok(w http.ResponseWriter, payload any) error {
	writeJSON(w, http.StatusOK, payload)
	return nil
}

func created(w http.ResponseWriter, payload any) error {
	writeJSON(w, http.StatusCreated, payload)
	return nil
}

func accepted(w http.ResponseWriter, payload any) error {
	writeJSON(w, http.StatusAccepted, payload)
	return nil
}

var statusByKind = map[apperr.Kind]int{
	apperr.KindInvalid:      http.StatusUnprocessableEntity,
	apperr.KindNotFound:     http.StatusNotFound,
	apperr.KindConflict:     http.StatusConflict,
	apperr.KindUnauthorized: http.StatusUnauthorized,
	apperr.KindForbidden:    http.StatusForbidden,
	apperr.KindUnavailable:  http.StatusBadGateway,
}

// writeError renders expected errors with their message and hides the
// details of unexpected ones, which are only logged.
func (a *API) writeError(w http.ResponseWriter, r *http.Request, err error) {
	language := i18n.FromContext(r.Context())
	if appErr, isExpected := apperr.As(err); isExpected {
		writeJSON(w, statusByKind[appErr.Kind], errorBody{Error: errorDetail{
			Code: string(appErr.Kind), Message: appErr.Localize(language), Fields: appErr.LocalizedFields(language),
		}})
		return
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{Error: errorDetail{
			Code: "payload_too_large", Message: language.Text("O corpo da requisição excede 2 MB."),
		}})
		return
	}
	a.Logger.Error("unhandled error", "method", r.Method, "path", r.URL.Path, "error", err)
	writeInternalError(w, r)
}

// writeInternalError answers a failure whose details only go to the log.
func writeInternalError(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusInternalServerError, errorBody{Error: errorDetail{
		Code: "internal_error", Message: i18n.FromContext(r.Context()).Text("Erro interno. Consulte os logs do Pier."),
	}})
}

// withLanguage answers each request in the language it asks for, or in the
// instance's.
func withLanguage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		language := i18n.FromAcceptLanguage(r.Header.Get("Accept-Language"), i18n.Instance())
		next.ServeHTTP(w, r.WithContext(i18n.WithLanguage(r.Context(), language)))
	})
}

// decode reads a JSON body into T, limiting its size.
func decode[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var payload T
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return payload, err
		}
		return payload, apperr.Invalid("JSON inválido no corpo da requisição.")
	}
	return payload, nil
}

func queryFlag(r *http.Request, name string) bool {
	value := r.URL.Query().Get(name)
	return value == "1" || value == "true"
}
