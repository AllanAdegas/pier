package volumebackup

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"

	"pier/internal/notify"
	"pier/internal/platform/i18n"
)

// AlertObserver records the state of a condition and publishes the
// transition once (monitor.State implements it): the same mechanism that
// raises backup.missed for databases.
type AlertObserver interface {
	Observe(ctx context.Context, publisher notify.Publisher, event notify.Event, active bool, now time.Time) error
}

// SetAlertObserver enables missed-run detection.
func (s *Service) SetAlertObserver(observer AlertObserver) { s.alerts = observer }

// Schedules lists the enabled policies the scheduler must run. Policies of
// resources that no longer resolve are skipped.
func (s *Service) Schedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.store.enabledPolicies(ctx)
	if err != nil {
		return nil, err
	}
	schedules := []Schedule{}
	for _, row := range rows {
		if _, err := s.resolve(ctx, row.Kind, row.ID); err == nil {
			schedules = append(schedules, Schedule{Kind: row.Kind, ID: row.ID, Cron: row.Policy.Cron})
		}
	}
	return schedules, nil
}

// ObserveMissed raises (or clears) backup.missed for every scheduled
// resource whose last successful archive is older than its cron allows.
// The scheduler calls it periodically; the monitor is not involved.
func (s *Service) ObserveMissed(ctx context.Context, now time.Time) {
	if s.alerts == nil {
		return
	}
	rows, err := s.store.enabledPolicies(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		resource, err := s.resolve(ctx, row.Kind, row.ID)
		if err != nil {
			continue
		}
		last, err := s.store.lastSuccess(ctx, row.Kind, row.ID)
		if err != nil {
			continue
		}
		event := notify.Event{
			Type: notify.BackupMissed, Resource: row.ID, ResourceType: row.Kind,
			Title: i18n.Instance().Text("Alerta de execução"),
			Message: i18n.Instance().Sprintf("O backup de volumes de %s não concluiu no horário esperado. "+
				"Confira o agendador, o espaço livre e o histórico de backups.", resource.Name),
		}
		_ = s.alerts.Observe(ctx, s.events, event, missedSchedule(row.Policy.Cron, row.Created, last, now), now)
	}
}

// missedSchedule reports whether the next expected run after the last success
// (or the creation of the policy) is more than 10 minutes in the past. Same
// rule as the monitor's database check.
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
	return !next.IsZero() && now.After(next.Add(10*time.Minute))
}
