package gitprovider

import (
	"math/rand/v2"
	"time"
)

// RetryPolicy says when a failed report is tried again and when it is given
// up (dead letter).
type RetryPolicy struct {
	// MaxAttempts is the number of sends before a report is dead.
	MaxAttempts int
	// BaseDelay is the wait after the first failure; it doubles each time.
	BaseDelay time.Duration
	// MaxDelay caps the wait.
	MaxDelay time.Duration
	// Jitter returns a number in [0, 1); nil uses math/rand.
	Jitter func() float64
}

// DefaultRetryPolicy tries 8 times over at most about 32 minutes:
// 15 s, 30 s, 1 min, 2 min, 4 min, 8 min, 16 min (each with jitter).
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 8, BaseDelay: 15 * time.Second, MaxDelay: time.Hour}
}

func (p RetryPolicy) normalized() RetryPolicy {
	defaults := DefaultRetryPolicy()
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = defaults.MaxAttempts
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = defaults.BaseDelay
	}
	if p.MaxDelay < p.BaseDelay {
		p.MaxDelay = max(defaults.MaxDelay, p.BaseDelay)
	}
	if p.Jitter == nil {
		p.Jitter = rand.Float64
	}
	return p
}

// Delay is the wait after the given number of failed attempts (>= 1):
// exponential, capped, with "equal jitter" (half fixed, half random) so
// many reports failing together do not retry in lockstep.
func (p RetryPolicy) Delay(attempts int) time.Duration {
	p = p.normalized()
	delay := p.BaseDelay
	for range max(attempts-1, 0) {
		if delay >= p.MaxDelay/2 {
			delay = p.MaxDelay
			break
		}
		delay *= 2
	}
	delay = min(delay, p.MaxDelay)
	half := delay / 2
	return half + time.Duration(p.Jitter()*float64(delay-half))
}
