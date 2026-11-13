package executor

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// terminalSpeed is the line speed advertised for the remote terminal; it
// only matters to programs that still look at it.
const terminalSpeed = 14400

// StartInteractive runs the command on a remote pseudo-terminal over the
// shared connection. A failure closes only this channel, never the transport.
func (s *SSH) StartInteractive(ctx context.Context, command string, size WindowSize) (InteractiveSession, error) {
	release, err := acquireSlot(&s.interactive, sshMaxInteractive)
	if err != nil {
		return nil, err
	}
	session, err := s.openChannel(ctx, release)
	if err != nil {
		return nil, err
	}
	stdin, stdout, err := s.attachTerminal(ctx, session, command, size)
	if err != nil {
		session.Close()
		return nil, err
	}
	return &sshTerminal{session: session, stdin: stdin, stdout: stdout}, nil
}

func (s *SSH) attachTerminal(ctx context.Context, session *sshSession, command string, size WindowSize) (io.WriteCloser, io.Reader, error) {
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: terminalSpeed, ssh.TTY_OP_OSPEED: terminalSpeed}
	err := sessionCall(ctx, session, func() error {
		return session.RequestPty(terminalType, int(size.Rows), int(size.Cols), modes)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("request terminal: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := sessionCall(ctx, session, func() error { return session.Start(command) }); err != nil {
		return nil, nil, fmt.Errorf("start terminal: %w", err)
	}
	return stdin, stdout, nil
}

type sshTerminal struct {
	session *sshSession
	stdin   io.WriteCloser
	stdout  io.Reader
}

func (t *sshTerminal) Read(p []byte) (int, error)  { return t.stdout.Read(p) }
func (t *sshTerminal) Write(p []byte) (int, error) { return t.stdin.Write(p) }

func (t *sshTerminal) Resize(size WindowSize) error {
	size = normalizeSize(size)
	return t.session.WindowChange(int(size.Rows), int(size.Cols))
}

func (t *sshTerminal) Wait() (int, error) {
	err := t.session.Wait()
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.Signal() != "" {
			return -1, fmt.Errorf("terminal ended by signal %s", exitErr.Signal())
		}
		return exitErr.ExitStatus(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// Close closes the channel; sshd then hangs up the remote terminal, which
// ends `docker exec` like a closed window does.
func (t *sshTerminal) Close() error {
	_ = t.stdin.Close()
	return t.session.Close()
}
