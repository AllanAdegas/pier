package executor

import (
	"bytes"
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
	return l.streamInput(ctx, command, nil, sink)
}

// RunInput executes a command with content on stdin, never in its arguments.
func (l *Local) RunInput(ctx context.Context, command string, content []byte) (string, error) {
	var output limitedBuffer
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(content)
	cmd.Stdout, cmd.Stderr = &output, &output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return output.String(), ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		err = &ExitError{Command: command, Code: exitErr.ExitCode(), Output: output.String()}
	}
	if err == nil && output.truncated {
		err = ErrFileTooLarge
	}
	return output.String(), err
}

func (l *Local) streamInput(ctx context.Context, command string, content []byte, sink LineSink) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(content)
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

// WriteFile replaces a file atomically, refusing a symlink target.
func (l *Local) WriteFile(ctx context.Context, path string, content []byte, mode os.FileMode) error {
	return l.writeFile(ctx, path, content, mode, false)
}

// WriteFileExclusive creates a file only when no directory entry exists.
func (l *Local) WriteFileExclusive(ctx context.Context, path string, content []byte, mode os.FileMode) error {
	return l.writeFile(ctx, path, content, mode, true)
}

func (l *Local) writeFile(ctx context.Context, path string, content []byte, mode os.FileMode, exclusive bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse non-regular target: %s", path)
		}
		if exclusive {
			return os.ErrExist
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".pier-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if _, err := temp.Write(content); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if exclusive {
		return os.Link(temp.Name(), path)
	}
	// Rename replaces an entry; even a symlink swapped in after Lstat is never followed.
	return os.Rename(temp.Name(), path)
}

// ReadFile opens only regular files, with cancellation and a bounded reader.
func (l *Local) ReadFile(ctx context.Context, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	if info.Size() > MaxFileBytes {
		file.Close()
		return nil, ErrFileTooLarge
	}
	return newFileReader(ctx, file, MaxFileBytes), nil
}
