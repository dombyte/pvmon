// Package eventbus is the typed in-process event bus through which background components
// meet (AGENTS.md section 4). Publish never blocks; each subscriber has a bounded buffer
// and its own overflow policy.
package eventbus

import (
	"sync"
	"sync/atomic"
	"time"
)

// Event is any published event; the concrete types are defined below.
type Event interface{ event() }

// ValueChanged reports a new live value of a key ("<meter>/<channel>").
type ValueChanged struct {
	Key string
	At  time.Time
}

// DeviceState reports a device health change (online, offline, stale, unconfigured).
type DeviceState struct {
	Device string
	State  string
}

// StatusChanged reports a changed device status register.
type StatusChanged struct {
	Device   string
	Register string
	Code     uint64
	Level    string
	At       time.Time
}

// SiteChanged reports a new configuration revision; Keys are the live keys of the new
// site (spec 9.3 site_changed).
type SiteChanged struct {
	Rev  int64
	Keys []string
}

func (ValueChanged) event()  {}
func (SiteChanged) event()   {}
func (DeviceState) event()   {}
func (StatusChanged) event() {}

// Overflow decides what happens when a subscriber's buffer is full.
type Overflow int

// Overflow policies.
const (
	DropNewest Overflow = iota // keep what is queued, drop the new event
	DropOldest                 // drop the oldest queued event to make room
)

// Subscription receives events on C until Unsubscribe; the bus closes C then.
type Subscription struct {
	ch      chan Event
	policy  Overflow
	dropped atomic.Uint64
}

// C returns the receive channel.
func (s *Subscription) C() <-chan Event { return s.ch }

// Dropped returns the number of events lost to overflow.
func (s *Subscription) Dropped() uint64 { return s.dropped.Load() }

// Bus fans events out to subscribers.
type Bus struct {
	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

// New returns an empty bus.
func New() *Bus { return &Bus{subs: map[*Subscription]struct{}{}} }

// Subscribe registers a subscriber with a buffer of size (at least 1).
func (b *Bus) Subscribe(size int, policy Overflow) *Subscription {
	s := &Subscription{ch: make(chan Event, max(size, 1)), policy: policy}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Unsubscribe removes s and closes its channel (the bus is the sender, so it closes).
func (b *Bus) Unsubscribe(s *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		close(s.ch)
	}
}

// Publish delivers e to every subscriber without blocking.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		s.offer(e)
	}
}

// offer runs under the bus lock, so no other sender touches s.ch concurrently.
func (s *Subscription) offer(e Event) {
	select {
	case s.ch <- e:
		return
	default:
	}
	s.dropped.Add(1)
	if s.policy == DropNewest {
		return
	}
	select {
	case <-s.ch:
	default:
	}
	select {
	case s.ch <- e:
	default:
	}
}
