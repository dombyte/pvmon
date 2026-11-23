package site

import (
	"sort"

	"github.com/dombyte/pvmon/internal/profile"
)

// Site quantity IDs (spec 2). Loadpoint quantities are QuantityLoadpoints + category.
const (
	QuantityPV          = "site/pv"
	QuantityGridIn      = "site/grid_in"
	QuantityGridOut     = "site/grid_out"
	QuantityBatteryIn   = "site/battery_in"
	QuantityBatteryOut  = "site/battery_out"
	QuantitySoC         = "site/soc"
	QuantityConsumption = "site/consumption"
	QuantityHouseRest   = "site/house_rest"
	QuantityLoadpoints  = "site/loadpoints/"
)

// Quantities returns the site quantities this site has, in a fixed order: PV only with a
// pv meter, battery quantities only with a battery, one loadpoint quantity per category
// present directly under the site.
func (s *Site) Quantities() []string {
	c := s.Capabilities()
	var out []string
	if c.PV > 0 {
		out = append(out, QuantityPV)
	}
	out = append(out, QuantityGridIn, QuantityGridOut)
	if c.Battery > 0 {
		out = append(out, QuantityBatteryIn, QuantityBatteryOut, QuantitySoC)
	}
	out = append(out, QuantityConsumption, QuantityHouseRest)
	for _, cat := range profile.Categories() {
		if c.Loadpoints[cat] > 0 {
			out = append(out, QuantityLoadpoints+cat)
		}
	}
	return out
}

// LiveKey returns the live key of a site quantity (spec 9.3): power quantities carry the
// suffix ".power", the state of charge is the bare quantity ID.
func LiveKey(quantity string) string {
	if quantity == QuantitySoC {
		return quantity
	}
	return quantity + ".power"
}

// LiveKeys returns every key a live client may subscribe to: all channels of every meter
// ("<meter>/<channel>") and the live site quantities, sorted.
func (s *Site) LiveKeys() []string {
	var out []string
	for _, id := range sorted(s.Meters) {
		for _, ch := range sorted(s.Meters[id].Block.Channels) {
			out = append(out, id+"/"+ch)
		}
	}
	for _, q := range s.Quantities() {
		out = append(out, LiveKey(q))
	}
	sort.Strings(out)
	return out
}
