# Grid netting

In a three-phase system, one phase can import and a different phase can export at the
same time. Most utility meters first add the phases. Some devices count each phase separately. Their import and
export values are then too high.

## Setting

Set `netting` on the device of the grid meter:

| Value | Function | Use it if |
| --- | --- | --- |
| `meter` | pvmon uses the import and export counters of the device | the device adds the phases |
| `app` | pvmon adds import and export at each poll and books the result | the device counts each phase separately |

```yaml
devices:
  meter: { address: "tcp://10.0.0.50:502", profile: eastron/sdm630, netting: app }
```

Each profile that can supply a grid meter has a default value. Your configuration has
priority.

| Profile | Default |
| --- | --- |
| `shelly/pro-3em` | `meter` |
| `shelly/3em` | `meter` (not verified) |
| `eastron/sdm630` | `app` |
| `solis/s6-hybrid` | `app` |

In a single-phase system, `app` causes no error.

## Method of `app`

If the counters have steps of 1 Wh or less, pvmon uses the difference between the import
and export counters. If the steps are larger, pvmon calculates the energy from the total
power.

Poll the grid meter every 1 to 2 seconds on a separate connection. If the direction of the
total power changes between two polls, import and export are too low. With fine counters,
the difference between import and export stays correct.

## How to select the value

Compare the counters of the device:

| Observation | Device | Value |
| --- | --- | --- |
| The sum of the phase imports is equal to the total import. This is also true for export | counts each phase separately | `app` |
| The two totals are lower than the phase sums by the same quantity | adds the phases | `meter` |

pvmon does not do this comparison.
