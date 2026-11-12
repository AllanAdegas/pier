package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"path"

	"pier/internal/volumebackup"
)

// volumeResourcePaths maps the URL collection to the resource kind.
var volumeResourcePaths = map[string]string{
	"applications": volumebackup.KindApplication,
	"stacks":       volumebackup.KindStack,
}

func (a *API) registerVolumeBackupRoutes(mux *http.ServeMux) {
	for collection, kind := range volumeResourcePaths {
		base := "/api/v1/" + collection + "/{id}"
		a.route(mux, "GET "+base+"/volume-mounts", canRead, a.volumeResource(kind, a.listVolumeMounts))
		a.route(mux, "GET "+base+"/volume-backup-policy", canRead, a.volumeResource(kind, a.getVolumePolicy))
		a.route(mux, "PUT "+base+"/volume-backup-policy", canWrite, a.volumeResource(kind, a.putVolumePolicy))
		a.route(mux, "GET "+base+"/volume-backups", canRead, a.volumeResource(kind, a.listVolumeBackups))
		a.route(mux, "POST "+base+"/volume-backups", canDeploy, a.volumeResource(kind, a.startVolumeBackup))
		a.route(mux, "GET "+base+"/volume-operations", canRead, a.volumeResource(kind, a.listVolumeOperations))
	}
	a.route(mux, "DELETE /api/v1/volume-backups/{id}", canWrite, a.deleteVolumeBackup)
	a.route(mux, "GET /api/v1/volume-backups/{id}/download", canRead, a.downloadVolumeBackup)
	a.route(mux, "POST /api/v1/volume-backups/{id}/restore", canWrite, a.restoreVolumeBackup)
}

type volumeHandler func(w http.ResponseWriter, r *http.Request, kind, id string) error

func (a *API) volumeResource(kind string, handler volumeHandler) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		return handler(w, r, kind, r.PathValue("id"))
	}
}

func (a *API) listVolumeMounts(w http.ResponseWriter, r *http.Request, kind, id string) error {
	mounts, err := a.VolumeBackups.Mounts(r.Context(), kind, id)
	if err != nil {
		return err
	}
	return ok(w, mounts)
}

func (a *API) getVolumePolicy(w http.ResponseWriter, r *http.Request, kind, id string) error {
	policy, err := a.VolumeBackups.Policy(r.Context(), kind, id)
	if err != nil {
		return err
	}
	return ok(w, policy)
}

func (a *API) putVolumePolicy(w http.ResponseWriter, r *http.Request, kind, id string) error {
	policy, err := decode[volumebackup.Policy](w, r)
	if err != nil {
		return err
	}
	saved, err := a.VolumeBackups.SetPolicy(r.Context(), kind, id, policy)
	if err != nil {
		return err
	}
	return ok(w, saved)
}

func (a *API) listVolumeBackups(w http.ResponseWriter, r *http.Request, kind, id string) error {
	backups, err := a.VolumeBackups.List(r.Context(), kind, id)
	if err != nil {
		return err
	}
	return ok(w, backups)
}

func (a *API) startVolumeBackup(w http.ResponseWriter, r *http.Request, kind, id string) error {
	backup, err := a.VolumeBackups.Start(r.Context(), kind, id, volumebackup.TriggerManual)
	if err != nil {
		return err
	}
	return accepted(w, backup)
}

func (a *API) listVolumeOperations(w http.ResponseWriter, r *http.Request, kind, id string) error {
	operations, err := a.VolumeBackups.Operations(r.Context(), kind, id)
	if err != nil {
		return err
	}
	return ok(w, operations)
}

func (a *API) deleteVolumeBackup(w http.ResponseWriter, r *http.Request) error {
	if err := a.VolumeBackups.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) restoreVolumeBackup(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[volumebackup.RestoreInput](w, r)
	if err != nil {
		return err
	}
	operation, err := a.VolumeBackups.StartRestore(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return accepted(w, operation)
}

func (a *API) downloadVolumeBackup(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	reader, backup, err := a.VolumeBackups.Open(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(backup.FilePath)))
	if backup.SizeBytes > 0 {
		w.Header().Set("Content-Length", fmt.Sprint(backup.SizeBytes))
	}
	_, _ = io.Copy(w, reader)
	return nil
}
