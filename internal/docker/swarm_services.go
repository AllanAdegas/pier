package docker

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

// SwarmService is one line of `docker service ls`.
type SwarmService struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Replicas string `json:"replicas"`
	Image    string `json:"image"`
	// Process is the pier.process label of the service.
	Process string `json:"process"`
}

// SwarmTask is one line of `docker service ps`.
type SwarmTask struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Node         string `json:"node"`
	DesiredState string `json:"desired_state"`
	CurrentState string `json:"current_state"`
	Error        string `json:"error"`
	Image        string `json:"image"`
}

// Running reports whether the task runs (and, with a health check, passed it:
// Swarm keeps a task "starting" until then).
func (t SwarmTask) Running() bool {
	return t.DesiredState == "running" && strings.HasPrefix(strings.ToLower(t.CurrentState), "running")
}

// Swarm update states a service reports while and after it changes.
const (
	UpdateCompleted         = "completed"
	UpdateRollbackCompleted = "rollback_completed"
	UpdatePaused            = "paused"
	UpdateRollbackPaused    = "rollback_paused"
)

var serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

// ValidServiceName reports whether name can be passed to the docker CLI as
// a service or stack name.
func ValidServiceName(name string) bool { return serviceNamePattern.MatchString(name) }

func checkServiceName(name string) error {
	if !ValidServiceName(name) {
		return fmt.Errorf("invalid service name %q", name)
	}
	return nil
}

// ListSwarmServices lists the services of a Pier resource (by its pier.id label).
func ListSwarmServices(ctx context.Context, ex executor.Executor, resourceID string) ([]SwarmService, error) {
	output, err := ex.Run(ctx, shell.Join("docker", "service", "ls", "--filter", "label="+LabelID+"="+resourceID,
		"--format", FormatColumns("{{.ID}}", "{{.Name}}", "{{.Replicas}}", "{{.Image}}", `{{.Label "pier.process"}}`)))
	if err != nil {
		return nil, fmt.Errorf("docker service ls: %w", err)
	}
	services := []SwarmService{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 4 || !ValidServiceName(fields[1]) {
			continue
		}
		fields = append(fields, "")
		services = append(services, SwarmService{ID: fields[0], Name: fields[1], Replicas: fields[2], Image: fields[3], Process: fields[4]})
	}
	return services, nil
}

// ListSwarmTasks lists the tasks of a service, the most recent first.
func ListSwarmTasks(ctx context.Context, ex executor.Executor, service string) ([]SwarmTask, error) {
	if err := checkServiceName(service); err != nil {
		return nil, err
	}
	output, err := ex.Run(ctx, shell.Join("docker", "service", "ps", "--no-trunc", "--format", FormatColumns(
		"{{.ID}}", "{{.Name}}", "{{.Node}}", "{{.DesiredState}}", "{{.CurrentState}}", "{{.Error}}", "{{.Image}}"), service))
	if err != nil {
		return nil, fmt.Errorf("docker service ps: %w", err)
	}
	tasks := []SwarmTask{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			continue
		}
		fields = append(fields, "", "")
		tasks = append(tasks, SwarmTask{
			ID: fields[0], Name: fields[1], Node: fields[2], DesiredState: strings.ToLower(fields[3]),
			CurrentState: fields[4], Error: fields[5], Image: fields[6],
		})
	}
	return tasks, nil
}

// SwarmUpdateState returns the state of the last update of a service; empty
// for a service never updated.
func SwarmUpdateState(ctx context.Context, ex executor.Executor, service string) (string, error) {
	if err := checkServiceName(service); err != nil {
		return "", err
	}
	output, err := ex.Run(ctx, shell.Join("docker", "service", "inspect", "--format",
		"{{if .UpdateStatus}}{{.UpdateStatus.State}}{{end}}", service))
	if err != nil {
		return "", fmt.Errorf("docker service inspect: %w", err)
	}
	return strings.TrimSpace(output), nil
}

// SwarmServiceImage returns the image the service spec runs now.
func SwarmServiceImage(ctx context.Context, ex executor.Executor, service string) (string, error) {
	if err := checkServiceName(service); err != nil {
		return "", err
	}
	output, err := ex.Run(ctx, shell.Join("docker", "service", "inspect", "--format",
		"{{.Spec.TaskTemplate.ContainerSpec.Image}}", service))
	if err != nil {
		return "", fmt.Errorf("docker service inspect: %w", err)
	}
	return strings.TrimSpace(output), nil
}

// RollbackSwarmService reverts a service to its previous specification.
func RollbackSwarmService(ctx context.Context, ex executor.Executor, service string) error {
	if err := checkServiceName(service); err != nil {
		return err
	}
	if _, err := ex.Run(ctx, shell.Join("docker", "service", "rollback", "--detach", service)); err != nil {
		return fmt.Errorf("docker service rollback: %w", err)
	}
	return nil
}

// ScaleSwarmService sets the number of replicas of a service.
func ScaleSwarmService(ctx context.Context, ex executor.Executor, service string, replicas int) error {
	if err := checkServiceName(service); err != nil {
		return err
	}
	if replicas < 0 {
		return fmt.Errorf("invalid replicas %d", replicas)
	}
	if _, err := ex.Run(ctx, shell.Join("docker", "service", "scale", "--detach", fmt.Sprintf("%s=%d", service, replicas))); err != nil {
		return fmt.Errorf("docker service scale: %w", err)
	}
	return nil
}

// RestartSwarmService replaces every task of a service with a new one.
func RestartSwarmService(ctx context.Context, ex executor.Executor, service string) error {
	if err := checkServiceName(service); err != nil {
		return err
	}
	if _, err := ex.Run(ctx, shell.Join("docker", "service", "update", "--force", "--detach", service)); err != nil {
		return fmt.Errorf("docker service update: %w", err)
	}
	return nil
}

// SwarmServiceLogsCommand prints the recent logs of every task of a service.
func SwarmServiceLogsCommand(service string, tail int) (string, error) {
	if err := checkServiceName(service); err != nil {
		return "", err
	}
	return shell.Join("docker", "service", "logs", "--timestamps", "--no-trunc", "--tail", fmt.Sprint(tail), service) + " 2>&1", nil
}

// RemoveSwarmStack removes every service of a stack.
func RemoveSwarmStack(ctx context.Context, ex executor.Executor, stack string) error {
	if err := checkServiceName(stack); err != nil {
		return err
	}
	if _, err := ex.Run(ctx, shell.Join("docker", "stack", "rm", stack)); err != nil {
		return fmt.Errorf("docker stack rm: %w", err)
	}
	return nil
}

// StackDeployArgs are the arguments of `docker stack deploy`, after the
// docker command (which may carry a private --config for registry auth).
func StackDeployArgs(file, stack string) (string, error) {
	if err := checkServiceName(stack); err != nil {
		return "", err
	}
	return shell.Join("stack", "deploy", "--with-registry-auth", "--compose-file", file, stack), nil
}
