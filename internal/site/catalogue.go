package site

import (
	"sort"

	"github.com/dombyte/pvmon/internal/profile"
)

// Series kinds and origins (spec 7.1).
const (
	KindGauge  = 1
	KindEnergy = 2

	OriginMeasured   = 1
	OriginIntegrated = 2
	OriginShare      = 3
)

// Hidden channel names of app netting (spec 6).
const (
	ChannelGridImportRaw = "grid_import_raw"
	ChannelGridExportRaw = "grid_export_raw"
	ChannelGridCancelled = "grid_cancelled"
)

// SeriesInfo describes one stored series of a meter channel.
type SeriesInfo struct {
	Meter   string
	Channel string
	Kind    int
	Origin  int
	Hidden  bool
	Unit    string
}

// Key returns "<meter>/<channel>".
func (s SeriesInfo) Key() string { return s.Meter + "/" + s.Channel }

// Series returns the stored series of the site, sorted by key: historised channels of
// every meter plus the hidden inputs of app netting.
func (s *Site) Series() []SeriesInfo {
	var out []SeriesInfo
	for _, id := range sorted(s.Meters) {
		m := s.Meters[id]
		out = append(out, s.meterSeries(m)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

func (s *Site) meterSeries(m *Meter) []SeriesInfo {
	dev := s.Devices[m.Device]
	var out []SeriesInfo
	for _, name := range sorted(m.Block.Channels) {
		c := m.Block.Channels[name]
		if !c.Historised(name) && !contains(dev.History, name) {
			continue
		}
		out = append(out, SeriesInfo{
			Meter: m.ID, Channel: name, Kind: kind(c), Origin: origin(c), Unit: unit(c),
		})
	}
	out = append(out, integratedSeries(m)...)
	if m.Role == profile.RoleGrid && dev.Netting == "app" {
		for _, ch := range []string{
			ChannelGridImportRaw, ChannelGridExportRaw,
			ChannelGridCancelled,
		} {
			out = append(out, SeriesInfo{
				Meter: m.ID, Channel: ch, Kind: KindEnergy, Origin: OriginMeasured,
				Hidden: true, Unit: "Wh",
			})
		}
	}
	return out
}

// IntegratedChannels returns the energy channels that power of a role is integrated into
// when its block has no energy channel: for positive and for negative power.
func IntegratedChannels(role string) (pos, neg string) {
	switch role {
	case profile.RolePV:
		return "energy_out", ""
	case profile.RoleGrid:
		return "energy_in", "energy_out"
	case profile.RoleBattery, profile.RoleInverter:
		return "energy_out", "energy_in"
	case profile.RoleConsumer, profile.RoleBackup:
		return "energy_in", ""
	}
	return "", ""
}

// integratedSeries returns the integrated energy series of a meter without energy channels.
func integratedSeries(m *Meter) []SeriesInfo {
	if _, ok := m.Block.Channels["power"]; !ok {
		return nil
	}
	for _, c := range m.Block.Channels {
		if c.IsEnergy() {
			return nil
		}
	}
	var out []SeriesInfo
	pos, neg := IntegratedChannels(m.Role)
	for _, ch := range []string{pos, neg} {
		if ch != "" {
			out = append(out, SeriesInfo{
				Meter: m.ID, Channel: ch, Kind: KindEnergy,
				Origin: OriginIntegrated, Unit: "Wh",
			})
		}
	}
	return out
}

func kind(c profile.Channel) int {
	if c.IsEnergy() {
		return KindEnergy
	}
	return KindGauge
}

func origin(c profile.Channel) int {
	switch {
	case c.Derive != "":
		return OriginShare
	case c.Counter == profile.CounterNone && c.IsEnergy():
		return OriginIntegrated
	default:
		return OriginMeasured
	}
}

// unit returns the stored unit: energy in Wh, power in W, else the profile unit.
func unit(c profile.Channel) string {
	switch {
	case kind(c) == KindEnergy:
		return "Wh"
	case c.Unit == "kW":
		return "W"
	default:
		return c.Unit
	}
}

// Capabilities summarises what the site has (spec 9.1 /api/v1/site).
type Capabilities struct {
	PV         int
	Battery    int
	Grid       bool
	Backup     bool
	Loadpoints map[string]int // per category, consumers directly under the site
}

// Capabilities counts meters per role and loadpoints per category.
func (s *Site) Capabilities() Capabilities {
	c := Capabilities{Loadpoints: map[string]int{}}
	for _, cat := range profile.Categories() {
		c.Loadpoints[cat] = 0
	}
	for _, m := range s.Meters {
		c.add(m)
	}
	return c
}

func (c *Capabilities) add(m *Meter) {
	if m.Nested() {
		return // PV strings and sub-meters are part of their parent
	}
	switch m.Role {
	case profile.RolePV:
		c.PV++
	case profile.RoleBattery:
		c.Battery++
	case profile.RoleGrid:
		c.Grid = true
	case profile.RoleBackup:
		c.Backup = true
	case profile.RoleConsumer:
		c.Loadpoints[m.Category]++
	}
}
