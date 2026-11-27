# Energy and data quality

## Calculation

pvmon calculates energy from the counters of the devices. It books the difference between
two polls. Live power is only for the display.

pvmon stores the data in intervals of 15 minutes. It calculates days, months, years and
total values from these intervals.

| Counter kind | Value from the device | Booked energy |
| --- | --- | --- |
| `lifetime` | a counter that only increases | the difference |
| `daily_reset` | a counter that the device resets each day | the difference, and the new value after a reset |
| `none` | power only | power × time between two good polls |

## Incorrect counter values

- If a counter increases faster than `max_power` permits, pvmon waits. It reads the value
  again at the next poll.
- If a `lifetime` counter decreases, pvmon assumes a new meter. It starts from the new
  value and books no energy for this step.

## Missing and estimated data

- pvmon does not show missing data as 0. A chart shows a gap.
- An incomplete period shows the number of complete days, as `n/m days`.
- The mark ≈ shows an estimate. For example, pvmon distributes the counter energy of an
  outage across the outage time. The total stays correct.
- For a device without a counter, the data of an outage stays missing.

## Time

pvmon uses the clock of the server for all timestamps. It does not use the clocks of the
devices. Keep the server clock correct.

A day has 96 intervals. On the days of a daylight saving time change, it has 92 or 100.
