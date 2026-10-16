// Package executor runs shell commands on a target machine. Everything Pier
// does on a server (docker, git, files) goes through the Executor interface,
// so local and SSH servers are interchangeable.
package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	maxLineBytes    = 1 << 20
	maxCapturedSize = 4 << 20
)

// LineSink receives one line of merged stdout/stderr output.
type LineSink func(line string)

// Executor runs commands and moves files on one machine.
type Executor interface {
	// Run executes the command and returns its trimmed merged output.
	Run(ctx context.Context, command string) (string, error)
	// Stream executes the command forwarding each output line to sink.
	Stream(ctx context.Context, command string, sink LineSink) error
	// WriteFile creates the parent directory and writes the file.
	WriteFile(ctx context.Context, path string, content []byte, mode os.FileMode) error
	// ReadFile opens a file for reading; the caller closes it.
	ReadFile(ctx context.Context, path string) (io.ReadCloser, error)
}

// ExitError reports a command that finished with a non-zero status.
type ExitError struct {
	Command string
	Code    int
	Output  string
}

func (e *ExitError) Error() string {
	output := strings.TrimSpace(e.Output)
	if output == "" {
		return fmt.Sprintf("command exited with status %d", e.Code)
	}
	return fmt.Sprintf("command exited with status %d: %s", e.Code, lastLines(output, 5))
}

// capture runs Stream collecting the output, shared by every implementation.
func capture(ctx context.Context, ex Executor, command string) (string, error) {
	var output strings.Builder
	err := ex.Stream(ctx, command, func(line string) {
		if output.Len() < maxCapturedSize {
			output.WriteString(line)
			output.WriteByte('\n')
		}
	})
	text := strings.TrimSpace(output.String())
	if exitErr, ok := err.(*ExitError); ok {
		exitErr.Output = text
	}
	return text, err
}

// pumpLines splits reader output into lines, treating carriage returns as
// line breaks so progress bars do not produce giant lines.
func pumpLines(reader io.Reader, sink LineSink) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	scanner.Split(scanLinesOrCarriageReturns)
	for scanner.Scan() {
		sink(scanner.Text())
	}
	// A line beyond maxLineBytes stops the scanner; drain so the writer never blocks.
	_, _ = io.Copy(io.Discard, reader)
}

func scanLinesOrCarriageReturns(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func lastLines(text string, count int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, "\n")
}
