# Example setups

pvmon has tests for these setups. You can start each setup with simulated devices:

```bash
pvmon simulate --setup R4
```

| ID | Devices | Result |
| --- | --- | --- |
| R1 | One hybrid inverter with a battery | The inverter supplies PV, battery and grid |
| R2 | A hybrid inverter and a second PV inverter | Only one device supplies grid values |
| R3 | A separate grid meter | The grid values come from this meter |
| R4 | Two wallboxes and a heat pump | The flow shows one node for each category |
| R5 | Two batteries on two inverters | The state of charge uses the capacities as weights |
| R6 | A micro inverter without an energy counter | pvmon calculates energy from power (≈) |
| R7 | Separate PV strings | Each string is a `pv` meter below the PV total |

## Separate grid meter

The inverter also supplies grid values. Use them only for diagnosis:

```yaml
meters:
  inv-grid: { device: wr1, source: grid, role: inverter }
  grid:     { device: meter, source: total, critical: true }
```

With `critical: true`, `GET /health` gives an error if this meter is offline longer than
`status.offline_after`.

## Wallbox and heat pump

```yaml
devices:
  goe:      { address: "http://10.0.0.40", profile: goe/charger-v2, interval: 10s }
  hp-meter: { connection: lan-solis, unit: 2, profile: eastron/sdm630 }

meters:
  wb-garage: { device: goe, source: charger, label: Garage }
  heatpump:  { device: hp-meter, source: total, role: consumer, category: heatpump, parent: site }
```

## PV strings

```yaml
meters:
  pv:       { device: wr1, source: pv, kwp: 10, label: Roof }
  pv-mppt1: { device: wr1, source: pv1, parent: pv, label: South }
  pv-mppt2: { device: wr1, source: pv2, parent: pv, label: West }
```
