package simulate

import (
	"fmt"
	"sort"
)

// Setups returns the built-in simulation setups (reference setups of spec 1). All of them
// write to ./data/simulate.db so real data is never touched.
func Setups() map[string]string {
	const r1 = `
storage: { path: ./data/simulate.db }
connections:
  lan-solis: { address: "tcp://sim-solis:502" }
devices:
  wr1: { connection: lan-solis, unit: 1, profile: solis/s6-hybrid, interval: 5s }
meters:
  pv:       { device: wr1, source: pv, kwp: 9.9, label: Roof }
  pv-mppt1: { device: wr1, source: pv1, parent: pv, label: South }
  pv-mppt2: { device: wr1, source: pv2, parent: pv, label: West }
  battery:  { device: wr1, source: battery, capacity: 10kWh }
  backup:   { device: wr1, source: backup }
`
	r3 := r1 + `
  inv-grid: { device: wr1, source: grid, role: inverter }
  grid:     { device: meter, source: total, critical: true }
`
	r3dev := `
  meter: { address: "http://sim-shelly", profile: shelly/pro-3em, interval: 2s }
`
	r4dev := r3dev + `
  goe:   { address: "http://sim-goe", profile: goe/charger-v2, interval: 10s }
  warp:  { address: "http://sim-warp", profile: tinkerforge/warp3, interval: 10s }
`
	// The heat pump meter shares the Solis connection as unit 2 (one RS485 bus).
	r4dev += "  hp-meter: { connection: lan-solis, unit: 2, profile: eastron/sdm630, " +
		"interval: 5s }\n"
	return map[string]string{
		"R1": r1 + grid,
		"R2": insertDevices(r1+grid+balcony("pv"), dtu),
		"R3": insertDevices(r3, r3dev),
		"R4": insertDevices(r3+r4meters, r4dev),
		"R5": r5,
		"R6": insertDevices(r1+grid+balcony("pv_power"), dtu),
		"R7": r7,
	}
}

// r4meters are the loadpoints of R4: two wallboxes and a heat pump.
const r4meters = `
  wb-garage: { device: goe, source: charger }
  wb-drive:  { device: warp, source: charger }
  heatpump:  { device: hp-meter, source: total, role: consumer, category: heatpump, parent: site }
`

// grid is the grid meter of the setups without a dedicated one.
const grid = "  grid:     { device: wr1, source: grid, critical: true }\n"

// balcony is the meter of the OpenDTU in R2 (lifetime counter) and R6 (power only).
func balcony(source string) string {
	return "  balcony:  { device: dtu, source: " + source + ", label: Balcony }\n"
}

// dtu is an AC-coupled second PV inverter (R2, R6): micro inverters behind an OpenDTU.
const dtu = `
  dtu: { address: "http://sim-dtu", profile: opendtu/total, interval: 5s }
`

// r5 has two hybrid inverters with one battery each on one bus; the first sees the grid.
const r5 = `
storage: { path: ./data/simulate.db }
connections:
  lan-solis: { address: "tcp://sim-solis:502" }
devices:
  wr1: { connection: lan-solis, unit: 1, profile: solis/s6-hybrid, interval: 5s }
  wr2: { connection: lan-solis, unit: 2, profile: solis/s6-hybrid, interval: 5s }
meters:
  pv-east:  { device: wr1, source: pv, label: East }
  pv-west:  { device: wr2, source: pv, label: West }
  bat1:     { device: wr1, source: battery, capacity: 10kWh }
  bat2:     { device: wr2, source: battery, capacity: 5kWh }
  grid:     { device: wr1, source: grid, critical: true }
`

// r7 shows all four strings below the PV total; no backup output is wired.
const r7 = `
storage: { path: ./data/simulate.db }
connections:
  lan-solis: { address: "tcp://sim-solis:502" }
devices:
  wr1: { connection: lan-solis, unit: 1, profile: solis/s6-hybrid, interval: 5s }
meters:
  pv:       { device: wr1, source: pv, kwp: 12 }
  mppt1:    { device: wr1, source: pv1, parent: pv }
  mppt2:    { device: wr1, source: pv2, parent: pv }
  mppt3:    { device: wr1, source: pv3, parent: pv }
  mppt4:    { device: wr1, source: pv4, parent: pv }
  battery:  { device: wr1, source: battery, capacity: 10kWh }
  grid:     { device: wr1, source: grid, critical: true }
`

// insertDevices adds device lines after the devices: section header.
func insertDevices(yaml, devices string) string {
	const header = "devices:\n"
	for i := 0; i+len(header) <= len(yaml); i++ {
		if yaml[i:i+len(header)] == header {
			return yaml[:i+len(header)] + devices[1:] + yaml[i+len(header):]
		}
	}
	return yaml
}

// Setup returns the YAML of a setup or an error listing the known ones.
func Setup(name string) ([]byte, error) {
	s, ok := Setups()[name]
	if !ok {
		names := make([]string, 0, len(Setups()))
		for n := range Setups() {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("simulate: unknown setup %q (known: %v)", name, names)
	}
	return []byte(s), nil
}
