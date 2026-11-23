package util

import "time"

// Backoff tracks exponential reconnect delays (min → max, doubling) on an injected clock.
// It is owned by one goroutine and not safe for concurrent use.
type Backoff struct {
	Min, Max time.Duration
	// Grace is the number of consecutive failures tolerated without a delay: a single
	// dropped request should not hold back the next one.
	Grace int
	fails int
	delay time.Duration
	next  time.Time
}

// Fail records a failure at now and returns the delay until the next attempt (0 while
// within the grace).
func (b *Backoff) Fail(now time.Time) time.Duration {
	b.fails++
	if b.fails <= b.Grace {
		return 0
	}
	if b.delay == 0 {
		b.delay = b.Min
	} else {
		b.delay = min(b.delay*2, b.Max) //nolint:mnd // doubling
	}
	b.next = now.Add(b.delay)
	return b.delay
}

// Ready reports whether an attempt is allowed at now.
func (b *Backoff) Ready(now time.Time) bool { return b.delay == 0 || !now.Before(b.next) }

// Next returns the time of the next allowed attempt.
func (b *Backoff) Next() time.Time { return b.next }

// Reset clears the backoff after a success.
func (b *Backoff) Reset() { b.delay, b.fails = 0, 0 }
