package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"pier/internal/application"
	"pier/internal/platform/apperr"
)

// deployRequest is the optional body of a deploy: a Git tag to deploy
// instead of the configured branch.
type deployRequest struct {
	Ref string `json:"ref"`
}

// decodeOptional accepts an empty body as the zero value, so existing
// clients that post nothing keep working.
func decodeOptional[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var payload T
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return payload, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return payload, apperr.Invalid("JSON inválido no corpo da requisição.")
	}
	return payload, nil
}

func deployTrigger(r *http.Request) application.Trigger {
	if principalFrom(r.Context()).ViaToken {
		return application.TriggerAPI
	}
	return application.TriggerManual
}

func (a *API) deployArchive(w http.ResponseWriter, r *http.Request) error {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/gzip" && mediaType != "application/x-gzip" {
		return apperr.Invalid("Envie o código como um .tar.gz (Content-Type: application/gzip).")
	}
	body := http.MaxBytesReader(w, r.Body, application.MaxArchiveBytes+1)
	deployment, err := a.Applications.DeployArchive(r.Context(), r.PathValue("id"), body)
	if err != nil {
		return err
	}
	return created(w, deployment)
}

type promoteRequest struct {
	TargetApplicationID string `json:"target_application_id"`
}

func (a *API) promoteApplication(w http.ResponseWriter, r *http.Request) error {
	request, err := decode[promoteRequest](w, r)
	if err != nil {
		return err
	}
	deployment, err := a.Applications.Promote(r.Context(), r.PathValue("id"), request.TargetApplicationID)
	if err != nil {
		return err
	}
	return created(w, deployment)
}
