// Package sitecalc computes the live site quantities (spec 5.5) from the live cache: on a
// change of a meter value, debounced, and on a refresh tick so missing inputs remove the
// quantity. Results go back into the live cache; they are never stored.
package sitecalc

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util"
)

// Timing (spec 5 step 7).
const (
	Debounce = 250 * time.Millisecond
	Refresh  = time.Second
	busSize  = 256
)

// Cache is the part of the live cache the calculator reads and writes.
type Cache interface {
	Get(key string) (livecache.Value, bool)
	Set(key string, v livecache.Value, interval time.Duration)
	Delete(keys ...string)
}

// Bus is the part of the event bus the calculator subscribes to.
type Bus interface {
	Subscribe(size int, policy eventbus.Overflow) *eventbus.Subscription
	Unsubscribe(s *eventbus.Subscription)
}

// Calculator is a restartable component (spec 8.2).
type Calculator struct {
	plan  plan
	cache Cache
	bus   Bus
	clock util.Clock

	beat   atomic.Int64
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New builds a calculator for the site s.
func New(s *site.Site, cache Cache, bus Bus, clock util.Clock) (*Calculator, error) {
	if err := util.RequireAll("sitecalc", util.D("site", s), util.D("cache", cache),
		util.D("bus", bus), util.D("clock", clock)); err != nil {
		return nil, err
	}
	return &Calculator{plan: newPlan(s), cache: cache, bus: bus, clock: clock}, nil
}

// Start runs the loop until ctx ends or Stop.
func (c *Calculator) Start(ctx context.Context) error {
	c.beat.Store(c.clock.Now().UnixNano())
	ctx, c.cancel = context.WithCancel(ctx)
	sub := c.bus.Subscribe(busSize, eventbus.DropOldest)
	c.wg.Add(1)
	go c.run(ctx, sub)
	return nil
}

func (c *Calculator) run(ctx context.Context, sub *eventbus.Subscription) {
	defer c.wg.Done()
	defer c.bus.Unsubscribe(sub)
	refresh := c.clock.NewTicker(Refresh)
	defer refresh.Stop()
	debounce := c.clock.NewTimer(Debounce)
	debounce.Stop()
	pending := false
	for {
		c.beat.Store(c.clock.Now().UnixNano())
		select {
		case <-ctx.Done():
			return
		case e := <-sub.C():
			pending = c.arm(e, pending, debounce)
		case <-debounce.C():
			pending = false
			c.update()
		case <-refresh.C():
			c.update()
		}
	}
}

// arm starts the debounce on the first meter change while none is pending.
func (c *Calculator) arm(e eventbus.Event, pending bool, debounce util.Timer) bool {
	if pending || !isMeterChange(e) {
		return pending
	}
	debounce.Reset(Debounce)
	return true
}

// isMeterChange ignores the calculator's own outputs, which would otherwise retrigger it.
func isMeterChange(e eventbus.Event) bool {
	v, ok := e.(eventbus.ValueChanged)
	return ok && !strings.HasPrefix(v.Key, "site/")
}

// update writes every computed quantity and removes the missing ones.
func (c *Calculator) update() {
	now := c.clock.Now()
	res := c.plan.compute(func(key string) (float64, bool) {
		v, ok := c.cache.Get(key)
		return v.Num, ok
	})
	var missing []string
	for _, k := range c.plan.keys {
		r, ok := res[k]
		if !ok {
			missing = append(missing, k)
			continue
		}
		c.cache.Set(k, livecache.Value{Num: r.Value, Unit: r.Unit, Approx: r.Approx, At: now},
			Refresh)
	}
	if len(missing) > 0 {
		c.cache.Delete(missing...)
	}
}

// Stop ends the loop and waits for it; idempotent.
func (c *Calculator) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	return nil
}

// State is always Healthy: missing inputs are data, not a failure.
func (c *Calculator) State() health.State { return health.Healthy }

// LastBeat returns the time of the last loop iteration.
func (c *Calculator) LastBeat() time.Time { return time.Unix(0, c.beat.Load()) }
