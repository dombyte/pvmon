// Package livecache keeps the newest value per live key ("<meter>/<channel>") and publishes
// a change event for every new value. Values older than 3 × their device interval are
// missing (spec 5.1).
package livecache

import (
	"sync"
	"time"

	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/util"
)

// staleFactor: a value older than staleFactor × interval is missing.
const staleFactor = 3

// Value is one live value.
type Value struct {
	Num    float64
	Text   string
	Unit   string
	Approx bool // estimated, e.g. a SoC averaged without capacities
	At     time.Time
}

// Publisher is the part of the event bus the cache needs.
type Publisher interface {
	Publish(e eventbus.Event)
}

type entry struct {
	v      Value
	maxAge time.Duration
}

// Cache is safe for concurrent use by collectors (writers) and readers.
type Cache struct {
	mu     sync.RWMutex
	values map[string]entry
	pub    Publisher
	clock  util.Clock
}

// New returns an empty cache.
func New(pub Publisher, clock util.Clock) (*Cache, error) {
	err := util.RequireAll("livecache", util.D("publisher", pub), util.D("clock", clock))
	if err != nil {
		return nil, err
	}
	return &Cache{values: map[string]entry{}, pub: pub, clock: clock}, nil
}

// Set stores v for key; interval is the device's poll interval. A change event is
// published when the value differs from the stored one.
func (c *Cache) Set(key string, v Value, interval time.Duration) {
	c.mu.Lock()
	old, had := c.values[key]
	c.values[key] = entry{v: v, maxAge: staleFactor * interval}
	c.mu.Unlock()
	if !had || old.v.Num != v.Num || old.v.Text != v.Text || old.v.Approx != v.Approx {
		c.pub.Publish(eventbus.ValueChanged{Key: key, At: v.At})
	}
}

// Get returns the value of key unless it is missing (unknown or too old).
func (c *Cache) Get(key string) (Value, bool) {
	c.mu.RLock()
	e, ok := c.values[key]
	c.mu.RUnlock()
	if !ok || c.clock.Now().Sub(e.v.At) > e.maxAge {
		return Value{}, false
	}
	return e.v, true
}

// Delete removes keys (meter removed by a config change).
func (c *Cache) Delete(keys ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		delete(c.values, k)
	}
}
