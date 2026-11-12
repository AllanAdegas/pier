package monitor

import (
	"context"
	"pier/internal/application"
	"pier/internal/docker"
)

func (m *Monitor) syncPlacement(ctx context.Context, app application.Application, serverID string) bool {
	targets, err := m.Applications.PlacementDestinations(ctx, app.ID)
	if err != nil {
		return true
	}
	if len(targets) == 0 {
		return false
	}
	if targets[0] != serverID {
		return true
	}
	all := []docker.Container{}
	for _, id := range targets {
		items, e := m.listContainers(ctx, id)
		group := docker.GroupByResource(items)[app.ID]
		if e != nil || len(group) == 0 {
			_ = m.Applications.SetStatus(ctx, app.ID, docker.StatusStopped)
			return true
		}
		all = append(all, group...)
	}
	_ = m.Applications.SetStatus(ctx, app.ID, docker.Summarize(all))
	return true
}
