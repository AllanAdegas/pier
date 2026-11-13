package logdrain

import (
	"context"
	"maps"
	"time"
)

// shipper sends the buffer of one drain to its sink in its own goroutine.
type shipper struct {
	drain   stored
	sink    Sink
	buffer  *buffer
	retries []time.Duration
	now     func() time.Time
	cancel  context.CancelFunc
	done    chan struct{}
}

// records builds the drain's records: Pier labels, then the drain's own.
func (s *shipper) records(lines []logLine, labels map[string]string, masker Masker) []Record {
	merged := maps.Clone(labels)
	maps.Copy(merged, s.drain.Labels)
	records := make([]Record, len(lines))
	for index, line := range lines {
		records[index] = Record{Time: line.at, Message: masker.Mask(line.message), Labels: merged}
	}
	return records
}

func (s *shipper) run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.buffer.wake:
		}
		for batch := s.buffer.take(batchSize); len(batch) > 0; batch = s.buffer.take(batchSize) {
			if !s.deliver(ctx, batch) {
				return
			}
		}
	}
}

// deliver sends a batch with a few retries; a batch that still fails is
// dropped and counted. It returns false when the shipper is stopping.
func (s *shipper) deliver(ctx context.Context, batch []Record) (running bool) {
	defer func() {
		if recover() != nil {
			s.buffer.failed(len(batch), "panic", s.now())
			running = true
		}
	}()
	for attempt := 0; ; attempt++ {
		err := s.sink.Send(ctx, batch)
		if err == nil {
			s.buffer.delivered(len(batch), s.now())
			return true
		}
		if attempt == len(s.retries) {
			s.buffer.failed(len(batch), err.Error(), s.now())
			return true
		}
		timer := time.NewTimer(s.retries[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			s.buffer.failed(len(batch), err.Error(), s.now())
			return false
		case <-timer.C:
		}
	}
}

// reconcile starts a shipper for each enabled drain and restarts it when
// the drain changed; shippers of removed or disabled drains stop.
func (f *Forwarder) reconcile(ctx context.Context, drains []stored) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wanted := map[string]bool{}
	for _, drain := range drains {
		wanted[drain.ID] = true
		current, running := f.shippers[drain.ID]
		if running && current.drain.UpdatedAt.Equal(drain.UpdatedAt) {
			continue
		}
		if running {
			current.stop()
		}
		token, err := f.openToken(drain)
		if err != nil {
			f.logger.Warn("log drain token unreadable", "drain", drain.ID)
			delete(f.shippers, drain.ID)
			continue
		}
		f.shippers[drain.ID] = f.start(ctx, drain, newSink(drain.Drain, token, f.client))
	}
	for id, current := range f.shippers {
		if !wanted[id] {
			current.stop()
			delete(f.shippers, id)
		}
	}
}

func (f *Forwarder) openToken(drain stored) (string, error) {
	if drain.sealedToken == "" {
		return "", nil
	}
	return f.box.Open(drain.sealedToken)
}

func (f *Forwarder) start(ctx context.Context, drain stored, sink Sink) *shipper {
	ctx, cancel := context.WithCancel(ctx)
	created := &shipper{drain: drain, sink: sink, buffer: newBuffer(bufferCapacity), retries: f.retries, now: f.now,
		cancel: cancel, done: make(chan struct{})}
	go created.run(ctx)
	return created
}

func (s *shipper) stop() {
	s.cancel()
	<-s.done
}

func (f *Forwarder) shipper(id string) *shipper {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shippers[id]
}

func (f *Forwarder) stopAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, current := range f.shippers {
		current.stop()
		delete(f.shippers, id)
	}
}

// Status returns the counters of a drain; a drain without a running shipper
// (disabled, or Pier just started) has empty counters.
func (f *Forwarder) Status(id string) Status {
	if current := f.shipper(id); current != nil {
		return current.buffer.snapshot()
	}
	return Status{Capacity: bufferCapacity}
}
