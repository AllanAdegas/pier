package httpapi

import (
	"net/http"

	"pier/internal/instance"
)

func (a *API) registerInstanceRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/instance/onboarding", canRead, func(w http.ResponseWriter, r *http.Request) error {
		o, err := a.Instance.Onboarding(r.Context())
		if err != nil {
			return err
		}
		return ok(w, o)
	})
	a.route(mux, "POST /api/v1/instance/backups", canWrite, a.instanceBackup)
	a.route(mux, "GET /api/v1/instance/backups/status", canRead, a.instanceBackupStatus)
	a.route(mux, "GET /api/v1/instance/backups/config", canWrite, a.instanceBackupConfig)
	a.route(mux, "PATCH /api/v1/instance/backups/config", canWrite, a.configureInstanceBackup)
}
func (a *API) instanceBackup(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[struct {
		Passphrase string `json:"passphrase"`
	}](w, r)
	if err != nil {
		return err
	}
	archive, err := a.Instance.Create(r.Context(), input.Passphrase)
	if err != nil {
		return err
	}
	if err = a.Instance.MarkComplete(r.Context()); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="pier-instance.pierbak"`)
	w.Header().Set("Cache-Control", "no-store")
	_, err = w.Write(archive)
	return err
}
func (a *API) instanceBackupStatus(w http.ResponseWriter, r *http.Request) error {
	status, err := a.Instance.Status(r.Context())
	if err != nil {
		return err
	}
	return ok(w, status)
}
func (a *API) instanceBackupConfig(w http.ResponseWriter, r *http.Request) error {
	c, err := a.Instance.Configuration(r.Context())
	if err != nil {
		return err
	}
	return ok(w, c)
}
func (a *API) configureInstanceBackup(w http.ResponseWriter, r *http.Request) error {
	c, err := decode[instance.BackupConfig](w, r)
	if err != nil {
		return err
	}
	c, err = a.Instance.Configure(r.Context(), c)
	if err != nil {
		return err
	}
	return ok(w, c)
}
