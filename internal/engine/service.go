package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
	"pier/internal/server"
)

const (
	pullTimeout    = 15 * time.Minute
	execTimeout    = 60 * time.Second
	maxExecOutput  = 64 * 1024
	composeProject = "com.docker.compose.project"
)

// Connector reaches a server.
type Connector interface {
	Executor(ctx context.Context, serverID string) (executor.Executor, server.Server, error)
}

// Service exposes plain Docker operations on a server.
type Service struct {
	connector Connector
}

// NewService creates the service.
func NewService(connector Connector) *Service { return &Service{connector: connector} }

// psRow mirrors the JSON `docker ps --format '{{json .}}'` prints per line.
type psRow struct {
	ID, Names, Image, Command, State, Status, Ports, Networks, CreatedAt, Labels string
}

// Containers lists every container of the server, running or not.
func (s *Service) Containers(ctx context.Context, serverID string) ([]Container, error) {
	rows, err := query[psRow](ctx, s, serverID, "docker", "ps", "--all", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	containers := make([]Container, len(rows))
	for i, row := range rows {
		labels := parseLabels(row.Labels)
		containers[i] = Container{
			ID: row.ID, Name: row.Names, Image: row.Image, Command: strings.Trim(row.Command, `"`),
			State: row.State, Status: row.Status, Ports: row.Ports, Networks: row.Networks, Created: row.CreatedAt,
			Managed: labels[docker.LabelManaged] == "true", ComposeProject: labels[composeProject],
		}
		if containers[i].Managed {
			containers[i].Owner = &Owner{Type: labels[docker.LabelType], ID: labels[docker.LabelID]}
		}
	}
	return containers, nil
}

// parseLabels reads docker's "k=v,k=v" rendering. A value containing a comma
// yields stray fragments, which is harmless: only well-known keys are read.
func parseLabels(rendered string) map[string]string {
	labels := map[string]string{}
	for _, pair := range strings.Split(rendered, ",") {
		if key, value, found := strings.Cut(pair, "="); found {
			labels[key] = value
		}
	}
	return labels
}

// Inspect returns the full `docker inspect` document of a container. It
// includes the container environment, so callers must treat it as sensitive.
func (s *Service) Inspect(ctx context.Context, serverID, containerID string) (json.RawMessage, error) {
	ex, err := s.reach(ctx, serverID, containerID)
	if err != nil {
		return nil, err
	}
	output, err := ex.Run(ctx, shell.Join("docker", "inspect", "--type", "container", containerID))
	if err != nil {
		return nil, notFoundOrUnavailable(err, "Container")
	}
	var documents []json.RawMessage
	if err := json.Unmarshal([]byte(output), &documents); err != nil || len(documents) == 0 {
		return nil, apperr.Unavailable("Resposta inesperada do docker inspect.")
	}
	return documents[0], nil
}

// containerActions maps the public action names to docker verbs.
var containerActions = map[string][]string{
	"start": {"start"}, "stop": {"stop", "--time", "30"}, "restart": {"restart", "--time", "30"},
	"kill": {"kill"}, "pause": {"pause"}, "unpause": {"unpause"},
}

// Act runs a lifecycle action on a container.
func (s *Service) Act(ctx context.Context, serverID, containerID, action string) error {
	verb, known := containerActions[action]
	if !known {
		return apperr.NotFound("Ação")
	}
	ex, err := s.reach(ctx, serverID, containerID)
	if err != nil {
		return err
	}
	_, err = ex.Run(ctx, shell.Join(append(append([]string{"docker"}, verb...), containerID)...))
	return notFoundOrUnavailable(err, "Container")
}

// Remove deletes a container. Containers owned by a Pier resource are
// refused: removing them here would leave that resource in an inconsistent
// state, and its own page is the right place to do it.
func (s *Service) Remove(ctx context.Context, serverID, containerID string, force bool) error {
	containers, err := s.Containers(ctx, serverID)
	if err != nil {
		return err
	}
	target, found := find(containers, containerID)
	if !found {
		return apperr.NotFound("Container")
	}
	if target.Managed {
		return apperr.Conflict("Este container é gerenciado pelo Pier (%s). Remova o recurso pela página dele.", target.Owner.Type)
	}
	ex, _, err := s.connector.Executor(ctx, serverID)
	if err != nil {
		return err
	}
	args := []string{"docker", "rm"}
	if force {
		args = append(args, "--force")
	}
	if _, err := ex.Run(ctx, shell.Join(append(args, target.ID)...)); err != nil {
		return apperr.Conflict("O Docker recusou a remoção: %s", lastLine(err))
	}
	return nil
}

func find(containers []Container, idOrName string) (Container, bool) {
	for _, container := range containers {
		if container.ID == idOrName || container.Name == idOrName || strings.HasPrefix(container.ID, idOrName) && len(idOrName) >= 12 {
			return container, true
		}
	}
	return Container{}, false
}

// Run starts a new container from an image.
func (s *Service) Run(ctx context.Context, serverID string, spec RunSpec) (string, error) {
	if err := spec.validate(); err != nil {
		return "", err
	}
	ex, _, err := s.connector.Executor(ctx, serverID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	output, err := ex.Run(ctx, shell.Join(runArgs(spec)...))
	if err != nil {
		return "", apperr.Unavailable("O Docker não iniciou o container: %s", lastLine(err))
	}
	return lastLineOf(output), nil
}

func runArgs(spec RunSpec) []string {
	args := []string{"docker", "run", "--detach", "--name", spec.Name, "--restart", spec.Restart,
		"--label", LabelStandalone + "=true"}
	if spec.Network != "" {
		args = append(args, "--network", spec.Network)
	}
	for _, entry := range spec.Env {
		args = append(args, "--env", entry)
	}
	for _, mapping := range spec.Ports {
		args = append(args, "--publish", mapping)
	}
	for _, mount := range spec.Volumes {
		args = append(args, "--volume", mount)
	}
	args = append(args, spec.Image)
	// The command is split on whitespace; each word becomes one argument.
	return append(args, strings.Fields(spec.Command)...)
}

// Exec runs a one-off shell command inside a container and returns its output.
func (s *Service) Exec(ctx context.Context, serverID, containerID, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", apperr.Invalid("Informe o comando a executar.")
	}
	ex, err := s.reach(ctx, serverID, containerID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	var output strings.Builder
	err = ex.Stream(ctx, docker.Exec(containerID, command), func(line string) {
		if output.Len() < maxExecOutput {
			output.WriteString(line + "\n")
		}
	})
	text := strings.TrimRight(output.String(), "\n")
	if err != nil {
		// A failing command is a normal outcome: report it with what it printed.
		return strings.TrimSpace(text + "\n" + err.Error()), nil
	}
	return text, nil
}

// LogsCommand returns the executor and command that follow a container's logs.
func (s *Service) LogsCommand(ctx context.Context, serverID, containerID string, tail int) (executor.Executor, string, error) {
	ex, err := s.reach(ctx, serverID, containerID)
	if err != nil {
		return nil, "", err
	}
	return ex, docker.LogsCommand(containerID, tail, true), nil
}

type imageRow struct {
	ID, Repository, Tag, Size, CreatedSince string
}

// Images lists the images stored on the server.
func (s *Service) Images(ctx context.Context, serverID string) ([]Image, error) {
	rows, err := query[imageRow](ctx, s, serverID, "docker", "images", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	images := make([]Image, len(rows))
	for i, row := range rows {
		images[i] = Image{ID: row.ID, Repository: row.Repository, Tag: row.Tag, Size: row.Size, Created: row.CreatedSince}
	}
	return images, nil
}

// PullImage downloads an image and returns the tail of docker's output.
func (s *Service) PullImage(ctx context.Context, serverID, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if !docker.ValidImage(reference) {
		fields := apperr.Fields{}
		fields.Add("image", "Informe uma imagem válida, ex.: nginx:1.27.")
		return "", fields.Err()
	}
	ex, _, err := s.connector.Executor(ctx, serverID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	output, err := ex.Run(ctx, shell.Join("docker", "pull", reference))
	if err != nil {
		return "", apperr.Unavailable("Não foi possível baixar a imagem: %s", lastLine(err))
	}
	return lastLineOf(output), nil
}

// RemoveImage deletes an image by reference or id. Docker refuses images in
// use unless forced.
func (s *Service) RemoveImage(ctx context.Context, serverID, reference string, force bool) error {
	if !docker.ValidImage(reference) && !docker.ValidObjectName(strings.TrimPrefix(reference, "sha256:")) {
		return apperr.Invalid("Referência de imagem inválida.")
	}
	ex, _, err := s.connector.Executor(ctx, serverID)
	if err != nil {
		return err
	}
	args := []string{"docker", "rmi"}
	if force {
		args = append(args, "--force")
	}
	_, err = ex.Run(ctx, shell.Join(append(args, reference)...))
	return refusedRemoval(err, "Imagem")
}

// PruneImages removes dangling (untagged, unused) images only.
func (s *Service) PruneImages(ctx context.Context, serverID string) (string, error) {
	return s.runSimple(ctx, serverID, "docker", "image", "prune", "--force")
}

type volumeRow struct {
	Name, Driver, Scope string
}

// Volumes lists the named volumes of the server.
func (s *Service) Volumes(ctx context.Context, serverID string) ([]Volume, error) {
	rows, err := query[volumeRow](ctx, s, serverID, "docker", "volume", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	volumes := make([]Volume, len(rows))
	for i, row := range rows {
		volumes[i] = Volume(row)
	}
	return volumes, nil
}

// CreateVolume creates a named volume.
func (s *Service) CreateVolume(ctx context.Context, serverID, name string) error {
	if err := requireName(name); err != nil {
		return err
	}
	_, err := s.runSimple(ctx, serverID, "docker", "volume", "create", name)
	return err
}

// RemoveVolume deletes a volume and its data. Docker refuses volumes in use.
func (s *Service) RemoveVolume(ctx context.Context, serverID, name string) error {
	if err := requireName(name); err != nil {
		return err
	}
	ex, _, err := s.connector.Executor(ctx, serverID)
	if err != nil {
		return err
	}
	_, err = ex.Run(ctx, shell.Join("docker", "volume", "rm", name))
	return refusedRemoval(err, "Volume")
}

type networkRow struct {
	ID, Name, Driver, Scope, Labels string
}

// builtinNetworks exist on every Docker host and cannot be removed.
var builtinNetworks = []string{"bridge", "host", "none"}

// Networks lists the docker networks of the server.
func (s *Service) Networks(ctx context.Context, serverID string) ([]Network, error) {
	rows, err := query[networkRow](ctx, s, serverID, "docker", "network", "ls", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	networks := make([]Network, len(rows))
	for i, row := range rows {
		networks[i] = Network{ID: row.ID, Name: row.Name, Driver: row.Driver, Scope: row.Scope, Reserved: reserved(row)}
	}
	return networks, nil
}

func reserved(row networkRow) bool {
	return row.Name == docker.Network || contains(builtinNetworks, row.Name) ||
		parseLabels(row.Labels)[docker.LabelIngress] == "true"
}

// CreateNetwork creates a bridge network.
func (s *Service) CreateNetwork(ctx context.Context, serverID, name string) error {
	if err := requireName(name); err != nil {
		return err
	}
	_, err := s.runSimple(ctx, serverID, "docker", "network", "create", name)
	return err
}

// RemoveNetwork deletes a network. Networks Pier depends on are refused.
func (s *Service) RemoveNetwork(ctx context.Context, serverID, name string) error {
	networks, err := s.Networks(ctx, serverID)
	if err != nil {
		return err
	}
	for _, network := range networks {
		if network.Name != name && network.ID != name {
			continue
		}
		if network.Reserved {
			return apperr.Conflict("A rede %q é usada pelo Docker ou pelo Pier e não pode ser removida aqui.", network.Name)
		}
		ex, _, err := s.connector.Executor(ctx, serverID)
		if err != nil {
			return err
		}
		if _, err := ex.Run(ctx, shell.Join("docker", "network", "rm", network.ID)); err != nil {
			return apperr.Conflict("O Docker recusou a remoção: %s", lastLine(err))
		}
		return nil
	}
	return apperr.NotFound("Rede")
}

// reach validates the object identifier before it reaches a command line.
func (s *Service) reach(ctx context.Context, serverID, objectID string) (executor.Executor, error) {
	if !docker.ValidObjectName(objectID) {
		return nil, apperr.NotFound("Container")
	}
	ex, _, err := s.connector.Executor(ctx, serverID)
	return ex, err
}

func (s *Service) runSimple(ctx context.Context, serverID string, args ...string) (string, error) {
	ex, _, err := s.connector.Executor(ctx, serverID)
	if err != nil {
		return "", err
	}
	output, err := ex.Run(ctx, shell.Join(args...))
	if err != nil {
		return "", apperr.Unavailable("O Docker recusou a operação: %s", lastLine(err))
	}
	return output, nil
}

// query runs a docker listing that prints one JSON object per line.
func query[T any](ctx context.Context, s *Service, serverID string, args ...string) ([]T, error) {
	ex, _, err := s.connector.Executor(ctx, serverID)
	if err != nil {
		return nil, err
	}
	output, err := ex.Run(ctx, shell.Join(args...))
	if err != nil {
		return nil, apperr.Unavailable("Não foi possível consultar o Docker: %s", lastLine(err))
	}
	rows := []T{}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row T
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("decode docker output %q: %w", line, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func requireName(name string) error {
	if !docker.ValidObjectName(name) {
		fields := apperr.Fields{}
		fields.Add("name", "Use letras, números, '-', '_' e '.', começando por letra ou número.")
		return fields.Err()
	}
	return nil
}

// refusedRemoval tells "it does not exist" apart from "docker will not let
// go of it" (still in use), which the caller can do something about.
func refusedRemoval(err error, what string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "no such") {
		return apperr.NotFound(what)
	}
	return apperr.Conflict("O Docker recusou a remoção: %s", lastLine(err))
}

func notFoundOrUnavailable(err error, what string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "no such") {
		return apperr.NotFound(what)
	}
	return apperr.Unavailable("O Docker recusou a operação: %s", lastLine(err))
}

func lastLine(err error) string { return lastLineOf(err.Error()) }

func lastLineOf(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
