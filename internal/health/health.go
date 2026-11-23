// Package health is the single supervisor: it starts restartable components, watches
// their state and heartbeat, restarts them within a budget and cancels the root context
// with ErrHealthFatal when that is no longer possible (spec 8.2).
package health

import (
	"context"
	"errors"
	"time"
)

// ErrHealthFatal cancels the root context: budget exhausted, a Stop timed out or a
// non-restartable part failed.
var ErrHealthFatal = errors.New("health: fatal")

// State of a component.
type State int

// Component states.
const (
	Healthy State = iota
	Recovering
	Failed
)

func (s State) String() string { return [...]string{"healthy", "recovering", "failed"}[s] }

// Component is a restartable background component.
type Component interface {
	Start(ctx context.Context) error // error only for unrecoverable conditions
	Stop() error                     // idempotent, graceful, bounded
	State() State
	LastBeat() time.Time
}

// Factory builds a fresh component instance; restart = Stop → Factory → Start.
type Factory func() (Component, error)

// Device states (spec 8.2).
const (
	DeviceOnline       = "online"
	DeviceOffline      = "offline"
	DeviceStale        = "stale"
	DeviceUnconfigured = "unconfigured"
)

// Reporter receives device health from collectors.
type Reporter interface {
	DeviceState(device, state string)
	// DevicePoll reports the last poll of a device: when it started, how long it took from
	// the first request to the decoded sample, and whether it succeeded.
	DevicePoll(device string, poll Poll)
}

// Poll is the last poll of a device.
type Poll struct {
	At   time.Time
	Took time.Duration
	OK   bool
}

// Limits of the supervisor.
const (
	RestartBudget = 3
	StopTimeout   = 10 * time.Second
)
