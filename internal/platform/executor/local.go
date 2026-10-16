package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Local runs commands on the machine where Pier itself is running.
type Local struct{}

// NewLocal creates a local executor.
func NewLocal() *Local { return &Local{} }

// Run implements Executor.
func (l *Local) Run(ctx context.Context, command string) (string, error) {
	return capture(ctx, l, command)
}

// Stream implements Executor.
func (l *Local) Stream(ctx context.Context, command string, sink LineSink) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	// Own process group so cancellation also kills grandchildren (docker build, git).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second

	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}
	pumped := make(chan struct{})
	go func() {
		pumpLines(reader, sink)
		close(pumped)
	}()
	waitErr := cmd.Wait()
	_ = writer.Close()
	<-pumped

	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return &ExitError{Command: command, Code: exitErr.ExitCode()}
	}
	return waitErr
}

// WriteFile implements Executor.
func (l *Local) WriteFile(_ context.Context, path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Chmod(path, mode)
}

// ReadFile implements Executor.
func (l *Local) ReadFile(_ context.Context, path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return file, nil
}
