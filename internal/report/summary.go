package report

const (
	// MinBalanceDays is the shortest period (complete days) with a battery quotient and a
	// balance difference: the battery level at the start and end is not stored, so over a
	// few days it dominates both values (spec 12).
	MinBalanceDays = 28
)

// Energy holds the site quantities of one day or a sum of days (Wh).
type Energy struct {
	PV, Consumption, Import, Export, Charge, Discharge float64
}

func (e Energy) add(o Energy) Energy {
	return Energy{
		PV: e.PV + o.PV, Consumption: e.Consumption + o.Consumption,
		Import: e.Import + o.Import, Export: e.Export + o.Export,
		Charge: e.Charge + o.Charge, Discharge: e.Discharge + o.Discharge,
	}
}

func (e Energy) scale(f float64) Energy {
	return Energy{
		PV: e.PV * f, Consumption: e.Consumption * f, Import: e.Import * f,
		Export: e.Export * f, Charge: e.Charge * f, Discharge: e.Discharge * f,
	}
}

// energyOf reads the core values of a day (absent keys are 0).
func energyOf(d Day) Energy {
	v := d.Values
	return Energy{
		PV: v[KeyPV], Consumption: v[KeyConsumption], Import: v[KeyImport],
		Export: v[KeyExport], Charge: v[KeyCharge], Discharge: v[KeyDischarge],
	}
}

// Split divides one energy amount into three parts (Wh) with their shares (0..1).
// Sources: Grid = imported, Battery = discharged minus the grid charge (bought energy
// counts once, as grid), Solar = the rest; they add up to the consumption. PV split:
// Solar = used directly, Battery = charged from PV, Grid = exported.
type Split struct {
	Solar, Battery, Grid                float64
	SolarShare, BatteryShare, GridShare *float64
}

func newSplit(solar, battery, grid float64) Split {
	sum := solar + battery + grid
	return Split{
		Solar: solar, Battery: battery, Grid: grid,
		SolarShare: share(solar, sum), BatteryShare: share(battery, sum),
		GridShare: share(grid, sum),
	}
}

// Summary is the evaluation of the complete days of one period.
type Summary struct {
	// Days is the number of calendar days, CompleteDays those with every core series.
	Days, CompleteDays int
	Total              Energy
	// PerDay is Total / CompleteDays; nil without complete days.
	PerDay *Energy
	// SelfSufficiency = (consumption − import) / consumption; SelfConsumption =
	// (PV − export) / PV; 0..1, nil without a denominator.
	SelfSufficiency, SelfConsumption *float64
	// GridCharge ≈ battery charge from the grid (time-resolved, spec 12).
	GridCharge float64
	// Sources: where the consumption came from; PVSplit: where the PV went (≈).
	Sources, PVSplit Split
	// BatteryRatio = discharged / charged (a plain quotient, never an efficiency);
	// BalanceDiff = energy in − energy out (Wh), BalanceDiffShare = BalanceDiff / in.
	// Nil below MinBalanceDays complete days.
	BatteryRatio, BalanceDiff, BalanceDiffShare *float64
	// FallbackDays had a gap, so their estimates come from the daily balance.
	FallbackDays int
}

// Complete reports whether every day of the period has data.
func (s Summary) Complete() bool { return s.CompleteDays == s.Days }

// summarize evaluates the complete days of a period with periodDays calendar days.
func summarize(days []Day, periodDays int) Summary {
	s := Summary{Days: periodDays, CompleteDays: len(days)}
	for _, d := range days {
		s.Total = s.Total.add(energyOf(d))
		s.addEstimate(d)
	}
	if s.CompleteDays > 0 {
		perDay := s.Total.scale(1 / float64(s.CompleteDays))
		s.PerDay = &perDay
	}
	t := s.Total
	s.SelfSufficiency = share(t.Consumption-t.Import, t.Consumption)
	s.SelfConsumption = share(t.PV-t.Export, t.PV)
	s.Sources = s.sources()
	fromPV := max(0, min(t.Charge-s.GridCharge, t.PV-t.Export))
	s.PVSplit = newSplit(max(0, t.PV-t.Export-fromPV), fromPV, t.Export)
	if s.CompleteDays >= MinBalanceDays {
		s.balance()
	}
	return s
}

// addEstimate adds a day's grid charge: from its slots, or min(charge, import) of the
// daily balance when a bucket had a gap.
func (s *Summary) addEstimate(d Day) {
	if d.Slots != nil {
		s.GridCharge += d.Slots.GridCharge
		return
	}
	e := energyOf(d)
	s.GridCharge += min(max(0, e.Charge), max(0, e.Import))
	s.FallbackDays++
}

// sources splits the consumption; each part is capped by what the consumption leaves.
func (s *Summary) sources() Split {
	t := s.Total
	cons := max(0, t.Consumption)
	grid := min(max(0, t.Import), cons)
	battery := min(max(0, t.Discharge-s.GridCharge), cons-grid)
	return newSplit(max(0, cons-grid-battery), battery, grid)
}

func (s *Summary) balance() {
	t := s.Total
	in := t.PV + t.Import + t.Discharge
	diff := in - (t.Consumption + t.Export + t.Charge)
	s.BalanceDiff = &diff
	s.BalanceDiffShare = quotient(diff, in)
	s.BatteryRatio = quotient(t.Discharge, t.Charge)
}

// quotient returns num / den, or nil when den is not positive.
func quotient(num, den float64) *float64 {
	if den <= 0 {
		return nil
	}
	q := num / den
	return &q
}

// share returns num / den clamped to 0..1, or nil when den is not positive.
func share(num, den float64) *float64 {
	q := quotient(num, den)
	if q != nil {
		*q = min(1, max(0, *q))
	}
	return q
}
