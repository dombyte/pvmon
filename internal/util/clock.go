// Package util holds small dependency-free helpers shared by all packages: the injected
// Clock, dependency checks, the Slot container and numeric helpers.
package util

import (
	"context"
	"time"
)

// Midnight returns the start of the local day of t in loc.
func Midnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// Clock is the only source of time in the pipeline. Production code gets the real clock,
// tests a clocktest.Clock that they advance by hand.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
	NewTicker(d time.Duration) Ticker
}

// Timer mirrors time.Timer behind an interface.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// Ticker mirrors time.Ticker behind an interface.
type Ticker interface {
	C() <-chan time.Time
	Stop()
	Reset(d time.Duration)
}

type realClock struct{}

// NewRealClock returns the Clock backed by the system time.
func NewRealClock() Clock { return realClock{} }

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

func (realClock) NewTicker(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time        { return r.t.C }
func (r realTimer) Stop() bool                 { return r.t.Stop() }
func (r realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time   { return r.t.C }
func (r realTicker) Stop()                 { r.t.Stop() }
func (r realTicker) Reset(d time.Duration) { r.t.Reset(d) }

// Sleep waits for d on clock c or returns ctx's error when ctx ends first.
func Sleep(ctx context.Context, c Clock, d time.Duration) error {
	t := c.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C():
		return nil
	}
}
