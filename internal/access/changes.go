package access

import "sync"

// changes tells open long-lived connections (terminals) that a user's roles
// changed, so they re-check at once instead of at their next poll.
type changes struct {
	mu   sync.Mutex
	next int
	subs map[string]map[int]chan struct{}
}

func newChanges() *changes { return &changes{subs: map[string]map[int]chan struct{}{}} }

func (c *changes) subscribe(user string) (<-chan struct{}, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id, signal := c.next, make(chan struct{}, 1)
	if c.subs[user] == nil {
		c.subs[user] = map[int]chan struct{}{}
	}
	c.subs[user][id] = signal
	return signal, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.subs[user], id)
		if len(c.subs[user]) == 0 {
			delete(c.subs, user)
		}
	}
}

// notify never blocks: one pending signal is enough to trigger a re-check.
func (c *changes) notify(user string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, signal := range c.subs[user] {
		select {
		case signal <- struct{}{}:
		default:
		}
	}
}
