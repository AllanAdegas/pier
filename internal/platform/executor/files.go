package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// MaxFileBytes bounds file transfers. Callers should impose a smaller limit
// when reading configuration rather than archives.
const MaxFileBytes = 256 << 20

var ErrFileTooLarge = errors.New("file exceeds size limit")

// RunInput keeps input out of process arguments and command logs.
func RunInput(ctx context.Context, ex Executor, command string, content []byte) (string, error) {
	if runner, ok := ex.(interface {
		RunInput(context.Context, string, []byte) (string, error)
	}); ok {
		return runner.RunInput(ctx, command, content)
	}
	return "", fmt.Errorf("executor does not support stdin")
}

// WriteFileExclusive creates a file without replacing any existing entry.
func WriteFileExclusive(ctx context.Context, ex Executor, path string, content []byte, mode os.FileMode) error {
	if writer, ok := ex.(interface {
		WriteFileExclusive(context.Context, string, []byte, os.FileMode) error
	}); ok {
		return writer.WriteFileExclusive(ctx, path, content, mode)
	}
	return fmt.Errorf("executor does not support exclusive writes")
}

type fileReader struct {
	reader    io.ReadCloser
	ctx       context.Context
	remaining int64
	stop      func() bool
	once      sync.Once
	closeErr  error
}

func newFileReader(ctx context.Context, reader io.ReadCloser, limit int64) *fileReader {
	r := &fileReader{reader: reader, ctx: ctx, remaining: limit}
	r.stop = context.AfterFunc(ctx, func() { r.closeUnderlying() })
	return r
}
func (r *fileReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.remaining == 0 {
		var extra [1]byte
		n, err := r.reader.Read(extra[:])
		if n > 0 {
			return 0, ErrFileTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	if r.ctx.Err() != nil {
		return n, r.ctx.Err()
	}
	return n, err
}
func (r *fileReader) closeUnderlying() { r.once.Do(func() { r.closeErr = r.reader.Close() }) }
func (r *fileReader) Close() error     { r.stop(); r.closeUnderlying(); return r.closeErr }

// limitedBuffer drains output while bounding retained bytes.
type limitedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) > maxCapturedSize-b.buffer.Len() {
		b.truncated = true
		p = p[:maxCapturedSize-b.buffer.Len()]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
