package application

import (
	"context"
	"slices"
	"strings"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
)

// ServerFor resolves the server a per-server view (logs, containers,
// metrics, terminal) targets: the application's own server when serverID is
// empty, otherwise one of the servers the application runs on or is leaving.
// Any other server is reported as not found, so choosing a server never
// reaches beyond what the application itself allows.
func (s *Service) ServerFor(ctx context.Context, id, serverID string) (Application, string, error) {
	app, err := s.Get(ctx, id)
	if err != nil {
		return Application{}, "", err
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" || serverID == app.ServerID {
		return app, app.ServerID, nil
	}
	targets, err := s.placementTargets(ctx, app)
	if err != nil {
		return Application{}, "", err
	}
	if !slices.Contains(targets, serverID) {
		return Application{}, "", apperr.NotFound("Servidor")
	}
	return app, serverID, nil
}

// ContainersOn lists the containers of the application on one of its
// servers; an empty serverID is its own server, exactly like Containers.
func (s *Service) ContainersOn(ctx context.Context, id, serverID string) ([]ApplicationContainer, error) {
	if strings.TrimSpace(serverID) == "" {
		return s.Containers(ctx, id)
	}
	_, target, err := s.ServerFor(ctx, id, serverID)
	if err != nil {
		return nil, err
	}
	ex, _, err := s.connector.Executor(ctx, target)
	if err != nil {
		return nil, err
	}
	containers, err := docker.ListByResource(ctx, ex, id)
	if err != nil {
		return nil, apperr.Unavailable("Não foi possível consultar o servidor: %s", err)
	}
	return withProcesses(containers, containerProcesses(ctx, ex, id)), nil
}

// LogSource is a container of the application whose logs may be followed.
type LogSource struct {
	Executor    executor.Executor
	ContainerID string
}

// ContainerLogSource finds a container of the application on one of its
// servers by id or name. The id handed back comes from docker, never from
// the request, and only containers labelled with the application qualify.
func (s *Service) ContainerLogSource(ctx context.Context, id, serverID, container string) (LogSource, error) {
	_, target, err := s.ServerFor(ctx, id, serverID)
	if err != nil {
		return LogSource{}, err
	}
	container = strings.TrimSpace(container)
	if container == "" {
		return LogSource{}, apperr.Invalid("Informe o container.")
	}
	ex, _, err := s.connector.Executor(ctx, target)
	if err != nil {
		return LogSource{}, err
	}
	containers, err := docker.ListByResource(ctx, ex, id)
	if err != nil {
		return LogSource{}, apperr.Unavailable("Não foi possível consultar o servidor: %s", err)
	}
	for _, candidate := range containers {
		if candidate.ID == container || candidate.Name == container {
			return LogSource{Executor: ex, ContainerID: candidate.ID}, nil
		}
	}
	return LogSource{}, apperr.NotFound("Container")
}
