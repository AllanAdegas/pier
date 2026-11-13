package logdrain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

// fixtureExecutor runs the production POSIX shell/od pipeline through the real local
// streaming executor. Only Docker is replaced; no daemon or container is used.
type fixtureExecutor struct {
	*executor.Fake
	script   string
	commands []string
}

func (e *fixtureExecutor) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	e.commands = append(e.commands, command)
	return executor.NewLocal().Stream(ctx, strings.Replace(command, "docker logs", shell.Quote(e.script)+" logs", 1), sink)
}

func TestReadJumboProgressAndEqualTimestampReplay(t *testing.T) {
	for _, middle := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "middle"}[middle], func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "logs")
			script := filepath.Join(dir, "docker")
			// This fixture emits retained history, including the inclusive cursor.
			if err := os.WriteFile(script, []byte("#!/bin/sh\ncat "+shell.Quote(file)+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			ex := &fixtureExecutor{Fake: executor.NewFake(), script: script}
			at := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
			stamp := at.Format(time.RFC3339Nano) + " "
			raw := stamp + strings.Repeat("x", maxReadBytes+100) + "\n"
			if middle {
				raw = stamp + "before\n" + raw
			}
			raw += stamp + "after\n" + stamp + "tail"
			if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			f := &Forwarder{cursors: map[string]time.Time{"id": at.Add(-time.Second)}}
			target := &shipper{buffer: newBuffer(10)}
			lines := f.read(context.Background(), ex, "id", target)
			want := 1
			if middle {
				want++
			}
			if len(lines) != want || lines[len(lines)-1].message != "after" {
				t.Fatalf("normal lines lost: %+v", lines)
			}
			if target.buffer.snapshot().Dropped != 1 || !f.cursors["id"].Equal(at) || f.offsets["id"] != uint64(want+1) {
				t.Fatalf("cursor/count: %v %v %+v", f.cursors, f.offsets, target.buffer.snapshot())
			}
			for i := 0; i < 3; i++ {
				if got := f.read(context.Background(), ex, "id", target); len(got) != 0 {
					t.Fatalf("replayed records: %+v", got)
				}
			}
			if target.buffer.snapshot().Dropped != 1 {
				t.Fatal("recounted jumbo")
			}
			// Finish the partial record and append another at exactly the same stamp.
			if err := os.WriteFile(file, []byte(raw+" completed\n"+stamp+"last\n"), 0600); err != nil {
				t.Fatal(err)
			}
			lines = f.read(context.Background(), ex, "id", target)
			if len(lines) != 2 || lines[0].message != "tail completed" || lines[1].message != "last" {
				t.Fatalf("tail/equal timestamp: %+v", lines)
			}
			if !strings.Contains(ex.commands[len(ex.commands)-1], at.Format(time.RFC3339Nano)) {
				t.Fatal("real cursor missing from command")
			}
		})
	}
}

func TestReadBatchCapAndFailureDoNotSilentlySkip(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
	stamp := at.Format(time.RFC3339Nano) + " "
	raw := strings.Repeat(stamp+"ok\n", bufferCapacity+3)
	ex := executor.NewFake().On("docker logs", hexLogs(raw), 1)
	f := &Forwarder{cursors: map[string]time.Time{"id": at.Add(-time.Second)}}
	target := &shipper{buffer: newBuffer(10)}
	if got := f.read(context.Background(), ex, "id", target); len(got) != 0 || !f.cursors["id"].Equal(at.Add(-time.Second)) || target.buffer.snapshot().Dropped != 0 {
		t.Fatal("failed read committed")
	}
	ex.On("docker logs", hexLogs(raw), 0)
	if got := f.read(context.Background(), ex, "id", target); len(got) != bufferCapacity {
		t.Fatalf("got %d", len(got))
	}
	if target.buffer.snapshot().Dropped != 3 || f.offsets["id"] != bufferCapacity+3 {
		t.Fatalf("%+v %v", target.buffer.snapshot(), f.offsets)
	}
	if got := f.read(context.Background(), ex, "id", target); len(got) != 0 || target.buffer.snapshot().Dropped != 3 {
		t.Fatal("batch cap replay")
	}
	// New same-timestamp records are accepted after the inclusive offset.
	ex.On("docker logs", hexLogs(raw+stamp+"new\n"), 0)
	if got := f.read(context.Background(), ex, "id", target); len(got) != 1 || got[0].message != "new" {
		t.Fatalf("%+v", got)
	}
}

func TestReadPipelineDockerFailureAndByteCap(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
	stamp := at.Format(time.RFC3339Nano) + " "
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' "+shell.Quote(stamp+"uncommitted")+"\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ex := &fixtureExecutor{Fake: executor.NewFake(), script: script}
	f := &Forwarder{cursors: map[string]time.Time{"id": at.Add(-time.Second)}}
	if got := f.read(context.Background(), ex, "id"); len(got) != 0 || !f.cursors["id"].Equal(at.Add(-time.Second)) {
		t.Fatal("pipeline hid Docker error")
	}
	raw := stamp + strings.Repeat("a", maxReadBytes/2) + "\n" + stamp + strings.Repeat("b", maxReadBytes/2) + "\n" + stamp + "later\n"
	fake := executor.NewFake().On("docker logs", hexLogs(raw), 0)
	target := &shipper{buffer: newBuffer(10)}
	got := f.read(context.Background(), fake, "id", target)
	if len(got) != 2 || got[1].message != "later" || target.buffer.snapshot().Dropped != 1 || f.offsets["id"] != 3 {
		t.Fatalf("byte cap: %d %+v %v", len(got), target.buffer.snapshot(), f.offsets)
	}
}
