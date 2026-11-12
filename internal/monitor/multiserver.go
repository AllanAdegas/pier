package monitor

import (
	"context"

	"pier/internal/application"
	"pier/internal/docker"
)

// syncPlacement sets the status of an application that runs on several
// servers, once per round (when its own server is sampled). handled is false
// for single-server applications, which keep the per-server rule.
//
// Every server running is "running"; some servers without a running release
// (unreachable or empty) make it "degraded"; none makes it "stopped".
func (m *Monitor) syncPlacement(ctx context.Context, app application.Application, serverID string) bool {
	servers, err := m.Applications.PlacementDestinations(ctx, app)
	if err != nil || len(servers) == 0 {
		return err != nil
	}
	if servers[0] != serverID {
		return true
	}
	all := []docker.Container{}
	missing := 0
	for _, id := range servers {
		containers, err := m.listContainers(ctx, id)
		group := docker.GroupByResource(containers)[app.ID]
		if err != nil || docker.Summarize(group) == docker.StatusStopped {
			missing++
			continue
		}
		all = append(all, group...)
	}
	_ = m.Applications.SetStatus(ctx, app.ID, placementStatus(all, missing, len(servers)))
	return true
}

func placementStatus(containers []docker.Container, missing, total int) string {
	switch {
	case missing == total:
		return docker.StatusStopped
	case missing > 0:
		return docker.StatusDegraded
	}
	return docker.Summarize(containers)
}
