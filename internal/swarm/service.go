package swarm

import (
	"context"
	"database/sql"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/idgen"
	"pier/internal/platform/shell"
	"pier/internal/server"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Connector interface {
	Executor(context.Context, string) (executor.Executor, server.Server, error)
}
type Service struct {
	store     store
	connector Connector
	mu        sync.Mutex
}

func NewService(db *sql.DB, c Connector) *Service                      { return &Service{store: store{db}, connector: c} }
func (s *Service) List(ctx context.Context) ([]Cluster, error)         { return s.store.list(ctx) }
func (s *Service) Get(ctx context.Context, id string) (Cluster, error) { return s.store.get(ctx, id) }
func (s *Service) Manager(ctx context.Context, id string) (executor.Executor, error) {
	c, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, n := range c.Nodes {
		if n.Role == docker.SwarmManager {
			ex, _, err := s.connector.Executor(ctx, n.ServerID)
			if err == nil {
				state, e := docker.ReadSwarmState(ctx, ex)
				if e == nil && state.Active() && state.ControlAvailable && state.NodeID == n.NodeID {
					return ex, nil
				}
			}
		}
	}
	return nil, apperr.Conflict("Nenhum manager Swarm está disponível.")
}
func (s *Service) Create(ctx context.Context, i CreateInput) (Cluster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := i.validate(); err != nil {
		return Cluster{}, err
	}
	if _, ok, err := s.store.membership(ctx, i.ManagerServerID); err != nil {
		return Cluster{}, err
	} else if ok {
		return Cluster{}, apperr.Conflict("O servidor já pertence a um Swarm.")
	}
	ex, target, err := s.connector.Executor(ctx, i.ManagerServerID)
	if err != nil {
		return Cluster{}, err
	}
	if i.AdvertiseAddr == "" {
		i.AdvertiseAddr = hostAddress(target.Host)
	}
	if !validAddress(i.AdvertiseAddr) {
		return Cluster{}, apperr.Conflict("Use um endereço IP, como 10.0.0.5.")
	}
	state, err := docker.ReadSwarmState(ctx, ex)
	if err != nil {
		return Cluster{}, err
	}
	if state.LocalNodeState != "inactive" {
		return Cluster{}, apperr.Conflict("O servidor já pertence a um Swarm.")
	}
	if err = docker.SwarmInit(ctx, ex, i.AdvertiseAddr); err != nil {
		return Cluster{}, err
	}
	state, err = docker.ReadSwarmState(ctx, ex)
	if err != nil {
		return Cluster{}, err
	}
	if !state.ControlAvailable || !docker.ValidNodeID(state.NodeID) {
		return Cluster{}, apperr.Conflict("Nenhum manager Swarm está disponível.")
	}
	c := Cluster{ID: idgen.New(), Name: i.Name, Description: i.Description, AdvertiseAddr: i.AdvertiseAddr, CreatedAt: time.Now()}
	n := Node{ServerID: i.ManagerServerID, NodeID: state.NodeID, Role: docker.SwarmManager, Primary: true, AdvertiseAddr: i.AdvertiseAddr, JoinedAt: c.CreatedAt}
	if err = s.store.insertCluster(ctx, c, n); err != nil {
		return Cluster{}, err
	}
	return s.Get(ctx, c.ID)
}
func (s *Service) Join(ctx context.Context, id string, i JoinInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := i.validate(); err != nil {
		return err
	}
	c, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if len(c.Nodes) >= maxClusterNodes {
		return apperr.Conflict("O Swarm atingiu o limite de nós.")
	}
	if _, ok, err := s.store.membership(ctx, i.ServerID); err != nil {
		return err
	} else if ok {
		return apperr.Conflict("O servidor já pertence a um Swarm.")
	}
	manager, err := s.Manager(ctx, id)
	if err != nil {
		return err
	}
	ex, _, err := s.connector.Executor(ctx, i.ServerID)
	if err != nil {
		return err
	}
	state, err := docker.ReadSwarmState(ctx, ex)
	if err != nil {
		return err
	}
	if state.LocalNodeState != "inactive" {
		return apperr.Conflict("O servidor já pertence a um Swarm.")
	}
	token, err := docker.SwarmJoinToken(ctx, manager, i.Role)
	if err != nil {
		return err
	}
	joinErr := docker.SwarmJoin(ctx, ex, token, c.AdvertiseAddr, i.AdvertiseAddr)
	rotateErr := docker.RotateSwarmJoinToken(ctx, manager, i.Role)
	if joinErr != nil {
		return joinErr
	}
	if rotateErr != nil {
		return rotateErr
	}
	state, err = docker.ReadSwarmState(ctx, ex)
	if err != nil {
		return err
	}
	if !state.Active() || !docker.ValidNodeID(state.NodeID) {
		return apperr.Conflict("O nó não confirmou a entrada no Swarm.")
	}
	return s.store.addNode(ctx, id, Node{ServerID: i.ServerID, NodeID: state.NodeID, Role: i.Role, AdvertiseAddr: i.AdvertiseAddr, JoinedAt: time.Now()})
}
func (s *Service) Nodes(ctx context.Context, id string) ([]docker.SwarmNode, error) {
	ex, e := s.Manager(ctx, id)
	if e != nil {
		return nil, e
	}
	return docker.ListSwarmNodes(ctx, ex)
}
func (s *Service) Availability(ctx context.Context, id, node, value string) error {
	ex, e := s.Manager(ctx, id)
	if e != nil {
		return e
	}
	return docker.SetNodeAvailability(ctx, ex, node, value)
}
func (s *Service) RemoveNode(ctx context.Context, id, serverID, confirmation string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if !confirms(confirmation, c.Name) {
		return apperr.Conflict("Digite o nome do Swarm para confirmar.")
	}
	for _, n := range c.Nodes {
		if n.ServerID != serverID {
			continue
		}
		if n.Primary || n.Role == docker.SwarmManager {
			return apperr.Conflict("Não remova managers pelo Pier; preserve o quorum do Swarm.")
		}
		manager, e := s.Manager(ctx, id)
		if e != nil {
			return e
		}
		ex, _, e := s.connector.Executor(ctx, serverID)
		if e != nil {
			return e
		}
		if e = docker.SwarmLeave(ctx, ex, false); e != nil {
			return e
		}
		if e = docker.RemoveNode(ctx, manager, n.NodeID); e != nil {
			return e
		}
		return s.store.removeNode(ctx, serverID)
	}
	return apperr.NotFound("Swarm")
}
func (s *Service) Delete(ctx context.Context, id, confirmation string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if !confirms(confirmation, c.Name) {
		return apperr.Conflict("Digite o nome do Swarm para confirmar.")
	}
	if len(c.Nodes) != 1 {
		return apperr.Conflict("Remova os workers antes de excluir o Swarm.")
	}
	ex, e := s.Manager(ctx, id)
	if e != nil {
		return e
	}
	liveNodes, e := docker.ListSwarmNodes(ctx, ex)
	if e != nil {
		return e
	}
	if len(liveNodes) != 1 || liveNodes[0].ID != c.Nodes[0].NodeID {
		return apperr.Conflict("Remova os outros nós antes de excluir o Swarm.")
	}
	output, e := ex.Run(ctx, shell.Join("docker", "service", "ls", "--quiet"))
	services := strings.TrimSpace(output)
	if e != nil {
		return e
	}
	count, e := s.store.applications(ctx, id)
	if e != nil {
		return e
	}
	if len(services) > 0 || count > 0 {
		return apperr.Conflict("Remova os serviços antes de excluir o Swarm.")
	}
	if e = docker.SwarmLeave(ctx, ex, true); e != nil {
		return e
	}
	return s.store.deleteCluster(ctx, id)
}

// Services are restricted to this cluster's label; arbitrary Docker services are never mutated.
func (s *Service) Services(ctx context.Context, id string) ([]docker.SwarmService, error) {
	ex, e := s.Manager(ctx, id)
	if e != nil {
		return nil, e
	}
	return docker.ListSwarmServices(ctx, ex, id)
}
func (s *Service) owned(ctx context.Context, id, name string) (executor.Executor, error) {
	ex, e := s.Manager(ctx, id)
	if e != nil {
		return nil, e
	}
	items, e := docker.ListSwarmServices(ctx, ex, id)
	if e != nil {
		return nil, e
	}
	for _, v := range items {
		if v.Name == name {
			return ex, nil
		}
	}
	return nil, apperr.NotFound("Swarm")
}

type DeployInput struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Replicas int    `json:"replicas"`
}

func (s *Service) Deploy(ctx context.Context, id string, i DeployInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !docker.ValidServiceName(i.Name) || !docker.ValidImage(i.Image) || i.Replicas < 0 || i.Replicas > 100 {
		return apperr.Conflict("Informe serviço, imagem e réplicas válidos (0 a 100).")
	}
	ex, e := s.Manager(ctx, id)
	if e != nil {
		return e
	}
	// A deterministic namespace prevents collisions with services outside this cluster.
	name := "pier-" + id + "-" + i.Name
	if !docker.ValidServiceName(name) {
		return apperr.Conflict("Informe serviço, imagem e réplicas válidos (0 a 100).")
	}
	items, e := docker.ListSwarmServices(ctx, ex, id)
	if e != nil {
		return e
	}
	for _, item := range items {
		if item.Name == name {
			_, e = ex.Run(ctx, shell.Join("docker", "service", "update", "--detach", "--image", i.Image, "--replicas", strconv.Itoa(i.Replicas), name))
			return e
		}
	}
	if e = docker.EnsureSwarmNetwork(ctx, ex); e != nil {
		return e
	}
	_, e = ex.Run(ctx, shell.Join("docker", "service", "create", "--detach", "--name", name, "--label", docker.LabelManaged+"=true", "--label", docker.LabelID+"="+id, "--network", docker.SwarmNetwork, "--replicas", strconv.Itoa(i.Replicas), i.Image))
	return e
}
func (s *Service) Action(ctx context.Context, id, name, action string, replicas int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ex, e := s.owned(ctx, id, name)
	if e != nil {
		return e
	}
	switch action {
	case "scale":
		if replicas < 0 || replicas > 100 {
			return apperr.Conflict("Informe serviço, imagem e réplicas válidos (0 a 100).")
		}
		return docker.ScaleSwarmService(ctx, ex, name, replicas)
	case "restart":
		return docker.RestartSwarmService(ctx, ex, name)
	case "rollback":
		return docker.RollbackSwarmService(ctx, ex, name)
	case "remove":
		_, e = ex.Run(ctx, shell.Join("docker", "service", "rm", name))
		return e
	}
	return apperr.Conflict("Ação Swarm inválida.")
}
func (s *Service) Logs(ctx context.Context, id, name string) (string, error) {
	ex, e := s.owned(ctx, id, name)
	if e != nil {
		return "", e
	}
	cmd, e := docker.SwarmServiceLogsCommand(name, 200)
	if e != nil {
		return "", e
	}
	return ex.Run(ctx, cmd)
}
func (s *Service) Tasks(ctx context.Context, id, name string) ([]docker.SwarmTask, error) {
	ex, e := s.owned(ctx, id, name)
	if e != nil {
		return nil, e
	}
	return docker.ListSwarmTasks(ctx, ex, name)
}
