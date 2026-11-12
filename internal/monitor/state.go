package monitor

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/platform/sqlite"
)

// State stores alert transitions, maintenance windows and uptime history.
type State struct {
	DB *sql.DB
	mu sync.Mutex
}

// Observe records one condition. A silence postpones delivery without losing the transition.
func (s *State) Observe(ctx context.Context, publisher notify.Publisher, event notify.Event, active bool, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var was bool
	var sent string
	err := s.DB.QueryRowContext(ctx, `SELECT active,last_sent FROM alert_states WHERE type=? AND resource=?`, event.Type, event.Resource).Scan(&was, &sent)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	var silenced int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_silences WHERE (resource='' OR resource=? OR substr(?,1,length(resource)+1)=resource||':') AND until_at>?`, event.Resource, event.Resource, sqlite.FormatTime(now)).Scan(&silenced); err != nil {
		return err
	}
	// Preserve the delivered state through maintenance; evaluate again when it ends.
	if silenced > 0 {
		return nil
	}
	send := active && (!was || now.Sub(sqlite.ParseTime(sent)) >= 6*time.Hour) || !active && was
	lastSent := sent
	if send {
		lastSent = sqlite.FormatTime(now)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO alert_states(type,resource,active,last_sent) VALUES(?,?,?,?) ON CONFLICT(type,resource) DO UPDATE SET active=excluded.active,last_sent=excluded.last_sent`, event.Type, event.Resource, active, lastSent); err != nil {
		return err
	}
	if send && publisher != nil {
		if !active {
			event.Message = i18n.Instance().Sprintf("O alerta %s voltou ao normal.", event.Type)
			event.Title = i18n.Instance().Text("Alerta resolvido")
			event.Type = notify.AlertResolved
		}
		event.OccurredAt = now
		publisher.Publish(event)
	}
	return nil
}

// Silence suppresses all alerts (empty resource) or just a resource until the given time.
func (s *State) Silence(ctx context.Context, resource string, until time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO alert_silences(resource,until_at) VALUES(?,?) ON CONFLICT(resource) DO UPDATE SET until_at=excluded.until_at`, resource, sqlite.FormatTime(until))
	return err
}

type Options struct {
	Disk    float64 `json:"disk"`
	Memory  float64 `json:"memory"`
	CPU     float64 `json:"cpu"`
	Minutes int     `json:"minutes"`
}

func (s *State) Options(ctx context.Context) (Options, error) {
	var o Options
	err := s.DB.QueryRowContext(ctx, `SELECT disk,memory,cpu,minutes FROM alert_options WHERE id=1`).Scan(&o.Disk, &o.Memory, &o.CPU, &o.Minutes)
	return o, err
}
func (s *State) SetOptions(ctx context.Context, o Options) error {
	if !(o.Disk > 0 && o.Disk <= 100 && o.Memory > 0 && o.Memory <= 100 && o.CPU > 0 && o.CPU <= 100) || o.Minutes < 1 || o.Minutes > 10080 {
		return apperr.Invalid("Informe limites percentuais maiores que 0 e até 100, e duração entre 1 e 10080 minutos.")
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE alert_options SET disk=?,memory=?,cpu=?,minutes=? WHERE id=1`, o.Disk, o.Memory, o.CPU, o.Minutes)
	return err
}
