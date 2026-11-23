package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/livecache"
)

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

// mapCache is a plain in-memory Cache.
type mapCache map[string]livecache.Value

func (m mapCache) Get(key string) (livecache.Value, bool) {
	v, ok := m[key]
	return v, ok
}

type frame map[string]any

func recv(t *testing.T, c *client) frame {
	t.Helper()
	select {
	case b := <-c.out:
		var f frame
		require.NoError(t, json.Unmarshal(b, &f))
		return f
	default:
		require.Fail(t, "no frame queued")
		return nil
	}
}

func assertEmpty(t *testing.T, c *client) {
	t.Helper()
	assert.Empty(t, c.out)
}

func known(keys ...string) func(string) bool {
	return func(k string) bool {
		for _, x := range keys {
			if x == k {
				return true
			}
		}
		return false
	}
}

func newTestClient() (*client, *int) {
	kills := 0
	return newClient(func() { kills++ }), &kills
}

func TestSubscribeSnapshotAndUnknownKeys(t *testing.T) {
	t.Parallel()
	cache := mapCache{
		"pv/power":  {Num: 1234.567, Unit: "W", At: t0},
		"bat/state": {Text: "charging", At: t0},
		"bat/soc":   {Num: 50, Unit: "%", Approx: true, At: t0},
	}
	c, _ := newTestClient()
	c.subscribe([]string{"pv/power", "bat/state", "bat/soc", "grid/power", "nope", "nope2"},
		known("pv/power", "bat/state", "bat/soc", "grid/power"), cache)

	e := recv(t, c)
	assert.Equal(t, "error", e["type"])
	assert.Equal(t, "unknown_keys", e["code"])
	assert.Equal(t, []any{"nope", "nope2"}, e["keys"])

	s := recv(t, c)
	assert.Equal(t, "snapshot", s["type"])
	vals := s["values"].(map[string]any)
	assert.Len(t, vals, 3, "grid/power has no value and is left out")
	pv := vals["pv/power"].(map[string]any)
	assert.InDelta(t, 1234.57, pv["value"], 1e-9)
	assert.Equal(t, "W", pv["unit"])
	assert.Equal(t, t0.Format(time.RFC3339Nano), pv["ts"])
	assert.Equal(t, "charging", vals["bat/state"].(map[string]any)["value"])
	assert.Equal(t, true, vals["bat/soc"].(map[string]any)["approx"])
	assertEmpty(t, c)

	// the valid keys stay subscribed
	assert.True(t, c.subs["pv/power"])
	assert.False(t, c.subs["nope"])
}

func TestSubscribeWithoutUnknownKeysSendsOnlySnapshot(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient()
	c.subscribe([]string{"a"}, known("a"), mapCache{})
	s := recv(t, c)
	assert.Equal(t, "snapshot", s["type"])
	assert.Empty(t, s["values"])
	assertEmpty(t, c)
}

func TestMarkDirtyOnlySubscribed(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient()
	c.subscribe([]string{"a"}, known("a", "b"), mapCache{})
	c.markDirty("a")
	c.markDirty("b")
	assert.Equal(t, map[string]bool{"a": true}, c.dirty)
}

func TestFlushDiff(t *testing.T) {
	t.Parallel()
	cache := mapCache{"a": {Num: 100.001, Unit: "W", At: t0}, "b": {Num: 5, At: t0}}
	c, _ := newTestClient()
	c.subscribe([]string{"a", "b", "c"}, known("a", "b", "c"), cache)
	recv(t, c)

	// nothing dirty: nothing sent
	c.flush(cache, t0)
	assertEmpty(t, c)

	// change below JSON precision: nothing sent
	cache["a"] = livecache.Value{Num: 100.004, Unit: "W", At: t0}
	c.markDirty("a")
	c.flush(cache, t0)
	assertEmpty(t, c)

	// real change, new key and a vanished value
	now := t0.Add(time.Second)
	cache["a"] = livecache.Value{Num: 101, Unit: "W", At: now}
	cache["c"] = livecache.Value{Text: "x", At: now}
	delete(cache, "b")
	c.markAll()
	c.flush(cache, now)
	u := recv(t, c)
	assert.Equal(t, "update", u["type"])
	assert.Equal(t, now.Format(time.RFC3339Nano), u["ts"])
	vals := u["values"].(map[string]any)
	assert.Len(t, vals, 2)
	assert.InDelta(t, 101, vals["a"].(map[string]any)["value"], 1e-9)
	assert.Nil(t, vals["a"].(map[string]any)["ts"], "no per-value ts in updates")
	assert.Equal(t, "x", vals["c"].(map[string]any)["value"])
	assert.Equal(t, []any{"b"}, u["removed"])
	assertEmpty(t, c)

	// removed only once
	c.markAll()
	c.flush(cache, now)
	assertEmpty(t, c)
}

func TestFlushChunksAtMaxKeys(t *testing.T) {
	t.Parallel()
	cache := mapCache{}
	var keys []string
	for i := range MaxKeys + 10 {
		k := "k" + string(rune('A'+i/26)) + string(rune('a'+i%26))
		keys = append(keys, k)
		cache[k] = livecache.Value{Num: 1, At: t0}
	}
	c, _ := newTestClient()
	c.subscribe(keys, func(string) bool { return true }, mapCache{})
	recv(t, c)
	c.markAll()
	c.flush(cache, t0)
	first, second := recv(t, c), recv(t, c)
	assert.Len(t, first["values"], MaxKeys)
	assert.Len(t, second["values"], 10)
	assertEmpty(t, c)

	// removals share the budget
	clear(cache)
	c.markAll()
	c.flush(cache, t0)
	a, b := recv(t, c), recv(t, c)
	assert.Len(t, a["removed"], MaxKeys)
	assert.Len(t, b["removed"], 10)
}

func TestUnsubscribe(t *testing.T) {
	t.Parallel()
	cache := mapCache{"a": {Num: 1, At: t0}}
	c, _ := newTestClient()
	c.subscribe([]string{"a"}, known("a"), cache)
	recv(t, c)
	c.markDirty("a")
	c.unsubscribe([]string{"a"})
	cache["a"] = livecache.Value{Num: 2, At: t0}
	c.markDirty("a")
	c.flush(cache, t0)
	assertEmpty(t, c)
	assert.Empty(t, c.subs)
	assert.Empty(t, c.sent)
}

func TestSendKillsOnFullBuffer(t *testing.T) {
	t.Parallel()
	c, kills := newTestClient()
	for range sendBuffer {
		c.send(simpleMsg{Type: typePong})
	}
	assert.Zero(t, *kills)
	c.send(simpleMsg{Type: typePong})
	assert.Equal(t, 1, *kills)
}

func TestSendDropsUnmarshalableFrame(t *testing.T) {
	t.Parallel()
	c, kills := newTestClient()
	c.send(make(chan int))
	assertEmpty(t, c)
	assert.Zero(t, *kills)
}

func TestWireAndSame(t *testing.T) {
	t.Parallel()
	v := livecache.Value{Num: 1.002, Unit: "W", At: t0}
	assert.False(t, same(v, livecache.Value{Num: 1.002, Unit: "kW"}))
	assert.False(t, same(v, livecache.Value{Num: 1.002, Unit: "W", Approx: true}))
	assert.False(t, same(v, livecache.Value{Num: 1.002, Unit: "W", Text: "x"}))
	assert.True(t, same(v, livecache.Value{Num: 1.001, Unit: "W"}))
	assert.Nil(t, wire(v, false).TS)
	require.NotNil(t, wire(v, true).TS)
}
