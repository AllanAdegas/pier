package indexnow

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"sync"

	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
	"pier/internal/server"
)

// Proxies reaches the servers whose proxy serves the keys.
type Proxies interface {
	Get(ctx context.Context, id string) (server.Server, error)
	Executor(ctx context.Context, id string) (executor.Executor, server.Server, error)
}

// serverLocks serializes the writes of one server's route file.
type serverLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *serverLocks) of(serverID string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks == nil {
		l.locks = map[string]*sync.Mutex{}
	}
	if l.locks[serverID] == nil {
		l.locks[serverID] = &sync.Mutex{}
	}
	return l.locks[serverID]
}

// reachable tells whether the proxy of every server can forward key
// requests to the panel: the local one always, the others through the
// public URL of the panel.
func (s *Service) reachable(ctx context.Context, serverIDs []string) (bool, error) {
	panelURL := s.publicURL(ctx)
	for _, id := range serverIDs {
		srv, err := s.proxies.Get(ctx, id)
		if err != nil {
			return false, err
		}
		if !(RouteTarget{Local: srv.Kind == server.KindLocal, PanelURL: panelURL}).Reachable() {
			return false, nil
		}
	}
	return true, nil
}

// syncServers rewrites the route file of each server with the sites whose
// resource it publishes. Every server is attempted; the errors are joined.
func (s *Service) syncServers(ctx context.Context, serverIDs []string) error {
	all, err := s.store.All(ctx)
	if err != nil {
		return err
	}
	placed := s.placeSites(ctx, all)
	var failures []error
	for _, id := range serverIDs {
		failures = append(failures, s.syncServer(ctx, id, placed[id]))
	}
	return errors.Join(failures...)
}

// SyncAll rewrites the route file of every server that has sites, for
// changes made while a server was unreachable or to the panel's public URL.
func (s *Service) SyncAll(ctx context.Context) error {
	all, err := s.store.All(ctx)
	if err != nil {
		return err
	}
	placed := s.placeSites(ctx, all)
	var failures []error
	for id, sites := range placed {
		failures = append(failures, s.syncServer(ctx, id, sites))
	}
	return errors.Join(failures...)
}

// placeSites groups the sites by the servers that publish their resource,
// dropping the ones whose resource lost the host.
func (s *Service) placeSites(ctx context.Context, all []Site) map[string][]Site {
	placed := map[string][]Site{}
	for _, site := range all {
		resource, sites, err := s.load(ctx, site.ResourceKind, site.ResourceID)
		if err != nil || !hasSite(sites, site.ID) || !resource.Proxied {
			continue
		}
		for _, id := range resource.ServerIDs {
			placed[id] = append(placed[id], site)
		}
	}
	for id := range placed {
		slices.SortFunc(placed[id], func(a, b Site) int { return strings.Compare(a.Host, b.Host) })
	}
	return placed
}

func (s *Service) syncServer(ctx context.Context, serverID string, sites []Site) error {
	lock := s.locks.of(serverID)
	lock.Lock()
	defer lock.Unlock()
	ex, srv, err := s.proxies.Executor(ctx, serverID)
	if err != nil {
		return err
	}
	target := RouteTarget{Local: srv.Kind == server.KindLocal, PanelURL: s.publicURL(ctx), Edge: srv.Proxy.Edge.Enabled()}
	content, err := RouteFile(sites, target)
	if err != nil {
		return err
	}
	file := path.Join(srv.ProxyDir(), "dynamic", RouteFileName)
	if content == nil {
		_, err = ex.Run(ctx, "rm -f -- "+shell.Quote(file))
		return err
	}
	return ex.WriteFile(ctx, file, content, 0o600)
}
