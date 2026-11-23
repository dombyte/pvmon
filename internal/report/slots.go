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
	GridCharge float64
	// Solar is the loadpoint energy covered by PV, Σ loadpoint × solar share.
	Solar map[string]float64
	// Profile is the consumption per slot of day (Wh, a repeated DST hour adds up),
	// Count the number of buckets per slot.
	Profile [SlotsPerDay]float64
	Count   [SlotsPerDay]int
	// Peak is the highest 15-minute mean consumption (W) at PeakAt.
	Peak   float64
	PeakAt time.Time
}

// EstimateDay sums the slots of one day; nil without slots or when any slot has a gap.
// loadpoints are the loadpoint IDs.
func EstimateDay(slots []Slot, loadpoints []string) *DaySlots {
	if len(slots) == 0 {
		return nil
	}
	out := &DaySlots{Solar: map[string]float64{}}
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
	share := solarShare(cons, imp)
	d.GridCharge += min(max(0, s.Values[KeyCharge]), max(0, imp))
	for _, id := range loadpoints {
		d.Solar[id] += s.Values[MeterKey(KeyLoadpoint, id)] * share
	}
	i := (s.At.Hour()*60 + s.At.Minute()) / minutesPerSlot
	d.Profile[i] += cons
	d.Count[i]++
	if w := cons * slotsPerHour; w > d.Peak || d.PeakAt.IsZero() {
		d.Peak, d.PeakAt = w, s.At
	}
}

// solarShare = clamp(1 − import / consumption, 0, 1); 0 without consumption.
func solarShare(cons, imp float64) float64 {
	if cons <= 0 {
		return 0
	}
	return min(1, max(0, 1-imp/cons))
}
