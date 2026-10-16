package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"pier/internal/platform/sqlite"
)

// Store persists servers and private keys.
type Store struct {
	db *sql.DB
}

// NewStore creates the store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const serverColumns = `id, name, description, kind, host, port, username, COALESCE(private_key_id, ''),
	host_key, data_dir, proxy, status, status_message, docker_version, last_checked_at, created_at`

func (s *Store) insert(ctx context.Context, server Server) error {
	proxy, err := json.Marshal(server.Proxy)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO servers
		(id, name, description, kind, host, port, username, private_key_id, host_key, data_dir, proxy, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?)`,
		server.ID, server.Name, server.Description, server.Kind, server.Host, server.Port, server.User,
		server.PrivateKeyID, server.HostKey, server.DataDir, string(proxy), server.Status,
		sqlite.FormatTime(server.CreatedAt))
	return err
}

func (s *Store) update(ctx context.Context, server Server) error {
	proxy, err := json.Marshal(server.Proxy)
	if err != nil {
		return err
	}
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `UPDATE servers SET
		name = ?, description = ?, host = ?, port = ?, username = ?, private_key_id = NULLIF(?, ''),
		host_key = ?, data_dir = ?, proxy = ? WHERE id = ?`,
		server.Name, server.Description, server.Host, server.Port, server.User, server.PrivateKeyID,
		server.HostKey, server.DataDir, string(proxy), server.ID))
}

func (s *Store) setHostKey(ctx context.Context, id, hostKey string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET host_key = ? WHERE id = ?`, hostKey, id)
	return err
}

func (s *Store) setStatus(ctx context.Context, id string, status Status, message, dockerVersion string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET status = ?, status_message = ?,
		docker_version = CASE WHEN ? = '' THEN docker_version ELSE ? END, last_checked_at = ? WHERE id = ?`,
		status, message, dockerVersion, dockerVersion, sqlite.FormatTime(time.Now()), id)
	return err
}

func (s *Store) delete(ctx context.Context, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM servers WHERE id = ?`, id))
}

// Get loads one server.
func (s *Store) Get(ctx context.Context, id string) (Server, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+serverColumns+` FROM servers WHERE id = ?`, id)
	server, err := scanServer(row)
	return server, sqlite.NotFoundIfNoRows(err)
}

// List loads every server, oldest first.
func (s *Store) List(ctx context.Context) ([]Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+serverColumns+` FROM servers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	servers := []Server{}
	for rows.Next() {
		server, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

func (s *Store) countResources(ctx context.Context, serverID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM applications WHERE server_id = ?1) +
		(SELECT COUNT(*) FROM databases WHERE server_id = ?1) +
		(SELECT COUNT(*) FROM stacks WHERE server_id = ?1) +
		(SELECT COUNT(*) FROM clusters WHERE server_id = ?1)`, serverID).Scan(&count)
	return count, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanServer(row rowScanner) (Server, error) {
	var server Server
	var proxy, createdAt string
	var lastCheckedAt sql.NullString
	err := row.Scan(&server.ID, &server.Name, &server.Description, &server.Kind, &server.Host, &server.Port,
		&server.User, &server.PrivateKeyID, &server.HostKey, &server.DataDir, &proxy, &server.Status,
		&server.StatusMessage, &server.DockerVersion, &lastCheckedAt, &createdAt)
	if err != nil {
		return Server{}, err
	}
	if err := json.Unmarshal([]byte(proxy), &server.Proxy); err != nil {
		return Server{}, fmt.Errorf("decode proxy settings of server %s: %w", server.ID, err)
	}
	server.LastCheckedAt = sqlite.ParseOptionalTime(lastCheckedAt)
	server.CreatedAt = sqlite.ParseTime(createdAt)
	return server, nil
}

func (s *Store) insertKey(ctx context.Context, key PrivateKey, sealedPrivateKey string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO private_keys
		(id, name, description, private_key, public_key, fingerprint, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.Name, key.Description, sealedPrivateKey, key.PublicKey, key.Fingerprint,
		sqlite.FormatTime(key.CreatedAt))
	return err
}

func (s *Store) listKeys(ctx context.Context) ([]PrivateKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, public_key, fingerprint, created_at FROM private_keys ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []PrivateKey{}
	for rows.Next() {
		var key PrivateKey
		var createdAt string
		if err := rows.Scan(&key.ID, &key.Name, &key.Description, &key.PublicKey, &key.Fingerprint, &createdAt); err != nil {
			return nil, err
		}
		key.CreatedAt = sqlite.ParseTime(createdAt)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) sealedKey(ctx context.Context, id string) (string, error) {
	var sealed string
	err := s.db.QueryRowContext(ctx, `SELECT private_key FROM private_keys WHERE id = ?`, id).Scan(&sealed)
	return sealed, sqlite.NotFoundIfNoRows(err)
}

func (s *Store) deleteKey(ctx context.Context, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM private_keys WHERE id = ?`, id))
}

func (s *Store) countServersUsingKey(ctx context.Context, keyID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM servers WHERE private_key_id = ?`, keyID).Scan(&count)
	return count, err
}
