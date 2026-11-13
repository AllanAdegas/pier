package monitor

import (
	"testing"

	"pier/internal/docker"
)

func TestPlacementStatusReportsServersWithoutTheApplication(t *testing.T) {
	running := []docker.Container{{State: "running", Status: "Up 1 minute"}}
	for _, tc := range []struct {
		missing, total int
		want           string
	}{{0, 2, docker.StatusRunning}, {1, 2, docker.StatusDegraded}, {2, 2, docker.StatusStopped}} {
		if got := placementStatus(running, tc.missing, tc.total); got != tc.want {
			t.Errorf("missing %d of %d: %q, want %q", tc.missing, tc.total, got, tc.want)
		}
	}
}
