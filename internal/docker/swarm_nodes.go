package docker

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"

	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

// SwarmNetwork is the attachable overlay network every Swarm service of Pier
// joins. It carries LabelIngress, so a recreated proxy re-attaches to it and
// keeps reaching the services by their aliases.
const SwarmNetwork = "pier-swarm"

// SwarmPort is where managers listen for cluster management traffic.
const SwarmPort = 2377

// Swarm node roles and availabilities, as the docker CLI names them.
const (
	SwarmManager = "manager"
	SwarmWorker  = "worker"

	AvailabilityActive = "active"
	AvailabilityDrain  = "drain"
)

// SwarmState is what `docker info` says about the Swarm membership of a host.
type SwarmState struct {
	// LocalNodeState is inactive, pending, active, error or locked.
	LocalNodeState   string
	NodeID           string
	ControlAvailable bool
}

// Active reports whether the host is part of a swarm.
func (s SwarmState) Active() bool { return s.LocalNodeState == "active" }

// SwarmNode is one line of `docker node ls`.
type SwarmNode struct {
	ID            string `json:"node_id"`
	Hostname      string `json:"hostname"`
	Status        string `json:"status"`
	Availability  string `json:"availability"`
	ManagerStatus string `json:"manager_status"`
	EngineVersion string `json:"engine_version"`
}

var (
	// joinTokenPattern matches a Swarm join token wherever it may surface
	// (the output of `docker swarm init`, an error message).
	joinTokenPattern = regexp.MustCompile(`SWMTKN-[A-Za-z0-9-]+`)
	nodeIDPattern    = regexp.MustCompile(`^[a-z0-9]{25}$`)
)

// ScrubJoinTokens removes every Swarm join token from a text.
func ScrubJoinTokens(text string) string {
	return joinTokenPattern.ReplaceAllString(text, "SWMTKN-••••••")
}

// ValidNodeID reports whether id has the shape of a Swarm node ID.
func ValidNodeID(id string) bool { return nodeIDPattern.MatchString(id) }

// scrubbed wraps an error so that no join token leaves this package.
func scrubbed(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %s", action, ScrubJoinTokens(err.Error()))
}

// ReadSwarmState reads the Swarm membership of the host.
func ReadSwarmState(ctx context.Context, ex executor.Executor) (SwarmState, error) {
	output, err := ex.Run(ctx, shell.Join("docker", "info", "--format",
		FormatColumns("{{.Swarm.LocalNodeState}}", "{{.Swarm.NodeID}}", "{{.Swarm.ControlAvailable}}")))
	if err != nil {
		return SwarmState{}, scrubbed("docker info", err)
	}
	fields := append(strings.Split(strings.TrimSpace(output), "\t"), "", "", "")
	return SwarmState{LocalNodeState: fields[0], NodeID: fields[1], ControlAvailable: fields[2] == "true"}, nil
}

// SwarmInit makes the host the first manager of a new swarm. The output
// (which prints a join command with the worker token) is discarded.
func SwarmInit(ctx context.Context, ex executor.Executor, advertiseAddr string) error {
	_, err := ex.Run(ctx, shell.Join("docker", "swarm", "init", "--advertise-addr", advertiseAddr)+" >/dev/null")
	return scrubbed("docker swarm init", err)
}

// SwarmJoinToken reads the join token of a role on a manager. The caller
// keeps it in memory only and never logs it.
func SwarmJoinToken(ctx context.Context, ex executor.Executor, role string) (string, error) {
	if role != SwarmManager && role != SwarmWorker {
		return "", fmt.Errorf("unknown swarm role %q", role)
	}
	output, err := ex.Run(ctx, shell.Join("docker", "swarm", "join-token", "--quiet", role))
	token := strings.TrimSpace(output)
	if err != nil {
		return "", scrubbed("docker swarm join-token", err)
	}
	if joinTokenPattern.FindString(token) != token || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("docker swarm join-token: unexpected output")
	}
	return token, nil
}

// RotateSwarmJoinToken replaces the join token of a role, so a token used
// for a join is worthless afterwards. The new token is not printed.
func RotateSwarmJoinToken(ctx context.Context, ex executor.Executor, role string) error {
	_, err := ex.Run(ctx, shell.Join("docker", "swarm", "join-token", "--rotate", "--quiet", role)+" >/dev/null")
	return scrubbed("docker swarm join-token --rotate", err)
}

// SwarmJoin joins the host to a swarm. The token travels on stdin, so it is
// never part of a command line Pier builds, logs or keeps.
func SwarmJoin(ctx context.Context, ex executor.Executor, token, managerAddr, advertiseAddr string) error {
	args := []string{}
	if advertiseAddr != "" {
		args = append(args, "--advertise-addr", advertiseAddr)
	}
	args = append(args, net.JoinHostPort(managerAddr, fmt.Sprint(SwarmPort)))
	script := `IFS= read -r token && exec docker swarm join --token "$token" "$@" >/dev/null`
	command := shell.Join(append([]string{"sh", "-c", script, "pier-swarm-join"}, args...)...)
	_, err := executor.RunInput(ctx, ex, command, []byte(token+"\n"))
	if err != nil {
		return fmt.Errorf("docker swarm join: %s", strings.ReplaceAll(ScrubJoinTokens(err.Error()), token, "••••••"))
	}
	return nil
}

// SwarmLeave makes the host leave its swarm; force is required for managers.
func SwarmLeave(ctx context.Context, ex executor.Executor, force bool) error {
	args := []string{"docker", "swarm", "leave"}
	if force {
		args = append(args, "--force")
	}
	_, err := ex.Run(ctx, shell.Join(args...))
	return scrubbed("docker swarm leave", err)
}

// ListSwarmNodes lists the nodes of the swarm, as seen by a manager.
func ListSwarmNodes(ctx context.Context, ex executor.Executor) ([]SwarmNode, error) {
	output, err := ex.Run(ctx, shell.Join("docker", "node", "ls", "--format", FormatColumns(
		"{{.ID}}", "{{.Hostname}}", "{{.Status}}", "{{.Availability}}", "{{.ManagerStatus}}", "{{.EngineVersion}}")))
	if err != nil {
		return nil, scrubbed("docker node ls", err)
	}
	nodes := []SwarmNode{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 4 || !ValidNodeID(fields[0]) {
			continue
		}
		fields = append(fields, "", "")
		nodes = append(nodes, SwarmNode{
			ID: fields[0], Hostname: fields[1], Status: strings.ToLower(fields[2]), Availability: strings.ToLower(fields[3]),
			ManagerStatus: fields[4], EngineVersion: fields[5],
		})
	}
	return nodes, nil
}

// SetNodeAvailability drains a node (its tasks move elsewhere) or makes it
// active again.
func SetNodeAvailability(ctx context.Context, ex executor.Executor, nodeID, availability string) error {
	if !ValidNodeID(nodeID) || (availability != AvailabilityActive && availability != AvailabilityDrain) {
		return fmt.Errorf("invalid node update")
	}
	_, err := ex.Run(ctx, shell.Join("docker", "node", "update", "--availability", availability, nodeID))
	return scrubbed("docker node update", err)
}

// DemoteNode turns a manager into a worker.
func DemoteNode(ctx context.Context, ex executor.Executor, nodeID string) error {
	if !ValidNodeID(nodeID) {
		return fmt.Errorf("invalid node id")
	}
	_, err := ex.Run(ctx, shell.Join("docker", "node", "demote", nodeID))
	return scrubbed("docker node demote", err)
}

// RemoveNode deletes a node from the swarm membership (after it left).
func RemoveNode(ctx context.Context, ex executor.Executor, nodeID string) error {
	if !ValidNodeID(nodeID) {
		return fmt.Errorf("invalid node id")
	}
	_, err := ex.Run(ctx, shell.Join("docker", "node", "rm", "--force", nodeID))
	return scrubbed("docker node rm", err)
}

// EnsureSwarmNetwork creates the attachable overlay network of Pier's Swarm
// services when it does not exist. It must run on a manager.
func EnsureSwarmNetwork(ctx context.Context, ex executor.Executor) error {
	create := shell.Join("docker", "network", "create", "--driver", "overlay", "--attachable",
		"--label", LabelManaged+"=true", "--label", LabelIngress+"=true", SwarmNetwork)
	command := shell.Join("docker", "network", "inspect", SwarmNetwork) + " >/dev/null 2>&1 || " + create
	if _, err := ex.Run(ctx, command); err != nil {
		return fmt.Errorf("ensure swarm network %s: %w", SwarmNetwork, err)
	}
	return nil
}
