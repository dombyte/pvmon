package report

import "time"

// SlotsPerDay is the number of 15-minute slots of a day without DST change.
const SlotsPerDay = 96

const (
	minutesPerSlot = 15
	slotsPerHour   = 4 // W = Wh × 4 for a 15-minute bucket
)

// Slot is one 15-minute bucket of the site (Wh per key); Gap marks a missing or
// incomplete value of any needed series.
type Slot struct {
	At     time.Time // local start of the slot
	Values map[string]float64
	Gap    bool
}

// DaySlots are the time-resolved sums of one day (spec 12, time-resolved estimates).
type DaySlots struct {
	// GridCharge = Σ min(charge, import): battery charge that came from the grid.
	// Discharge = Σ battery discharge.
	GridCharge, Discharge float64
	// Solar and SolarConsumption are what the PV estimates of each loadpoint and of
	// the consumption are made of.
	Solar            map[string]SolarParts
	SolarConsumption SolarParts
	// Profile is the consumption per slot of day (Wh, a repeated DST hour adds up),
	// Count the number of buckets per slot.
	Profile [SlotsPerDay]float64
	Count   [SlotsPerDay]int
	// Peak is the highest 15-minute mean consumption (W) at PeakAt.
	Peak   float64
	PeakAt time.Time
}

// SolarParts are the slot sums of one consumer for its PV estimate. The PV-covered
// energy depends on how much of the battery output was bought from the grid, which is
// known only for the whole range.
type SolarParts struct {
	// NotImported = Σ energy × share not taken from the grid directly; Battery =
	// Σ energy × share taken from the battery, a part of NotImported.
	NotImported, Battery float64
}

// solar is the PV-covered energy when gridPart of the battery output counts as grid.
func (p SolarParts) solar(gridPart float64) float64 {
	return p.NotImported - gridPart*p.Battery
}

func (p SolarParts) add(wh float64, s sourceShares) SolarParts {
	return SolarParts{p.NotImported + wh*s.notImported, p.Battery + wh*s.battery}
}

// EstimateDay sums the slots of one day; nil without slots or when any slot has a gap.
// loadpoints are the loadpoint IDs.
func EstimateDay(slots []Slot, loadpoints []string) *DaySlots {
	if len(slots) == 0 {
		return nil
	}
	out := &DaySlots{Solar: map[string]SolarParts{}}
	for _, s := range slots {
		if s.Gap {
			return nil
		}
		out.add(s, loadpoints)
	}
	return out
}

func (d *DaySlots) add(s Slot, loadpoints []string) {
	cons, imp := s.Values[KeyConsumption], s.Values[KeyImport]
	gridCharge := min(max(0, s.Values[KeyCharge]), max(0, imp))
	discharge := max(0, s.Values[KeyDischarge])
	d.GridCharge += gridCharge
	d.Discharge += discharge
	// The import that charges the battery does not supply the consumers.
	shares := sharesOf(cons, max(0, imp)-gridCharge, discharge)
	for _, id := range loadpoints {
		d.Solar[id] = d.Solar[id].add(s.Values[MeterKey(KeyLoadpoint, id)], shares)
	}
	d.SolarConsumption = d.SolarConsumption.add(cons, shares)
	i := (s.At.Hour()*60 + s.At.Minute()) / minutesPerSlot
	d.Profile[i] += cons
	d.Count[i]++
	if w := cons * slotsPerHour; w > d.Peak || d.PeakAt.IsZero() {
		d.Peak, d.PeakAt = w, s.At
	}
}

// sourceShares are the shares (0..1) of one slot's consumption by source.
type sourceShares struct {
	notImported, battery float64
}

// sharesOf splits the consumption of a slot: the direct grid import first, then the
// battery from what is left. Both are 0 without consumption.
func sharesOf(cons, directImport, discharge float64) sourceShares {
	if cons <= 0 {
		return sourceShares{}
	}
	rest := solarShare(cons, directImport)
	return sourceShares{notImported: rest, battery: min(rest, discharge/cons)}
}

// solarShare = clamp(1 − import / consumption, 0, 1); 0 without consumption.
func solarShare(cons, imp float64) float64 {
	if cons <= 0 {
		return 0
	}
	return min(1, max(0, 1-imp/cons))
}
