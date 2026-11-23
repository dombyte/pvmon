package sitecalc

import (
	"sort"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
)

// Result is one computed live site quantity.
type Result struct {
	Value  float64
	Unit   string
	Approx bool // SoC as plain mean because a battery has no capacity
}

// battery is a battery meter with its capacity for SoC weighting.
type battery struct {
	id       string
	capacity float64
}

// plan groups the meters of a site by what the calculation needs; built once per site.
type plan struct {
	pv        []string
	grid      string
	batteries []battery
	direct    map[string][]string // category → consumers directly under the site
	keys      []string            // all output keys, for deletion when missing
}

func newPlan(s *site.Site) plan {
	p := plan{direct: map[string][]string{}}
	ids := make([]string, 0, len(s.Meters))
	for id := range s.Meters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p.add(s.Meters[id])
	}
	for _, q := range s.Quantities() {
		p.keys = append(p.keys, outKey(q))
	}
	return p
}

func (p *plan) add(m *site.Meter) {
	if m.Nested() {
		return // PV strings and sub-meters are inside their parent
	}
	switch m.Role {
	case profile.RolePV:
		p.pv = append(p.pv, m.ID)
	case profile.RoleGrid:
		p.grid = m.ID
	case profile.RoleBattery:
		p.batteries = append(p.batteries, battery{id: m.ID, capacity: m.CapacityWh})
	case profile.RoleConsumer:
		p.direct[m.Category] = append(p.direct[m.Category], m.ID)
	}
}

func outKey(quantity string) string { return site.LiveKey(quantity) }

// Getter returns the newest value of a live key, false when it is missing.
type Getter func(key string) (float64, bool)

// compute derives the live site quantities (spec 2, 5.5). A quantity whose inputs are
// missing is left out, never computed from partial data. Pure: no I/O, clock or logging.
func (p plan) compute(get Getter) map[string]Result {
	out := map[string]Result{}
	put := func(q string, v float64, ok bool) {
		if ok {
			out[outKey(q)] = Result{Value: v, Unit: "W"}
		}
	}
	pv, pvOK := sumPower(get, p.pv)
	if len(p.pv) > 0 {
		put(site.QuantityPV, pv, pvOK)
	}
	grid, gridOK := get(p.grid + "/power")
	put(site.QuantityGridIn, max(grid, 0), gridOK)
	put(site.QuantityGridOut, max(-grid, 0), gridOK)
	bIn, bOut, batOK := p.batteryPower(get)
	if len(p.batteries) > 0 {
		put(site.QuantityBatteryIn, bIn, batOK)
		put(site.QuantityBatteryOut, bOut, batOK)
		p.soc(get, out)
	}
	cons := pv + max(grid, 0) - max(-grid, 0) + bOut - bIn
	consOK := pvOK && gridOK && batOK
	put(site.QuantityConsumption, cons, consOK)
	// Like evcc's home power: the backup output is inside it, wherever loads are wired.
	direct, directOK := p.loadpoints(get, put)
	put(site.QuantityHouseRest, cons-direct, consOK && directOK)
	return out
}

// loadpoints puts one quantity per category and returns the sum of all direct consumers.
func (p plan) loadpoints(get Getter, put func(string, float64, bool)) (float64, bool) {
	var total float64
	allOK := true
	for _, cat := range profile.Categories() {
		ids := p.direct[cat]
		if len(ids) == 0 {
			continue
		}
		v, ok := sumPower(get, ids)
		put(site.QuantityLoadpoints+cat, v, ok)
		total += v
		allOK = allOK && ok
	}
	return total, allOK
}

func (p plan) batteryPower(get Getter) (in, out float64, ok bool) {
	for _, b := range p.batteries {
		v, has := get(b.id + "/power")
		if !has {
			return 0, 0, false
		}
		in += max(-v, 0)
		out += max(v, 0)
	}
	return in, out, true
}

// soc is weighted by capacity when every battery has one, else the plain mean (≈).
func (p plan) soc(get Getter, out map[string]Result) {
	var sum, weights float64
	weighted := true
	for _, b := range p.batteries {
		weighted = weighted && b.capacity > 0
	}
	for _, b := range p.batteries {
		v, ok := get(b.id + "/soc")
		if !ok {
			return
		}
		w := 1.0
		if weighted {
			w = b.capacity
		}
		sum += v * w
		weights += w
	}
	out[site.QuantitySoC] = Result{Value: sum / weights, Unit: "%", Approx: !weighted}
}

func sumPower(get Getter, ids []string) (float64, bool) {
	var sum float64
	for _, id := range ids {
		v, ok := get(id + "/power")
		if !ok {
			return 0, false
		}
		sum += v
	}
	return sum, true
}
