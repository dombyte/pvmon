package health_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/health/mocks"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

const (
	sweep = 10 * time.Second
	grace = 25 * time.Second
)

type harness struct {
	clock  *clocktest.Clock
	sup    *health.Supervisor
	ctx    context.Context
	cancel context.CancelFunc
	done   chan error
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	clock := clocktest.New(t0)
	root, cancelCause := context.WithCancelCause(context.Background())
	sup, err := health.NewSupervisor(clock, sweep, cancelCause, zerolog.Nop())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(root)
	return &harness{clock: clock, sup: sup, ctx: ctx, cancel: cancel, done: make(chan error, 1)}
}

func (h *harness) run() {
	go func() { h.done <- h.sup.Run(h.ctx) }()
	h.clock.BlockUntil(1) // ticker created
}

// tick advances one sweep and waits until the supervisor finished it.
func (h *harness) tick(t *testing.T) {
	t.Helper()
	before := h.sup.Snapshot().At
	h.clock.Advance(sweep)
	require.Eventually(t, func() bool { return h.sup.Snapshot().At.After(before) },
		time.Second, time.Millisecond)
}

// fake is the observable state of a mock component, shared with the supervisor goroutine.
type fake struct {
	failed  atomic.Bool
	silent  atomic.Bool
	lastNow atomic.Int64
}

// component returns a mock whose beat follows the clock unless silent.
func component(t *testing.T, h *harness, f *fake) *mocks.MockComponent {
	t.Helper()
	c := mocks.NewMockComponent(t)
	c.EXPECT().Start(mock.Anything).Return(nil).Maybe()
	c.EXPECT().Stop().Return(nil).Maybe()
	c.EXPECT().State().RunAndReturn(func() health.State {
		if f.failed.Load() {
			return health.Failed
		}
		return health.Healthy
	}).Maybe()
	c.EXPECT().LastBeat().RunAndReturn(func() time.Time {
		if !f.silent.Load() {
			f.lastNow.Store(h.clock.Now().UnixNano())
		}
		return time.Unix(0, f.lastNow.Load())
	}).Maybe()
	return c
}

func TestRestartAndBudget(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	f := &fake{}
	f.lastNow.Store(t0.UnixNano())
	var builds atomic.Int32
	h.sup.Add("collector:lan", func() (health.Component, error) {
		builds.Add(1)
		return component(t, h, f), nil
	}, grace)
	h.run()
	h.tick(t)
	assert.Equal(t, int32(1), builds.Load())

	f.failed.Store(true)
	h.tick(t)
	assert.Equal(t, int32(2), builds.Load(), "failed component restarted")
	assert.Equal(t, 1, h.sup.Snapshot().Components[0].Restarts)

	f.failed.Store(false)
	for range 3 { // kept beating for the grace: budget reset
		h.tick(t)
	}
	assert.Equal(t, 0, h.sup.Snapshot().Components[0].Restarts)

	f.silent.Store(true) // no beat: restarted after the grace until the budget is gone
	for range 6 {
		h.tick(t)
	}
	assert.True(t, h.sup.Snapshot().Fatal)
	h.cancel()
	require.NoError(t, <-h.done)
	assert.Equal(t, int32(5), builds.Load(), "initial + 1 + 3 restarts")
}

func TestStartErrorIsFatal(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.sup.Add("broken", func() (health.Component, error) { return nil, errors.New("nope") },
		grace)
	h.sup.DeviceState("wr1", health.DeviceOffline)
	poll := health.Poll{At: time.Unix(5, 0), Took: 240 * time.Millisecond, OK: true}
	h.sup.DevicePoll("wr1", poll)
	go func() { h.done <- h.sup.Run(h.ctx) }()
	require.NoError(t, <-h.done, "root context cancelled by the fatal error")
	snap := h.sup.Snapshot()
	assert.True(t, snap.Fatal)
	assert.Equal(t, "failed", snap.Components[0].State)
	assert.Equal(t, health.DeviceOffline, snap.Devices["wr1"])
	assert.Equal(t, poll, snap.Polls["wr1"])
	assert.Equal(t, "recovering", health.Recovering.String())

	c := mocks.NewMockComponent(t)
	c.EXPECT().Start(mock.Anything).Return(errors.New("start failed"))
	c.EXPECT().Stop().Return(nil)
	h2 := newHarness(t)
	h2.sup.Add("c", func() (health.Component, error) { return c, nil }, grace)
	go func() { h2.done <- h2.sup.Run(h2.ctx) }()
	require.NoError(t, <-h2.done)
	assert.True(t, h2.sup.Snapshot().Fatal)
}

func TestRestartStopTimeoutIsFatal(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	release := make(chan struct{})
	c := mocks.NewMockComponent(t)
	c.EXPECT().Start(mock.Anything).Return(nil)
	c.EXPECT().State().Return(health.Failed)
	c.EXPECT().LastBeat().Return(t0).Maybe()
	c.EXPECT().Stop().RunAndReturn(func() error { <-release; return nil })
	h.sup.Add("stuck", func() (health.Component, error) { return c, nil }, grace)
	h.run()
	h.clock.Advance(sweep)
	h.clock.BlockUntil(2) // ticker + stop timer
	h.clock.Advance(health.StopTimeout)
	require.Eventually(t, func() bool { return h.sup.Snapshot().Fatal }, time.Second,
		time.Millisecond)
	close(release)
	require.NoError(t, <-h.done, "the released component stops cleanly on shutdown")
}

func TestStopAllTimeout(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	release := make(chan struct{})
	c := mocks.NewMockComponent(t)
	c.EXPECT().Start(mock.Anything).Return(nil)
	c.EXPECT().Stop().RunAndReturn(func() error { <-release; return errors.New("x") }).Maybe()
	h.sup.Add("slow", func() (health.Component, error) { return c, nil }, grace)
	h.run()
	h.cancel()
	h.clock.BlockUntil(2) // ticker + stop timer
	h.clock.Advance(health.StopTimeout)
	require.ErrorContains(t, <-h.done, "stop exceeded")
	close(release)

	_, err := health.NewSupervisor(nil, sweep, nil, zerolog.Nop())
	require.Error(t, err)
	h.sup.Fail(errors.New("storage down"))
	assert.True(t, h.sup.Snapshot().Fatal)
}

func TestStopError(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := mocks.NewMockComponent(t)
	c.EXPECT().Start(mock.Anything).Return(nil)
	c.EXPECT().Stop().Return(errors.New("not clean"))
	h.sup.Add("c", func() (health.Component, error) { return c, nil }, grace)
	h.run()
	h.cancel()
	require.NoError(t, <-h.done, "a Stop error is logged, not fatal")
}
