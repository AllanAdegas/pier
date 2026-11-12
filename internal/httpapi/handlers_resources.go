package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"path"

	"pier/internal/database"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/settings"
	"pier/internal/stack"
)

func (a *API) registerDatabaseRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/settings/backup-s3", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		value, err := a.Databases.S3Settings(r.Context())
		if err != nil {
			return err
		}
		return ok(w, value)
	})
	a.route(mux, "PATCH /api/v1/settings/backup-s3", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		value, err := decode[settings.S3Settings](w, r)
		if err != nil {
			return err
		}
		if err := a.Databases.UpdateS3Settings(r.Context(), value); err != nil {
			return err
		}
		return noContent(w)
	})
	a.route(mux, "POST /api/v1/backups/{id}/restore", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		input, err := decode[database.RestoreInput](w, r)
		if err != nil {
			return err
		}
		op, err := a.Databases.StartRestore(r.Context(), r.PathValue("id"), input)
		if err != nil {
			return err
		}
		return accepted(w, op)
	})
	a.route(mux, "POST /api/v1/databases/{id}/restore-tests", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		op, err := a.Databases.StartRestoreTest(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return accepted(w, op)
	})
	a.route(mux, "GET /api/v1/databases/{id}/operations", canRead, func(w http.ResponseWriter, r *http.Request) error {
		ops, err := a.Databases.ListOperations(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return ok(w, ops)
	})

	a.route(mux, "GET /api/v1/database-engines", canRead, func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, database.Engines(i18n.FromContext(r.Context())))
	})
	a.route(mux, "GET /api/v1/databases", canRead, a.listDatabases)
	a.route(mux, "POST /api/v1/databases", canWrite, a.createDatabase)
	a.route(mux, "GET /api/v1/databases/{id}", canRead, a.getDatabase)
	a.route(mux, "PATCH /api/v1/databases/{id}", canWrite, a.updateDatabase)
	a.route(mux, "DELETE /api/v1/databases/{id}", canWrite, a.deleteDatabase)
	a.route(mux, "POST /api/v1/databases/{id}/actions/{action}", canDeploy, a.operateDatabase)
	a.route(mux, "GET /api/v1/databases/{id}/connection", canRead, a.databaseConnection)
	a.route(mux, "GET /api/v1/databases/{id}/containers", canRead, a.databaseContainers)
	a.route(mux, "GET /api/v1/databases/{id}/backups", canRead, a.listBackups)
	a.route(mux, "POST /api/v1/databases/{id}/backups", canDeploy, a.startBackup)
	a.route(mux, "DELETE /api/v1/backups/{id}", canWrite, a.deleteBackup)
	a.route(mux, "GET /api/v1/backups/{id}/download", canRead, a.downloadBackup)
}

func (a *API) listDatabases(w http.ResponseWriter, r *http.Request) error {
	databases, err := a.Databases.List(r.Context(), r.URL.Query().Get("environment_id"))
	if err != nil {
		return err
	}
	return ok(w, databases)
}

func (a *API) getDatabase(w http.ResponseWriter, r *http.Request) error {
	item, err := a.Databases.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, item)
}

func (a *API) createDatabase(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[database.CreateInput](w, r)
	if err != nil {
		return err
	}
	if err := a.requirePlacement(r, input.EnvironmentID, input.ServerID); err != nil {
		return err
	}
	item, err := a.Databases.Create(r.Context(), input)
	if err != nil {
		return err
	}
	return created(w, item)
}

// requirePlacement checks that the environment and server of a new resource exist.
func (a *API) requirePlacement(r *http.Request, environmentID, serverID string) error {
	if environmentID != "" {
		if _, err := a.Projects.GetEnvironment(r.Context(), environmentID); err != nil {
			return err
		}
	}
	if serverID != "" {
		if _, err := a.Servers.Get(r.Context(), serverID); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) updateDatabase(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[database.UpdateInput](w, r)
	if err != nil {
		return err
	}
	item, err := a.Databases.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return ok(w, item)
}

func (a *API) deleteDatabase(w http.ResponseWriter, r *http.Request) error {
	database, err := a.Databases.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	if err := a.checkDestroy(r, database.EnvironmentID); err != nil {
		return err
	}
	err = a.Databases.Delete(r.Context(), r.PathValue("id"), queryFlag(r, "volumes"), queryFlag(r, "force"))
	if err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) operateDatabase(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "start":
		err = a.Databases.Start(r.Context(), id)
	case "stop":
		err = a.Databases.Stop(r.Context(), id)
	case "restart":
		err = a.Databases.Restart(r.Context(), id)
	default:
		err = apperr.NotFound("Ação")
	}
	if err != nil {
		return err
	}
	return a.getDatabase(w, r)
}

func (a *API) databaseConnection(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	connection, err := a.Databases.Connection(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, connection)
}

func (a *API) databaseContainers(w http.ResponseWriter, r *http.Request) error {
	containers, err := a.Databases.Containers(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, containers)
}

func (a *API) listBackups(w http.ResponseWriter, r *http.Request) error {
	backups, err := a.Databases.ListBackups(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, backups)
}

func (a *API) startBackup(w http.ResponseWriter, r *http.Request) error {
	backup, err := a.Databases.StartBackup(r.Context(), r.PathValue("id"), database.BackupTriggerManual)
	if err != nil {
		return err
	}
	return accepted(w, backup)
}

func (a *API) deleteBackup(w http.ResponseWriter, r *http.Request) error {
	if err := a.Databases.DeleteBackup(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) downloadBackup(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	reader, backup, err := a.Databases.OpenBackup(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(backup.FilePath)))
	if backup.SizeBytes > 0 {
		w.Header().Set("Content-Length", fmt.Sprint(backup.SizeBytes))
	}
	_, _ = io.Copy(w, reader)
	return nil
}

func (a *API) registerStackRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/stack-templates", canRead, a.listStackTemplates)
	a.route(mux, "GET /api/v1/stack-templates/{slug}", canRead, a.getStackTemplate)
	a.route(mux, "GET /api/v1/stacks", canRead, a.listStacks)
	a.route(mux, "POST /api/v1/stacks", canWrite, a.createStack)
	a.route(mux, "GET /api/v1/stacks/{id}", canRead, a.getStack)
	a.route(mux, "PATCH /api/v1/stacks/{id}", canWrite, a.updateStack)
	a.route(mux, "DELETE /api/v1/stacks/{id}", canWrite, a.deleteStack)
	a.route(mux, "POST /api/v1/stacks/{id}/actions/{action}", canDeploy, a.operateStack)
	a.route(mux, "GET /api/v1/stacks/{id}/operation", canRead, a.stackOperation)
	a.route(mux, "GET /api/v1/stacks/{id}/containers", canRead, a.stackContainers)
	a.route(mux, "GET /api/v1/stacks/{id}/services", canRead, a.stackServices)
	a.route(mux, "GET /api/v1/stacks/{id}/source", canRead, a.stackSource)
	a.route(mux, "GET /api/v1/stacks/{id}/variables", canRead, a.stackVariables)
	a.registerEnvRoutes(mux, "stacks", envvar.OwnerStack)
}

// listStackTemplates returns the catalog without the compose files: with
// hundreds of templates the full payload would be megabytes.
func (a *API) listStackTemplates(w http.ResponseWriter, r *http.Request) error {
	templates, err := stack.Templates()
	if err != nil {
		return err
	}
	language := i18n.FromContext(r.Context())
	summaries := make([]stack.Template, len(templates))
	for index, template := range templates {
		summaries[index] = template.Localized(language).Summary()
	}
	return ok(w, summaries)
}

func (a *API) getStackTemplate(w http.ResponseWriter, r *http.Request) error {
	template, found := stack.FindTemplate(r.PathValue("slug"))
	if !found {
		return apperr.NotFound("Template")
	}
	return ok(w, template.Localized(i18n.FromContext(r.Context())))
}

func (a *API) listStacks(w http.ResponseWriter, r *http.Request) error {
	stacks, err := a.Stacks.List(r.Context(), r.URL.Query().Get("environment_id"))
	if err != nil {
		return err
	}
	return ok(w, allForCaller(r, stacks))
}

func (a *API) getStack(w http.ResponseWriter, r *http.Request) error {
	item, err := a.Stacks.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, forCaller(r, item))
}

func (a *API) createStack(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[stack.CreateInput](w, r)
	if err != nil {
		return err
	}
	if err := a.requirePlacement(r, input.EnvironmentID, input.ServerID); err != nil {
		return err
	}
	item, err := a.Stacks.Create(r.Context(), input)
	if err != nil {
		return err
	}
	return created(w, item)
}

func (a *API) updateStack(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[stack.UpdateInput](w, r)
	if err != nil {
		return err
	}
	item, err := a.Stacks.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		return err
	}
	return ok(w, item)
}

func (a *API) deleteStack(w http.ResponseWriter, r *http.Request) error {
	stack, err := a.Stacks.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	if err := a.checkDestroy(r, stack.EnvironmentID); err != nil {
		return err
	}
	err = a.Stacks.Delete(r.Context(), r.PathValue("id"), queryFlag(r, "volumes"), queryFlag(r, "force"))
	if err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) operateStack(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "deploy":
		err = a.Stacks.Deploy(r.Context(), id)
	case "stop":
		err = a.Stacks.Stop(r.Context(), id)
	case "restart":
		err = a.Stacks.Restart(r.Context(), id)
	default:
		err = apperr.NotFound("Ação")
	}
	if err != nil {
		return err
	}
	return accepted(w, a.Stacks.Operation(id))
}

func (a *API) stackOperation(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.Stacks.Get(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return ok(w, a.Stacks.Operation(r.PathValue("id")))
}

func (a *API) stackContainers(w http.ResponseWriter, r *http.Request) error {
	containers, err := a.Stacks.Containers(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, containers)
}

func (a *API) stackServices(w http.ResponseWriter, r *http.Request) error {
	services, err := a.Stacks.Services(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, services)
}

// stackSource returns the compose file as it is on disk right now. A file
// kept in a folder of the server may hold anything, so it is sensitive.
func (a *API) stackSource(w http.ResponseWriter, r *http.Request) error {
	if err := requireSensitive(r); err != nil {
		return err
	}
	source, err := a.Stacks.Source(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, source)
}

// stackVariables lists names and defaults only, never stored values. The
// defaults are left out for callers without access to secrets.
func (a *API) stackVariables(w http.ResponseWriter, r *http.Request) error {
	variables, err := a.Stacks.Variables(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, allForCaller(r, variables))
}
