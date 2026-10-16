package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Fake is a scriptable Executor for tests. Commands are matched by substring.
type Fake struct {
	mu        sync.Mutex
	responses []fakeResponse
	Commands  []string
	Files     map[string][]byte
}

type fakeResponse struct {
	match  string
	output string
	code   int
}

// NewFake creates a fake where every unmatched command succeeds silently.
func NewFake() *Fake { return &Fake{Files: make(map[string][]byte)} }

// On registers the output and exit code for commands containing match.
// Later registrations win, so tests can override defaults.
func (f *Fake) On(match, output string, code int) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append([]fakeResponse{{match: match, output: output, code: code}}, f.responses...)
	return f
}

// Ran reports whether any executed command contains the fragment.
func (f *Fake) Ran(fragment string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, command := range f.Commands {
		if strings.Contains(command, fragment) {
			return true
		}
	}
	return false
}

// Run implements Executor.
func (f *Fake) Run(ctx context.Context, command string) (string, error) {
	return capture(ctx, f, command)
}

// Stream implements Executor.
func (f *Fake) Stream(ctx context.Context, command string, sink LineSink) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	f.Commands = append(f.Commands, command)
	response := f.find(command)
	f.mu.Unlock()

	if response.output != "" {
		for _, line := range strings.Split(response.output, "\n") {
			sink(line)
		}
	}
	if response.code != 0 {
		return &ExitError{Command: command, Code: response.code}
	}
	return nil
}

func (f *Fake) find(command string) fakeResponse {
	for _, response := range f.responses {
		if strings.Contains(command, response.match) {
			return response
		}
	}
	return fakeResponse{}
}

// WriteFile implements Executor.
func (f *Fake) WriteFile(_ context.Context, path string, content []byte, _ os.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Files[path] = bytes.Clone(content)
	return nil
}

// ReadFile implements Executor.
func (f *Fake) ReadFile(_ context.Context, path string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	content, ok := f.Files[path]
	if !ok {
		return nil, fmt.Errorf("open %s: %w", path, os.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}
