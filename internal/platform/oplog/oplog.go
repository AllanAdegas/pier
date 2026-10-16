// Package oplog keeps, in memory, the output of the last long-running
// operation of each resource so the UI can follow it by polling.
package oplog

import (
	"sync"
	"time"
)

const maxLines = 2000

// Snapshot is the observable state of an operation.
type Snapshot struct {
	Title      string     `json:"title"`
	Running    bool       `json:"running"`
	Error      string     `json:"error"`
	Lines      []string   `json:"lines"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// Operation collects the output of one run.
type Operation struct {
	mu       sync.Mutex
	snapshot Snapshot
}

// Append adds an output line; it is an executor.LineSink.
func (o *Operation) Append(line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.snapshot.Lines) >= maxLines {
		o.snapshot.Lines = o.snapshot.Lines[1:]
	}
	o.snapshot.Lines = append(o.snapshot.Lines, line)
}

// Finish closes the operation, recording the error when it failed.
func (o *Operation) Finish(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	o.snapshot.Running = false
	o.snapshot.FinishedAt = &now
	if err != nil {
		o.snapshot.Error = err.Error()
	}
}

func (o *Operation) read() Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	copied := o.snapshot
	copied.Lines = append([]string{}, o.snapshot.Lines...)
	return copied
}

// Registry tracks the latest operation per key (a resource id).
type Registry struct {
	mu         sync.Mutex
	operations map[string]*Operation
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{operations: make(map[string]*Operation)} }

// Begin starts a new operation for the key. It returns false when one is
// already running, so two operations never overlap on the same resource.
func (r *Registry) Begin(key, title string) (*Operation, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, exists := r.operations[key]; exists && current.read().Running {
		return nil, false
	}
	operation := &Operation{snapshot: Snapshot{Title: title, Running: true, StartedAt: time.Now(), Lines: []string{}}}
	r.operations[key] = operation
	return operation, true
}

// Get returns the latest operation of the key, if any.
func (r *Registry) Get(key string) (Snapshot, bool) {
	r.mu.Lock()
	operation, exists := r.operations[key]
	r.mu.Unlock()
	if !exists {
		return Snapshot{Lines: []string{}}, false
	}
	return operation.read(), true
}

// Forget drops the operation of a deleted resource.
func (r *Registry) Forget(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.operations, key)
}
