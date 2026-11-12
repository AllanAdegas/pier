package executor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshFixture is an in-process SSH peer; no Docker or real server is involved.
func sshFixture(t *testing.T, authenticate bool, maxSessions int, pause ...<-chan struct{}) (SSHTarget, *atomic.Int32) {
	t.Helper()
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(private)
	encoded, _ := ssh.MarshalPrivateKey(private, "test")
	config := &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
		if !authenticate {
			return nil, errors.New("authentication refused")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var connections []net.Conn
	var opened atomic.Int32
	t.Cleanup(func() {
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range connections {
			conn.Close()
		}
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			go func() {
				peer, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					conn.Close()
					return
				}
				defer peer.Close()
				go func() {
					for req := range requests {
						_ = req.Reply(true, nil)
					}
				}()
				var active atomic.Int32
				for incoming := range channels {
					if len(pause) > 0 {
						<-pause[0]
					}
					if active.Load() >= int32(maxSessions) {
						_ = incoming.Reject(ssh.ResourceShortage, "MaxSessions")
						continue
					}
					channel, requests, err := incoming.Accept()
					if err != nil {
						continue
					}
					opened.Add(1)
					active.Add(1)
					go func() {
						defer active.Add(-1)
						defer channel.Close()
						for req := range requests {
							if req.Type == "exec" {
								_ = req.Reply(true, nil)
								var request struct{ Command string }
								_ = ssh.Unmarshal(req.Payload, &request)
								if request.Command == "stall" || strings.Contains(request.Command, "/stall") {
									continue
								}
								if request.Command == "large" {
									_, _ = io.WriteString(channel, strings.Repeat("x", maxCapturedSize+1))
								} else if request.Command == "cat" {
									_, _ = io.Copy(channel, channel)
								} else {
									_, _ = io.WriteString(channel, "ok\n")
								}
								_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
								return
							}
							_ = req.Reply(true, nil)
						}
					}()
				}
			}()
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return SSHTarget{Host: host, Port: port, User: "test", PrivateKey: string(pem.EncodeToMemory(encoded))}, &opened
}

func TestHostKeyIsPersistedOnlyAfterAuthentication(t *testing.T) {
	for _, authenticate := range []bool{false, true} {
		t.Run(fmt.Sprint(authenticate), func(t *testing.T) {
			target, _ := sshFixture(t, authenticate, 10)
			calls := 0
			target.OnHostKey = func(key string) error {
				calls++
				if !strings.HasPrefix(key, "ssh-ed25519 ") {
					t.Error("invalid key")
				}
				return nil
			}
			ex := NewSSH(target)
			defer ex.Close()
			_, err := ex.Run(context.Background(), "true")
			if authenticate && (err != nil || calls != 1) {
				t.Fatalf("authenticated: calls=%d err=%v", calls, err)
			}
			if !authenticate && (err == nil || calls != 0 || ex.target.KnownHostKey != "") {
				t.Fatalf("unauthenticated peer pinned: calls=%d err=%v", calls, err)
			}
		})
	}
}
func TestPinPersistenceFailureRefusesConnection(t *testing.T) {
	target, opened := sshFixture(t, true, 10)
	denied := errors.New("pin write denied")
	target.OnHostKey = func(string) error { return denied }
	ex := NewSSH(target)
	defer ex.Close()
	if _, err := ex.Run(context.Background(), "true"); !errors.Is(err, denied) {
		t.Fatalf("err=%v", err)
	}
	if opened.Load() != 0 || ex.target.KnownHostKey != "" || ex.client != nil {
		t.Fatal("failed pin must not publish a client or open a session")
	}
}
func TestChannelRejectionDoesNotCloseSharedTransport(t *testing.T) {
	target, _ := sshFixture(t, true, 1)
	ex := NewSSH(target)
	defer ex.Close()
	first, err := ex.newSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := ex.newSession(context.Background()); err == nil {
		t.Fatal("expected channel rejection")
	}
	if err := first.Run("true"); err != nil {
		t.Fatalf("existing session was interrupted: %v", err)
	}
}
func TestSessionSemaphoreQueuesAndHonorsCancellation(t *testing.T) {
	target, opened := sshFixture(t, true, 10)
	ex := NewSSH(target)
	defer ex.Close()
	for range sshMaxSessions {
		session, err := ex.newSession(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := ex.newSession(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if opened.Load() != sshMaxSessions {
		t.Fatalf("opened=%d", opened.Load())
	}
}
func TestStaleFailureCannotCloseNewClient(t *testing.T) {
	target, _ := sshFixture(t, true, 10)
	ex := NewSSH(target)
	defer ex.Close()
	old, err := ex.connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	replacement := NewSSH(target)
	defer replacement.Close()
	current, err := replacement.connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ex.mu.Lock()
	ex.client = current
	ex.mu.Unlock()
	ex.discard(old)
	if _, err := ex.Run(context.Background(), "true"); err != nil {
		t.Fatalf("replacement connection closed: %v", err)
	}
}
func TestSessionCancellationLeavesOtherSessionsAlive(t *testing.T) {
	target, _ := sshFixture(t, true, 10)
	ex := NewSSH(target)
	defer ex.Close()
	ctx, cancel := context.WithCancel(context.Background())
	canceled, err := ex.newSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer canceled.Close()
	other, err := ex.newSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	cancel()
	if err := other.Run("true"); err != nil {
		t.Fatalf("unrelated session canceled: %v", err)
	}
}

func TestSSHRunInputPreservesStdinBytes(t *testing.T) {
	target, _ := sshFixture(t, true, 10)
	ex := NewSSH(target)
	defer ex.Close()
	content := "private configuration\n\n"
	output, err := ex.RunInput(context.Background(), "cat", []byte(content))
	if err != nil || output != content {
		t.Fatalf("output=%q err=%v", output, err)
	}
}
func TestTransportFailureReconnectsWithPinnedKey(t *testing.T) {
	target, _ := sshFixture(t, true, 10)
	pins := 0
	target.OnHostKey = func(string) error { pins++; return nil }
	ex := NewSSH(target)
	defer ex.Close()
	client, err := ex.connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if _, err := ex.Run(context.Background(), "true"); err != nil {
		t.Fatal(err)
	}
	if pins != 1 {
		t.Fatalf("pin called %d times", pins)
	}
}
func TestClosedExecutorCannotReconnect(t *testing.T) {
	target, opened := sshFixture(t, true, 10)
	ex := NewSSH(target)
	if err := ex.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.Run(context.Background(), "true"); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("err=%v", err)
	}
	if opened.Load() != 0 {
		t.Fatal("forgotten executor opened a stale session")
	}
}
func TestPinnedKeyMismatchDoesNotCallPersistence(t *testing.T) {
	target, _ := sshFixture(t, true, 10)
	target.KnownHostKey = "different key"
	target.OnHostKey = func(string) error { t.Error("mismatched key persisted"); return nil }
	ex := NewSSH(target)
	defer ex.Close()
	if _, err := ex.Run(context.Background(), "true"); !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestNewSessionHonorsContextDuringChannelOpen(t *testing.T) {
	release := make(chan struct{})
	target, _ := sshFixture(t, true, 10, release)
	ex := NewSSH(target)
	defer ex.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := ex.newSession(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("channel open ignored context")
	}
}
func TestSSHReadAndRunInputHonorCancellation(t *testing.T) {
	target, _ := sshFixture(t, true, 10)
	ex := NewSSH(target)
	defer ex.Close()
	ctx, cancel := context.WithCancel(context.Background())
	reader, err := ex.ReadFile(ctx, "/stall")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(reader); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("read err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("read ignored cancellation")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := ex.RunInput(ctx, "stall", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run err=%v", err)
	}
}
func TestSSHInputOutputCaptureCannotBypassLimitWithReaderFrom(t *testing.T) {
	target, _ := sshFixture(t, true, 10)
	ex := NewSSH(target)
	defer ex.Close()
	output, err := ex.RunInput(context.Background(), "large", nil)
	if !errors.Is(err, ErrFileTooLarge) || len(output) != maxCapturedSize {
		t.Fatalf("length=%d err=%v", len(output), err)
	}
}
