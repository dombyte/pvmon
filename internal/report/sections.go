package report

import "time"

// minSources is the number of PV meters (or batteries) from which they are listed
// one by one (spec 12).
const minSources = 2

// MeterEnergy is the energy of one meter over the days its section counts.
type MeterEnergy struct {
	Meter
	// In / Out (Wh): battery charged / discharged, PV yield in Out, loadpoint in In.
	In, Out float64
	// Share of the section total (PV); Solar ≈ PV-covered energy and SolarShare
	// (loadpoints); Ratio = Out / In (battery, from MinBalanceDays); SpecificYield =
	// kWh/kWp (PV with kwp).
	Share, SolarShare, Ratio, SpecificYield *float64
	Solar                                   float64
}

// Section is a list of meters over the days with every series of the section.
type Section struct {
	CompleteDays int
	Meters       []MeterEnergy
}

// Category sums the loadpoints of one category.
type Category struct {
	Category   string
	In, Solar  float64
	SolarShare *float64
}

// Loadpoints is the loadpoint section.
type Loadpoints struct {
	Section
	Categories []Category
	// House = consumption − loadpoints over the same days (Wh); nil without such days.
	// It can be negative when meters disagree.
	House *float64
	// HouseSolar ≈ PV-covered part of House, within 0..House; the share is nil when
	// House is not positive.
	HouseSolar      float64
	HouseSolarShare *float64
}

// Profile is the load profile: mean consumption per slot of day (W, nil without data)
// and the highest 15-minute mean.
type Profile struct {
	Mean   [SlotsPerDay]*float64
	Peak   *float64
	PeakAt time.Time
}

func filterDays(days []Day, keys []string) []Day {
	out := make([]Day, 0, len(days))
	for _, d := range days {
		if d.has(keys) {
			out = append(out, d)
		}
	}
	return out
}

func meterKeys(kind string, ms []Meter) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = MeterKey(kind, m.ID)
	}
	return out
}

// pvSources lists the PV meters; nil below minSources.
func pvSources(days []Day, ms []Meter) *Section {
	if len(ms) < minSources {
		return nil
	}
	days = filterDays(days, meterKeys(KeyPV, ms))
	out := &Section{CompleteDays: len(days)}
	var total float64
	for _, m := range ms {
		me := MeterEnergy{Meter: m}
		for _, d := range days {
			me.Out += d.Values[MeterKey(KeyPV, m.ID)]
		}
		total += me.Out
		if m.KWp > 0 {
			y := me.Out / whPerKWh / m.KWp
			me.SpecificYield = &y
		}
		out.Meters = append(out.Meters, me)
	}
	for i := range out.Meters {
		out.Meters[i].Share = share(out.Meters[i].Out, total)
	}
	return out
}

const whPerKWh = 1000

// batteries lists each battery; nil below minSources.
func batteries(days []Day, ms []Meter) *Section {
	if len(ms) < minSources {
		return nil
	}
	days = filterDays(days, append(meterKeys(KeyCharge, ms), meterKeys(KeyDischarge, ms)...))
	out := &Section{CompleteDays: len(days)}
	for _, m := range ms {
		me := MeterEnergy{Meter: m}
		for _, d := range days {
			me.In += d.Values[MeterKey(KeyCharge, m.ID)]
			me.Out += d.Values[MeterKey(KeyDischarge, m.ID)]
		}
		if len(days) >= MinBalanceDays {
			me.Ratio = quotient(me.Out, me.In)
		}
		out.Meters = append(out.Meters, me)
	}
	return out
}

// loadpoints sums each loadpoint and its PV-covered energy ≈; nil without loadpoints.
func loadpoints(days []Day, ms []Meter) *Loadpoints {
	if len(ms) == 0 {
		return nil
	}
	days = filterDays(days, meterKeys(KeyLoadpoint, ms))
	out := &Loadpoints{Section: Section{CompleteDays: len(days)}}
	gridPart := batteryGridPart(days)
	for _, m := range ms {
		me := MeterEnergy{Meter: m}
		for _, d := range days {
			in := d.Values[MeterKey(KeyLoadpoint, m.ID)]
			me.In += in
			me.Solar += daySolar(d, m.ID, in, gridPart)
		}
		me.SolarShare = share(me.Solar, me.In)
		out.Meters = append(out.Meters, me)
	}
	out.Categories = categories(out.Meters)
	out.House = house(days, out.Meters)
	out.addHouseSolar(days, gridPart)
	return out
}

// house is what the loadpoints leave of the consumption of days.
func house(days []Day, ms []MeterEnergy) *float64 {
	if len(days) == 0 {
		return nil
	}
	var rest float64
	for _, d := range days {
		rest += d.Values[KeyConsumption]
	}
	for _, m := range ms {
		rest -= m.In
	}
	return &rest
}

// batteryGridPart is the part of the battery output that counts as bought from the
// grid: grid charge / discharge over the days with slots, at most 1. Like the energy
// flows, all bought energy counts once as grid, battery losses included.
func batteryGridPart(days []Day) float64 {
	var charged, discharged float64
	for _, d := range days {
		if d.Slots != nil {
			charged += d.Slots.GridCharge
			discharged += d.Slots.Discharge
		}
	}
	if part := share(charged, discharged); part != nil {
		return *part
	}
	return 0
}

// addHouseSolar estimates what PV covers of the house: the PV-covered consumption
// minus the loadpoints' part of it.
func (l *Loadpoints) addHouseSolar(days []Day, gridPart float64) {
	if l.House == nil {
		return
	}
	var solar float64
	for _, d := range days {
		solar += daySolarConsumption(d, gridPart)
	}
	for _, m := range l.Meters {
		solar -= m.Solar
	}
	l.HouseSolar = max(0, min(solar, *l.House))
	l.HouseSolarShare = share(l.HouseSolar, *l.House)
}

// daySolarConsumption is the PV-covered consumption of one day, from the same source as
// daySolar.
func daySolarConsumption(d Day, gridPart float64) float64 {
	if d.Slots != nil {
		return d.Slots.SolarConsumption.solar(gridPart)
	}
	cons := d.Values[KeyConsumption]
	return cons * solarShare(cons, d.Values[KeyImport])
}

// daySolar is a loadpoint's PV-covered energy of one day: from the slots, or with the
// daily solar share when a bucket had a gap.
func daySolar(d Day, id string, in, gridPart float64) float64 {
	if d.Slots != nil {
		return d.Slots.Solar[id].solar(gridPart)
	}
	return in * solarShare(d.Values[KeyConsumption], d.Values[KeyImport])
}

func categories(ms []MeterEnergy) []Category {
	var out []Category
	index := map[string]int{}
	for _, m := range ms {
		i, ok := index[m.Category]
		if !ok {
			i = len(out)
			index[m.Category] = i
			out = append(out, Category{Category: m.Category})
		}
		out[i].In += m.In
		out[i].Solar += m.Solar
	}
	for i := range out {
		out[i].SolarShare = share(out[i].Solar, out[i].In)
	}
	return out
}

// loadProfile averages the slots of the days that have them.
func loadProfile(days []Day) Profile {
	var sum [SlotsPerDay]float64
	var n [SlotsPerDay]int
	var p Profile
	for _, d := range days {
		if d.Slots == nil {
			continue
		}
		for i := range SlotsPerDay {
			sum[i] += d.Slots.Profile[i]
			n[i] += d.Slots.Count[i]
		}
		p.peak(d.Slots)
	}
	for i := range SlotsPerDay {
		if n[i] > 0 {
			w := sum[i] / float64(n[i]) * slotsPerHour
			p.Mean[i] = &w
		}
	}
	return p
}

// peak keeps the highest 15-minute mean of p and d.
func (p *Profile) peak(d *DaySlots) {
	if d.PeakAt.IsZero() || (p.Peak != nil && d.Peak <= *p.Peak) {
		return
	}
	v := d.Peak
	p.Peak, p.PeakAt = &v, d.PeakAt
}
