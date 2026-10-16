// Package docker wraps the docker CLI calls shared by every resource type.
// It is an anti-corruption layer: the rest of the code never parses CLI output.
package docker

import (
	"context"
	"fmt"
	"strings"

	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

const (
	// Network is the bridge every managed container and the proxy share.
	Network = "pier"

	LabelManaged    = "pier.managed"
	LabelType       = "pier.type"
	LabelID         = "pier.id"
	LabelDeployment = "pier.deployment"
	// LabelIngress marks the networks the proxy must join to reach stacks.
	LabelIngress = "pier.ingress"

	// ProxyContainer is the fixed name of the reverse proxy container.
	ProxyContainer = "pier-proxy"

	TypeApplication = "application"
	TypeDatabase    = "database"
	TypeStack       = "stack"
	TypeProxy       = "proxy"
	TypeCluster     = "cluster"
)

// Container is the subset of `docker ps` Pier cares about.
type Container struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	State      string `json:"state"`
	Status     string `json:"status"`
	Type       string `json:"type"`
	ResourceID string `json:"resource_id"`
	Deployment string `json:"deployment"`
}

// Healthy reports whether the container is running and not failing its healthcheck.
func (c Container) Healthy() bool {
	return c.State == "running" && !strings.Contains(c.Status, "unhealthy") &&
		!strings.Contains(c.Status, "health: starting")
}

// Labels returns the identification labels of a managed container.
func Labels(resourceType, resourceID string) map[string]string {
	return map[string]string{
		LabelManaged: "true",
		LabelType:    resourceType,
		LabelID:      resourceID,
	}
}

// psRequiredFields counts the columns that are never empty (id to status).
const psRequiredFields = 5

// Columns are separated by a real tab character: only some docker commands
// expand a literal "\t" in --format, so the escape must not reach the CLI.
var psFormat = FormatColumns("{{.ID}}", "{{.Names}}", "{{.Image}}", "{{.State}}", "{{.Status}}",
	`{{.Label "pier.type"}}`, `{{.Label "pier.id"}}`, `{{.Label "pier.deployment"}}`)

// FormatColumns builds a --format template whose columns are tab-separated.
func FormatColumns(columns ...string) string { return strings.Join(columns, "\t") }

// ListManaged returns every container created by Pier on the server.
func ListManaged(ctx context.Context, ex executor.Executor) ([]Container, error) {
	return list(ctx, ex, LabelManaged+"=true")
}

// ListByResource returns the containers of one resource, running or not.
func ListByResource(ctx context.Context, ex executor.Executor, resourceID string) ([]Container, error) {
	return list(ctx, ex, LabelID+"="+resourceID)
}

func list(ctx context.Context, ex executor.Executor, labelFilter string) ([]Container, error) {
	command := shell.Join("docker", "ps", "-a", "--no-trunc", "--filter", "label="+labelFilter, "--format", psFormat)
	output, err := ex.Run(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	containers := []Container{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < psRequiredFields {
			continue
		}
		// Empty trailing labels are trimmed from the output; pad them back.
		fields = append(fields, "", "", "")
		containers = append(containers, Container{
			ID: fields[0], Name: fields[1], Image: fields[2], State: fields[3], Status: fields[4],
			Type: fields[5], ResourceID: fields[6], Deployment: fields[7],
		})
	}
	return containers, nil
}

// Remove force-removes the containers; an empty list is a no-op.
func Remove(ctx context.Context, ex executor.Executor, containers []Container) error {
	return containerCommand(ctx, ex, containers, "rm", "-f")
}

// Stop stops the containers gracefully.
func Stop(ctx context.Context, ex executor.Executor, containers []Container) error {
	return containerCommand(ctx, ex, containers, "stop", "--time", "30")
}

// Start starts stopped containers.
func Start(ctx context.Context, ex executor.Executor, containers []Container) error {
	return containerCommand(ctx, ex, containers, "start")
}

// Restart restarts the containers.
func Restart(ctx context.Context, ex executor.Executor, containers []Container) error {
	return containerCommand(ctx, ex, containers, "restart", "--time", "30")
}

func containerCommand(ctx context.Context, ex executor.Executor, containers []Container, verb ...string) error {
	if len(containers) == 0 {
		return nil
	}
	args := append([]string{"docker"}, verb...)
	for _, container := range containers {
		args = append(args, container.ID)
	}
	if _, err := ex.Run(ctx, shell.Join(args...)); err != nil {
		return fmt.Errorf("docker %s: %w", verb[0], err)
	}
	return nil
}

// EnsureNetwork creates the shared bridge network when it does not exist.
func EnsureNetwork(ctx context.Context, ex executor.Executor) error {
	command := fmt.Sprintf("docker network inspect %[1]s >/dev/null 2>&1 || docker network create %[1]s", Network)
	if _, err := ex.Run(ctx, command); err != nil {
		return fmt.Errorf("ensure docker network %s: %w", Network, err)
	}
	return nil
}

// LogsCommand builds the command that prints (and optionally follows) container logs.
func LogsCommand(containerID string, tail int, follow bool) string {
	args := []string{"docker", "logs", "--timestamps", "--tail", fmt.Sprint(tail)}
	if follow {
		args = append(args, "--follow")
	}
	return shell.Join(append(args, containerID)...) + " 2>&1"
}

// Inspect evaluates a Go template against a container or image.
func Inspect(ctx context.Context, ex executor.Executor, target, format string) (string, error) {
	return ex.Run(ctx, shell.Join("docker", "inspect", "--format", format, target))
}

// ImageExists reports whether the image is present on the server.
func ImageExists(ctx context.Context, ex executor.Executor, image string) bool {
	_, err := ex.Run(ctx, shell.Join("docker", "image", "inspect", "--format", "{{.Id}}", image))
	return err == nil
}

// Exec builds a `docker exec` running a shell script inside the container.
func Exec(containerID, script string) string {
	return shell.Join("docker", "exec", containerID, "sh", "-c", script)
}

// ConnectProxy attaches the proxy to a network so it can route to the
// containers in it. A stopped proxy or an existing attachment is not an error.
func ConnectProxy(ctx context.Context, ex executor.Executor, network string) {
	_, _ = ex.Run(ctx, shell.Join("docker", "network", "connect", network, ProxyContainer))
}

// ProxyRunning reports whether the reverse proxy of the server is up.
// Without it no domain of the server answers.
func ProxyRunning(ctx context.Context, ex executor.Executor) bool {
	output, err := ex.Run(ctx, shell.Join("docker", "inspect", "--format", "{{.State.Running}}", ProxyContainer))
	return err == nil && strings.TrimSpace(output) == "true"
}

// addressPoolsExhausted is how the daemon says it has no subnet left for a
// new network. A default daemon has about 31 (172.17–31.0.0/16 and
// 192.168.0.0/16 split in /20), and every stack takes at least one.
const addressPoolsExhausted = "all predefined address pools have been fully subnetted"

// Explain turns daemon failures with a known remedy into a message that
// says what to do; anything else is returned as is.
func Explain(err error) error {
	if err == nil || !strings.Contains(err.Error(), addressPoolsExhausted) {
		return err
	}
	return fmt.Errorf("o Docker deste servidor não tem mais faixas de IP para redes novas (cada stack usa ao menos "+
		"uma, e o padrão do Docker comporta cerca de 31). Remova redes sem uso com `docker network prune` ou "+
		"amplie as faixas em \"default-address-pools\" do /etc/docker/daemon.json (ex.: "+
		"{\"base\":\"10.200.0.0/16\",\"size\":24}, 256 redes) e reinicie o Docker. Erro original: %w", err)
}

// ProxyStoppedWarning is what a deployment says when it publishes domains on
// a server whose proxy is not running.
const ProxyStoppedWarning = "AVISO: o proxy deste servidor está parado, então os domínios configurados não respondem. " +
	"Inicie-o em Infraestrutura › servidor › Proxy."

// DisconnectProxy detaches the proxy from a network about to be removed.
func DisconnectProxy(ctx context.Context, ex executor.Executor, network string) {
	_, _ = ex.Run(ctx, shell.Join("docker", "network", "disconnect", "--force", network, ProxyContainer))
}

// ConnectProxyToIngressNetworks re-attaches the proxy to every stack ingress
// network, which is needed after the proxy container is recreated.
func ConnectProxyToIngressNetworks(ctx context.Context, ex executor.Executor) error {
	output, err := ex.Run(ctx, shell.Join("docker", "network", "ls", "--filter", "label="+LabelIngress+"=true",
		"--format", "{{.Name}}"))
	if err != nil {
		return fmt.Errorf("list ingress networks: %w", err)
	}
	for _, network := range strings.Fields(output) {
		ConnectProxy(ctx, ex, network)
	}
	return nil
}
