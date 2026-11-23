#!/usr/bin/env bash
# Writes expected.json for the Gate 1 replay test from a solis database:
#   scripts/gate1-expected.sh <solis.db> <YYYY-MM-DD> > expected.json
# Maps the solis daily registers onto the series of the Gate 1 site (internal/collector,
# solisSite): pv, battery, grid (netting: meter), backup (33596). Consumption
# (energy_consumption_daily, household_energy_daily) is no collector series; pvmon derives
# it per site. solis stores the inverter's daily grid registers (0.1 kWh), pvmon books the
# external meter's lifetime counters (0.01 kWh): grid is compared within 0.1 kWh.
set -euo pipefail
db=${1:?solis.db}; day=${2:?date YYYY-MM-DD}
q() { sqlite3 "$db" "SELECT value FROM daily_values WHERE date='$day' AND register_key='$1'"; }
cat <<JSON
{
  "date": "$day",
  "series": {
    "pv/energy_out": $(q pv_energy_daily),
    "battery/energy_in": $(q battery_charge_daily),
    "battery/energy_out": $(q battery_discharge_daily),
    "grid/energy_in": $(q grid_import_daily),
    "grid/energy_out": $(q grid_export_daily),
    "backup/energy_in": $(q backup_energy_daily)
  },
  "tolerance": { "grid/energy_in": 0.1, "grid/energy_out": 0.1 }
}
JSON
