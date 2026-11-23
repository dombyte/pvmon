package auth

import (
	"sync"
	"time"
)

// Brute-force limits (spec 10): after 5 failures per IP or account every further attempt
// waits 1 s, doubling per failure, at most 15 min.
const (
	freeFailures = 5
	firstDelay   = time.Second
	maxDelay     = 15 * time.Minute
)

// failures counts failed attempts of one key.
type failures struct {
	count int
	last  time.Time
	until time.Time // no attempt before this instant
}

// limiter tracks failed attempts per key ("ip:…", "user:…"); it owns its map.
type limiter struct {
	mu   sync.Mutex
	keys map[string]*failures
}

func newLimiter() *limiter { return &limiter{keys: map[string]*failures{}} }

// wait returns how long the caller must still wait before an attempt for any of keys.
func (l *limiter) wait(now time.Time, keys ...string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var d time.Duration
	for _, k := range keys {
		if f := l.keys[k]; f != nil && f.until.After(now) {
			d = max(d, f.until.Sub(now))
		}
	}
	return d
}

// fail records a failed attempt for every key.
func (l *limiter) fail(now time.Time, keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	for _, k := range keys {
		f := l.keys[k]
		if f == nil {
			f = &failures{}
			l.keys[k] = f
		}
		f.count++
		f.last = now
		if f.count >= freeFailures {
			f.until = now.Add(delay(f.count - freeFailures))
		}
	}
}

// delay is firstDelay doubled n times, capped at maxDelay.
func delay(n int) time.Duration {
	d := firstDelay
	for range n {
		if d >= maxDelay {
			break
		}
		d *= 2
	}
	return min(d, maxDelay)
}

// succeed forgets the failures of keys.
func (l *limiter) succeed(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.keys, k)
	}
}

// prune forgets keys whose last failure and block lie more than maxDelay in the past, so
// the map stays bounded by the attempts of the last 15 minutes.
func (l *limiter) prune(now time.Time) {
	for k, f := range l.keys {
		if now.Sub(f.last) > maxDelay && !f.until.After(now) {
			delete(l.keys, k)
		}
	}
}
