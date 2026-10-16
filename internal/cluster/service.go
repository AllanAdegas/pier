package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/kube"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/idgen"
	"pier/internal/platform/oplog"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

const (
	provisionTimeout   = 15 * time.Minute
	checkTimeout       = 10 * time.Second
	discoveryLifetime  = 5 * time.Minute
	kubeconfigWait     = 3 * time.Minute
	readinessWait      = 5 * time.Minute
	provisionPollEvery = 3 * time.Second
	portProbeTimeout   = 1500 * time.Millisecond
	cancelGracePeriod  = 20 * time.Second
)

// Connector reaches the server a K3s cluster runs on.
type Connector interface {
	Executor(ctx context.Context, serverID string) (executor.Executor, server.Server, error)
}

// Service implements the cluster use cases.
type Service struct {
	db         *sql.DB
	box        *secretbox.Box
	connector  Connector
	operations *oplog.Registry

	// portInUse reports whether something already answers on an address; it
	// is a field so tests do not depend on the network.
	portInUse func(ctx context.Context, address string) bool
	// localAddress is the loopback, unless Pier runs in a container.
	localAddress string

	mu      sync.Mutex
	clients map[string]*session
	// cancels stops the creation of a cluster that is still in progress.
	cancels map[string]context.CancelFunc
}

// session caches the client of a cluster and what was discovered through it.
type session struct {
	client       *kube.Client
	resources    []kube.Resource
	discoveredAt time.Time
}

// NewService creates the service. localAddress is where the ports published
// by the local server answer, seen from this process.
func NewService(
	db *sql.DB, box *secretbox.Box, connector Connector, operations *oplog.Registry, localAddress string,
) *Service {
	return &Service{
		db: db, box: box, connector: connector, operations: operations,
		portInUse: dialSucceeds, localAddress: localAddress,
		clients: make(map[string]*session), cancels: make(map[string]context.CancelFunc),
	}
}

const clusterColumns = `id, name, description, source, COALESCE(server_id, ''), config, endpoint, status,
	status_message, version, nodes, last_checked_at, created_at`

// List returns every cluster.
func (s *Service) List(ctx context.Context) ([]Cluster, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+clusterColumns+` FROM clusters ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clusters := []Cluster{}
	for rows.Next() {
		cluster, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, cluster)
	}
	return clusters, rows.Err()
}

// Get returns one cluster.
func (s *Service) Get(ctx context.Context, id string) (Cluster, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE id = ?`, id)
	cluster, err := scanCluster(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Cluster{}, apperr.NotFound("Cluster")
	}
	return cluster, err
}

// Import registers an existing cluster from its kubeconfig. The credentials
// are checked for a form Pier can use; an unreachable cluster is still
// registered (with that status), since it may only be temporarily down.
func (s *Service) Import(ctx context.Context, input ImportInput) (Cluster, error) {
	fields := apperr.Fields{}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		fields.Add("name", "Informe o nome do cluster.")
	}
	config, err := kube.ParseKubeconfig([]byte(input.Kubeconfig), strings.TrimSpace(input.Context))
	if err != nil {
		fields.Add("kubeconfig", "%s", capitalize(err.Error()))
	}
	if err := fields.Err(); err != nil {
		return Cluster{}, err
	}
	cluster := Cluster{
		ID: idgen.New(), Name: name, Description: strings.TrimSpace(input.Description), Source: SourceImported,
		Endpoint: config.Server, Status: StatusUnknown, CreatedAt: time.Now(),
	}
	// Only the selected context is kept: the stored kubeconfig must not carry
	// credentials for other clusters that happened to be in the same file.
	minimal, err := config.Render()
	if err != nil {
		return Cluster{}, err
	}
	if err := s.insert(ctx, cluster, string(minimal)); err != nil {
		return Cluster{}, err
	}
	return s.Check(ctx, cluster.ID)
}

// Operation returns the progress of the latest background operation.
func (s *Service) Operation(id string) oplog.Snapshot {
	snapshot, _ := s.operations.Get(id)
	return snapshot
}

// Update renames a cluster.
func (s *Service) Update(ctx context.Context, id, name, description string) (Cluster, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		fields := apperr.Fields{}
		fields.Add("name", "Informe o nome do cluster.")
		return Cluster{}, fields.Err()
	}
	err := sqlite.RequireAffected(s.db.ExecContext(ctx, `UPDATE clusters SET name = ?, description = ? WHERE id = ?`,
		name, strings.TrimSpace(description), id))
	if errors.Is(err, sqlite.ErrNotFound) {
		return Cluster{}, apperr.NotFound("Cluster")
	}
	if err != nil {
		return Cluster{}, err
	}
	return s.Get(ctx, id)
}

// Check probes the cluster and records version, node count and reachability.
func (s *Service) Check(ctx context.Context, id string) (Cluster, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	version, nodes, err := s.probe(ctx, id)
	background := context.WithoutCancel(ctx)
	if err != nil {
		if _, isAppErr := apperr.As(err); isAppErr {
			return Cluster{}, err
		}
		s.forget(id)
		_ = s.setStatus(background, id, StatusUnreachable, err.Error(), "", 0)
	} else {
		_ = s.setStatus(background, id, StatusReady, "", version, nodes)
	}
	return s.Get(background, id)
}

func (s *Service) probe(ctx context.Context, id string) (string, int, error) {
	client, err := s.client(ctx, id)
	if err != nil {
		return "", 0, err
	}
	version, err := client.Version(ctx)
	if err != nil {
		return "", 0, err
	}
	nodes, err := client.List(ctx, kube.Resource{Version: "v1", Name: "nodes", Kind: "Node"}, "")
	if err != nil {
		return "", 0, err
	}
	return version, len(nodes), nil
}

// CheckAll refreshes every cluster that is not being provisioned. It is
// called periodically by the monitor.
func (s *Service) CheckAll(ctx context.Context) {
	clusters, err := s.List(ctx)
	if err != nil {
		return
	}
	for _, cluster := range clusters {
		if cluster.Status == StatusProvisioning || ctx.Err() != nil {
			continue
		}
		_, _ = s.Check(ctx, cluster.ID)
	}
}

// Kubeconfig returns the stored kubeconfig, for use with kubectl.
func (s *Service) Kubeconfig(ctx context.Context, id string) (string, error) {
	var sealed string
	err := s.db.QueryRowContext(ctx, `SELECT kubeconfig FROM clusters WHERE id = ?`, id).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperr.NotFound("Cluster")
	}
	if err != nil {
		return "", err
	}
	kubeconfig, err := s.box.Open(sealed)
	if err != nil {
		return "", err
	}
	if kubeconfig == "" {
		return "", apperr.Conflict("O cluster ainda está sendo criado; o kubeconfig não está disponível.")
	}
	return kubeconfig, nil
}

// Delete forgets a cluster. An imported cluster is only unregistered. A K3s
// cluster is owned by Pier, so its container and data are destroyed; with
// force, an unreachable server does not block removing the record.
func (s *Service) Delete(ctx context.Context, id string, force bool) error {
	cluster, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.cancelProvisioning(ctx, id); err != nil {
		return err
	}
	if cluster.Source == SourceK3s {
		if err := s.destroyK3s(ctx, cluster); err != nil && !force {
			return apperr.Unavailable("Não foi possível limpar o servidor (%s). Use a remoção forçada para "+
				"apagar apenas o registro.", err)
		}
	}
	s.forget(id)
	s.operations.Forget(id)
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM clusters WHERE id = ?`, id))
}

func (s *Service) project(cluster Cluster, target server.Server) compose.Project {
	return compose.Project{Name: cluster.ID, Dir: target.ResourceDir(docker.TypeCluster, cluster.ID)}
}

// client returns the cached client of a cluster, building it on first use.
func (s *Service) client(ctx context.Context, id string) (*kube.Client, error) {
	current, err := s.session(ctx, id)
	if err != nil {
		return nil, err
	}
	return current.client, nil
}

func (s *Service) session(ctx context.Context, id string) (*session, error) {
	s.mu.Lock()
	cached, ok := s.clients[id]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	kubeconfig, err := s.Kubeconfig(ctx, id)
	if err != nil {
		return nil, err
	}
	config, err := kube.ParseKubeconfig([]byte(kubeconfig), "")
	if err != nil {
		return nil, apperr.Conflict("O kubeconfig armazenado não pode ser usado: %s", err)
	}
	client, err := kube.NewClient(config)
	if err != nil {
		return nil, apperr.Conflict("O kubeconfig armazenado não pode ser usado: %s", err)
	}
	created := &session{client: client}
	s.mu.Lock()
	s.clients[id] = created
	s.mu.Unlock()
	return created, nil
}

func (s *Service) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, id)
}

func (s *Service) insert(ctx context.Context, cluster Cluster, kubeconfig string) error {
	config, err := json.Marshal(cluster.K3s)
	if err != nil {
		return err
	}
	sealed, err := s.box.Seal(kubeconfig)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO clusters
		(id, name, description, source, server_id, config, kubeconfig, endpoint, status, created_at)
		VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?)`,
		cluster.ID, cluster.Name, cluster.Description, cluster.Source, cluster.ServerID, string(config), sealed,
		cluster.Endpoint, cluster.Status, sqlite.FormatTime(cluster.CreatedAt))
	return err
}

func (s *Service) setKubeconfig(ctx context.Context, id, kubeconfig string) error {
	sealed, err := s.box.Seal(kubeconfig)
	if err != nil {
		return err
	}
	s.forget(id)
	_, err = s.db.ExecContext(ctx, `UPDATE clusters SET kubeconfig = ? WHERE id = ?`, sealed, id)
	return err
}

func (s *Service) setStatus(ctx context.Context, id string, status Status, message, version string, nodes int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE clusters SET status = ?, status_message = ?,
		version = CASE WHEN ? = '' THEN version ELSE ? END, nodes = ?, last_checked_at = ? WHERE id = ?`,
		status, message, version, version, nodes, sqlite.FormatTime(time.Now()), id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCluster(row rowScanner) (Cluster, error) {
	var cluster Cluster
	var config, createdAt string
	var lastCheckedAt sql.NullString
	err := row.Scan(&cluster.ID, &cluster.Name, &cluster.Description, &cluster.Source, &cluster.ServerID, &config,
		&cluster.Endpoint, &cluster.Status, &cluster.StatusMessage, &cluster.Version, &cluster.Nodes,
		&lastCheckedAt, &createdAt)
	if err != nil {
		return Cluster{}, err
	}
	if err := json.Unmarshal([]byte(config), &cluster.K3s); err != nil {
		return Cluster{}, fmt.Errorf("decode config of cluster %s: %w", cluster.ID, err)
	}
	cluster.LastCheckedAt = sqlite.ParseOptionalTime(lastCheckedAt)
	cluster.CreatedAt = sqlite.ParseTime(createdAt)
	return cluster, nil
}

func portMapping(hostPort, containerPort int) string {
	return fmt.Sprintf("%d:%d", hostPort, containerPort)
}

func capitalize(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
