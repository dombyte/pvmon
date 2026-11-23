package derive

import (
	"sort"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
)

// Site quantity names that are not in the live set (spec 2).
const (
	QuantityGridNet         = "site/grid_net"
	QuantitySelfSufficiency = "site/self_sufficiency"
	QuantitySelfConsumption = "site/self_consumption"
)

// Term is one stored series with its coefficient.
type Term struct {
	Series string // "<meter>/<channel>"
	Coef   float64
}

// Formula is a linear combination of stored series; a ratio has a denominator.
type Formula struct {
	Terms []Term
	Den   []Term // non-empty for ratios: 100 × Σ Terms / Σ Den
}

// Ratio reports whether f yields a percentage.
func (f Formula) Ratio() bool { return len(f.Den) > 0 }

// Catalogue returns every energy name a client may request: the visible stored energy
// series and the site quantities, as formulas over stored series.
func Catalogue(s *site.Site) map[string]Formula {
	out := map[string]Formula{}
	roles := map[string][]string{} // role or "lp:<category>" → meter IDs with the channel
	for _, si := range s.Series() {
		if si.Kind != site.KindEnergy || si.Hidden {
			continue
		}
		out[si.Key()] = Formula{Terms: []Term{{si.Key(), 1}}}
	}
	for _, id := range sortedMeters(s) {
		m := s.Meters[id]
		if m.Nested() {
			continue // inside its parent: PV string or consumer sub-meter
		}
		group := m.Role
		if m.Role == profile.RoleConsumer {
			group = "lp:" + m.Category
		}
		roles[group] = append(roles[group], id)
	}
	addSite(out, roles)
	return out
}

func sortedMeters(s *site.Site) []string {
	ids := make([]string, 0, len(s.Meters))
	for id := range s.Meters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func terms(ids []string, channel string, coef float64) []Term {
	out := make([]Term, 0, len(ids))
	for _, id := range ids {
		out = append(out, Term{id + "/" + channel, coef})
	}
	return out
}

func addSite(out map[string]Formula, roles map[string][]string) {
	pv := terms(roles[profile.RolePV], "energy_out", 1)
	gIn := terms(roles[profile.RoleGrid], "energy_in", 1)
	gOut := terms(roles[profile.RoleGrid], "energy_out", 1)
	bIn := terms(roles[profile.RoleBattery], "energy_in", 1)
	bOut := terms(roles[profile.RoleBattery], "energy_out", 1)
	// Like evcc's home power: consumption minus loadpoints, wherever the loads are wired
	// (household or backup output of a hybrid inverter, behind the meter).
	var direct []Term
	for _, cat := range profile.Categories() {
		lp := terms(roles["lp:"+cat], "energy_in", 1)
		if len(lp) > 0 {
			out[site.QuantityLoadpoints+cat] = Formula{Terms: lp}
			direct = append(direct, lp...)
		}
	}
	cons := concat(pv, gIn, neg(gOut), bOut, neg(bIn))
	if len(pv) > 0 {
		out[site.QuantityPV] = Formula{Terms: pv}
		out[QuantitySelfConsumption] = Formula{Terms: concat(pv, neg(gOut)), Den: pv}
	}
	if len(bIn) > 0 {
		out[site.QuantityBatteryIn] = Formula{Terms: bIn}
		out[site.QuantityBatteryOut] = Formula{Terms: bOut}
	}
	out[site.QuantityGridIn] = Formula{Terms: gIn}
	out[site.QuantityGridOut] = Formula{Terms: gOut}
	out[QuantityGridNet] = Formula{Terms: concat(gOut, neg(gIn))}
	out[site.QuantityConsumption] = Formula{Terms: cons}
	out[site.QuantityHouseRest] = Formula{Terms: concat(cons, neg(direct))}
	out[QuantitySelfSufficiency] = Formula{Terms: concat(cons, neg(gIn)), Den: cons}
}

func neg(ts []Term) []Term {
	out := make([]Term, len(ts))
	for i, t := range ts {
		out[i] = Term{t.Series, -t.Coef}
	}
	return out
}

func concat(parts ...[]Term) []Term {
	var out []Term
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// Value is the result of a formula over one period.
type Value struct {
	Value       *float64 // Wh or percent; nil when there is no data
	Complete    bool
	Estimated   bool
	MissingDays int
}

// Evaluate applies f to the aggregates of its series (absent = no data).
func (f Formula) Evaluate(aggs map[string]Agg) Value {
	num := sum(f.Terms, aggs)
	quality := num
	var v float64
	ok := num.Data
	if f.Ratio() {
		den := sum(f.Den, aggs)
		quality = num.Combine(0, den)
		ok = quality.Data && den.Wh > 0
		if ok {
			v = percent * num.Wh / den.Wh
		}
	} else {
		v = num.Wh
	}
	out := Value{
		Complete: ok && len(quality.Missing) == 0, Estimated: quality.Estimated,
		MissingDays: len(quality.Missing),
	}
	if ok {
		out.Value = &v
	}
	return out
}

const percent = 100

func sum(ts []Term, aggs map[string]Agg) Agg {
	acc := Agg{Data: len(ts) > 0}
	for _, t := range ts {
		acc = acc.Combine(t.Coef, aggs[t.Series])
	}
	return acc
}
