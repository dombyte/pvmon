package util

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlot(t *testing.T) {
	t.Parallel()
	var s Slot[string]
	_, ok := s.Load()
	assert.False(t, ok)
	s.ClearIf(func(string) bool { return true })
	s.Store("new")
	s.ClearIf(func(v string) bool { return v == "old" })
	v, ok := s.Load()
	require.True(t, ok, "a stale owner never clears its successor")
	assert.Equal(t, "new", v)
	s.ClearIf(func(v string) bool { return v == "new" })
	_, ok = s.Load()
	assert.False(t, ok)
}

type iface interface{ M() }

type impl struct{}

func (*impl) M() {}

func TestRequireAll(t *testing.T) {
	t.Parallel()
	var nilPtr *impl
	var nilIface iface = nilPtr
	var nilMap map[string]int
	require.NoError(t, RequireAll("c", D("a", &impl{}), D("n", 3)))
	err := RequireAll("comp", D("a", nil), D("b", nilIface), D("c", nilMap), D("d", 1))
	var de *DependencyError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, []string{"a", "b", "c"}, de.Missing)
	assert.Equal(t, "comp: missing dependencies: a, b, c", err.Error())
}

func TestMath(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, 1.24, Round(1.235, 2), 1e-9)
	assert.InDelta(t, -1.24, Round(-1.235, 2), 1e-9)
	assert.InDelta(t, 2.0, Clamp(3, 0, 2), 0)
	assert.InDelta(t, 0.0, Clamp(-1, 0, 2), 0)
	assert.True(t, AlmostEqual(1, 1.005, 0.01))
	assert.False(t, AlmostEqual(1, 1.02, 0.01))
}

func TestRealClock(t *testing.T) {
	t.Parallel()
	c := NewRealClock()
	assert.WithinDuration(t, time.Now(), c.Now(), time.Second)
	tm := c.NewTimer(time.Millisecond)
	<-tm.C()
	assert.False(t, tm.Stop())
	tm.Reset(time.Millisecond)
	<-tm.C()
	tk := c.NewTicker(time.Millisecond)
	<-tk.C()
	tk.Reset(time.Millisecond)
	<-tk.C()
	tk.Stop()
}

func TestSleep(t *testing.T) {
	t.Parallel()
	c := NewRealClock()
	require.NoError(t, Sleep(context.Background(), c, time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, Sleep(ctx, c, time.Hour), context.Canceled)
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := Backoff{Min: time.Second, Max: 4 * time.Second}
	assert.True(t, b.Ready(now))
	assert.Equal(t, time.Second, b.Fail(now))
	assert.False(t, b.Ready(now))
	assert.True(t, b.Ready(now.Add(time.Second)))
	assert.Equal(t, now.Add(time.Second), b.Next())
	assert.Equal(t, 2*time.Second, b.Fail(now))
	assert.Equal(t, 4*time.Second, b.Fail(now))
	assert.Equal(t, 4*time.Second, b.Fail(now))
	b.Reset()
	assert.True(t, b.Ready(now))
}

func TestBackoffGrace(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	b := Backoff{Min: time.Second, Max: 4 * time.Second, Grace: 1}
	assert.Equal(t, time.Duration(0), b.Fail(now), "first failure: no delay")
	assert.True(t, b.Ready(now))
	assert.Equal(t, time.Second, b.Fail(now))
	assert.False(t, b.Ready(now))
	b.Reset()
	assert.Equal(t, time.Duration(0), b.Fail(now), "grace again after a success")
}

func TestMidnight(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	// 22:30 UTC on the DST day (2026-03-29, 23 h) is already 00:30 of the next local day
	got := Midnight(time.Date(2026, 3, 29, 22, 30, 0, 0, time.UTC), loc)
	assert.Equal(t, time.Date(2026, 3, 30, 0, 0, 0, 0, loc), got)
	got = Midnight(time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC), loc)
	assert.Equal(t, time.Date(2026, 3, 29, 0, 0, 0, 0, loc), got)
}
