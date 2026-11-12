package main

import (
	"context"
	"time"

	"pier/internal/schedule"
	"pier/internal/volumebackup"
)

// missedCheckCron is how often missed volume backups are looked for.
const missedCheckCron = "*/10 * * * *"

// volumeBackupJobs adapts volume backup policies to scheduler jobs, plus the
// periodic check that raises backup.missed.
func volumeBackupJobs(volumes *volumebackup.Service) schedule.Source {
	return func(ctx context.Context) ([]schedule.Job, error) {
		schedules, err := volumes.Schedules(ctx)
		if err != nil {
			return nil, err
		}
		jobs := []schedule.Job{{Name: "volume-backups:missed", Cron: missedCheckCron, Run: func() {
			volumes.ObserveMissed(context.Background(), time.Now())
		}}}
		for _, item := range schedules {
			jobs = append(jobs, schedule.Job{Name: "volume-backup:" + item.Kind + ":" + item.ID, Cron: item.Cron, Run: func() {
				_, _ = volumes.Start(context.Background(), item.Kind, item.ID, volumebackup.TriggerScheduled)
			}})
		}
		return jobs, nil
	}
}
