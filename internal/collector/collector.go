package collector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// FlushInterval is the upsert interval of open buckets (spec 5.1).
const FlushInterval = time.Minute

// stopFlushTimeout bounds the final flush in Stop.
const stopFlushTimeout = 10 * time.Second

// LiveCache receives live values.
type LiveCache interface {
	Set(key string, v livecache.Value, interval time.Duration)
}

// Publisher receives status events.
type Publisher interface {
	Publish(e eventbus.Event)
}

// Transport is the connection lifecycle owned by the collector (Modbus connection; a
// no-op for HTTP devices, whose clients need none).
type Transport interface {
	Start()
	Stop()
}

// Settings selects the connection and its devices.
type Settings struct {
	Site       *site.Site
	Connection string            // connection address key
	Sources    map[string]Source // per device ID
	Location   *time.Location    // site time zone (TZ)
}

// Deps are the collector's dependencies.
type Deps struct {
	Store     storage.CollectorStore
	Cache     LiveCache
	Bus       Publisher
	Reporter  health.Reporter
	Transport Transport
	Clock     util.Clock
	Log       zerolog.Logger
}

// Collector polls the devices of one connection (health.Component).
type Collector struct {
	s     Settings
	d     Deps
	cache LiveCache
	acc   *Accumulator
	devs  []*deviceRun

	cancel    context.CancelFunc
	startCtx  context.Context
	wg        sync.WaitGroup
	stopOnce  sync.Once
	beat      atomic.Int64
	state     atomic.Int32
	nextFlush time.Time
}

// New builds a collector; nothing is read or written before Start.
func New(s Settings, d Deps) (*Collector, error) {
	err := util.RequireAll("collector", util.D("site", s.Site), util.D("location", s.Location),
		util.D("store", d.Store), util.D("cache", d.Cache), util.D("bus", d.Bus),
		util.D("reporter", d.Reporter), util.D("transport", d.Transport), util.D("clock", d.Clock))
	if err != nil {
		return nil, err
	}
	conn, ok := s.Site.Connections[s.Connection]
	if !ok {
		return nil, fmt.Errorf("collector: unknown connection %q", s.Connection)
	}
	for _, id := range conn.Devices {
		if s.Sources[id] == nil {
			return nil, fmt.Errorf("collector: no source for device %s", id)
		}
	}
	d.Log = d.Log.With().Str("component", "collector").Str("address", s.Connection).Logger()
	return &Collector{s: s, d: d, cache: d.Cache, acc: NewAccumulator()}, nil
}

// Start restores state and starts the polling loop.
func (c *Collector) Start(ctx context.Context) error {
	c.beat.Store(c.d.Clock.Now().UnixNano())
	if err := c.setup(ctx); err != nil {
		return err
	}
	c.d.Transport.Start()
	c.startCtx = ctx
	loopCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.wg.Add(1)
	go c.run(loopCtx)
	return nil
}

// setup ensures series and restores counter states of every device.
func (c *Collector) setup(ctx context.Context) error {
	now := c.d.Clock.Now()
	for _, id := range c.s.Site.Connections[c.s.Connection].Devices {
		dev := c.s.Site.Devices[id]
		pl := buildPlan(c.s.Site, dev)
		ids, err := c.d.Store.EnsureSeries(ctx, Slot(now), pl.defs)
		if err != nil {
			return fmt.Errorf("collector: %s: %w", id, err)
		}
		states, err := c.d.Store.CounterStates(ctx, seriesIDs(ids))
		if err != nil {
			return fmt.Errorf("collector: %s: %w", id, err)
		}
		last, err := c.d.Store.LastStatus(ctx, id)
		if err != nil {
			return fmt.Errorf("collector: %s: %w", id, err)
		}
		r := newDeviceRun(runInput{
			dev: dev, plan: pl, src: c.s.Sources[id], ids: ids,
			states: states, loc: c.s.Location, rollover: rollover(dev),
		})
		r.seedStatus(last)
		r.next, r.state = now, ""
		c.devs = append(c.devs, r)
	}
	c.nextFlush = flushTime(now)
	return nil
}

func seriesIDs(m map[storage.SeriesKey]int64) []int64 {
	out := make([]int64, 0, len(m))
	for _, id := range m {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// rollover parses the device's HH:MM rollover (validated by config) into a duration.
func rollover(d *site.Device) time.Duration {
	t, err := time.Parse("15:04", d.Rollover)
	if err != nil {
		return 0
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute
}

// flushTime is the next flush: in FlushInterval or at the next slot end.
func flushTime(now time.Time) time.Time {
	return minTime(now.Add(FlushInterval), SlotStart(Slot(now)+1))
}

// Stop ends the loop, flushes open buckets and stops the transport. It is idempotent.
func (c *Collector) Stop() error {
	var err error
	c.stopOnce.Do(func() {
		if c.cancel == nil {
			return
		}
		c.cancel()
		c.wg.Wait()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.startCtx), stopFlushTimeout)
		defer cancel()
		now := c.d.Clock.Now()
		err = c.flush(ctx, now)
		c.storeStopped(ctx, now)
		c.d.Transport.Stop()
	})
	return err
}

// State implements health.Component.
func (c *Collector) State() health.State { return health.State(c.state.Load()) }

// LastBeat implements health.Component.
func (c *Collector) LastBeat() time.Time { return time.Unix(0, c.beat.Load()) }

func (c *Collector) run(ctx context.Context) {
	defer c.wg.Done()
	for {
		now := c.d.Clock.Now()
		c.beat.Store(now.UnixNano())
		r := c.nextDevice()
		wake := minTime(r.next, c.nextFlush)
		if err := util.Sleep(ctx, c.d.Clock, wake.Sub(now)); err != nil {
			return
		}
		c.step(ctx, r)
	}
}

func (c *Collector) nextDevice() *deviceRun {
	next := c.devs[0]
	for _, r := range c.devs[1:] {
		if r.next.Before(next.next) {
			next = r
		}
	}
	return next
}

// step polls the due device and flushes when due.
func (c *Collector) step(ctx context.Context, r *deviceRun) {
	now := c.d.Clock.Now()
	if !now.Before(r.next) {
		start := now
		res, err := c.poll(ctx, r)
		c.afterPoll(ctx, r, res, err, now)
		r.next = maxTime(start.Add(r.dev.Interval), c.d.Clock.Now())
	}
	if now = c.d.Clock.Now(); !now.Before(c.nextFlush) {
		if err := c.flush(ctx, now); err != nil {
			c.d.Log.Warn().Err(err).Msg("flush failed, retrying")
		}
		c.nextFlush = flushTime(now)
	}
	c.updateState()
}

func (c *Collector) afterPoll(ctx context.Context, r *deviceRun, res pollResult, err error,
	now time.Time,
) {
	log := c.d.Log.With().Str("device", r.dev.ID).Logger()
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Debug().Err(err).Msg("poll failed")
	}
	for _, msg := range res.logs {
		if !r.logged[msg] {
			r.logged[msg] = true
			log.Info().Msg(msg)
		}
	}
	events := res.statusEvents
	if e, ok := c.reportDevice(r, now); ok {
		events = append(events, e)
	}
	c.storeStatus(ctx, r, events, log)
}

// storeStatus stores status changes and publishes those of profile registers.
func (c *Collector) storeStatus(ctx context.Context, r *deviceRun,
	events []storage.StatusEvent, log zerolog.Logger,
) {
	if len(events) == 0 {
		return
	}
	if err := c.d.Store.AddStatus(ctx, events); err != nil {
		log.Warn().Err(err).Msg("store status")
	}
	for _, e := range events {
		if e.Register == storage.ConnectionRegister {
			continue // published as DeviceState
		}
		sv := r.dev.Profile.DescribeStatus(e.Register, uint64(e.Code))
		c.d.Bus.Publish(eventbus.StatusChanged{
			Device: e.Device, Register: e.Register, Level: sv.Level,
			Code: uint64(e.Code), At: time.UnixMilli(e.At),
		})
	}
}

// storeStopped marks every device as stopped, so the time pvmon does not run is no data
// instead of the last connection state.
func (c *Collector) storeStopped(ctx context.Context, now time.Time) {
	var events []storage.StatusEvent
	for _, r := range c.devs {
		if e, ok := r.connectionChange(storage.ConnectionStopped, now); ok {
			events = append(events, e)
		}
	}
	if len(events) == 0 {
		return
	}
	if err := c.d.Store.AddStatus(ctx, events); err != nil {
		c.d.Log.Warn().Err(err).Msg("store stopped")
	}
}

// reportDevice reports online / offline (no success for 3 intervals) on change and
// returns the connection event to store when it differs from the stored one.
func (c *Collector) reportDevice(r *deviceRun, now time.Time) (storage.StatusEvent, bool) {
	state, code := health.DeviceOnline, storage.ConnectionOnline
	if r.failed && (r.lastOK.IsZero() || now.Sub(r.lastOK) > staleAfter(r.dev.Interval)) {
		state, code = health.DeviceOffline, storage.ConnectionOffline
	}
	if state != r.state {
		r.state = state
		c.d.Reporter.DeviceState(r.dev.ID, state)
		c.d.Bus.Publish(eventbus.DeviceState{Device: r.dev.ID, State: state})
	}
	return r.connectionChange(code, now)
}

func (c *Collector) updateState() {
	state := health.Healthy
	for _, r := range c.devs {
		if r.state != health.DeviceOnline {
			state = health.Recovering
		}
	}
	c.state.Store(int32(state))
}

// flush writes everything pending; a failed batch is restored for the next attempt.
func (c *Collector) flush(ctx context.Context, now time.Time) error {
	if c.acc.Empty() {
		return nil
	}
	b := c.acc.Batch(Slot(util.Midnight(now, c.s.Location)))
	if err := c.d.Store.Flush(ctx, b); err != nil {
		c.acc.Restore(b)
		return fmt.Errorf("collector: flush: %w", err)
	}
	return nil
}
