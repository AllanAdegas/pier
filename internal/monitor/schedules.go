package monitor

import (
	"context"
	"github.com/robfig/cron/v3"
	"pier/internal/notify"
	"pier/internal/platform/i18n"
	"time"
)

// missedSchedule calculates the next expected success using the cron itself,
// avoiding fixed interval assumptions for monthly schedules and DST.
func missedSchedule(expression string, created time.Time, last *time.Time, now time.Time) bool {
	schedule, err := cron.ParseStandard(expression)
	if err != nil {
		return false
	}
	base := created
	if last != nil {
		base = *last
	}
	next := schedule.Next(base)
	if next.IsZero() {
		return false
	}
	return now.After(next.Add(10 * time.Minute))
}
func (m *Monitor) checkSchedules(ctx context.Context) {
	if m.Alerts == nil {
		return
	}
	if m.Tasks != nil {
		tasks, err := m.Tasks.EnabledTasks(ctx)
		if err == nil {
			for _, task := range tasks {
				last, err := m.Tasks.LastSuccess(ctx, task.ID)
				if err != nil {
					continue
				}
				m.observe(ctx, notify.TaskMissed, task.ID, i18n.Instance().Text("A tarefa não concluiu no horário esperado. Confira o agendador e o histórico de execuções."), missedSchedule(task.Cron, task.CreatedAt, last, time.Now()))
			}
		}
	}
	if m.BackupService != nil {
		databases, err := m.BackupService.List(ctx, "")
		if err == nil {
			for _, db := range databases {
				if !db.Config.Backup.Enabled {
					continue
				}
				last, err := m.BackupService.LastSuccessfulBackup(ctx, db.ID)
				if err != nil {
					continue
				}
				m.observe(ctx, notify.BackupMissed, db.ID, i18n.Instance().Text("O backup não concluiu no horário esperado. Confira o agendador, o espaço livre e o histórico de backups."), missedSchedule(db.Config.Backup.Cron, db.CreatedAt, last, time.Now()))
			}
		}
	}
}
