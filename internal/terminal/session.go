package terminal

import (
	"context"
	"errors"
	"sync"
	"time"

	"pier/internal/platform/executor"
)

const (
	outputChunk = 32 << 10
	sendTimeout = 10 * time.Second
)

// Session is an open terminal waiting to be served to a peer.
type Session struct {
	ctx     context.Context
	cancel  context.CancelFunc
	release func()
	grant   Grant
	pty     executor.InteractiveSession
	limits  Limits
	started time.Time
	once    sync.Once
	sendMu  sync.Mutex
	// input is cancelled only by Close: cancelling a WebSocket read tears
	// the connection down, and the last control message must still go out.
	input       context.Context
	cancelInput context.CancelFunc
}

func newSession(ctx context.Context, cancel context.CancelFunc, release func(), grant Grant,
	pty executor.InteractiveSession, limits Limits) *Session {
	input, cancelInput := context.WithCancel(context.WithoutCancel(ctx))
	return &Session{ctx: ctx, cancel: cancel, release: release, grant: grant, pty: pty, limits: limits,
		started: time.Now(), input: input, cancelInput: cancelInput}
}

// Grant tells which container the session runs in.
func (s *Session) Grant() Grant { return s.grant }

// Close ends the shell and frees the slot; safe to call more than once.
func (s *Session) Close() {
	s.once.Do(func() {
		_ = s.pty.Close()
		s.cancel()
		s.cancelInput()
		s.release()
	})
}

// Serve bridges the shell and the peer until one of them ends, the user
// stops typing for IdleTimeout or MaxDuration passes. It ends the shell; the
// caller closes the transport and then the session.
func (s *Session) Serve(peer Peer) Report {
	defer s.pty.Close()
	s.send(peer, controlFrame(serverMessage{Type: serverReady, Container: s.grant.Container.Name,
		Service: s.grant.Container.Service, Shell: s.grant.Shell,
		IdleTimeout: int(s.limits.IdleTimeout.Seconds()), MaxDuration: int(s.limits.MaxDuration.Seconds())}))
	outputEnded := make(chan error, 1)
	go func() { outputEnded <- s.pumpOutput(peer) }()
	inputEnded := make(chan error, 1)
	activity := make(chan struct{}, 1)
	go func() { inputEnded <- s.pumpInput(peer, activity) }()
	report := Report{Grant: s.grant, Started: s.started}
	report.Reason, report.ExitCode = s.watch(outputEnded, inputEnded, activity)
	s.announceEnd(peer, report)
	report.Duration = time.Since(s.started)
	return report
}

// watch waits for whatever ends the terminal and says what it was.
func (s *Session) watch(outputEnded, inputEnded <-chan error, activity <-chan struct{}) (string, *int) {
	idle := time.NewTimer(s.limits.IdleTimeout)
	defer idle.Stop()
	for {
		select {
		case <-activity:
			idle.Reset(s.limits.IdleTimeout)
		case <-idle.C:
			return ReasonIdle, nil
		case <-inputEnded:
			return ReasonClient, nil
		case <-s.ctx.Done():
			return s.contextReason(), nil
		case sendErr := <-outputEnded:
			if s.ctx.Err() != nil {
				return s.contextReason(), nil
			}
			if sendErr != nil {
				return ReasonClient, nil
			}
			code, err := s.pty.Wait()
			if err != nil {
				return ReasonExit, nil
			}
			return ReasonExit, &code
		}
	}
}

func (s *Session) contextReason() string {
	if errors.Is(s.ctx.Err(), context.DeadlineExceeded) {
		return ReasonMaxDuration
	}
	return ReasonShutdown
}

func (s *Session) announceEnd(peer Peer, report Report) {
	switch report.Reason {
	case ReasonClient:
		return
	case ReasonExit:
		s.send(peer, controlFrame(serverMessage{Type: serverExit, Code: report.ExitCode}))
	default:
		s.send(peer, controlFrame(serverMessage{Type: serverClosed, Reason: report.Reason}))
	}
}

// pumpOutput forwards the terminal output. It returns nil when the shell's
// output ends and the send error when the peer stops accepting it.
func (s *Session) pumpOutput(peer Peer) error {
	buffer := make([]byte, outputChunk)
	for {
		n, readErr := s.pty.Read(buffer)
		if n > 0 {
			if err := s.send(peer, Frame{Binary: true, Data: append([]byte(nil), buffer[:n]...)}); err != nil {
				return err
			}
		}
		if readErr != nil {
			return nil
		}
	}
}

// pumpInput forwards keystrokes and resizes; both count as activity. Output
// does not: a terminal left streaming logs is still an abandoned terminal.
func (s *Session) pumpInput(peer Peer, activity chan<- struct{}) error {
	for {
		frame, err := peer.Receive(s.input)
		if err != nil {
			return err
		}
		if len(frame.Data) > MaxFrameBytes {
			continue
		}
		if frame.Binary {
			// A failed write means the shell is gone; its output end reports it.
			_, _ = s.pty.Write(frame.Data)
		} else if size, ok := parseResize(frame.Data); ok {
			_ = s.pty.Resize(size)
		} else {
			continue
		}
		select {
		case activity <- struct{}{}:
		default:
		}
	}
}

func (s *Session) send(peer Peer, frame Frame) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), sendTimeout)
	defer cancel()
	return peer.Send(ctx, frame)
}
