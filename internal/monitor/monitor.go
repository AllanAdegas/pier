// Package monitor periodically reconciles what Pier believes is running with
// what Docker reports on each server.
package monitor

import (
	"context"
	"log/slog"
	"time"

	"pier/internal/application"
	"pier/internal/database"
	"pier/internal/docker"
	"pier/internal/notify"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/schedule"
	"pier/internal/server"
	"pier/internal/stack"
)

const serverTimeout = 20 * time.Second

// Connector reaches a server.
type Connector interface {
	Executor(ctx context.Context, serverID string) (executor.Executor, server.Server, error)
}

// ClusterChecker refreshes the health of the registered clusters.
type ClusterChecker interface {
	CheckAll(ctx context.Context)
}

// Monitor updates server reachability and resource statuses.
type Monitor struct {
	Servers          *server.Service
	Connector        Connector
	Applications     *application.Store
	Databases        *database.Store
	Stacks           *stack.Service
	Clusters         ClusterChecker
	Events           notify.Publisher
	Interval         time.Duration
	Logger           *slog.Logger
	Alerts           *State
	Tasks            *schedule.TaskService
	BackupService    *database.Service
	restarts         map[string][]restartSample
	highSince        map[string]time.Time
	lastCertificates time.Time
}

// Run reconciles until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.Interval)
	defer ticker.Stop()
	for {
		m.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Monitor) reconcile(ctx context.Context) {
	servers, err := m.Servers.List(ctx)
	if err != nil {
		m.Logger.Error("monitor: list servers", "error", err)
		return
	}
	for _, target := range servers {
		if ctx.Err() != nil {
			return
		}
		m.reconcileServer(ctx, target)
	}
	if m.Clusters != nil {
		m.Clusters.CheckAll(ctx)
	}
	m.checkUptime(ctx)
	m.checkSchedules(ctx)
	if time.Since(m.lastCertificates) >= time.Hour {
		m.checkCertificates(ctx)
		m.lastCertificates = time.Now()
	}
}

func (m *Monitor) reconcileServer(ctx context.Context, target server.Server) {
	ctx, cancel := context.WithTimeout(ctx, serverTimeout)
	defer cancel()
	containers, err := m.listContainers(ctx, target.ID)
	if pingErr := m.Servers.Ping(ctx, target.ID); pingErr != nil {
		m.Logger.Warn("monitor: record server status", "server", target.Name, "error", pingErr)
	}
	event := notify.Event{
		Type: notify.ServerUnreachable, Resource: target.ID,
		Title:   i18n.Instance().Sprintf("Servidor inacessível: %s", target.Name),
		Message: i18n.Instance().Sprintf("O Pier não conseguiu consultar o Docker do servidor: %s", err),
	}
	if m.Alerts != nil {
		if stateErr := m.Alerts.Observe(ctx, m.Events, event, err != nil, time.Now()); stateErr != nil {
			m.Logger.Warn("monitor: server alert", "error", stateErr)
		}
	} else if err != nil && target.Status == server.StatusReachable && m.Events != nil {
		m.Events.Publish(event)
	}
	if err != nil {
		m.resetThresholds(target.ID)
		return
	}

	m.runtimeAlerts(ctx, target, containers)
	groups := docker.GroupByResource(containers)
	m.syncApplications(ctx, target.ID, groups)
	m.syncDatabases(ctx, target.ID, groups)
	m.syncStacks(ctx, target.ID, groups)
}

func (m *Monitor) listContainers(ctx context.Context, serverID string) ([]docker.Container, error) {
	ex, _, err := m.Connector.Executor(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return docker.ListManaged(ctx, ex)
}

func (m *Monitor) syncApplications(ctx context.Context, serverID string, groups map[string][]docker.Container) {
	apps, err := m.Applications.List(ctx, "")
	if err != nil {
		return
	}
	for _, app := range apps {
		if m.syncPlacement(ctx, app, serverID) {
			continue
		}
		if app.ServerID == serverID {
			_ = m.Applications.SetStatus(ctx, app.ID, docker.Summarize(groups[app.ID]))
		}
	}
}

func (m *Monitor) syncDatabases(ctx context.Context, serverID string, groups map[string][]docker.Container) {
	databases, err := m.Databases.List(ctx, "")
	if err != nil {
		return
	}
	for _, item := range databases {
		if item.ServerID == serverID {
			_ = m.Databases.SetStatus(ctx, item.ID, docker.Summarize(groups[item.ID]))
		}
	}
}

func (m *Monitor) syncStacks(ctx context.Context, serverID string, groups map[string][]docker.Container) {
	stacks, err := m.Stacks.List(ctx, "")
	if err != nil {
		return
	}
	for _, item := range stacks {
		if item.ServerID == serverID {
			_ = m.Stacks.SetStatus(ctx, item.ID, docker.Summarize(groups[item.ID]))
		}
	}
}
