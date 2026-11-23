package ws

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/dombyte/pvmon/internal/livecache"
)

// Limits (spec 9.3).
const (
	MaxClients   = 256
	MaxKeys      = 256 // per frame, in both directions
	MaxMessage   = 1 << 20
	sendBuffer   = 32
	coalesceTick = 75 * time.Millisecond
)

// Cache is the part of the live cache the hub reads.
type Cache interface {
	Get(key string) (livecache.Value, bool)
}

// client is the protocol state of one connection, independent of the socket: subscribed
// keys, what was last sent per key and the keys changed since the last flush.
type client struct {
	out  chan []byte // drained by the connection's writer; never closed
	kill func()      // closes the connection (slow consumer)

	mu    sync.Mutex
	subs  map[string]bool
	sent  map[string]livecache.Value // absent = sent as missing (or never)
	dirty map[string]bool
}

func newClient(kill func()) *client {
	return &client{
		out: make(chan []byte, sendBuffer), kill: kill,
		subs: map[string]bool{}, sent: map[string]livecache.Value{}, dirty: map[string]bool{},
	}
}

// send queues a frame; a client that cannot keep up is disconnected instead of blocking.
func (c *client) send(msg any) {
	b, err := json.Marshal(msg)
	if err != nil {
		return // only plain structs are sent; Marshal cannot fail on them
	}
	select {
	case c.out <- b:
	default:
		c.kill()
	}
}

// subscribe adds keys and sends a snapshot of their present values; unknown keys are
// reported, never fatal.
func (c *client) subscribe(keys []string, known func(string) bool, cache Cache) {
	var unknown []string
	snap := map[string]value{}
	c.mu.Lock()
	for _, k := range keys {
		if !known(k) {
			unknown = append(unknown, k)
			continue
		}
		c.subs[k] = true
		delete(c.sent, k)
		if v, ok := cache.Get(k); ok {
			c.sent[k] = v
			snap[k] = wire(v, true)
		}
	}
	c.mu.Unlock()
	if len(unknown) > 0 {
		c.send(errorMsg{Type: typeError, Code: codeUnknownKeys, Keys: unknown})
	}
	c.send(snapshotMsg{Type: typeSnapshot, Values: snap})
}

func (c *client) unsubscribe(keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		delete(c.subs, k)
		delete(c.sent, k)
		delete(c.dirty, k)
	}
}

func (c *client) markDirty(key string) {
	c.mu.Lock()
	if c.subs[key] {
		c.dirty[key] = true
	}
	c.mu.Unlock()
}

// markAll rechecks every subscribed key, so values that expired without an event are
// reported as removed.
func (c *client) markAll() {
	c.mu.Lock()
	for k := range c.subs {
		c.dirty[k] = true
	}
	c.mu.Unlock()
}

// flush sends the changed keys as update frames of at most MaxKeys keys each.
func (c *client) flush(cache Cache, now time.Time) {
	values, removed := c.diff(cache)
	for len(values) > 0 || len(removed) > 0 {
		msg := updateMsg{Type: typeUpdate, TS: now, Values: map[string]value{}, Removed: []string{}}
		for len(values) > 0 && len(msg.Values) < MaxKeys {
			msg.Values[values[0].key] = wire(values[0].v, false)
			values = values[1:]
		}
		for len(removed) > 0 && len(msg.Values)+len(msg.Removed) < MaxKeys {
			msg.Removed = append(msg.Removed, removed[0])
			removed = removed[1:]
		}
		c.send(msg)
	}
}

type keyed struct {
	key string
	v   livecache.Value
}

// diff compares the dirty keys with what was sent and records the new state as sent.
func (c *client) diff(cache Cache) (values []keyed, removed []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.dirty {
		old, had := c.sent[k]
		v, ok := cache.Get(k)
		switch {
		case ok && (!had || !same(old, v)):
			c.sent[k] = v
			values = append(values, keyed{k, v})
		case !ok && had:
			delete(c.sent, k)
			removed = append(removed, k)
		}
	}
	clear(c.dirty)
	sort.Slice(values, func(i, j int) bool { return values[i].key < values[j].key })
	sort.Strings(removed)
	return values, removed
}
