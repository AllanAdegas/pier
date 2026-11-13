package logdrain

import (
	"strings"
	"time"
)

// logRead retains a bounded prefix until newline, discarding jumbo payloads.
// Cursor offsets count ALL complete valid records at the timestamp, including
// drops. Replay assumes stable chronological Docker output and retained history.
type logRead struct {
	initial, cursor    time.Time
	skip, seen, offset uint64
	prefix             []byte
	oversized, invalid bool
	used, dropped      int
	lines              []logLine
}

func newLogRead(at time.Time, skip uint64) *logRead {
	return &logRead{initial: at, cursor: at, skip: skip, offset: skip}
}

func (r *logRead) byte(b byte) {
	if b != '\n' {
		if len(r.prefix) < maxReadBytes {
			r.prefix = append(r.prefix, b)
		} else {
			r.oversized = true
		}
		return
	}
	defer func() { r.prefix = r.prefix[:0]; r.oversized = false }()
	stamp, message, found := strings.Cut(string(r.prefix), " ")
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if !found || err != nil || at.Before(r.initial) {
		return
	}
	if at.Equal(r.initial) {
		r.seen++
		if r.seen <= r.skip {
			return
		}
	}
	// Docker timestamps are expected in order. Do not regress on diagnostics
	// or out-of-order records; such records cannot be recovered by a time cursor.
	if at.Before(r.cursor) {
		r.dropped++
		return
	}
	if at.Equal(r.cursor) {
		r.offset++
	} else {
		r.cursor, r.offset = at, 1
	}
	if r.oversized || r.used+len(r.prefix)+1 > maxReadBytes || len(r.lines) >= bufferCapacity {
		r.dropped++
		return
	}
	r.used += len(r.prefix) + 1
	r.lines = append(r.lines, logLine{at: at, message: message})
}
