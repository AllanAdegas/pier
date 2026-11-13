package executor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// terminalRecord is what an in-process SSH peer saw of the terminal requests.
type terminalRecord struct {
	mu      sync.Mutex
	ptys    []string
	resizes []string
}

func (r *terminalRecord) add(list *[]string, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*list = append(*list, value)
}

func (r *terminalRecord) snapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ptys...), append([]string(nil), r.resizes...)
}

// terminalPeer is an in-process SSH server whose exec echoes the channel
// input; typing "q" ends the command with status 7. refusePty rejects pty-req.
func terminalPeer(t *testing.T, refusePty bool) (SSHTarget, *terminalRecord) {
	t.Helper()
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(private)
	encoded, _ := ssh.MarshalPrivateKey(private, "test")
	config := &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	record := &terminalRecord{}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { conn.Close() })
			go serveTerminalPeer(conn, config, record, refusePty)
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return SSHTarget{Host: host, Port: port, User: "test", PrivateKey: string(pem.EncodeToMemory(encoded))}, record
}

func serveTerminalPeer(conn net.Conn, config *ssh.ServerConfig, record *terminalRecord, refusePty bool) {
	peer, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		conn.Close()
		return
	}
	defer peer.Close()
	go ssh.DiscardRequests(requests)
	for incoming := range channels {
		channel, requests, err := incoming.Accept()
		if err != nil {
			continue
		}
		go serveTerminalChannel(channel, requests, record, refusePty)
	}
}

func serveTerminalChannel(channel ssh.Channel, requests <-chan *ssh.Request, record *terminalRecord, refusePty bool) {
	defer channel.Close()
	for req := range requests {
		switch req.Type {
		case "pty-req":
			var pty struct {
				Term                    string
				Cols, Rows, Width, High uint32
				Modes                   string
			}
			_ = ssh.Unmarshal(req.Payload, &pty)
			record.add(&record.ptys, pty.Term+" "+strconv.Itoa(int(pty.Cols))+"x"+strconv.Itoa(int(pty.Rows)))
			_ = req.Reply(!refusePty, nil)
		case "window-change":
			var size struct{ Cols, Rows, Width, High uint32 }
			_ = ssh.Unmarshal(req.Payload, &size)
			record.add(&record.resizes, strconv.Itoa(int(size.Cols))+"x"+strconv.Itoa(int(size.Rows)))
		case "exec":
			_ = req.Reply(true, nil)
			go echoUntilQuit(channel)
		default:
			_ = req.Reply(true, nil)
		}
	}
}

func echoUntilQuit(channel ssh.Channel) {
	buffer := make([]byte, 256)
	for {
		n, err := channel.Read(buffer)
		if index := bytes.IndexByte(buffer[:n], 'q'); index >= 0 {
			_, _ = channel.Write(buffer[:index])
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{7}))
			_ = channel.Close()
			return
		}
		if n > 0 {
			_, _ = channel.Write(buffer[:n])
		}
		if err != nil {
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			_ = channel.Close()
			return
		}
	}
}

func readUntil(t *testing.T, reader io.Reader, want string) string {
	t.Helper()
	var seen strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 512)
		for !strings.Contains(seen.String(), want) {
			n, err := reader.Read(buffer)
			seen.Write(buffer[:n])
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
	if !strings.Contains(seen.String(), want) {
		t.Fatalf("output %q lacks %q", seen.String(), want)
	}
	return seen.String()
}

func TestSSHTerminalRequestsPtyResizesAndReportsExitStatus(t *testing.T) {
	target, record := terminalPeer(t, false)
	ex := NewSSH(target)
	defer ex.Close()
	session, err := StartInteractive(context.Background(), ex, "docker exec -it c sh", WindowSize{Cols: 120, Rows: 40})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, session, "abc")
	if err := session.Resize(WindowSize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	_, _ = session.Write([]byte("q"))
	code, err := session.Wait()
	if err != nil || code != 7 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	// The window-change request is asynchronous: the peer may record it after the exit status arrives.
	var ptys, resizes []string
	deadline := time.Now().Add(2 * time.Second)
	for {
		ptys, resizes = record.snapshot()
		if len(resizes) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(ptys) != 1 || ptys[0] != "xterm-256color 120x40" || len(resizes) != 1 || resizes[0] != "100x30" {
		t.Fatalf("pty=%v resizes=%v", ptys, resizes)
	}
}

func TestSSHTerminalCapLeavesCommandSlotsAndTransport(t *testing.T) {
	target, _ := terminalPeer(t, false)
	ex := NewSSH(target)
	defer ex.Close()
	for range sshMaxInteractive {
		session, err := ex.StartInteractive(context.Background(), "sh", WindowSize{})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
	}
	if _, err := ex.StartInteractive(context.Background(), "sh", WindowSize{}); !errors.Is(err, ErrInteractiveLimit) {
		t.Fatalf("err=%v", err)
	}
	if _, err := ex.Run(context.Background(), "echo"); err != nil {
		t.Fatalf("commands starved by terminals: %v", err)
	}
}

func TestSSHTerminalCancellationAndRefusalKeepSharedConnection(t *testing.T) {
	target, _ := terminalPeer(t, false)
	ex := NewSSH(target)
	defer ex.Close()
	ctx, cancel := context.WithCancel(context.Background())
	session, err := ex.StartInteractive(ctx, "sh", WindowSize{})
	if err != nil {
		t.Fatal(err)
	}
	client := ex.client
	cancel()
	waited := make(chan struct{})
	go func() { _, _ = session.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled terminal kept running")
	}
	session.Close()
	if _, err := ex.Run(context.Background(), "echo"); err != nil || ex.client != client {
		t.Fatalf("shared connection replaced or broken: %v", err)
	}
	waitForSlots(t, ex, 0)

	refusing, _ := terminalPeer(t, true)
	other := NewSSH(refusing)
	defer other.Close()
	if _, err := other.StartInteractive(context.Background(), "sh", WindowSize{}); err == nil {
		t.Fatal("refused pty accepted")
	}
	waitForSlots(t, other, 0)
	if _, err := other.Run(context.Background(), "echo"); err != nil {
		t.Fatalf("pty refusal broke the transport: %v", err)
	}
}

func waitForSlots(t *testing.T, ex *SSH, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for ex.interactive.Load() != want {
		if time.Now().After(deadline) {
			t.Fatalf("interactive slots=%d, want %d", ex.interactive.Load(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLocalTerminalHasSizeInputAndExitStatus(t *testing.T) {
	local := NewLocal()
	session, err := local.StartInteractive(context.Background(),
		`printf 'ready\n'; read line; stty size; echo "got:$line"; exit 3`, WindowSize{Cols: 90, Rows: 20})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	readUntil(t, session, "ready")
	if err := session.Resize(WindowSize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Write([]byte("hi\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, session, "got:hi")
	code, err := session.Wait()
	if err != nil || code != 3 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestLocalTerminalSizeIsAppliedOnResize(t *testing.T) {
	session, err := NewLocal().StartInteractive(context.Background(), `read line; stty size`, WindowSize{Cols: 90, Rows: 20})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Resize(WindowSize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	_, _ = session.Write([]byte("\n"))
	readUntil(t, session, "30 100")
}

func TestLocalTerminalCapAndCancellationReleaseSlots(t *testing.T) {
	local := NewLocal()
	local.interactive.Store(localMaxInteractive)
	if _, err := local.StartInteractive(context.Background(), "true", WindowSize{}); !errors.Is(err, ErrInteractiveLimit) {
		t.Fatalf("err=%v", err)
	}
	local.interactive.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	session, err := local.StartInteractive(ctx, "sleep 30", WindowSize{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	waited := make(chan struct{})
	go func() { _, _ = session.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled terminal kept running")
	}
	deadline := time.Now().Add(5 * time.Second)
	for local.interactive.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("slot not released")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestFakeTerminalEchoesAndEndsOnEndOfInput(t *testing.T) {
	fake := NewFake().On("docker exec", "welcome", 5)
	session, err := StartInteractive(context.Background(), fake, "docker exec -it c sh", WindowSize{Cols: 10, Rows: 5})
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, session, "welcome")
	go func() { _, _ = session.Write([]byte{'x', FakeEndOfInput}) }()
	readUntil(t, session, "x")
	if code, err := session.Wait(); code != 5 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if terminal := fake.Terminals()[0]; terminal.Input() != "x\x04" || terminal.Resizes()[0] != (WindowSize{Cols: 10, Rows: 5}) {
		t.Fatalf("input=%q", terminal.Input())
	}
}
