package collector

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
)

// ref names a block channel of a device profile ("block.channel").
type ref struct{ block, channel string }

func (r ref) String() string { return r.block + "." + r.channel }

func parseRef(s string) ref {
	b, c, _ := profile.ChannelRef(s)
	return ref{b, c}
}

// counterPlan is one counter input of a device.
type counterPlan struct {
	ref
	kind  string // lifetime | daily_reset
	group string // reset group of daily counters
	cfg   CounterConfig
	input storage.SeriesKey  // series holding its counter_state
	book  *storage.SeriesKey // energy series booked directly (nil: netted or unused)
	power ref                // power of the same block that shapes its delta (sign != 0)
	sign  float64            // power sign that flows into the counter: +1, -1, 0 none
}

// sharePlan splits the delta of a counter over channels by their power (derive: share).
type sharePlan struct {
	of     ref
	by     ref
	peers  []ref // weight channels of every block sharing "of", configured or not
	series storage.SeriesKey
}

// integratePlan integrates power into energy for a meter without energy channels.
type integratePlan struct {
	power    ref
	pos, neg *storage.SeriesKey // energy for positive and negative power
}

// nettingPlan nets a grid meter per poll (spec 6, netting: app).
type nettingPlan struct {
	imp, exp, power    ref
	in, out, cancelled storage.SeriesKey
}

// gaugePlan stores a gauge channel of a meter.
type gaugePlan struct {
	ref
	series storage.SeriesKey
}

// livePlan publishes every channel of a meter block under "<meter>/<channel>".
type livePlan struct {
	meter, block string
}

// devicePlan is everything one device poll feeds.
type devicePlan struct {
	counters   []counterPlan
	shares     []sharePlan
	integrates []integratePlan
	netting    *nettingPlan
	gauges     []gaugePlan
	live       []livePlan
	defs       []storage.SeriesDef
	needs      profile.Needs // channels decoded per poll: configured blocks + dependencies
}

// hiddenMeter names the hidden input series of a block that no meter uses.
func hiddenMeter(device, block string) string { return device + ":" + block }

// buildPlan derives the routing of one device from the site and its profile.
func buildPlan(s *site.Site, d *site.Device) *devicePlan {
	pl := &devicePlan{needs: d.Profile.NeedsFor(d.Blocks)}
	meters := blockMeters(s, d)
	addDefs(pl, s, d)
	for _, blockName := range pl.needs.Blocks() {
		blk := d.Profile.Blocks[blockName]
		m := meters[blockName]
		for _, ch := range sortedChannels(blk) {
			if pl.needs.Has(blockName, ch) {
				planChannel(pl, d, m, ref{blockName, ch}, blk.Channels[ch])
			}
		}
		if m != nil {
			pl.live = append(pl.live, livePlan{meter: m.ID, block: blockName})
			planIntegration(pl, m, blk)
		}
	}
	if m := gridMeter(meters); m != nil && d.Netting == "app" {
		planNetting(pl, m)
	}
	return pl
}

// blockMeters maps block names to the meter using them on this device.
func blockMeters(s *site.Site, d *site.Device) map[string]*site.Meter {
	out := map[string]*site.Meter{}
	for _, id := range d.Meters {
		m := s.Meters[id]
		out[m.Source] = m
	}
	return out
}

func gridMeter(meters map[string]*site.Meter) *site.Meter {
	for _, m := range meters {
		if m.Role == profile.RoleGrid {
			return m
		}
	}
	return nil
}

func sortedChannels(b profile.Block) []string {
	out := make([]string, 0, len(b.Channels))
	for c := range b.Channels {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func planChannel(pl *devicePlan, d *site.Device, m *site.Meter, r ref, c profile.Channel) {
	block, name := r.block, r.channel
	stored := m != nil && historised(d, name, c)
	switch {
	case c.Counter != profile.CounterNone:
		planCounter(pl, d, counterSpec{meter: m, ref: r, ch: c, stored: stored})
	case c.Derive != "" && m != nil:
		pl.shares = append(pl.shares, sharePlan{
			of: parseRef(c.Of), by: ref{block, c.By}, peers: sharePeers(d.Profile.Profile, c.Of),
			series: storage.SeriesKey{Meter: m.ID, Channel: name},
		})
	case stored && !c.IsEnergy():
		pl.gauges = append(pl.gauges, gaugePlan{r, storage.SeriesKey{
			Meter:   m.ID,
			Channel: name,
		}})
	}
}

// sharePeers returns the weight channels of every block sharing the counter "of".
func sharePeers(p *profile.Profile, of string) []ref {
	var out []ref
	for _, s := range p.Sharers(of) {
		out = append(out, ref{s[0], s[1]})
	}
	return out
}

// historised reports whether a channel is stored: by default or via devices.<id>.history.
func historised(d *site.Device, name string, c profile.Channel) bool {
	return c.Historised(name) || contains(d.History, name)
}

// counterSpec is one counter channel of a block.
type counterSpec struct {
	meter  *site.Meter
	ref    ref
	ch     profile.Channel
	stored bool
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func planCounter(pl *devicePlan, d *site.Device, cs counterSpec) {
	m, r, c := cs.meter, cs.ref, cs.ch
	cp := counterPlan{ref: r, kind: c.Counter, group: c.ResetGroup, cfg: counterConfig(d, c)}
	if !cs.stored {
		cp.input = storage.SeriesKey{Meter: hiddenMeter(d.ID, r.block), Channel: r.channel}
		pl.defs = append(pl.defs, hiddenDef(cp.input))
	} else {
		key := storage.SeriesKey{Meter: m.ID, Channel: r.channel}
		cp.input, cp.book = key, &key
		cp.power, cp.sign = ref{r.block, "power"}, powerSign(d, m, r)
	}
	pl.counters = append(pl.counters, cp)
}

// powerSign is the sign of the block power that flows into counter r of meter m (spec 2),
// or 0 when the block has no power channel or the role has no direction for r.
func powerSign(d *site.Device, m *site.Meter, r ref) float64 {
	if _, ok := d.Profile.Blocks[r.block].Channels["power"]; !ok {
		return 0
	}
	switch pos, neg := site.IntegratedChannels(m.Role); r.channel {
	case pos:
		return 1
	case neg:
		return -1
	}
	return 0
}

func hiddenDef(k storage.SeriesKey) storage.SeriesDef {
	return storage.SeriesDef{
		SeriesKey: k, Kind: site.KindEnergy, Origin: site.OriginMeasured,
		Hidden: true, Unit: "Wh",
	}
}

// isInteger reports whether a register type counts in whole raw units.
func isInteger(typ string) bool {
	switch typ {
	case "u16", "i16", "u32", "i32", "u64":
		return true
	}
	return false
}

// counterConfig maps profile and device data onto the tracker configuration.
func counterConfig(d *site.Device, c profile.Channel) CounterConfig {
	const kWh, mWh = 1e3, 1e6
	factor := map[string]float64{"Wh": 1, "kWh": kWh, "MWh": mWh}[c.Unit]
	cfg := CounterConfig{
		Resolution: c.Resolution * factor, MaxPower: d.Profile.MaxPower,
		Float32: c.Type == "f32", UnitFactor: factor,
	}
	if isInteger(c.Type) {
		cfg.Step = c.ScaleOr1() * factor
	}
	if c.Type == "u32" {
		cfg.Wrap = (math.MaxUint32 + 1) * c.ScaleOr1() * factor
	}
	return cfg
}

// planIntegration integrates power for meters whose block has no energy channel.
func planIntegration(pl *devicePlan, m *site.Meter, blk profile.Block) {
	for name, c := range blk.Channels {
		if c.IsEnergy() || strings.HasPrefix(name, "energy_") {
			return
		}
	}
	if _, ok := blk.Channels["power"]; !ok {
		return
	}
	pos, neg := site.IntegratedChannels(m.Role)
	ip := integratePlan{power: ref{m.Source, "power"}}
	if pos != "" {
		ip.pos = &storage.SeriesKey{Meter: m.ID, Channel: pos}
	}
	if neg != "" {
		ip.neg = &storage.SeriesKey{Meter: m.ID, Channel: neg}
	}
	pl.integrates = append(pl.integrates, ip)
}

// planNetting moves the grid meter's counters to hidden raw series and books netted energy.
func planNetting(pl *devicePlan, m *site.Meter) {
	np := &nettingPlan{
		imp: ref{m.Source, "energy_in"}, exp: ref{m.Source, "energy_out"},
		power:     ref{m.Source, "power"},
		in:        storage.SeriesKey{Meter: m.ID, Channel: "energy_in"},
		out:       storage.SeriesKey{Meter: m.ID, Channel: "energy_out"},
		cancelled: storage.SeriesKey{Meter: m.ID, Channel: site.ChannelGridCancelled},
	}
	raw := map[string]string{
		"energy_in":  site.ChannelGridImportRaw,
		"energy_out": site.ChannelGridExportRaw,
	}
	for i, cp := range pl.counters {
		if ch, ok := raw[cp.channel]; ok && cp.block == m.Source {
			pl.counters[i].input = storage.SeriesKey{Meter: m.ID, Channel: ch}
			pl.counters[i].book = nil
		}
	}
	pl.netting = np
}

// addDefs adds the catalogue series of this device's meters.
func addDefs(pl *devicePlan, s *site.Site, d *site.Device) {
	for _, si := range s.Series() {
		if s.Meters[si.Meter].Device != d.ID {
			continue
		}
		pl.defs = append(pl.defs, storage.SeriesDef{
			SeriesKey: storage.SeriesKey{Meter: si.Meter, Channel: si.Channel},
			Kind:      si.Kind, Origin: si.Origin, Hidden: si.Hidden, Unit: si.Unit,
		})
	}
}

// staleAfter is how long a device may go without a successful poll before it is offline.
func staleAfter(interval time.Duration) time.Duration {
	const intervals = 3 // spec 8.2
	return intervals * interval
}
