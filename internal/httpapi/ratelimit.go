package httpapi

import (
	"container/list"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultAPIRateLimit is the requests per minute each API token may make.
const DefaultAPIRateLimit = 600

// maxTokenBuckets bounds the limiter's memory. Tokens are few (people make
// them by hand); when the bound is hit, the least recently used bucket goes.
const maxTokenBuckets = 4096

// ParseAPIRateLimit reads PIER_API_RATE_LIMIT: empty means the default,
// 0 disables the limit, anything else must be a positive integer.
func ParseAPIRateLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultAPIRateLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0, fmt.Errorf("PIER_API_RATE_LIMIT must be a non-negative integer (requests per minute), got %q", raw)
	}
	return limit, nil
}

// tokenBuckets is a token bucket per API token: a full minute of requests
// may come in a burst, then they refill at limit/60 per second. A request
// costs one token however long it lasts, so a log stream counts once.
type tokenBuckets struct {
	capacity float64
	perSec   float64
	mu       sync.Mutex
	buckets  map[string]*list.Element
	recency  *list.List // front: most recently used
	now      func() time.Time
}

type bucket struct {
	key     string
	tokens  float64
	updated time.Time
}

// newTokenBuckets returns nil for a limit of 0: no limit at all.
func newTokenBuckets(perMinute int) *tokenBuckets {
	if perMinute <= 0 {
		return nil
	}
	return &tokenBuckets{
		capacity: float64(perMinute), perSec: float64(perMinute) / 60,
		buckets: map[string]*list.Element{}, recency: list.New(), now: time.Now,
	}
}

// take spends one token of key. When none is left it returns false and how
// long until the next one.
func (b *tokenBuckets) take(key string) (bool, time.Duration) {
	if b == nil {
		return true, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	current := b.bucketFor(key, now)
	elapsed := now.Sub(current.updated).Seconds()
	if elapsed > 0 {
		current.tokens = math.Min(b.capacity, current.tokens+elapsed*b.perSec)
		current.updated = now
	}
	if current.tokens >= 1 {
		current.tokens--
		return true, 0
	}
	wait := time.Duration((1 - current.tokens) / b.perSec * float64(time.Second))
	return false, wait
}

func (b *tokenBuckets) bucketFor(key string, now time.Time) *bucket {
	if element, found := b.buckets[key]; found {
		b.recency.MoveToFront(element)
		return element.Value.(*bucket)
	}
	if b.recency.Len() >= maxTokenBuckets {
		oldest := b.recency.Back()
		b.recency.Remove(oldest)
		delete(b.buckets, oldest.Value.(*bucket).key)
	}
	fresh := &bucket{key: key, tokens: b.capacity, updated: now}
	b.buckets[key] = b.recency.PushFront(fresh)
	return fresh
}

func (b *tokenBuckets) size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buckets)
}

// retryAfterSeconds rounds the wait up: a client that waits what the header
// says always finds a token.
func retryAfterSeconds(wait time.Duration) int {
	return max(1, int(math.Ceil(wait.Seconds())))
}
