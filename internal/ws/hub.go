// Package ws serves live values over WebSocket (spec 9.3): subscriptions per client, a
// per-client diff against what was sent, pushes coalesced into one frame per tick.
package ws

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util"
)

// recheck: how often every subscribed key is compared, to report expired values.
const (
	recheck = time.Second
	busSize = 1024
)

// errFull rejects the connection above MaxClients.
var errFull = errors.New("ws: too many clients")

// Bus is the part of the event bus the hub subscribes to.
type Bus interface {
	Subscribe(size int, policy eventbus.Overflow) *eventbus.Subscription
	Unsubscribe(s *eventbus.Subscription)
}

// Hub is the restartable component that owns all clients. The HTTP handler reaches the
// current instance through a util.Slot, because a restart replaces it.
type Hub struct {
	known atomic.Pointer[map[string]bool] // replaced on site_changed
	cache Cache
	bus   Bus
	clock util.Clock
	slot  *util.Slot[*Hub]

	mu      sync.Mutex
	clients map[*client]bool

	beat   atomic.Int64
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewHub builds a hub for the live keys of site s and publishes it in slot on Start.
func NewHub(s *site.Site, cache Cache, bus Bus, clock util.Clock, slot *util.Slot[*Hub],
) (*Hub, error) {
	if err := util.RequireAll("ws", util.D("site", s), util.D("cache", cache),
		util.D("bus", bus), util.D("clock", clock), util.D("slot", slot)); err != nil {
		return nil, err
	}
	h := &Hub{
		cache: cache, bus: bus, clock: clock, slot: slot, clients: map[*client]bool{},
	}
	h.setKnown(s.LiveKeys())
	return h, nil
}

// Start runs the hub loop and publishes the hub for new connections.
func (h *Hub) Start(ctx context.Context) error {
	h.beat.Store(h.clock.Now().UnixNano())
	ctx, h.cancel = context.WithCancel(ctx)
	sub := h.bus.Subscribe(busSize, eventbus.DropOldest)
	h.wg.Add(1)
	go h.run(ctx, sub)
	h.slot.Store(h)
	return nil
}

func (h *Hub) run(ctx context.Context, sub *eventbus.Subscription) {
	defer h.wg.Done()
	defer h.bus.Unsubscribe(sub)
	flush := h.clock.NewTicker(coalesceTick)
	defer flush.Stop()
	check := h.clock.NewTicker(recheck)
	defer check.Stop()
	for {
		h.beat.Store(h.clock.Now().UnixNano())
		select {
		case <-ctx.Done():
			return
		case e := <-sub.C():
			h.dispatch(e)
		case <-flush.C():
			now := h.clock.Now()
			h.each(func(c *client) { c.flush(h.cache, now) })
		case <-check.C():
			h.each((*client).markAll)
		}
	}
}

// dispatch marks changed keys and forwards device and status events immediately.
func (h *Hub) dispatch(e eventbus.Event) {
	switch ev := e.(type) {
	case eventbus.ValueChanged:
		h.each(func(c *client) { c.markDirty(ev.Key) })
	case eventbus.DeviceState:
		msg := deviceStateMsg{Type: typeDeviceState, Device: ev.Device, State: ev.State}
		h.each(func(c *client) { c.send(msg) })
	case eventbus.StatusChanged:
		msg := statusMsg{
			Type: typeStatus, Device: ev.Device, Register: ev.Register,
			Code: ev.Code, Level: ev.Level,
		}
		h.each(func(c *client) { c.send(msg) })
	case eventbus.SiteChanged:
		h.setKnown(ev.Keys)
		msg := siteChangedMsg{Type: typeSiteChanged, ConfigRev: ev.Rev}
		h.each(func(c *client) { c.send(msg) })
	}
}

func (h *Hub) each(f func(*client)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		f(c)
	}
}

func (h *Hub) register(c *client) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) >= MaxClients {
		return errFull
	}
	h.clients[c] = true
	return nil
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func (h *Hub) isKnown(key string) bool { return (*h.known.Load())[key] }

func (h *Hub) setKnown(keys []string) {
	known := make(map[string]bool, len(keys))
	for _, k := range keys {
		known[k] = true
	}
	h.known.Store(&known)
}

// Stop withdraws the hub from the slot, disconnects every client and waits for the loop.
func (h *Hub) Stop() error {
	h.slot.ClearIf(func(cur *Hub) bool { return cur == h })
	if h.cancel != nil {
		h.cancel()
	}
	h.wg.Wait()
	h.each(func(c *client) { c.kill() })
	return nil
}

// State is always Healthy: a broken client is that client's problem.
func (h *Hub) State() health.State { return health.Healthy }

// LastBeat returns the time of the last loop iteration.
func (h *Hub) LastBeat() time.Time { return time.Unix(0, h.beat.Load()) }
