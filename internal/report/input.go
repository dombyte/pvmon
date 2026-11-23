// Package report is the pure energy report model (spec 12): complete days of the site
// quantities go in, sums, ratios, sections, patterns and key figures (with an optional
// comparison period) come out. No I/O, no clock, no logging; renderers (JSON, CSV, PDF)
// only format what Build returns.
//
// Every ratio is computed from the summed energy of a period, never as an average of
// daily ratios. A day counts for a section only with a complete value of every series
// the section needs; other days are reported as missing, never as 0 kWh.
package report

import "time"

// Keys of Day.Values and Slot.Values (Wh). Per-meter keys come from MeterKey.
const (
	KeyPV          = "pv"
	KeyConsumption = "consumption"
	KeyImport      = "import"
	KeyExport      = "export"
	KeyCharge      = "charge"
	KeyDischarge   = "discharge"
)

// MeterKey returns the key of one meter's energy, e.g. MeterKey(KeyPV, "roof").
func MeterKey(kind, id string) string { return kind + ":" + id }

// KeyLoadpoint is the kind of a loadpoint's energy key.
const KeyLoadpoint = "lp"

// Meter is a meter a section reports on.
type Meter struct {
	ID, Label, Category string
	// KWp is the peak power of a PV meter (0 = unknown).
	KWp float64
}

// Layout is the part of the site that decides which sections exist (spec 12).
type Layout struct {
	// PV are the top-level PV meters; empty = no PV.
	PV []Meter
	// Batteries are the batteries; empty = no battery section.
	Batteries []Meter
	// Loadpoints are the consumers directly under the site.
	Loadpoints []Meter
}

// HasPV reports whether the site has PV.
func (l Layout) HasPV() bool { return len(l.PV) > 0 }

// HasBattery reports whether the site has a battery.
func (l Layout) HasBattery() bool { return len(l.Batteries) > 0 }

// coreKeys are the series every summary day needs.
func (l Layout) coreKeys() []string {
	keys := []string{KeyConsumption, KeyImport, KeyExport}
	if l.HasPV() {
		keys = append(keys, KeyPV)
	}
	if l.HasBattery() {
		keys = append(keys, KeyCharge, KeyDischarge)
	}
	return keys
}

// Day is one local calendar day.
type Day struct {
	// Date is the local calendar day as midnight UTC (calendar math without DST).
	Date time.Time
	// Values holds the series that are complete on this day (Wh); a missing key means
	// incomplete or no data.
	Values map[string]float64
	// Slots are the time-resolved sums of the day; nil when a bucket has a gap, so the
	// estimates fall back to the daily balance.
	Slots *DaySlots
}

// has reports whether d has every key.
func (d Day) has(keys []string) bool {
	for _, k := range keys {
		if _, ok := d.Values[k]; !ok {
			return false
		}
	}
	return true
}

// MeterQuality is the data quality of one meter in the range (spec 12, data quality).
type MeterQuality struct {
	ID, Label string
	// MissingDays have no complete value; EstimatedDays contain estimated buckets.
	MissingDays, EstimatedDays int
}

// Input is everything Build needs.
type Input struct {
	Range       Range
	Granularity Granularity
	Layout      Layout
	// Days of the range (others are ignored), any order.
	Days []Day
	// Compare is the comparison period; nil = no comparison.
	Compare *CompareInput
	// TodayExcluded says the requested range ended today and was cut to end yesterday.
	TodayExcluded bool
	// Quality lists the meters' data quality in the range.
	Quality []MeterQuality
}

// CompareInput is the comparison period and its days.
type CompareInput struct {
	Mode  CompareMode
	Range Range
	Days  []Day
}
