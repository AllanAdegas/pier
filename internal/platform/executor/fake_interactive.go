package executor

import (
	"bytes"
	"context"
	"io"
	"sync"
)

// FakeEndOfInput (Ctrl-D) typed into a fake terminal ends its command.
const FakeEndOfInput = 0x04

// StartInteractive opens a fake terminal: it prints the output registered
// with On for the command, then echoes every keystroke back, like `cat` on a
// terminal. Ctrl-D ends it with the registered exit code.
func (f *Fake) StartInteractive(ctx context.Context, command string, size WindowSize) (InteractiveSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.Commands = append(f.Commands, command)
	response := f.find(command)
	f.mu.Unlock()
	reader, writer := io.Pipe()
	session := &FakeTerminal{output: writer, reader: reader, code: response.code, done: make(chan struct{})}
	session.sizes = append(session.sizes, size)
	f.mu.Lock()
	f.terminals = append(f.terminals, session)
	f.mu.Unlock()
	if response.output != "" {
		go func() { _, _ = io.WriteString(writer, response.output+"\n") }()
	}
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	go func() { <-session.done; stop() }()
	return session, nil
}

// Terminals returns the fake terminals opened so far.
func (f *Fake) Terminals() []*FakeTerminal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*FakeTerminal(nil), f.terminals...)
}

// FakeTerminal is the session a Fake opens; tests inspect what reached it.
type FakeTerminal struct {
	mu     sync.Mutex
	output *io.PipeWriter
	reader *io.PipeReader
	input  bytes.Buffer
	code   int
	exited bool
	closed bool
	done   chan struct{}
	once   sync.Once
	// sizes holds the initial size followed by every resize.
	sizes []WindowSize
}

func (t *FakeTerminal) Read(p []byte) (int, error) { return t.reader.Read(p) }

func (t *FakeTerminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.input.Write(p)
	t.mu.Unlock()
	if index := bytes.IndexByte(p, FakeEndOfInput); index >= 0 {
		_, _ = t.output.Write(p[:index])
		t.finish(true)
		return len(p), nil
	}
	if _, err := t.output.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *FakeTerminal) Resize(size WindowSize) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sizes = append(t.sizes, size)
	return nil
}

func (t *FakeTerminal) Wait() (int, error) {
	<-t.done
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.exited {
		return -1, io.ErrClosedPipe
	}
	return t.code, nil
}

func (t *FakeTerminal) Close() error {
	t.finish(false)
	return nil
}

// Input returns every byte typed into the terminal.
func (t *FakeTerminal) Input() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.input.String()
}

// Closed reports whether the session was closed by its caller.
func (t *FakeTerminal) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// Resizes returns a copy of the sizes the terminal had.
func (t *FakeTerminal) Resizes() []WindowSize {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]WindowSize(nil), t.sizes...)
}

func (t *FakeTerminal) finish(exited bool) {
	t.once.Do(func() {
		t.mu.Lock()
		t.exited = exited
		t.mu.Unlock()
		_ = t.output.Close()
		close(t.done)
	})
	if !exited {
		t.mu.Lock()
		t.closed = true
		t.mu.Unlock()
	}
}
