// Package pubsub is an in-memory topic broadcaster used for live log streaming.
package pubsub

import "sync"

const subscriberBuffer = 512

// Hub fans out messages of type T to the subscribers of a topic.
type Hub[T any] struct {
	mu     sync.Mutex
	topics map[string]map[chan T]struct{}
}

// NewHub creates an empty hub.
func NewHub[T any]() *Hub[T] {
	return &Hub[T]{topics: make(map[string]map[chan T]struct{})}
}

// Subscribe returns a channel of messages and a function to stop listening.
func (h *Hub[T]) Subscribe(topic string) (<-chan T, func()) {
	ch := make(chan T, subscriberBuffer)
	h.mu.Lock()
	if h.topics[topic] == nil {
		h.topics[topic] = make(map[chan T]struct{})
	}
	h.topics[topic][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.topics[topic], ch)
			if len(h.topics[topic]) == 0 {
				delete(h.topics, topic)
			}
			h.mu.Unlock()
		})
	}
	return ch, unsubscribe
}

// Publish delivers a message to every subscriber. Slow subscribers lose
// messages instead of blocking the publisher; they recover from persistence.
func (h *Hub[T]) Publish(topic string, message T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.topics[topic] {
		select {
		case ch <- message:
		default:
		}
	}
}
