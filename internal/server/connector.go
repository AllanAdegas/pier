package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/sqlite"
)

// Connector hands out the Executor that reaches a server, reusing SSH connections.
type Connector struct {
	store *Store
	keys  *KeyService
	local *executor.Local

	mu          sync.Mutex
	connections map[string]*executor.SSH
}

// NewConnector creates the connector.
func NewConnector(store *Store, keys *KeyService) *Connector {
	return &Connector{
		store:       store,
		keys:        keys,
		local:       executor.NewLocal(),
		connections: make(map[string]*executor.SSH),
	}
}

// Executor returns the executor and the current state of a server.
func (c *Connector) Executor(ctx context.Context, serverID string) (executor.Executor, Server, error) {
	server, err := c.store.Get(ctx, serverID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return nil, Server{}, apperr.NotFound("Servidor")
		}
		return nil, Server{}, err
	}
	if server.Kind == KindLocal {
		return c.local, server, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if connection, ok := c.connections[serverID]; ok {
		return connection, server, nil
	}
	privateKey, err := c.keys.Material(ctx, server.PrivateKeyID)
	if err != nil {
		return nil, Server{}, err
	}
	connection := executor.NewSSH(executor.SSHTarget{
		Host:         server.Host,
		Port:         server.Port,
		User:         server.User,
		PrivateKey:   privateKey,
		KnownHostKey: server.HostKey,
		// Trust on first use: pin the key the server presents the first time.
		OnHostKey: func(hostKey string) error {
			pinCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return c.store.setHostKey(pinCtx, serverID, hostKey, server.Host, server.Port)
		},
	})
	c.connections[serverID] = connection
	return connection, server, nil
}

// Forget drops the cached connection so the next call reconnects with fresh settings.
func (c *Connector) Forget(serverID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if connection, ok := c.connections[serverID]; ok {
		_ = connection.Close()
		delete(c.connections, serverID)
	}
}

// Close drops every cached connection.
func (c *Connector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, connection := range c.connections {
		_ = connection.Close()
		delete(c.connections, id)
	}
}
