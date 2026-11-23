package derive

import (
	"sort"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
)

// Gauge series combine stored 15-minute gauges. Pure: no I/O, clock or logging.

// gaugeTerm is one stored gauge series with its weight in a combination.
type gaugeTerm struct {
	Series string
	Weight float64
}

// GaugeFormula combines gauge series slot by slot: Σ weight × avg/min/max. The min and
// max of a sum are the sums of the parts' extremes, a band that contains the true one.
type GaugeFormula struct {
	Terms []gaugeTerm
	Unit  string
}

// GaugePoint is one slot of a gauge series.
type GaugePoint struct {
	Slot          int64
	Avg, Min, Max float64
	Cover         int // seconds covered by every term
}

// SiteGauges are the site quantities of the gauge endpoint.
const (
	GaugeSitePV  = "site/pv"
	GaugeSiteSoC = "site/soc"
)

// GaugeCatalogue returns every gauge name: the visible gauge series of the meters, the PV
// power of the site (Σ top-level PV meters) and the site SoC (weighted by capacity when
// every battery has one, else the mean).
func GaugeCatalogue(s *site.Site) map[string]GaugeFormula {
	out := map[string]GaugeFormula{}
	for _, si := range s.Series() {
		if si.Kind == site.KindGauge && !si.Hidden {
			out[si.Key()] = GaugeFormula{Terms: []gaugeTerm{{si.Key(), 1}}, Unit: si.Unit}
		}
	}
	pv, bats := topLevel(s, profile.RolePV), topLevel(s, profile.RoleBattery)
	if len(pv) > 0 {
		terms := make([]gaugeTerm, len(pv))
		for i, m := range pv {
			terms[i] = gaugeTerm{m.ID + "/power", 1}
		}
		out[GaugeSitePV] = GaugeFormula{Terms: terms, Unit: "W"}
	}
	if len(bats) > 0 {
		out[GaugeSiteSoC] = GaugeFormula{Terms: socTerms(bats), Unit: "%"}
	}
	return out
}

// topLevel returns the meters of a role directly under the site, sorted by ID.
func topLevel(s *site.Site, role string) []*site.Meter {
	var out []*site.Meter
	for _, id := range sortedMeters(s) {
		if m := s.Meters[id]; m.Role == role && !m.Nested() {
			out = append(out, m)
		}
	}
	return out
}

func socTerms(bats []*site.Meter) []gaugeTerm {
	total := 0.0
	for _, m := range bats {
		if m.CapacityWh <= 0 {
			total = 0
			break
		}
		total += m.CapacityWh
	}
	out := make([]gaugeTerm, len(bats))
	for i, m := range bats {
		w := 1 / float64(len(bats))
		if total > 0 {
			w = m.CapacityWh / total
		}
		out[i] = gaugeTerm{m.ID + "/soc", w}
	}
	return out
}

// Combine applies f to the buckets of its terms; a slot is only present when every term
// has it (a missing part never counts as 0).
func (f GaugeFormula) Combine(buckets map[string][]storage.GaugeBucket) []GaugePoint {
	bySlot := map[int64]*GaugePoint{}
	count := map[int64]int{}
	for _, t := range f.Terms {
		for _, b := range buckets[t.Series] {
			p, ok := bySlot[b.Slot]
			if !ok {
				p = &GaugePoint{Slot: b.Slot, Cover: b.Cover}
				bySlot[b.Slot] = p
			}
			p.Avg += t.Weight * b.Avg
			p.Min += t.Weight * b.Min
			p.Max += t.Weight * b.Max
			p.Cover = min(p.Cover, b.Cover)
			count[b.Slot]++
		}
	}
	out := make([]GaugePoint, 0, len(bySlot))
	for slot, p := range bySlot {
		if count[slot] == len(f.Terms) {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}
