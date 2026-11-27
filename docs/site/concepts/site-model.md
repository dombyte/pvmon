# Site model

pvmon reads **devices**. It calculates and shows values for **meters**.

| Term | Description | Example |
| --- | --- | --- |
| Site | The household. Each installation has one site | Home |
| Connection | A physical access path with an address | `tcp://10.0.0.5:502` |
| Device | A connection, a unit ID (Modbus) and a profile | Solis S6 |
| Profile | The description of a device model | `solis/s6-hybrid` |
| Meter | A measurement point with one role. One device supplies it | Roof, Grid, Wallbox |
| Channel | A value of a meter with a unit and a sign rule | `power`, `energy_in` |
| Series | The stored history of a channel or a site quantity | `grid/energy_in` |

## Roles and signs

| Role | A `power` above 0 is | Energy channels | More than one |
| --- | --- | --- | --- |
| `pv` | generation | `energy_out` | yes |
| `grid` | import | `energy_in` (import), `energy_out` (export) | no, exactly one is necessary |
| `battery` | discharge | `energy_in` (charge), `energy_out` (discharge), `soc` | yes |
| `consumer` | consumption | `energy_in` | yes, each with a category |
| `backup` | consumption on the backup output | `energy_in` | yes |
| `inverter` | AC output of an inverter, for diagnosis | `energy_out`, `energy_in` | yes |

The profile sets the signs. The web interface does not change them.

## Parent meters

Each consumer has a parent: the site or a different meter. pvmon subtracts the consumer
from the consumption of its parent. Thus pvmon counts the energy one time.

For the PV strings (MPPT) of an inverter, set the total `pv` meter as their parent. pvmon
does not add the strings to the PV total.

The consumer categories are `wallbox`, `heatpump`, `heater` and `other`.

## Site quantities

pvmon calculates the site quantities from the meters.

| Quantity | Definition |
| --- | --- |
| `site/pv` | Sum of the PV meters that have no parent |
| `site/grid_in`, `site/grid_out` | Import and export of the grid meter, after netting |
| `site/battery_in`, `site/battery_out` | Sum of the batteries |
| `site/soc` | State of charge of all batteries |
| `site/consumption` | PV + import − export + discharge − charge |
| `site/loadpoints/<category>` | Sum of the consumers of one category with the site as parent |
| `site/house_rest` | Consumption minus the consumers with the site as parent |
| `site/self_sufficiency` | (consumption − import) / consumption |
| `site/self_consumption` | (PV − export) / PV |

If each battery has a `capacity`, `site/soc` uses the capacities as weights. If not,
`site/soc` is the mean value, with the mark ≈.

A negative `site/house_rest` shows that the meter values do not agree.

## Grid meter and inverter

An inverter can report grid values also if you have a separate grid meter. In this case,
give the grid block of the inverter the role `inverter`.
