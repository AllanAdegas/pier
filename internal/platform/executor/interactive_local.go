package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// hangupGrace is how long a hung-up terminal may take to exit before it is killed.
const hangupGrace = 2 * time.Second

// StartInteractive runs the command on a local pseudo-terminal.
func (l *Local) StartInteractive(ctx context.Context, command string, size WindowSize) (InteractiveSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release, err := acquireSlot(&l.interactive, localMaxInteractive)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "TERM="+terminalType)
	// pty.StartWithSize starts the command in its own session (and so its own
	// process group), which lets Close signal every process it spawned.
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: size.Cols, Rows: size.Rows})
	if err != nil {
		release()
		return nil, fmt.Errorf("start terminal: %w", err)
	}
	session := &localSession{cmd: cmd, terminal: terminal, release: release, exited: make(chan struct{})}
	go session.reap()
	session.stopMu.Lock()
	session.stop = context.AfterFunc(ctx, func() { _ = session.Close() })
	session.stopMu.Unlock()
	return session, nil
}

type localSession struct {
	cmd      *exec.Cmd
	terminal *os.File
	release  func()
	stop     func() bool
	stopMu   sync.Mutex
	exited   chan struct{}
	waitErr  error
	once     sync.Once
}

func (s *localSession) reap() {
	s.waitErr = s.cmd.Wait()
	close(s.exited)
}

// Read returns the terminal output. Linux reports EIO once the last process
// holding the terminal exits; that is the end of the output, not a failure.
func (s *localSession) Read(p []byte) (int, error) {
	n, err := s.terminal.Read(p)
	if err != nil && errors.Is(err, syscall.EIO) {
		return n, io.EOF
	}
	return n, err
}

func (s *localSession) Write(p []byte) (int, error) { return s.terminal.Write(p) }

func (s *localSession) Resize(size WindowSize) error {
	size = normalizeSize(size)
	return pty.Setsize(s.terminal, &pty.Winsize{Cols: size.Cols, Rows: size.Rows})
}

func (s *localSession) Wait() (int, error) {
	<-s.exited
	var exitErr *exec.ExitError
	if errors.As(s.waitErr, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return -1, fmt.Errorf("terminal ended by signal %s", status.Signal())
		}
		return exitErr.ExitCode(), nil
	}
	if s.waitErr != nil {
		return -1, s.waitErr
	}
	return 0, nil
}

// Close hangs up the terminal like a closed window does, then kills what is
// left of the process group after a grace period.
func (s *localSession) Close() error {
	s.once.Do(func() {
		s.stopMu.Lock()
		if s.stop != nil {
			s.stop()
		}
		s.stopMu.Unlock()
		_ = s.terminal.Close()
		group := -s.cmd.Process.Pid
		select {
		case <-s.exited:
			// Reaped already: the group id may belong to someone else now.
			s.release()
			return
		default:
		}
		_ = syscall.Kill(group, syscall.SIGHUP)
		go func() {
			timer := time.NewTimer(hangupGrace)
			defer timer.Stop()
			select {
			case <-s.exited:
			case <-timer.C:
				_ = syscall.Kill(group, syscall.SIGKILL)
				<-s.exited
			}
			s.release()
		}()
	})
	return nil
}
