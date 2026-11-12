package application

import (
	"context"
	"errors"
	"fmt"

	"pier/internal/docker"
)

// Keep original containers: their immutable Docker configuration includes the
// old environment, mounts, image ID and healthcheck, even if tags/config change.
// Only running containers are restarted; stopped containers remain stopped.
type releaseRecovery struct {
	previous []docker.Container
	replicas []replica
	touched  bool
	retain   bool
}

func (r *releaseRecovery) restore(ctx context.Context, j *job) error {
	var failures []error
	next := make([]docker.Container, len(r.replicas))
	for i, replica := range r.replicas {
		next[i] = docker.Container{ID: replica.Name}
	}
	// Failure to remove the new version may leave host ports occupied. Report it,
	// but still attempt every original container rather than abandoning recovery.
	if err := docker.Remove(ctx, j.ex, next); err != nil {
		failures = append(failures, err)
	}
	for _, container := range r.previous {
		if container.State != "running" {
			continue
		}
		if err := docker.Start(ctx, j.ex, []docker.Container{container}); err != nil {
			failures = append(failures, err)
			continue
		}
		// Docker retains the original healthcheck; no release command/hooks run here.
		if err := j.waitUntilReady(ctx, container.ID); err != nil {
			failures = append(failures, fmt.Errorf("restaurar container %s: %w", container.ID, err))
		}
	}
	return errors.Join(failures...)
}
