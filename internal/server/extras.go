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

const (
	defaultCleanupCron = "0 4 * * 0"
	defaultCleanupAge  = 168
	minCleanupAgeHours = 24
	maxCleanupAgeHours = 8760
	cleanupTimeout     = 5 * time.Minute
)

// OperationsPolicy is the operator's choice for one server: whether new
// deployments are paused (maintenance) and whether the safe cleanup runs on
// a schedule. Cleanup itself is described in extras_cleanup.go.
type OperationsPolicy struct {
	Maintenance    bool   `json:"maintenance"`
	CleanupEnabled bool   `json:"cleanup_enabled"`
	CleanupCron    string `json:"cleanup_cron"`
	MinAgeHours    int    `json:"min_age_hours"`
}

// DefaultOperationsPolicy is the policy of a server nobody configured.
func DefaultOperationsPolicy() OperationsPolicy {
	return OperationsPolicy{CleanupCron: defaultCleanupCron, MinAgeHours: defaultCleanupAge}
}

// Validate normalizes the empty schedule and rejects out-of-range values.
func (p OperationsPolicy) Validate() (OperationsPolicy, error) {
	if p.CleanupCron == "" {
		p.CleanupCron = defaultCleanupCron
	}
	fields := apperr.Fields{}
	if _, err := cron.ParseStandard(p.CleanupCron); err != nil {
		fields.Add("cleanup_cron", "Expressão cron inválida (use 5 campos, ex.: 0 3 * * *).")
	}
	if p.MinAgeHours < minCleanupAgeHours || p.MinAgeHours > maxCleanupAgeHours {
		fields.Add("min_age_hours", "A limpeza exige idade mínima entre 24 e 8760 horas.")
	}
	return p, fields.Err()
}

// operationsConnector is the part of Connector the operations use.
type operationsConnector interface {
	Executor(context.Context, string) (executor.Executor, Server, error)
}

// Operations holds the maintenance mode and the safe cleanup of servers.
type Operations struct {
	db        *sql.DB
	connector operationsConnector
	now       func() time.Time

	mu      sync.Mutex
	active  map[string]bool
	last    map[string]CleanupReport
	changed func()
}

// NewOperations creates the service.
func NewOperations(db *sql.DB, connector operationsConnector) *Operations {
	return &Operations{db: db, connector: connector, now: time.Now, active: map[string]bool{}, last: map[string]CleanupReport{}}
}

// OnChange registers the callback that reloads the scheduler. Set it once,
// at startup, before serving requests.
func (o *Operations) OnChange(callback func()) { o.changed = callback }

// Policy returns the policy of a server, or the default one.
func (o *Operations) Policy(ctx context.Context, id string) (OperationsPolicy, error) {
	if _, err := NewStore(o.db).Get(ctx, id); err != nil {
		return OperationsPolicy{}, serverNotFound(err)
	}
	policy := DefaultOperationsPolicy()
	err := o.db.QueryRowContext(ctx, `SELECT maintenance, cleanup_enabled, cleanup_cron, min_age_hours
		FROM server_operations WHERE server_id = ?`, id).
		Scan(&policy.Maintenance, &policy.CleanupEnabled, &policy.CleanupCron, &policy.MinAgeHours)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, nil
	}
	return policy, err
}

// SetPolicy validates and stores the policy of a server.
func (o *Operations) SetPolicy(ctx context.Context, id string, policy OperationsPolicy) (OperationsPolicy, error) {
	policy, err := policy.Validate()
	if err != nil {
		return OperationsPolicy{}, err
	}
	if _, err := o.Policy(ctx, id); err != nil {
		return OperationsPolicy{}, err
	}
	_, err = o.db.ExecContext(ctx, `INSERT INTO server_operations (server_id, maintenance, cleanup_enabled, cleanup_cron, min_age_hours)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (server_id) DO UPDATE SET maintenance = excluded.maintenance, cleanup_enabled = excluded.cleanup_enabled,
			cleanup_cron = excluded.cleanup_cron, min_age_hours = excluded.min_age_hours`,
		id, policy.Maintenance, policy.CleanupEnabled, policy.CleanupCron, policy.MinAgeHours)
	if err != nil {
		return OperationsPolicy{}, err
	}
	if o.changed != nil {
		o.changed()
	}
	return policy, nil
}

// CleanupSchedule is one server whose safe cleanup runs on a schedule.
type CleanupSchedule struct{ ServerID, Cron string }

// Schedules lists the servers with scheduled cleanup.
func (o *Operations) Schedules(ctx context.Context) ([]CleanupSchedule, error) {
	rows, err := o.db.QueryContext(ctx, "SELECT server_id, cleanup_cron FROM server_operations WHERE cleanup_enabled = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	schedules := []CleanupSchedule{}
	for rows.Next() {
		var schedule CleanupSchedule
		if err := rows.Scan(&schedule.ServerID, &schedule.Cron); err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	return schedules, rows.Err()
}

// CheckDeployment refuses new deployments on a server under maintenance.
// Reads, stops, restores and running containers are not affected.
func (c *Connector) CheckDeployment(ctx context.Context, id string) error {
	return CheckDeployment(ctx, c.store.db, id)
}

// CheckDeployment is the admission rule behind Connector.CheckDeployment.
func CheckDeployment(ctx context.Context, db *sql.DB, id string) error {
	var maintenance bool
	err := db.QueryRowContext(ctx, "SELECT maintenance FROM server_operations WHERE server_id = ?", id).Scan(&maintenance)
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

// CheckConnectorDeployment applies the admission check when the connector
// offers it, so test connectors keep their small Executor contract.
func CheckConnectorDeployment(ctx context.Context, connector any, id string) error {
	if checker, ok := connector.(interface {
		CheckDeployment(context.Context, string) error
	}); ok {
		return checker.CheckDeployment(ctx, id)
	}
	return nil
}

// Cleanup runs the safe cleanup of a server (a dry run only lists). Only one
// cleanup runs per server at a time.
func (o *Operations) Cleanup(ctx context.Context, id string, dryRun bool) (CleanupReport, error) {
	policy, err := o.Policy(ctx, id)
	if err != nil {
		return CleanupReport{}, err
	}
	if !o.begin(id) {
		return CleanupReport{}, apperr.Conflict("A limpeza deste servidor já está em andamento.")
	}
	defer o.end(id)
	ex, _, err := o.connector.Executor(ctx, id)
	if err != nil {
		return CleanupReport{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()
	sweep := cleanupSweep{ex: ex, owners: resourceOwners{db: o.db}, before: o.now().Add(-time.Duration(policy.MinAgeHours) * time.Hour),
		ageHours: policy.MinAgeHours, dryRun: dryRun}
	report, err := sweep.run(ctx)
	if err != nil {
		return CleanupReport{}, apperr.Unavailable("Falha na limpeza: %s", describeFailure(err))
	}
	report.FinishedAt = o.now().UTC()
	o.remember(id, report)
	return report, nil
}

// LastCleanup returns the report of the latest cleanup since Pier started;
// found is false when none ran.
func (o *Operations) LastCleanup(ctx context.Context, id string) (report CleanupReport, found bool, err error) {
	if _, err := o.Policy(ctx, id); err != nil {
		return CleanupReport{}, false, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	report, found = o.last[id]
	return report, found, nil
}

func (o *Operations) begin(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active[id] {
		return false
	}
	o.active[id] = true
	return true
}

func (o *Operations) end(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.active, id)
}

func (o *Operations) remember(id string, report CleanupReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.last[id] = report
}
