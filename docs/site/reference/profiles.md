# Profile schema

The example shows the structure of a profile. Its addresses are not real. For complete
examples, refer to the
[built-in profiles](https://github.com/dombyte/pvmon/tree/main/profiles).

```yaml
schema: 1
profile: vendor/model             # unique ID
version: 1                        # increase it at each change
vendor: Vendor
models: ["Model-*"]
transport: modbus                 # modbus | http
verified: false
modbus: { byte_order: big, word_order: big, function: input }   # input (04) | holding (03)
read:
  mode: range                     # range | direct
  ranges: [[33000, 33124]]        # not larger than max_registers, tested on a real device
max_power: 15000                  # W, necessary for a lifetime counter
netting: app                      # default for grid blocks
blocks:
  battery:
    role: battery
    channels:
      power:     { addr: 33149, type: i32, unit: W, sign_from: { addr: 33135, discharge_when: 1 } }
      soc:       { addr: 33139, type: u16, unit: "%" }
      energy_in: { addr: 33163, type: u16, scale: 0.1, unit: kWh, counter: daily_reset, reset_group: daily, resolution: 0.1 }
status:
  operating: { addr: 33095, type: u16, map: states }
maps:
  states:
    0x0003: { level: ok,    text: { en: Generating, de: Einspeisung } }
    0x1015: { level: fault, text: { en: Grid overvoltage, de: Netzüberspannung } }
```

## HTTP profiles

An HTTP profile has a list of endpoints. A channel uses `path` as an alternative to `addr`.
The path is a subset of JSONPath, for example `$.a.b[0].c`.

```yaml
transport: http
http:
  auth: digest                    # basic | digest, if the device has authentication on
  endpoints:
    - { id: live, url: "/rpc/EM.GetStatus?id=0" }
blocks:
  total:
    role: grid
    roles_allowed: [grid, consumer]
    channels:
      power: { endpoint: live, path: "$.total_act_power", type: number, unit: W }
```

## Values

| Item | Values |
| --- | --- |
| Modbus types | `u16`, `i16`, `u32`, `i32`, `u64`, `f32`, `f64`, `bitfield`, `string` |
| JSON types | `number`, `bool`, `string` |
| Transforms | `scale`, `offset`, `invert`, `sign_from`, `sum`, `product`, `derive` |
| Counter kinds | `lifetime`, `daily_reset`, `none` |
| Status levels | `ok`, `notice`, `fault` |

## Other elements

| Element | Function |
| --- | --- |
| `read.max_registers` | Maximum registers in one Modbus request (default 125) |
| `reset_group` | Daily counters that the device resets together |
| `resolution` | Step size of a counter |
| `derive: share` | Divides a shared counter between blocks by their power (estimate) |
| `product: [a, b]` | Channel that is the product of two channels |
| `sum: [paths]` | Channel that is the sum of some values |
| `value_id` | Finds a value by its ID in a parallel list of IDs |
| role `phase` | Block of one phase. It cannot be a site meter |
| status `list`, `bool`, `nonzero` | Status of HTTP devices |
| `label` | Name of a status entry in `de` and `en` |
| `renamed_from` | Keeps the history of a channel with a new name |

## Rules

- Set the signs as the [site model](/concepts/site-model) defines them.
- For import and export, use only active energy. Do not use apparent energy.
