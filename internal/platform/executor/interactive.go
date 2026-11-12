package executor

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// Interactive sessions hold a pseudo-terminal for as long as a person keeps
// it open, so each executor caps them apart from the short commands.
const (
	localMaxInteractive = 8
	// OpenSSH allows 10 channels per connection by default (MaxSessions):
	// 8 are kept for commands (sshMaxSessions), 2 for terminals.
	sshMaxInteractive = 2
	terminalType      = "xterm-256color"
)

// ErrInteractiveLimit reports that the executor already runs as many
// interactive sessions as it allows.
var ErrInteractiveLimit = errors.New("too many interactive sessions on this server")

// ErrInteractiveUnsupported reports an executor without a pseudo-terminal.
var ErrInteractiveUnsupported = errors.New("executor does not support interactive sessions")

// WindowSize is a terminal size in character cells.
type WindowSize struct {
	Cols uint16
	Rows uint16
}

// InteractiveSession is a command attached to a pseudo-terminal. Read
// returns what the terminal prints (stdout and stderr merged), Write sends
// keystrokes. Close ends the command and frees its slot, so callers always
// call it, also after the command exited; it is safe to call more than once.
type InteractiveSession interface {
	io.Reader
	io.Writer
	Resize(size WindowSize) error
	// Wait blocks until the command ends and returns its exit status. A
	// session closed before the command finished returns a non-nil error.
	Wait() (int, error)
	Close() error
}

type interactiveStarter interface {
	StartInteractive(ctx context.Context, command string, size WindowSize) (InteractiveSession, error)
}

// StartInteractive runs the command on a pseudo-terminal of the given size.
// Cancelling ctx ends the session; it must therefore live as long as the
// session, not just the call.
func StartInteractive(ctx context.Context, ex Executor, command string, size WindowSize) (InteractiveSession, error) {
	starter, ok := ex.(interactiveStarter)
	if !ok {
		return nil, ErrInteractiveUnsupported
	}
	return starter.StartInteractive(ctx, command, normalizeSize(size))
}

func normalizeSize(size WindowSize) WindowSize {
	if size.Cols == 0 {
		size.Cols = 80
	}
	if size.Rows == 0 {
		size.Rows = 24
	}
	return size
}

// acquireSlot takes one of limit slots without waiting: a terminal that
// cannot open now is refused, never queued behind sessions that last hours.
func acquireSlot(counter *atomic.Int32, limit int32) (release func(), err error) {
	for {
		current := counter.Load()
		if current >= limit {
			return nil, ErrInteractiveLimit
		}
		if counter.CompareAndSwap(current, current+1) {
			var once sync.Once
			return func() { once.Do(func() { counter.Add(-1) }) }, nil
		}
	}
}
