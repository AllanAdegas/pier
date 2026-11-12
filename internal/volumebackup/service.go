package volumebackup

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"pier/internal/database"
	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

const operationTimeout = 2 * time.Hour

// Resources is what the service needs to know about applications and stacks:
// who they are and whether a deployment or another operation is running.
type Resources interface {
	Resolve(ctx context.Context, kind, id string) (Resource, error)
	// EnsureIdle fails with a conflict while a deploy or operation runs.
	EnsureIdle(ctx context.Context, kind, id string) error
}

// Service implements the volume backup use cases.
type Service struct {
	mu        sync.Mutex
	active    map[string]bool
	store     *Store
	offsite   *database.Offsite
	connector database.Connector
	resources Resources
	events    notify.Publisher
	alerts    AlertObserver
	onChange  func()
}

// NewService creates the service.
func NewService(db *sql.DB, box *secretbox.Box, connector database.Connector, resources Resources,
	events notify.Publisher) *Service {
	return &Service{
		active: map[string]bool{}, store: NewStore(db), offsite: database.NewOffsite(db, box),
		connector: connector, resources: resources, events: events, onChange: func() {},
	}
}

// OnScheduleChange registers the callback fired when a policy changes.
func (s *Service) OnScheduleChange(callback func()) { s.onChange = callback }

func (s *Service) resolve(ctx context.Context, kind, id string) (Resource, error) {
	if !ValidKind(kind) {
		return Resource{}, apperr.NotFound("Recurso")
	}
	return s.resources.Resolve(ctx, kind, id)
}

func (s *Service) reach(ctx context.Context, serverID string) (executor.Executor, server.Server, error) {
	return s.connector.Executor(ctx, serverID)
}

// Policy returns the backup policy of a resource (defaults when never saved).
func (s *Service) Policy(ctx context.Context, kind, id string) (Policy, error) {
	if _, err := s.resolve(ctx, kind, id); err != nil {
		return Policy{}, err
	}
	return s.store.policy(ctx, kind, id)
}

// SetPolicy validates and saves the policy of a resource.
func (s *Service) SetPolicy(ctx context.Context, kind, id string, policy Policy) (Policy, error) {
	if _, err := s.resolve(ctx, kind, id); err != nil {
		return Policy{}, err
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	if err := s.store.setPolicy(ctx, kind, id, policy); err != nil {
		return Policy{}, err
	}
	s.onChange()
	return policy, nil
}

// Mounts lists the persistent mounts of the resource and which of them the
// archive includes under the current policy.
func (s *Service) Mounts(ctx context.Context, kind, id string) ([]Mount, error) {
	resource, err := s.resolve(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	policy, err := s.store.policy(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	ex, target, err := s.reach(ctx, resource.ServerID)
	if err != nil {
		return nil, err
	}
	mounts, _, err := discoverMounts(ctx, ex, id, target.DataDir, policy)
	if err != nil {
		return nil, apperr.Unavailable("Não foi possível listar os volumes: %s", err)
	}
	return mounts, nil
}

// List returns the archives of a resource, newest first.
func (s *Service) List(ctx context.Context, kind, id string) ([]Backup, error) {
	if _, err := s.resolve(ctx, kind, id); err != nil {
		return nil, err
	}
	return s.store.listBackups(ctx, kind, id)
}

// Operations returns the latest restores of a resource.
func (s *Service) Operations(ctx context.Context, kind, id string) ([]Operation, error) {
	if _, err := s.resolve(ctx, kind, id); err != nil {
		return nil, err
	}
	return s.store.operations(ctx, kind, id)
}

func notFound(err error) error {
	if err == sql.ErrNoRows || err == sqlite.ErrNotFound {
		return apperr.NotFound("Backup")
	}
	return err
}
