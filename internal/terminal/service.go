package terminal

import (
	"context"
	"errors"
	"sync"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/server"
)

// Connector reaches a server.
type Connector interface {
	Executor(ctx context.Context, serverID string) (executor.Executor, server.Server, error)
}

// Service issues terminal tickets and opens the terminals they grant.
type Service struct {
	connector Connector
	limits    Limits
	tickets   *tickets

	mu        sync.Mutex
	perUser   map[string]int
	perServer map[string]int
	sessions  map[*Session]struct{}
	closed    bool
}

// NewService creates the service with the default limits.
func NewService(connector Connector) *Service {
	return NewServiceWithLimits(connector, DefaultLimits())
}

// NewServiceWithLimits creates the service with explicit limits (tests).
func NewServiceWithLimits(connector Connector, limits Limits) *Service {
	return &Service{connector: connector, limits: limits, tickets: newTickets(limits.TicketTTL),
		perUser: map[string]int{}, perServer: map[string]int{}, sessions: map[*Session]struct{}{}}
}

// Issue checks the request, finds its container and hands out a ticket the
// browser redeems when it opens the WebSocket.
func (s *Service) Issue(ctx context.Context, owner Owner, request Request) (Ticket, error) {
	if err := request.validate(); err != nil {
		return Ticket{}, err
	}
	if err := s.checkCapacity(owner.UserID, request.ServerID); err != nil {
		return Ticket{}, err
	}
	ex, _, err := s.connector.Executor(ctx, request.ServerID)
	if err != nil {
		return Ticket{}, err
	}
	container, err := resolve(ctx, ex, request)
	if err != nil {
		return Ticket{}, err
	}
	grant := Grant{Kind: request.Kind, ServerID: request.ServerID, ResourceID: request.ResourceID,
		Container: container, Shell: request.Shell}
	token, err := s.tickets.issue(owner, grant)
	if err != nil {
		return Ticket{}, err
	}
	return Ticket{Ticket: token, ExpiresIn: int(s.limits.TicketTTL.Seconds()), Container: container, Shell: request.Shell}, nil
}

// Open redeems a ticket, takes a slot and starts the shell. ctx must live as
// long as the terminal: the session ends when it is cancelled. The caller
// must Close the returned session or Serve it.
func (s *Service) Open(ctx context.Context, owner Owner, token string, size executor.WindowSize) (*Session, error) {
	grant, err := s.tickets.redeem(owner, token)
	if err != nil {
		return nil, err
	}
	release, err := s.reserve(owner.UserID, grant.ServerID)
	if err != nil {
		return nil, err
	}
	ex, _, err := s.connector.Executor(ctx, grant.ServerID)
	if err != nil {
		release()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.limits.MaxDuration)
	pty, err := executor.StartInteractive(ctx, ex, execCommand(grant.Container.ID, grant.Shell), clampSize(size))
	if err != nil {
		cancel()
		release()
		return nil, startError(err)
	}
	var session *Session
	session = newSession(ctx, cancel, func() {
		s.mu.Lock()
		delete(s.sessions, session)
		s.mu.Unlock()
		release()
	}, grant, pty, s.limits)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		session.Close()
		return nil, context.Canceled
	}
	s.sessions[session] = struct{}{}
	s.mu.Unlock()
	return session, nil
}

func startError(err error) error {
	if errors.Is(err, executor.ErrInteractiveLimit) {
		return apperr.Conflict("O servidor já tem o máximo de terminais abertos. Feche um deles e tente de novo.")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return apperr.Unavailable("Não foi possível abrir o terminal: %s", lastLine(err.Error()))
}

func (s *Service) checkCapacity(userID, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capacityLocked(userID, serverID)
}

func (s *Service) capacityLocked(userID, serverID string) error {
	if s.closed {
		return context.Canceled
	}
	if s.perUser[userID] >= s.limits.PerUser {
		return apperr.Conflict("Você já tem %d terminais abertos. Feche um deles e tente de novo.", s.limits.PerUser)
	}
	if s.perServer[serverID] >= s.limits.PerServer {
		return apperr.Conflict("Este servidor já tem %d terminais abertos. Feche um deles e tente de novo.", s.limits.PerServer)
	}
	return nil
}

// reserve takes a slot for the user and the server; release gives it back once.
func (s *Service) reserve(userID, serverID string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.capacityLocked(userID, serverID); err != nil {
		return nil, err
	}
	s.perUser[userID]++
	s.perServer[serverID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			decrement(s.perUser, userID)
			decrement(s.perServer, serverID)
		})
	}, nil
}

func decrement(counts map[string]int, key string) {
	if counts[key] <= 1 {
		delete(counts, key)
		return
	}
	counts[key]--
}

// OpenCount counts the terminals open now, for tests and diagnostics.
func (s *Service) OpenCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, count := range s.perServer {
		total += count
	}
	return total
}

func clampSize(size executor.WindowSize) executor.WindowSize {
	size.Cols = min(size.Cols, maxCols)
	size.Rows = min(size.Rows, maxRows)
	return size
}

// Close ends all sessions before the panel shuts down. No new terminal may open.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	sessions := make([]*Session, 0, len(s.sessions))
	for session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.Unlock()
	for _, session := range sessions {
		session.Close()
	}
}
