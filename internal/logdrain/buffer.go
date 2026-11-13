package logdrain

import (
	"sync"
	"time"
)

// Status is what the panel shows of a drain: the counters since Pier
// started, the buffer usage and the last failure.
type Status struct {
	Buffered      int        `json:"buffered"`
	Capacity      int        `json:"capacity"`
	Sent          uint64     `json:"sent"`
	Dropped       uint64     `json:"dropped"`
	FailedBatches uint64     `json:"failed_batches"`
	LastError     string     `json:"last_error"`
	LastErrorAt   *time.Time `json:"last_error_at"`
	LastSentAt    *time.Time `json:"last_sent_at"`
}

// buffer is a bounded FIFO of records. When full it drops the oldest record:
// a slow or failing sink costs old lines, never memory or a blocked collector.
type buffer struct {
	mu      sync.Mutex
	records []Record
	start   int
	size    int
	status  Status
	wake    chan struct{}
}

func newBuffer(capacity int) *buffer {
	return &buffer{records: make([]Record, capacity), status: Status{Capacity: capacity}, wake: make(chan struct{}, 1)}
}

// push appends records, evicting the oldest ones beyond the capacity.
func (b *buffer) push(records ...Record) {
	b.mu.Lock()
	defer b.mu.Unlock()
	capacity := len(b.records)
	for _, record := range records {
		if b.size == capacity {
			b.start = (b.start + 1) % capacity
			b.size--
			b.status.Dropped++
		}
		b.records[(b.start+b.size)%capacity] = record
		b.size++
	}
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// drop counts records lost before reaching the buffer.
func (b *buffer) drop(count int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status.Dropped += uint64(count)
}

// take removes up to max records from the front.
func (b *buffer) take(max int) []Record {
	b.mu.Lock()
	defer b.mu.Unlock()
	count := min(max, b.size)
	batch := make([]Record, count)
	for index := range batch {
		slot := (b.start + index) % len(b.records)
		batch[index] = b.records[slot]
		b.records[slot] = Record{}
	}
	b.start = (b.start + count) % len(b.records)
	b.size -= count
	return batch
}

func (b *buffer) delivered(count int, at time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status.Sent += uint64(count)
	b.status.LastSentAt = &at
}

func (b *buffer) failed(count int, message string, at time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status.Dropped += uint64(count)
	b.status.FailedBatches++
	b.status.LastError, b.status.LastErrorAt = message, &at
}

func (b *buffer) snapshot() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	status := b.status
	status.Buffered = b.size
	return status
}
