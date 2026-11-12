package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"pier/internal/application"
	"pier/internal/envvar"
	"pier/internal/logdrain"
	"pier/internal/schedule"
	"pier/internal/server"
)

// serverOperationsJobs schedules the safe cleanup of the servers that enabled it.
func serverOperationsJobs(operations *server.Operations) schedule.Source {
	return func(ctx context.Context) ([]schedule.Job, error) {
		configs, err := operations.Schedules(ctx)
		if err != nil {
			return nil, err
		}
		jobs := []schedule.Job{}
		for _, config := range configs {
			jobs = append(jobs, schedule.Job{Name: "server-cleanup:" + config.ServerID, Cron: config.Cron, Run: func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				report, err := operations.Cleanup(ctx, config.ServerID, false)
				if err != nil {
					slog.Error("scheduled safe cleanup failed", "server", config.ServerID, "error", err)
					return
				}
				slog.Info("scheduled safe cleanup", "server", config.ServerID, "removed", len(report.Removed), "failed", len(report.Failed))
			}})
		}
		return jobs, nil
	}
}

// logDrainRedactors masks, in forwarded lines, the current secret values of
// the resource: the deploy variables of an application (also for its
// previews) and the variables of a stack. Same OutputRedactor as deploy logs.
func logDrainRedactors(db *sql.DB, applications *application.Service, env *envvar.Store) logdrain.RedactorSource {
	return func(ctx context.Context, resourceType, id string) (logdrain.Masker, error) {
		values, err := resourceSecrets(ctx, db, applications, env, resourceType, id)
		if err != nil {
			return nil, err
		}
		redactor := &application.OutputRedactor{}
		redactor.Redact(values...)
		return redactor, nil
	}
}

func resourceSecrets(ctx context.Context, db *sql.DB, applications *application.Service, env *envvar.Store, resourceType, id string) ([]string, error) {
	if resourceType == string(envvar.OwnerStack) {
		variables, err := env.List(ctx, envvar.Owner{Type: envvar.OwnerStack, ID: id})
		values := make([]string, 0, len(variables))
		for _, variable := range variables {
			values = append(values, variable.Value)
		}
		return values, err
	}
	var parent string
	if err := db.QueryRowContext(ctx, `SELECT application_id FROM application_previews WHERE id = ?`, id).Scan(&parent); err == nil {
		id = parent
	}
	return applications.OutputSecrets(ctx, id)
}
