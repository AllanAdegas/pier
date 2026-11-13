package logdrain

import (
	"context"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"pier/internal/docker"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/shell"
	"pier/internal/server"
)

// Collection retains at most maxReadBytes per container per cycle.
// Excess complete records are consumed and counted as dropped.
const (
	collectInterval = 10 * time.Second
	serverTimeout   = 30 * time.Second
	bufferCapacity  = 5000
	batchSize       = 500
	maxReadBytes    = 1 << 20
	flushInterval   = time.Second
)

var defaultRetries = []time.Duration{500 * time.Millisecond, 2 * time.Second, 5 * time.Second}

// Connector reaches a server.
type Connector interface {
	Executor(ctx context.Context, serverID string) (executor.Executor, server.Server, error)
}

// Masker hides secrets in a line.
type Masker interface{ Mask(text string) string }

// RedactorSource returns the masker with the current secrets of a resource.
// When it fails, the lines of that resource are dropped, never sent unmasked.
type RedactorSource func(ctx context.Context, resourceType, id string) (Masker, error)

// Forwarder collects container logs and feeds the drains.
type Forwarder struct {
	store     store
	box       *secretbox.Box
	directory directory
	connector Connector
	redactors RedactorSource
	client    *http.Client
	logger    *slog.Logger
	retries   []time.Duration
	now       func() time.Time

	mu       sync.Mutex
	shippers map[string]*shipper
	cursors  map[string]time.Time
	offsets  map[string]uint64
}

// Run collects every collectInterval until ctx ends. A failing cycle is
// logged and the next one starts fresh.
func (f *Forwarder) Run(ctx context.Context) {
	ticker := time.NewTicker(collectInterval)
	defer ticker.Stop()
	defer f.stopAll()
	for {
		f.safeCycle(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (f *Forwarder) safeCycle(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			f.logger.Error("log drain cycle panicked", "panic", recovered)
		}
	}()
	if err := f.cycle(ctx); err != nil {
		f.logger.Warn("log drain cycle failed", "error", err)
	}
}

// cycle reconciles the shippers with the enabled drains and collects once.
func (f *Forwarder) cycle(ctx context.Context) error {
	drains, err := f.store.list(ctx, true)
	if err != nil {
		return err
	}
	f.reconcile(ctx, drains)
	routes, err := f.routes(ctx, drains)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for serverID, wanted := range routes {
		f.collect(ctx, serverID, wanted, seen)
	}
	f.forgetCursors(seen)
	return nil
}

// routes maps server → resource key → shippers interested in it.
func (f *Forwarder) routes(ctx context.Context, drains []stored) (map[string]map[string][]*shipper, error) {
	routes := map[string]map[string][]*shipper{}
	for _, drain := range drains {
		target := f.shipper(drain.ID)
		if target == nil {
			continue
		}
		resources, err := f.directory.resources(ctx, drain.ScopeType, drain.ScopeID)
		if err != nil {
			return nil, err
		}
		for _, item := range resources {
			for _, serverID := range item.Servers {
				if routes[serverID] == nil {
					routes[serverID] = map[string][]*shipper{}
				}
				routes[serverID][item.key()] = append(routes[serverID][item.key()], target)
			}
		}
	}
	return routes, nil
}

// collect reads the new lines of the wanted running containers of a server,
// one command at a time, so a drain never holds more than one SSH session.
func (f *Forwarder) collect(ctx context.Context, serverID string, wanted map[string][]*shipper, seen map[string]bool) {
	ctx, cancel := context.WithTimeout(ctx, serverTimeout)
	defer cancel()
	ex, _, err := f.connector.Executor(ctx, serverID)
	if err != nil {
		return
	}
	containers, err := docker.ListManaged(ctx, ex)
	if err != nil {
		return
	}
	maskers := map[string]Masker{}
	for _, container := range containers {
		key := container.Type + "/" + container.ResourceID
		targets := wanted[key]
		if len(targets) == 0 || container.State != "running" {
			continue
		}
		seen[container.ID] = true
		lines := f.read(ctx, ex, container.ID, targets...)
		if len(lines) == 0 {
			continue
		}
		masker, found := maskers[key]
		if !found {
			masker, err = f.redactors(ctx, container.Type, container.ResourceID)
			if err != nil {
				for _, target := range targets {
					target.buffer.drop(len(lines))
				}
				continue
			}
			maskers[key] = masker
		}
		labels := map[string]string{"pier_server": serverID, "pier_resource_type": container.Type,
			"pier_resource_id": container.ResourceID, "pier_container": container.Name}
		for _, target := range targets {
			target.buffer.push(target.records(lines, labels, masker)...)
		}
	}
}

type logLine struct {
	at      time.Time
	message string
}

// read returns the lines logged since the container's cursor. A container
// seen for the first time starts now: drains do not backfill history.
func (f *Forwarder) read(ctx context.Context, ex executor.Executor, containerID string, targets ...*shipper) []logLine {
	f.mu.Lock()
	cursor, known := f.cursors[containerID]
	if f.offsets == nil {
		f.offsets = map[string]uint64{}
	}
	skip := f.offsets[containerID]
	if !known {
		f.cursors[containerID] = f.now().UTC()
	}
	f.mu.Unlock()
	if !known {
		return nil
	}
	// od emits bounded ASCII lines even for arbitrarily large Docker records.
	// A fixed trailer carries Docker status through the POSIX pipeline.
	// It is held back from the parser, including on an unterminated log tail.
	command := "{ " + shell.Join("docker", "logs", "--timestamps", "--since", cursor.Format(time.RFC3339Nano), containerID) +
		` 2>&1; code=$?; printf "PIER_LOG_EXIT=%03d" "$code"; } | od -An -v -tx1`
	reader := newLogRead(cursor, skip)
	var trailer []byte
	err := ex.Stream(ctx, command, func(line string) {
		for _, word := range strings.Fields(line) {
			var decoded [1]byte
			n, err := hex.Decode(decoded[:], []byte(word))
			if err != nil || n != 1 {
				reader.invalid = true
				continue
			}
			trailer = append(trailer, decoded[0])
			if len(trailer) > len("PIER_LOG_EXIT=000") {
				reader.byte(trailer[0])
				trailer = trailer[1:]
			}
		}
	})
	if err != nil || reader.invalid || string(trailer) != "PIER_LOG_EXIT=000" {
		return nil
	}
	// An unterminated tail is intentionally retried, never marked consumed.
	f.mu.Lock()
	f.cursors[containerID], f.offsets[containerID] = reader.cursor, reader.offset
	f.mu.Unlock()
	for _, target := range targets {
		target.buffer.drop(reader.dropped)
	}
	return reader.lines
}

func (f *Forwarder) forgetCursors(seen map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id := range f.cursors {
		if !seen[id] {
			delete(f.cursors, id)
			delete(f.offsets, id)
		}
	}
}
