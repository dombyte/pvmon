package ws

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util"
	"github.com/dombyte/pvmon/internal/util/clocktest"
	"github.com/dombyte/pvmon/profiles"
)

const (
	wait     = 5 * time.Second
	tick     = time.Millisecond
	interval = 10 * time.Second
)

func loadProfile(t *testing.T, id string) *profile.Loaded {
	t.Helper()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	p, err := l.Load(id, ".")
	require.NoError(t, err)
	return p
}

func testSite(t *testing.T) *site.Site {
	t.Helper()
	s, err := site.Build(site.Spec{
		Name: "Home",
		Connections: map[string]site.ConnectionSpec{
			"lan": {Address: "tcp://10.0.0.20", Timeout: 2 * time.Second},
		},
		Devices: map[string]site.DeviceSpec{
			"wr1": {
				Connection: "lan", Unit: 1, Profile: loadProfile(t, "solis/s6-hybrid"),
				Interval: 5 * time.Second, Rollover: "23:59",
			},
			"meter": {
				Address: "http://10.0.0.31", Profile: loadProfile(t, "shelly/pro-3em"),
				Interval: 2 * time.Second, Timeout: time.Second,
			},
		},
		Meters: map[string]site.MeterSpec{
			"pv":   {Device: "wr1", Source: "pv"},
			"grid": {Device: "meter", Source: "total", Critical: true},
		},
	})
	require.NoError(t, err)
	return s
}

type hubEnv struct {
	clock *clocktest.Clock
	bus   *eventbus.Bus
	cache *livecache.Cache
	slot  *util.Slot[*Hub]
	hub   *Hub
}

func newHubEnv(t *testing.T) *hubEnv {
	t.Helper()
	clock := clocktest.New(t0)
	bus := eventbus.New()
	cache, err := livecache.New(bus, clock)
	require.NoError(t, err)
	slot := &util.Slot[*Hub]{}
	hub, err := NewHub(testSite(t), cache, bus, clock, slot)
	require.NoError(t, err)
	return &hubEnv{clock: clock, bus: bus, cache: cache, slot: slot, hub: hub}
}

// start runs the hub and waits until its loop idles on both tickers.
func (e *hubEnv) start(t *testing.T) {
	t.Helper()
	require.NoError(t, e.hub.Start(context.Background()))
	t.Cleanup(func() { assert.NoError(t, e.hub.Stop()) })
	require.Eventually(t, func() bool { return e.clock.Waiters() == 2 }, wait, tick)
}

func (e *hubEnv) addClient(t *testing.T, keys ...string) (*client, *int) {
	t.Helper()
	c, kills := newTestClient()
	require.NoError(t, e.hub.register(c))
	c.subscribe(keys, e.hub.isKnown, e.cache)
	recv(t, c)
	return c, kills
}

// nextFrame waits for a queued frame; the hub loop runs in its own goroutine.
func nextFrame(t *testing.T, c *client) frame {
	t.Helper()
	require.Eventually(t, func() bool { return len(c.out) > 0 }, wait, tick)
	return recv(t, c)
}

func TestNewHubRejectsNilDependencies(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	s := testSite(t)
	tests := []struct {
		name  string
		build func() (*Hub, error)
	}{
		{"site", func() (*Hub, error) { return NewHub(nil, e.cache, e.bus, e.clock, e.slot) }},
		{"cache", func() (*Hub, error) { return NewHub(s, nil, e.bus, e.clock, e.slot) }},
		{"bus", func() (*Hub, error) { return NewHub(s, e.cache, nil, e.clock, e.slot) }},
		{"clock", func() (*Hub, error) { return NewHub(s, e.cache, e.bus, nil, e.slot) }},
		{"slot", func() (*Hub, error) { return NewHub(s, e.cache, e.bus, e.clock, nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, err := tt.build()
			require.Error(t, err)
			assert.Nil(t, h)
			assert.Contains(t, err.Error(), tt.name)
		})
	}
}

func TestHubLifecycle(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	assert.Equal(t, health.Healthy, e.hub.State())
	require.NoError(t, e.hub.Stop(), "stop before start")

	_, ok := e.slot.Load()
	assert.False(t, ok)
	e.start(t)
	cur, ok := e.slot.Load()
	require.True(t, ok)
	assert.Same(t, e.hub, cur)
	first := e.hub.LastBeat()
	assert.True(t, e.clock.Now().Equal(first))

	e.clock.Advance(coalesceTick)
	require.Eventually(t, func() bool { return e.hub.LastBeat().After(first) }, wait, tick)

	_, kills := e.addClient(t, "pv/power")
	require.NoError(t, e.hub.Stop())
	_, ok = e.slot.Load()
	assert.False(t, ok)
	assert.Equal(t, 1, *kills, "clients are disconnected")
	require.NoError(t, e.hub.Stop(), "idempotent")
}

func TestStopKeepsSuccessorInSlot(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	other := &Hub{}
	e.slot.Store(other)
	require.NoError(t, e.hub.Stop())
	cur, ok := e.slot.Load()
	require.True(t, ok)
	assert.Same(t, other, cur)
}

func TestRegisterLimit(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	var first *client
	for i := range MaxClients {
		c, _ := newTestClient()
		if i == 0 {
			first = c
		}
		require.NoError(t, e.hub.register(c))
	}
	extra, _ := newTestClient()
	require.ErrorIs(t, e.hub.register(extra), errFull)
	e.hub.unregister(first)
	require.NoError(t, e.hub.register(extra))
}

func TestValueChangedIsCoalescedAndPushed(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	e.start(t)
	c, _ := e.addClient(t, "pv/power", "grid/power")

	e.cache.Set("pv/power", livecache.Value{Num: 500, Unit: "W", At: e.clock.Now()}, interval)
	e.cache.Set("pv/power", livecache.Value{Num: 600, Unit: "W", At: e.clock.Now()}, interval)
	e.cache.Set("other/x", livecache.Value{Num: 1, At: e.clock.Now()}, interval)
	// the loop has consumed the events once the tick finds them
	require.Eventually(t, func() bool {
		e.hub.mu.Lock()
		defer e.hub.mu.Unlock()
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.dirty) == 1
	}, wait, tick)
	assertEmpty(t, c)

	e.clock.Advance(coalesceTick)
	u := nextFrame(t, c)
	assert.Equal(t, "update", u["type"])
	vals := u["values"].(map[string]any)
	require.Len(t, vals, 1)
	assert.InDelta(t, 600, vals["pv/power"].(map[string]any)["value"], 1e-9)
}

func TestDeviceStateAndStatusAreForwarded(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	e.start(t)
	c, _ := e.addClient(t)

	e.bus.Publish(eventbus.DeviceState{Device: "wr1", State: "offline"})
	f := nextFrame(t, c)
	assert.Equal(t, frame{"type": "device_state", "device": "wr1", "state": "offline"}, f)

	e.bus.Publish(eventbus.StatusChanged{
		Device: "wr1", Register: "status", Code: 3, Level: "warn", At: e.clock.Now(),
	})
	f = nextFrame(t, c)
	assert.Equal(t, frame{
		"type": "status", "device": "wr1", "register": "status", "code": float64(3),
		"level": "warn",
	}, f)
}

func TestExpiredValueIsReportedRemoved(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	e.start(t)
	e.cache.Set("pv/power", livecache.Value{Num: 500, Unit: "W", At: e.clock.Now()}, interval)
	c, _ := e.addClient(t, "pv/power")

	// older than 3 x interval at the next 1 s recheck; no event announces that
	e.clock.Advance(3*interval + time.Second)
	var removed []any
	require.Eventually(t, func() bool {
		select {
		case b := <-c.out:
			f := frame{}
			require.NoError(t, json.Unmarshal(b, &f))
			if r, ok := f["removed"].([]any); ok && len(r) > 0 {
				removed = r
				return true
			}
		default:
		}
		return false
	}, wait, tick)
	assert.Equal(t, []any{"pv/power"}, removed)
}

func TestSiteChangedReplacesKnownKeys(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	e.start(t)
	c, _ := e.addClient(t, "pv/power")

	e.bus.Publish(eventbus.SiteChanged{Rev: 7, Keys: []string{"newkey/power", "grid/power"}})
	f := nextFrame(t, c)
	assert.Equal(t, frame{"type": "site_changed", "config_rev": float64(7)}, f)

	c.subscribe([]string{"newkey/power", "pv/power"}, e.hub.isKnown, e.cache)
	var unknown []any
	require.Eventually(t, func() bool {
		for len(c.out) > 0 {
			if got := recv(t, c); got["code"] == "unknown_keys" {
				unknown, _ = got["keys"].([]any)
			}
		}
		return unknown != nil
	}, wait, tick)
	assert.Equal(t, []any{"pv/power"}, unknown, "only the removed key is unknown")
}
