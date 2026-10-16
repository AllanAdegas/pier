package docker

import "strings"

// Runtime statuses shared by applications, databases and stacks.
const (
	StatusRunning    = "running"
	StatusStarting   = "starting"
	StatusUnhealthy  = "unhealthy"
	StatusRestarting = "restarting"
	StatusDegraded   = "degraded"
	StatusStopped    = "stopped"
)

// Summarize reduces the containers of a resource to a single status.
func Summarize(containers []Container) string {
	running, restarting, unhealthy, starting := 0, 0, 0, 0
	for _, container := range containers {
		switch {
		case container.State == "restarting":
			restarting++
		case container.State != "running":
		case strings.Contains(container.Status, "unhealthy"):
			unhealthy++
		case strings.Contains(container.Status, "health: starting"):
			starting++
		default:
			running++
		}
	}
	alive := running + unhealthy + starting
	switch {
	case restarting > 0:
		return StatusRestarting
	case alive == 0:
		return StatusStopped
	case unhealthy > 0:
		return StatusUnhealthy
	case starting > 0:
		return StatusStarting
	case alive < len(containers):
		return StatusDegraded
	}
	return StatusRunning
}

// GroupByResource indexes containers by the resource that owns them.
func GroupByResource(containers []Container) map[string][]Container {
	groups := make(map[string][]Container)
	for _, container := range containers {
		if container.ResourceID != "" {
			groups[container.ResourceID] = append(groups[container.ResourceID], container)
		}
	}
	return groups
}
