// Package memo shares expensive reads between callers: concurrent requests
// for the same key wait for one load, and its result is reused for a while.
// It bounds the work a burst of identical requests can trigger, such as a
// panel polled by many tabs or a client hammering an endpoint that shells
// out to docker.
package memo

import (
	"context"
	"sync"
	"time"
)

// Group memoizes loads per key for a fixed time.
type Group[V any] struct {
	ttl     time.Duration
	timeout time.Duration
	now     func() time.Time
	mu      sync.Mutex
	entries map[string]*entry[V]
}

type entry[V any] struct {
	done     chan struct{}
	loadedAt time.Time
	value    V
	err      error
}

// New creates a group whose results live for ttl. A load runs detached from
// the caller that started it (others may be waiting on it), bounded by timeout.
func New[V any](ttl, timeout time.Duration) *Group[V] {
	return &Group[V]{ttl: ttl, timeout: timeout, now: time.Now, entries: map[string]*entry[V]{}}
}

// Do returns the fresh result for key, joining a load in progress or
// starting one. Failed loads are not kept: the next call tries again.
func (g *Group[V]) Do(ctx context.Context, key string, load func(context.Context) (V, error)) (V, error) {
	g.mu.Lock()
	current, found := g.entries[key]
	if !found || g.expired(current) {
		current = &entry[V]{done: make(chan struct{})}
		g.entries[key] = current
		go g.run(ctx, key, current, load)
	}
	g.mu.Unlock()
	select {
	case <-current.done:
		return current.value, current.err
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
}

// expired reports whether a finished entry is too old or failed. An entry
// still loading is never expired: callers join it.
func (g *Group[V]) expired(e *entry[V]) bool {
	select {
	case <-e.done:
		return e.err != nil || g.now().Sub(e.loadedAt) >= g.ttl
	default:
		return false
	}
}

func (g *Group[V]) run(ctx context.Context, key string, e *entry[V], load func(context.Context) (V, error)) {
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), g.timeout)
	defer cancel()
	e.value, e.err = load(loadCtx)
	e.loadedAt = g.now()
	close(e.done)
	if e.err != nil {
		g.mu.Lock()
		if g.entries[key] == e {
			delete(g.entries, key)
		}
		g.mu.Unlock()
	}
}

// Forget drops the cached result of key, so the next call loads again.
func (g *Group[V]) Forget(key string) {
	g.mu.Lock()
	delete(g.entries, key)
	g.mu.Unlock()
}
