// Package clocktest provides a manually driven util.Clock for tests: time only moves when
// the test calls Advance or Set, so polling, backoff and debounce run without sleeps.
package clocktest

import (
	"sort"
	"sync"
	"time"

	"github.com/dombyte/pvmon/internal/util"
)

// Clock is a fake util.Clock. The zero value is not usable; use New.
type Clock struct {
	mu      sync.Mutex
	cond    *sync.Cond
	now     time.Time
	waiters []*waiter
}

type waiter struct {
	clock  *Clock
	ch     chan time.Time
	at     time.Time
	period time.Duration // > 0 for tickers
	active bool
}

// New returns a fake clock standing at start.
func New(start time.Time) *Clock {
	c := &Clock{now: start}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// Now returns the fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer returns a timer that fires once the clock passed now+d.
func (c *Clock) NewTimer(d time.Duration) util.Timer {
	return timer{c.add(d, 0)}
}

// NewTicker returns a ticker firing every d of fake time.
func (c *Clock) NewTicker(d time.Duration) util.Ticker {
	return ticker{c.add(d, d)}
}

func (c *Clock) add(d, period time.Duration) *waiter {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := &waiter{clock: c, ch: make(chan time.Time, 1), at: c.now.Add(d), period: period}
	c.activate(w)
	return w
}

// activate must be called with c.mu held.
func (c *Clock) activate(w *waiter) {
	if !w.active {
		w.active = true
		c.waiters = append(c.waiters, w)
	}
	c.cond.Broadcast()
}

// deactivate must be called with c.mu held; it reports whether w was active.
func (c *Clock) deactivate(w *waiter) bool {
	if !w.active {
		return false
	}
	w.active = false
	for i, x := range c.waiters {
		if x == w {
			c.waiters = append(c.waiters[:i], c.waiters[i+1:]...)
			break
		}
	}
	return true
}

// Advance moves the clock forward by d and fires every timer and ticker that falls due, in
// time order. Tickers fire once per period passed; like time.Ticker, ticks are dropped
// while the channel is full.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	end := c.now.Add(d)
	for {
		w := c.nextDue(end)
		if w == nil {
			break
		}
		c.now = w.at
		select {
		case w.ch <- w.at:
		default:
		}
		if w.period > 0 {
			w.at = w.at.Add(w.period)
		} else {
			c.deactivate(w)
		}
	}
	c.now = end
}

// nextDue returns the earliest active waiter due at or before end; c.mu held.
func (c *Clock) nextDue(end time.Time) *waiter {
	sort.SliceStable(c.waiters, func(i, j int) bool {
		return c.waiters[i].at.Before(c.waiters[j].at)
	})
	if len(c.waiters) == 0 || c.waiters[0].at.After(end) {
		return nil
	}
	return c.waiters[0]
}

// Set moves the clock to t (forward only; an earlier t only sets the time, as a clock jump).
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	d := t.Sub(c.now)
	if d < 0 {
		c.now = t
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.Advance(d)
}

// Waiters returns the number of active timers and tickers.
func (c *Clock) Waiters() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// BlockUntil blocks until at least n timers or tickers are active, so a test can wait for
// a goroutine to reach its next wait before advancing the clock.
func (c *Clock) BlockUntil(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.waiters) < n {
		c.cond.Wait()
	}
}

type timer struct{ *waiter }

func (t timer) C() <-chan time.Time        { return t.ch }
func (t timer) Stop() bool                 { return t.stop() }
func (t timer) Reset(d time.Duration) bool { return t.reset(d) }

type ticker struct{ *waiter }

func (t ticker) C() <-chan time.Time   { return t.ch }
func (t ticker) Stop()                 { t.stop() }
func (t ticker) Reset(d time.Duration) { t.reset(d) }

func (w *waiter) stop() bool {
	w.clock.mu.Lock()
	defer w.clock.mu.Unlock()
	return w.clock.deactivate(w)
}

func (w *waiter) reset(d time.Duration) bool {
	w.clock.mu.Lock()
	defer w.clock.mu.Unlock()
	was := w.clock.deactivate(w)
	w.at = w.clock.now.Add(d)
	if w.period > 0 {
		w.period = d
	}
	w.clock.activate(w)
	return was
}
