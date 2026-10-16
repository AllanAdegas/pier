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
	"time"

	"golang.org/x/crypto/ssh"

	"pier/internal/platform/shell"
)

const sshDialTimeout = 10 * time.Second

// ErrHostKeyMismatch means the server presented a different key than the one
// pinned on first contact — a possible man-in-the-middle.
var ErrHostKeyMismatch = errors.New("ssh host key does not match the pinned key")

// SSHTarget describes how to reach a remote machine.
type SSHTarget struct {
	Host       string
	Port       int
	User       string
	PrivateKey string
	// KnownHostKey is the pinned key in authorized_keys format. Empty means
	// trust on first use: the key seen is reported through OnHostKey.
	KnownHostKey string
	OnHostKey    func(authorizedKey string)
}

// SSH runs commands on a remote machine over a persistent connection.
type SSH struct {
	target SSHTarget
	mu     sync.Mutex
	client *ssh.Client
}

// NewSSH creates an executor; the connection is opened lazily.
func NewSSH(target SSHTarget) *SSH { return &SSH{target: target} }

// Close drops the underlying connection.
func (s *SSH) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil
	}
	err := s.client.Close()
	s.client = nil
	return err
}

// Run implements Executor.
func (s *SSH) Run(ctx context.Context, command string) (string, error) {
	return capture(ctx, s, command)
}

// Stream implements Executor.
func (s *SSH) Stream(ctx context.Context, command string, sink LineSink) error {
	session, err := s.newSession(ctx)
	if err != nil {
		return err
	}
	defer session.Close()

	reader, writer := io.Pipe()
	session.Stdout = writer
	session.Stderr = writer
	pumped := make(chan struct{})
	go func() {
		pumpLines(reader, sink)
		close(pumped)
	}()

	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		<-done
		runErr = ctx.Err()
	}
	_ = writer.Close()
	<-pumped
	return translateSSHError(command, runErr)
}

// WriteFile implements Executor.
func (s *SSH) WriteFile(ctx context.Context, filePath string, content []byte, mode os.FileMode) error {
	session, err := s.newSession(ctx)
	if err != nil {
		return err
	}
	defer session.Close()

	octal := "0" + strconv.FormatUint(uint64(mode.Perm()), 8)
	command := fmt.Sprintf("mkdir -p %s && umask 077 && cat > %s && chmod %s %s",
		shell.Quote(path.Dir(filePath)), shell.Quote(filePath), octal, shell.Quote(filePath))
	session.Stdin = bytes.NewReader(content)
	var stderr bytes.Buffer
	session.Stderr = &stderr
	if err := session.Run(command); err != nil {
		return fmt.Errorf("write %s over ssh: %w: %s", filePath, err, stderr.String())
	}
	return nil
}

// ReadFile implements Executor.
func (s *SSH) ReadFile(ctx context.Context, filePath string) (io.ReadCloser, error) {
	session, err := s.newSession(ctx)
	if err != nil {
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("open stdout pipe: %w", err)
	}
	if err := session.Start("cat " + shell.Quote(filePath)); err != nil {
		session.Close()
		return nil, fmt.Errorf("read %s over ssh: %w", filePath, err)
	}
	return &sessionReader{Reader: stdout, session: session}, nil
}

type sessionReader struct {
	io.Reader
	session *ssh.Session
}

func (r *sessionReader) Close() error { return r.session.Close() }

func (s *SSH) newSession(ctx context.Context) (*ssh.Session, error) {
	client, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	session, err := client.NewSession()
	if err == nil {
		return session, nil
	}
	// The cached connection died (idle timeout, reboot): reconnect once.
	_ = s.Close()
	client, err = s.connect(ctx)
	if err != nil {
		return nil, err
	}
	session, err = client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open ssh session: %w", err)
	}
	return session, nil
}

func (s *SSH) connect(ctx context.Context) (*ssh.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	signer, err := ssh.ParsePrivateKey([]byte(s.target.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	clientConfig := &ssh.ClientConfig{
		User:            s.target.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: s.verifyHostKey,
		Timeout:         sshDialTimeout,
	}
	address := net.JoinHostPort(s.target.Host, strconv.Itoa(s.target.Port))
	dialer := net.Dialer{Timeout: sshDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", address, err)
	}
	sshConn, channels, requests, err := ssh.NewClientConn(conn, address, clientConfig)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh handshake with %s: %w", address, err)
	}
	s.client = ssh.NewClient(sshConn, channels, requests)
	return s.client, nil
}

func (s *SSH) verifyHostKey(_ string, _ net.Addr, key ssh.PublicKey) error {
	presented := string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)))
	if s.target.KnownHostKey == "" {
		if s.target.OnHostKey != nil {
			s.target.OnHostKey(presented)
		}
		s.target.KnownHostKey = presented
		return nil
	}
	if presented != s.target.KnownHostKey {
		return ErrHostKeyMismatch
	}
	return nil
}

func translateSSHError(command string, err error) error {
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return &ExitError{Command: command, Code: exitErr.ExitStatus()}
	}
	return err
}
