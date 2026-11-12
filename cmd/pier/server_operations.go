package main

import (
	"context"
	"log/slog"
	"pier/internal/schedule"
	"pier/internal/server"
	"time"
)

func serverOperationsJobs(operations *server.Operations) schedule.Source {
	return func(ctx context.Context) ([]schedule.Job, error) {
		configs, err := operations.Schedules(ctx)
		if err != nil {
			return nil, err
		}
		jobs := []schedule.Job{}
		for _, config := range configs {
			jobs = append(jobs, schedule.Job{Name: "server-cleanup:" + config.ServerID, Cron: config.Cron, Run: func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				if _, err := operations.Cleanup(ctx, config.ServerID, false); err != nil {
					slog.Error("scheduled safe cleanup failed", "server", config.ServerID, "error", err)
				}
			}})
		}
		return jobs, nil
	}
}
