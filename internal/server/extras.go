package server

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
)

// OperationsPolicy is opt-in. Cleanup only touches objects explicitly labeled
// pier.cleanup=true; normal app versions, backups and volumes are excluded.
type OperationsPolicy struct {
	Maintenance    bool   `json:"maintenance"`
	CleanupEnabled bool   `json:"cleanup_enabled"`
	CleanupCron    string `json:"cleanup_cron"`
	MinAgeHours    int    `json:"min_age_hours"`
}

func (p *OperationsPolicy) Validate() error {
	if p.CleanupCron == "" {
		p.CleanupCron = "0 4 * * 0"
	}
	if _, err := cron.ParseStandard(p.CleanupCron); err != nil {
		return apperr.Invalid("Expressão cron inválida (use 5 campos, ex.: 0 3 * * *).")
	}
	if p.MinAgeHours < 24 || p.MinAgeHours > 8760 {
		return apperr.Invalid("A limpeza exige idade mínima entre 24 e 8760 horas.")
	}
	return nil
}

type operationsConnector interface {
	Executor(context.Context, string) (executor.Executor, Server, error)
}
type Operations struct {
	db        *sql.DB
	connector operationsConnector
	mu        sync.Mutex
	active    map[string]bool
	changed   func()
}

func NewOperations(db *sql.DB, c operationsConnector) *Operations {
	return &Operations{db: db, connector: c, active: map[string]bool{}}
}
func (o *Operations) OnChange(f func()) { o.changed = f }
func (o *Operations) Policy(ctx context.Context, id string) (OperationsPolicy, error) {
	if _, err := NewStore(o.db).Get(ctx, id); err != nil {
		return OperationsPolicy{}, serverNotFound(err)
	}
	p := OperationsPolicy{CleanupCron: "0 4 * * 0", MinAgeHours: 168}
	err := o.db.QueryRowContext(ctx, "SELECT maintenance,cleanup_enabled,cleanup_cron,min_age_hours FROM server_operations WHERE server_id=?", id).Scan(&p.Maintenance, &p.CleanupEnabled, &p.CleanupCron, &p.MinAgeHours)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return p, err
}
func (o *Operations) SetPolicy(ctx context.Context, id string, p OperationsPolicy) (OperationsPolicy, error) {
	if err := p.Validate(); err != nil {
		return p, err
	}
	if _, err := o.Policy(ctx, id); err != nil {
		return p, err
	}
	_, err := o.db.ExecContext(ctx, `INSERT INTO server_operations(server_id,maintenance,cleanup_enabled,cleanup_cron,min_age_hours) VALUES(?,?,?,?,?) ON CONFLICT(server_id) DO UPDATE SET maintenance=excluded.maintenance,cleanup_enabled=excluded.cleanup_enabled,cleanup_cron=excluded.cleanup_cron,min_age_hours=excluded.min_age_hours`, id, p.Maintenance, p.CleanupEnabled, p.CleanupCron, p.MinAgeHours)
	if err == nil && o.changed != nil {
		o.changed()
	}
	return p, err
}

type CleanupSchedule struct{ ServerID, Cron string }

func (o *Operations) Schedules(ctx context.Context) ([]CleanupSchedule, error) {
	rows, err := o.db.QueryContext(ctx, "SELECT server_id,cleanup_cron FROM server_operations WHERE cleanup_enabled=1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CleanupSchedule{}
	for rows.Next() {
		var s CleanupSchedule
		if err := rows.Scan(&s.ServerID, &s.Cron); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CheckDeployment blocks new deployment execution, not reads, restores or stop.
func (c *Connector) CheckDeployment(ctx context.Context, id string) error {
	return CheckDeployment(ctx, c.store.db, id)
}
func CheckDeployment(ctx context.Context, db *sql.DB, id string) error {
	var maintenance bool
	err := db.QueryRowContext(ctx, "SELECT maintenance FROM server_operations WHERE server_id=?", id).Scan(&maintenance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if maintenance {
		return apperr.Conflict("Servidor em manutenção. Desative a manutenção para implantar.")
	}
	return nil
}
func (o *Operations) Cleanup(ctx context.Context, id string, dryRun bool) (CleanupReport, error) {
	p, err := o.Policy(ctx, id)
	if err != nil {
		return CleanupReport{}, err
	}
	o.mu.Lock()
	if o.active[id] {
		o.mu.Unlock()
		return CleanupReport{}, apperr.Conflict("A limpeza deste servidor já está em andamento.")
	}
	o.active[id] = true
	o.mu.Unlock()
	defer func() { o.mu.Lock(); delete(o.active, id); o.mu.Unlock() }()
	ex, _, err := o.connector.Executor(ctx, id)
	if err != nil {
		return CleanupReport{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return cleanupEligible(ctx, ex, time.Now().Add(-time.Duration(p.MinAgeHours)*time.Hour), dryRun)
}

// CheckConnectorDeployment lets test connectors and older embedders opt in to
// the admission check without changing their Executor contract.
func CheckConnectorDeployment(ctx context.Context, connector any, id string) error {
	if c, ok := connector.(interface {
		CheckDeployment(context.Context, string) error
	}); ok {
		return c.CheckDeployment(ctx, id)
	}
	return nil
}
