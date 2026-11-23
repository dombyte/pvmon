package health_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/health/mocks"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

// rootHarness exposes the root context so tests can read the fatal cause.
type rootHarness struct {
	*harness
	root context.Context
}

func newRootHarness(t *testing.T) *rootHarness {
	t.Helper()
	clock := clocktest.New(t0)
	root, cancelCause := context.WithCancelCause(context.Background())
	sup, err := health.NewSupervisor(clock, sweep, cancelCause, zerolog.Nop())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(root)
	return &rootHarness{
		harness: &harness{
			clock: clock, sup: sup, ctx: ctx, cancel: cancel, done: make(chan error, 1),
		},
		root: root,
	}
}

func okFactory(c health.Component) health.Factory {
	return func() (health.Component, error) { return c, nil }
}

// tracked is a component whose Start and Stop must each be called once.
func tracked(t *testing.T) *mocks.MockComponent {
	t.Helper()
	c := mocks.NewMockComponent(t)
	c.EXPECT().Start(mock.Anything).Return(nil).Once()
	c.EXPECT().Stop().Return(nil).Once()
	c.EXPECT().State().Return(health.Healthy).Maybe()
	c.EXPECT().LastBeat().Return(t0).Maybe()
	return c
}

func TestSetBeforeRunOnlyRegisters(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := tracked(t) // Start only happens in Run
	h.sup.Set("a", okFactory(c), grace)
	assert.Equal(t, []string{"a"}, h.sup.Names())
	assert.Equal(t, "failed", h.sup.Snapshot().Components[0].State, "not started yet")
	h.run()
	h.tick(t)
	assert.Equal(t, "healthy", h.sup.Snapshot().Components[0].State)
	h.cancel()
	require.NoError(t, <-h.done)
}

func TestSetReplacesRunning(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	f := &fake{}
	f.lastNow.Store(t0.UnixNano())
	h.sup.Add("a", func() (health.Component, error) { return component(t, h, f), nil }, grace)
	h.run()
	f.failed.Store(true)
	h.tick(t)
	require.Equal(t, 1, h.sup.Snapshot().Components[0].Restarts)

	f.failed.Store(false)
	fresh := tracked(t)
	h.sup.Set("a", okFactory(fresh), grace)
	snap := h.sup.Snapshot()
	require.Len(t, snap.Components, 1)
	assert.Equal(t, 0, snap.Components[0].Restarts, "fresh restart budget")
	assert.Equal(t, "healthy", snap.Components[0].State)
	h.cancel()
	require.NoError(t, <-h.done)
}

func TestSetNewNameWhileRunning(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.run()
	b := tracked(t)
	h.sup.Set("b", okFactory(b), grace)
	assert.Equal(t, []string{"b"}, h.sup.Names())
	assert.Equal(t, "healthy", h.sup.Snapshot().Components[0].State)
	h.cancel()
	require.NoError(t, <-h.done)
}

func TestSetFactoryError(t *testing.T) {
	t.Parallel()
	h := newRootHarness(t)
	h.run()
	h.sup.Set("a", func() (health.Component, error) { return nil, errors.New("nope") }, grace)
	snap := h.sup.Snapshot()
	require.Len(t, snap.Components, 1)
	assert.Equal(t, "failed", snap.Components[0].State)
	assert.False(t, snap.Fatal, "a build error on Set is logged, the sweep retries")
	h.cancel()
	require.NoError(t, <-h.done)
}

// stuck returns a component whose Stop blocks until release is closed.
func stuck(t *testing.T, release chan struct{}) *mocks.MockComponent {
	t.Helper()
	c := mocks.NewMockComponent(t)
	c.EXPECT().Start(mock.Anything).Return(nil)
	c.EXPECT().State().Return(health.Healthy).Maybe()
	c.EXPECT().LastBeat().Return(t0).Maybe()
	c.EXPECT().Stop().RunAndReturn(func() error { <-release; return nil }).Maybe()
	return c
}

func TestSetAndRemoveStopTimeoutIsFatal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		op   func(*health.Supervisor)
	}{
		{"set", func(s *health.Supervisor) {
			s.Set("a", func() (health.Component, error) { return nil, errors.New("x") }, grace)
		}},
		{"remove", func(s *health.Supervisor) { s.Remove("a") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newRootHarness(t)
			release := make(chan struct{})
			h.sup.Add("a", okFactory(stuck(t, release)), grace)
			h.run()
			finished := make(chan struct{})
			go func() { tc.op(h.sup); close(finished) }()
			h.clock.BlockUntil(2) // ticker + stop timer
			h.clock.Advance(health.StopTimeout)
			<-finished
			assert.True(t, h.sup.Snapshot().Fatal)
			assert.ErrorIs(t, context.Cause(h.root), health.ErrHealthFatal)
			close(release)
			h.cancel()
			require.NoError(t, <-h.done)
		})
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.sup.Remove("ghost") // unknown: no-op
	h.sup.Add("a", okFactory(tracked(t)), grace)
	h.sup.Add("b", okFactory(component(t, h, &fake{})), grace)
	h.run()
	h.sup.Remove("a")
	h.sup.Remove("a")
	assert.Equal(t, []string{"b"}, h.sup.Names())
	snap := h.sup.Snapshot()
	require.Len(t, snap.Components, 1)
	assert.Equal(t, "b", snap.Components[0].Name)
	h.cancel()
	require.NoError(t, <-h.done)
}

func TestRemoveNeverStarted(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.sup.Add("a", okFactory(mocks.NewMockComponent(t)), grace)
	h.sup.Remove("a")
	assert.Empty(t, h.sup.Names())
}

func TestForgetDevice(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.sup.DeviceState("wr1", health.DeviceOffline)
	h.sup.DeviceState("wr2", health.DeviceOffline)
	h.sup.DevicePoll("wr1", health.Poll{OK: true})
	h.sup.DevicePoll("wr2", health.Poll{OK: true})
	h.sup.ForgetDevice("wr1")
	snap := h.sup.Snapshot()
	assert.NotContains(t, snap.Devices, "wr1")
	assert.NotContains(t, snap.Polls, "wr1")
	assert.Contains(t, snap.Devices, "wr2")
	assert.Contains(t, snap.Polls, "wr2")
}
