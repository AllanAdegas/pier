package swarm

import (
	"context"
	"database/sql"
	"errors"

	"pier/internal/platform/apperr"
	"pier/internal/platform/sqlite"
)

// store persists clusters and their nodes; nothing secret is kept.
type store struct {
	db *sql.DB
}

func (s store) list(ctx context.Context) ([]Cluster, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, swarm_id, advertise_addr, created_at
		FROM swarm_clusters ORDER BY created_at`)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range clusters {
		if clusters[index].Nodes, err = s.nodes(ctx, clusters[index].ID); err != nil {
			return nil, err
		}
	}
	return clusters, nil
}

func (s store) get(ctx context.Context, id string) (Cluster, error) {
	cluster, err := scanCluster(s.db.QueryRowContext(ctx, `SELECT id, name, description, swarm_id, advertise_addr, created_at
		FROM swarm_clusters WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Cluster{}, apperr.NotFound("Swarm")
	}
	if err != nil {
		return Cluster{}, err
	}
	cluster.Nodes, err = s.nodes(ctx, id)
	return cluster, err
}

// nodes returns the nodes of a cluster, the primary manager first, then
// the other managers, then the workers.
func (s store) nodes(ctx context.Context, clusterID string) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n.server_id, COALESCE(sv.name, n.server_id), n.node_id, n.role,
		n.is_primary, n.advertise_addr, n.joined_at
		FROM swarm_nodes n LEFT JOIN servers sv ON sv.id = n.server_id WHERE n.cluster_id = ?
		ORDER BY n.is_primary DESC, n.role = 'worker', n.joined_at`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []Node{}
	for rows.Next() {
		var node Node
		var joined string
		if err := rows.Scan(&node.ServerID, &node.ServerName, &node.NodeID, &node.Role, &node.Primary,
			&node.AdvertiseAddr, &joined); err != nil {
			return nil, err
		}
		node.JoinedAt = sqlite.ParseTime(joined)
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

// membership returns the cluster a server belongs to, if any.
func (s store) membership(ctx context.Context, serverID string) (string, bool, error) {
	var clusterID string
	err := s.db.QueryRowContext(ctx, `SELECT cluster_id FROM swarm_nodes WHERE server_id = ?`, serverID).Scan(&clusterID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return clusterID, err == nil, err
}

func (s store) insertCluster(ctx context.Context, cluster Cluster, manager Node) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO swarm_clusters (id, name, description, swarm_id, advertise_addr, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, cluster.ID, cluster.Name, cluster.Description, cluster.SwarmID, cluster.AdvertiseAddr,
		sqlite.FormatTime(cluster.CreatedAt)); err != nil {
		return err
	}
	if err := insertNode(ctx, tx, cluster.ID, manager); err != nil {
		return err
	}
	return tx.Commit()
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertNode(ctx context.Context, db execer, clusterID string, node Node) error {
	_, err := db.ExecContext(ctx, `INSERT INTO swarm_nodes (server_id, cluster_id, node_id, role, is_primary, advertise_addr, joined_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, node.ServerID, clusterID, node.NodeID, node.Role, node.Primary, node.AdvertiseAddr,
		sqlite.FormatTime(node.JoinedAt))
	return err
}

func (s store) addNode(ctx context.Context, clusterID string, node Node) error {
	return insertNode(ctx, s.db, clusterID, node)
}

func (s store) removeNode(ctx context.Context, serverID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM swarm_nodes WHERE server_id = ?`, serverID)
	return err
}

func (s store) rename(ctx context.Context, id, name, description string) error {
	err := sqlite.RequireAffected(s.db.ExecContext(ctx, `UPDATE swarm_clusters SET name = ?, description = ? WHERE id = ?`,
		name, description, id))
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound("Swarm")
	}
	return err
}

func (s store) deleteCluster(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM swarm_clusters WHERE id = ?`, id)
	return err
}

// applications counts the applications deployed to a cluster.
func (s store) applications(ctx context.Context, clusterID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_swarm WHERE cluster_id = ?`, clusterID).Scan(&count)
	return count, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCluster(row rowScanner) (Cluster, error) {
	var cluster Cluster
	var created string
	if err := row.Scan(&cluster.ID, &cluster.Name, &cluster.Description, &cluster.SwarmID, &cluster.AdvertiseAddr, &created); err != nil {
		return Cluster{}, err
	}
	cluster.CreatedAt = sqlite.ParseTime(created)
	cluster.Nodes = []Node{}
	return cluster, nil
}
