package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"pier/internal/platform/shell"
)

const sshDialTimeout = 10 * time.Second
const sshMaxSessions = 8
const sshKeepaliveInterval = 30 * time.Second

// ErrHostKeyMismatch indicates a possible man-in-the-middle.
var ErrHostKeyMismatch = errors.New("ssh host key does not match the pinned key")

// SSHTarget describes how to reach a remote machine. OnHostKey runs only
// after authentication succeeds; an error prevents use of the connection.
type SSHTarget struct {
	Host         string
	Port         int
	User         string
	PrivateKey   string
	KnownHostKey string
	OnHostKey    func(authorizedKey string) error
}

// SSH shares one transport and queues channel requests below MaxSessions.
type SSH struct {
	target     SSHTarget
	mu         sync.Mutex
	client     *ssh.Client
	closed     bool
	dialCancel context.CancelFunc
	dialGate   chan struct{}
	sessions   chan struct{}
	// interactive counts terminals, which do not take command slots.
	interactive atomic.Int32
}

func NewSSH(target SSHTarget) *SSH {
	return &SSH{target: target, dialGate: make(chan struct{}, 1), sessions: make(chan struct{}, sshMaxSessions)}
}

func (s *SSH) Close() error {
	s.mu.Lock()
	client := s.client
	s.closed = true
	cancel := s.dialCancel
	s.client = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var err error
	if client != nil {
		err = client.Close()
	}
	// Forget/Reset must finish any pending pin write before returning.
	s.dialGate <- struct{}{}
	<-s.dialGate
	return err
}

func (s *SSH) discard(client *ssh.Client) {
	s.mu.Lock()
	if s.client == client {
		s.client = nil
		_ = client.Close()
	}
	s.mu.Unlock()
}

func (s *SSH) Run(ctx context.Context, command string) (string, error) {
	return capture(ctx, s, command)
}
func (s *SSH) Stream(ctx context.Context, command string, sink LineSink) error {
	return s.streamInput(ctx, command, nil, sink)
}
func (s *SSH) RunInput(ctx context.Context, command string, content []byte) (string, error) {
	session, err := s.newSession(ctx)
	if err != nil {
		return "", err
	}
	defer session.Close()
	var output limitedBuffer
	session.Stdin = bytes.NewReader(content)
	session.Stdout, session.Stderr = &output, &output
	err = translateSSHError(command, sessionCall(ctx, session, func() error { return session.Run(command) }))
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if exitErr, ok := err.(*ExitError); ok {
		exitErr.Output = output.String()
	}
	if err == nil && output.truncated {
		err = ErrFileTooLarge
	}
	return output.String(), err
}

func (s *SSH) streamInput(ctx context.Context, command string, content []byte, sink LineSink) error {
	session, err := s.newSession(ctx)
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = bytes.NewReader(content)
	reader, writer := io.Pipe()
	session.Stdout, session.Stderr = writer, writer
	pumped := make(chan struct{})
	go func() { pumpLines(reader, sink); close(pumped) }()
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		session.cancel()
		runErr = ctx.Err()
	}
	_ = writer.Close()
	<-pumped
	return translateSSHError(command, runErr)
}

// WriteFile atomically replaces the target, never following its symlink.
func (s *SSH) WriteFile(ctx context.Context, filePath string, content []byte, mode os.FileMode) error {
	return s.writeFile(ctx, filePath, content, mode, false)
}
func (s *SSH) WriteFileExclusive(ctx context.Context, filePath string, content []byte, mode os.FileMode) error {
	return s.writeFile(ctx, filePath, content, mode, true)
}
func (s *SSH) writeFile(ctx context.Context, filePath string, content []byte, mode os.FileMode, exclusive bool) error {
	destination := shell.Quote(filePath)
	finish := "mv -fT -- \"$tmp\" " + destination
	if exclusive {
		finish = "ln -T -- \"$tmp\" " + destination
	}
	// The temporary file is created by mktemp, not a predictable pathname.
	command := "umask 077; mkdir -p -- " + shell.Quote(path.Dir(filePath)) +
		" && test ! -L " + destination + " && { test ! -e " + destination + " || test -f " + destination + "; }" +
		" && tmp=$(mktemp " + shell.Quote(path.Join(path.Dir(filePath), ".pier-write-XXXXXX")) + ") || exit 1; " +
		"trap 'rm -f -- \"$tmp\"' EXIT; cat > \"$tmp\" && chmod " + strconv.FormatUint(uint64(mode.Perm()), 8) +
		" \"$tmp\" && " + finish
	_, err := s.RunInput(ctx, command, content)
	return err
}
func (s *SSH) ReadFile(ctx context.Context, filePath string) (io.ReadCloser, error) {
	session, err := s.newSession(ctx)
	if err != nil {
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	command := "test ! -L " + shell.Quote(filePath) + " && test -f " + shell.Quote(filePath) +
		" && head -c " + strconv.Itoa(MaxFileBytes+1) + " -- " + shell.Quote(filePath)
	if err := sessionCall(ctx, session, func() error { return session.Start(command) }); err != nil {
		session.Close()
		return nil, err
	}
	reader, writer := io.Pipe()
	go func() {
		_, copyErr := io.Copy(writer, stdout)
		waitErr := session.Wait()
		_ = writer.CloseWithError(errors.Join(copyErr, waitErr))
	}()
	return newFileReader(ctx, &sessionReader{PipeReader: reader, session: session}, MaxFileBytes), nil
}

type sessionReader struct {
	*io.PipeReader
	session *sshSession
}

func (r *sessionReader) Close() error {
	_ = r.PipeReader.Close()
	return r.session.Close()
}

// A stalled SSH peer must not hold a caller past its context deadline.
func sessionCall(ctx context.Context, session *sshSession, call func() error) error {
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		session.cancel()
		return ctx.Err()
	}
}

type sshSession struct {
	*ssh.Session
	once    sync.Once
	stop    func() bool
	release func()
}

func (s *sshSession) abort()  { s.finish(false) }
func (s *sshSession) cancel() { s.finish(true) }
func (s *sshSession) finish(kill bool) {
	s.once.Do(func() {
		go func() {
			if kill {
				_ = s.Session.Signal(ssh.SIGKILL)
			}
			_ = s.Session.Close()
			s.release()
		}()
	})
}
func (s *sshSession) Close() error {
	if s.stop != nil {
		s.stop()
	}
	s.abort()
	return nil
}

func (s *SSH) newSession(ctx context.Context) (*sshSession, error) {
	select {
	case s.sessions <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.openChannel(ctx, func() { <-s.sessions })
}

// openChannel opens a session channel on the shared transport. release frees
// the caller's slot once the channel is gone.
func (s *SSH) openChannel(ctx context.Context, release func()) (*sshSession, error) {
	for attempt := 0; attempt < 2; attempt++ {
		client, err := s.connect(ctx)
		if err != nil {
			release()
			return nil, err
		}
		type result struct {
			session *ssh.Session
			err     error
		}
		done := make(chan result, 1)
		go func() {
			session, err := client.NewSession()
			done <- result{session, err}
		}()
		var opened result
		select {
		case opened = <-done:
		case <-ctx.Done():
			// Keep the slot until the in-flight channel open completes. Closing the
			// shared client here would interrupt unrelated commands.
			go func() {
				opened := <-done
				if opened.session != nil {
					opened.session.Close()
				}
				release()
			}()
			return nil, ctx.Err()
		}
		if opened.err == nil {
			session := &sshSession{Session: opened.session, release: release}
			// Cancel closes this channel only, not the shared transport.
			session.stop = context.AfterFunc(ctx, session.cancel)
			if ctx.Err() != nil {
				session.Close()
				return nil, ctx.Err()
			}
			return session, nil
		}
		var rejected *ssh.OpenChannelError
		if errors.As(opened.err, &rejected) {
			release()
			return nil, fmt.Errorf("open ssh session: %w", opened.err)
		}
		s.discard(client)
		if attempt == 1 {
			release()
			return nil, fmt.Errorf("open ssh session: %w", opened.err)
		}
	}
	panic("unreachable")
}

func (s *SSH) connect(ctx context.Context) (*ssh.Client, error) {
	select {
	case s.dialGate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.dialGate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	client := s.client
	closed := s.closed
	s.dialCancel = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.dialCancel = nil; s.mu.Unlock() }()
	if closed {
		return nil, net.ErrClosed
	}
	if client != nil {
		return client, nil
	}
	signer, err := ssh.ParsePrivateKey([]byte(s.target.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	var presented string
	config := &ssh.ClientConfig{
		User: s.target.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, Timeout: sshDialTimeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			presented = string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)))
			if s.target.KnownHostKey != "" && presented != s.target.KnownHostKey {
				return ErrHostKeyMismatch
			}
			return nil
		},
	}
	address := net.JoinHostPort(s.target.Host, strconv.Itoa(s.target.Port))
	conn, err := (&net.Dialer{Timeout: sshDialTimeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(sshDialTimeout))
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	sshConn, channels, requests, err := ssh.NewClientConn(conn, address, config)
	if err != nil || ctx.Err() != nil {
		stop()
		conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ssh handshake with %s: %w", address, err)
	}
	if s.target.KnownHostKey == "" {
		if s.target.OnHostKey != nil {
			if err := s.target.OnHostKey(presented); err != nil {
				stop()
				sshConn.Close()
				return nil, fmt.Errorf("persist ssh host key: %w", err)
			}
		}
		s.target.KnownHostKey = presented
	}
	if !stop() || ctx.Err() != nil {
		sshConn.Close()
		return nil, ctx.Err()
	}
	_ = conn.SetDeadline(time.Time{})
	client = ssh.NewClient(sshConn, channels, requests)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		client.Close()
		return nil, net.ErrClosed
	}
	s.client = client
	s.mu.Unlock()
	go s.keepalive(client)
	return client, nil
}
func (s *SSH) probe(client *ssh.Client) {
	done := make(chan struct{})
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		if err != nil {
			s.discard(client)
		}
		close(done)
	}()
	timer := time.NewTimer(sshDialTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		s.discard(client)
	}
}
func (s *SSH) keepalive(client *ssh.Client) {
	done := make(chan struct{})
	go func() { _ = client.Wait(); close(done) }()
	ticker := time.NewTicker(sshKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			s.probe(client)
		}
	}
}
func translateSSHError(command string, err error) error {
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return &ExitError{Command: command, Code: exitErr.ExitStatus()}
	}
	return err
}
