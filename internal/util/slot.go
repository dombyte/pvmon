package util

import "sync/atomic"

// Slot holds the current instance of a value that is swapped at runtime, for example a
// component rebuilt by the supervisor while readers keep going through the slot.
type Slot[T any] struct {
	p atomic.Pointer[T]
}

// Store publishes v as the current value.
func (s *Slot[T]) Store(v T) { s.p.Store(&v) }

// Load returns the current value and whether one is set.
func (s *Slot[T]) Load() (T, bool) {
	p := s.p.Load()
	if p == nil {
		var zero T
		return zero, false
	}
	return *p, true
}

// ClearIf removes the current value only while match reports it as the caller's own, so a
// late clear by a replaced instance never withdraws its successor.
func (s *Slot[T]) ClearIf(match func(T) bool) {
	for {
		p := s.p.Load()
		if p == nil || !match(*p) {
			return
		}
		if s.p.CompareAndSwap(p, nil) {
			return
		}
	}
}
