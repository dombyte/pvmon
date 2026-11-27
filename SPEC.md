# pvmon — Specification

This file is the complete specification of pvmon (PV Monitor). It describes the product behaviour and the architecture. It records every product and design decision. [AGENTS.md](AGENTS.md) gives the rules for the code.

The keywords have these meanings:

- **MUST** means the rule is required.
- **SHOULD** means the rule is required unless a written reason exists.
- **MAY** means the rule is optional.

## 1. Purpose and scope

PV Monitor shows a household where its electricity comes from, where it goes and how self-sufficient it is. It also shows when a device is faulty. It works for any combination of inverters, batteries, meters and loadpoints from different vendors.

pvmon is a new application. It is not a fork of solis, an earlier monitoring application for Solis inverters. It keeps the proven rules of solis but not its code.

**Goals (v1):**

1. Describe one site from devices and measuring points, through the UI, a config file or both.
2. Read devices over Modbus TCP, Modbus RTU and HTTP/JSON with declarative profiles. New models need no Go code.
3. Keep one consistent energy balance for the site, with a defined source of truth for every quantity.
4. Store only raw data in 15-minute buckets. Derive day, month, year and lifetime values on demand and cache them.
5. Show live values over WebSocket. Show history, reports and status over REST.
6. Build the UI from the site model. The UI shows only what exists in the setup.
7. Support German and English from the first release. Offer an installable PWA that works offline.
8. Support multiple admin accounts, secure login and API tokens. Reading works without login.
9. Import an existing solis database without data loss.

**Not in v1, but designed for later:**

- MQTT import and export
- webhooks
- SunSpec discovery
- single control commands, such as "set max SoC"
- an admin UI for data correction
- reading the utility meter with an optical head
- comparison with utility meter readings

**Never:**

- an energy management system (surplus charging, control loops, tariff optimisation)
- forecasts
- cost or savings calculation
- metrics export (Prometheus)
- cloud connectivity
- multiple sites per installation
- telemetry

**Reference setups.** All reference setups MUST work. Golden tests check them.

| ID | Setup | Characteristic |
| --- | --- | --- |
| R1 | One hybrid inverter (Solis S6) with battery | the inverter reports PV, battery and grid |
| R2 | Hybrid inverter and a second PV inverter (AC-coupled) | PV from two devices, only one device sees the grid |
| R3 | Dedicated grid meter (SDM630 or Shelly Pro 3EM) | grid values come from the meter, not from the inverter |
| R4 | Loadpoints: two wallboxes (go-e, WARP3) and a heat pump | measured sub-consumers, one flow node for each category |
| R5 | Two batteries on two inverters | values for each battery and capacity-weighted SoC |
| R6 | Micro inverter without energy counter | energy is integrated from power and marked ≈ |
| R7 | PV strings shown separately (MPPT 1/2) | strings are `pv` meters below the PV total (`parent`) and are never added to `site/pv` again |

## 2. Domain model

The core separation is **device** (what pvmon polls) versus **meter** (what pvmon measures and where it sits in the energy flow). All evaluation, storage and UI work only with meters and site quantities. They never use vendor-specific registers.

| Term | Definition | Example |
| --- | --- | --- |
| Site | The household. Exactly one for each installation. The time zone comes only from `TZ` | "Home" |
| Connection | A physical access path, identified by its address. Devices with the same address share it | `rtu:///dev/ttyUSB0`, `tcp://10.0.0.5:502`, `http://10.0.0.30` |
| Device | Connection + unit ID (Modbus) + profile | Solis S6, Shelly Pro 3EM |
| Profile | Declarative description of a device model (section 4) | `solis/s6-hybrid` |
| Meter | A logical measuring point with one **role**. Exactly one device provides it | "PV roof", "Grid", "Wallbox garage" |
| Channel | A normalised quantity of a meter with a fixed unit and sign rule | `power`, `energy_in`, `energy_out`, `soc` |
| Series | A stored time series: one meter channel or one site quantity | `garage/energy_in`, `site/consumption` |
| Status | Operating and fault state of a device, mapped to `ok`, `notice` or `fault` | Solis `0x1015` → fault |

**Roles and sign rules.** The profile MUST normalise the signs. The UI never flips signs.

| Role | `power` > 0 means | Energy channels | Multiple per site |
| --- | --- | --- | --- |
| `pv` | generation | `energy_out` (generated) | yes |
| `grid` | import from grid | `energy_in` (import), `energy_out` (export) | exactly one, required |
| `battery` | discharging | `energy_in` (charged), `energy_out` (discharged); `soc` in % | yes |
| `consumer` | consumption | `energy_in` | yes; has `category` |
| `backup` | consumption on the backup output | `energy_in` | yes |
| `inverter` | AC output of an inverter (diagnostic only) | `energy_out`, `energy_in` | yes |

**Consumer categories:** `wallbox`, `heatpump`, `heater`, `other`. The category controls the flow diagram (one node for each category) and the report sections.

**Hierarchy.** Every `consumer` has a `parent`. The parent is the site or another meter. The remainder of a parent is its consumption minus its sub-meters. Pvmon subtracts a sub-meter from the remainder of its parent and never adds it. This gives "House (rest)" and prevents double counting. A cycle is a validation error.

A `pv` meter MAY name a top-level `pv` meter as `parent` (one level). This is a string (MPPT) inside the PV total of the inverter. The breakdown of the total shows the strings. Pvmon never adds strings to `site/pv`.

**Site quantities.** Pvmon computes site quantities only from meters. It never takes them from a device.

| Quantity | Definition |
| --- | --- |
| `site/pv` | Σ top-level `pv` meters (strings below a PV total are inside it) |
| `site/grid_in`, `site/grid_out` | the single `grid` meter (after netting, section 6) |
| `site/battery_in`, `site/battery_out`, `site/soc` | Σ `battery` meters. SoC is weighted by `capacity` when every battery has one. Otherwise it is the arithmetic mean, marked ≈ |
| `site/consumption` | PV + grid import − grid export + battery discharge − battery charge |
| `site/loadpoints/<category>` | Σ consumers of that category directly under the site |
| `site/house_rest` | consumption − Σ direct consumer children (like the home power of evcc). `backup` meters are inside it. Loads can be wired to the household or to the backup output, and only the balance does not depend on the wiring. A negative value is reported as balance difference and is never clamped |
| `site/self_sufficiency` | (consumption − import) / consumption, from summed energy |
| `site/self_consumption` | (PV − export) / PV, from summed energy |
| `site/grid_net` | export − import. This is a debug value and is never in reports |

A device can report grid values while a dedicated grid meter exists. In this case the grid channels of the device become `inverter` diagnostics.

**Only what is used.** A device polls only the channels of the blocks that its configured meters use. It also polls their dependencies and its status fields. The dependencies are:

- the product inputs
- for a share channel: the shared counter and the weight channel of every block that shares it

Pvmon decodes and tracks only these channels. By default it stores only `power`, `soc` and energy counters of them. The key `history` adds more channels. A dependency that is not a meter keeps only its hidden counter state.

## 3. Configuration

Configuration has two layers with one schema. The first layer is `config.yaml` (plus `PVMON_` environment overrides). The second layer is the UI configuration stored in the database. The file always wins. You MAY use both layers at the same time.

**Rules:**

- The effective configuration is the DB layer, overlaid by the file layer, overlaid by environment variables. The order of evaluation is: defaults → DB → file → env → validate. koanf merges the layers. It uses the confmap provider for defaults and the DB layer, the file provider with a YAML parser, and the env provider.
- Objects (connections, devices, meters) have stable IDs (`[a-z][a-z0-9-]{0,31}`). An object whose ID appears in the file belongs entirely to the file. The UI shows it as read-only (lock icon, tooltip "defined in config.yaml"). Pvmon ignores a stored UI object with the same ID. Scalar settings defined in the file are locked in the same way. Keys that environment variables set (and the objects they touch) are also locked.
- The UI changes objects and only these settings: `site.name`, `site.interval`, `auth.public_read`, `status.offline_after`, `storage.retention`, `storage.backup_interval`, `storage.max_backups`. All other settings need a restart. These are port, paths, logging, sessions and trusted proxies. Set them in the file or in the environment.
- Every saved UI change increments `config_rev`. Pvmon validates the change as a whole and applies it without restart. If connections, devices, meters or `site.interval` changed, the supervisor rebuilds the collectors and the site calculator. It stops the collectors of removed connections. The HTTP routes always switch to the new configuration. Clients receive `site_changed`.
- A stored UI configuration can become invalid because the file or a profile changed. This is a startup error that lists its problems.
- Validation reports every problem with its path (`meters.grid.device: unknown device "meterr"`), never only the first. An invalid file or env value is a startup error. Pvmon rejects an invalid UI change with HTTP 422 and the full list.
- Every key has a default (zero values included). A test enforces this. Durations need a unit (`5s`, `30d`, `10y`). Pvmon rejects bare numbers.
- Environment overrides use the prefix `PVMON_` and a double underscore for nesting. `PVMON_APP__LOG_LEVEL` sets `app.log_level`. `PVMON_DEVICES__WR1__INTERVAL` sets `devices.wr1.interval`. You cannot set IDs with `-` by env. Sizes and energies also need a unit (`50MB`, `10kWh`).
- In the file, write secrets only as references: `${env:NAME}` or `${file:/run/secrets/name}`. Pvmon stores secrets that you enter in the UI in encrypted form (section 10). Secrets never appear in logs, in `/api/site` or in `pvmon config export`. Pvmon shows them as `***`.
- The time zone comes only from `TZ`.
- A removed or unknown key is a startup warning. Pvmon never ignores it silently.

**App settings (excerpt, all with defaults):**

| Key | Default | Meaning |
| --- | --- | --- |
| `app.port` | 8080 | HTTP port |
| `app.log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `app.request_timeout` | `30s` | HTTP and storage read timeout (≥ 1s) |
| `auth.public_read` | `true` | read access (UI, REST, WS) without login |
| `auth.session_idle` / `auth.session_max` | `7d` / `30d` | session lifetime |
| `storage.path` | `./data/pvmon.db` | database file |
| `storage.retention` | `10y` | the single retention for all time-dependent data |
| `storage.backup_interval` / `storage.max_backups` | `24h` / `3` | online backups |
| `status.offline_after` | `15m` | daytime offline duration that counts as fault |

**Logging settings:**

| Key | Default | Meaning |
| --- | --- | --- |
| `log.file` | empty | empty = stdout only |
| `log.max_size` | `50MB` | size at which the file rotates |
| `log.max_backups` | 5 | number of rotated files to keep |
| `log.max_age` | 30d | maximum age of rotated files |
| `log.rotate_at` | none | optional list of daily times, for example `["00:00"]` |
| `log.compression` | `gzip` | `gzip`, `zstd` or `none` |

When `log.file` is set, zerolog writes JSON to stdout and to the file. timberjack rotates the file.

**Site schema (file form):**

```yaml
site:
  name: Home
  interval: 5s      # poll interval of every device without its own
connections:
  lan-solis: { address: "tcp://10.0.0.20:502", timeout: 2s }
  rs485:     { address: "rtu:///dev/ttyUSB0", speed: 9600, parity: N, stop_bits: 1 }
devices:
  wr1:       { connection: lan-solis, unit: 1, profile: solis/s6-hybrid, interval: 5s, rollover: "23:59" }
  meter:     { address: "http://10.0.0.31", profile: shelly/pro-3em, interval: 2s }
  goe:       { address: "http://10.0.0.40", profile: goe/charger-v2, interval: 10s }
  warp:      { address: "http://10.0.0.41", profile: tinkerforge/warp3, interval: 10s,
               auth: { user: admin, password: "${env:WARP_PASSWORD}" } }
meters:
  pv:        { device: wr1, source: pv, role: pv, kwp: 10, label: "Roof" }
  pv-mppt1:  { device: wr1, source: pv1, role: pv, parent: pv, label: "South" }
  pv-mppt2:  { device: wr1, source: pv2, role: pv, parent: pv, label: "West" }
  battery:   { device: wr1, source: battery, role: battery, capacity: 10kWh }
  grid:      { device: meter, source: total, role: grid, critical: true }   # netting from profile: meter
  backup:    { device: wr1, source: backup, role: backup }
  wb-garage: { device: goe, source: charger, role: consumer, category: wallbox, parent: site }
  wb-drive:  { device: warp, source: charger, role: consumer, category: wallbox, parent: site }
```

- `source` names a meter block of the profile. `role` defaults to the role that the profile gives to that block. You MAY override `role` only where the profile allows it (for example an SDM630 as `grid` or `consumer`).
- `profile` is the ID of a built-in profile (`solis/s6-hybrid`) or the path to a local profile file (`./profiles/my-meter.yaml`, `/etc/pvmon/sdm72.yaml`; section 4.5).
- `site.interval` (default 5s, ≥ 1s) is the poll interval of every device.
- The per-device options are:
  - `interval`: overrides `site.interval` (≥ 1s).
  - `timeout`: the request timeout.
  - `read`: `range` or `direct` (see 4.3).
  - `rollover`: only for `daily_reset` counters. It uses strict `HH:MM` and defaults to `00:00`. It is the local time at which the device is expected to reset its daily counters. It only limits booking before the first observed reset (see 5.3).
  - `netting`: for grid meters (section 6).
  - `history`: a list of extra channels to store. The default is only `power`, `soc` and energy counters.
- Exactly one `grid` meter is REQUIRED when any meter is configured. A configuration without meters is an empty site. The server then starts without collectors, and the UI leads the admin into the setup wizard (11.14).
- Connections with the same address are merged. Conflicting serial parameters on the same address are a validation error.

## 4. Device integration

You add new device models as YAML profiles. This needs no Go code and no release. Go code is only necessary for new transports. Version 1 is strictly read-only.

### 4.1 Layers

| Layer | Responsibility | v1 | Extended by |
| --- | --- | --- | --- |
| Transport | fetch bytes; reconnect with exponential backoff; classify errors | Modbus TCP, Modbus RTU, HTTP(S) JSON | Go code (later: MQTT subscribe, RTU-over-TCP) |
| Profile | raw data → channels: address or JSON path, type, scale, sign, counter kind, status maps | 6 profiles (4.6) | YAML |
| Special driver | devices that a profile cannot describe | none | Go, same interface |

### 4.2 Profile schema

```yaml
schema: 1
profile: solis/s6-hybrid          # unique ID, path-like
version: 3                        # bumped on every change
vendor: Solis
models: ["S6-EH1P*", "RHI-*"]
transport: modbus                 # modbus | http
verified: true                    # false = built from documentation only
modbus: { byte_order: big, word_order: big, function: input }   # input (04) | holding (03)
read:
  mode: range                     # range | direct
  ranges: [[33000, 33124], [33125, 33180]]   # each <= 125 registers, checked on a real device
max_power: 15000                  # W, REQUIRED when any counter is lifetime
netting: app                      # default for grid blocks (section 6)
blocks:                           # one block per meter the device can provide
  pv1:
    role: pv
    channels:
      power:      { addr: 33049, type: u16, scale: 10, unit: W }
      voltage:    { addr: 33049, type: u16, scale: 0.1, unit: V, history: false }
      energy_out: { derive: share, of: pv_total.energy_out, by: power }   # split of a shared counter, marked estimated
  battery:
    role: battery
    channels:
      power:      { addr: 33149, type: i32, unit: W, sign_from: { addr: 33135, discharge_when: 1 } }
      soc:        { addr: 33139, type: u16, unit: "%" }
      energy_in:  { addr: 33163, type: u16, scale: 0.1, unit: kWh, counter: daily_reset, reset_group: daily, resolution: 0.1 }
      energy_out: { addr: 33167, type: u16, scale: 0.1, unit: kWh, counter: daily_reset, reset_group: daily, resolution: 0.1 }
  grid:
    role: grid
    roles_allowed: [grid, inverter]
    channels:
      power: { addr: 33263, type: i32, unit: W, invert: true }   # device: + = export
status:
  operating: { addr: 33095, type: u16, map: solis_status }
  faults:    { addr: [33116, 33117, 33118, 33119, 33120], type: bitfield, map: solis_faults }
maps:
  solis_status:
    0x0003: { level: ok,    text: { en: Generating, de: Einspeisung } }
    0x1015: { level: fault, text: { en: Grid overvoltage, de: Netzüberspannung } }
```

The addresses in this example are illustrative. The real table is in `profiles/solis/s6-hybrid.yaml`. It comes from the solis register map and from the register table of solis. HTTP profiles use `path` (JSONPath subset: `$.a.b[0].c`) instead of `addr`. They also use `endpoints: [{ id, url, interval? }]`.

**Types:** `u16`, `i16`, `u32`, `i32`, `u64`, `f32`, `f64`, `bitfield`, `string`. JSON types: `number`, `bool`, `string`.

**Transforms:** `scale`, `offset`, `invert`, `sign_from`, `sum: [paths]`, `derive`.

**Counter kinds:** `lifetime`, `daily_reset`, `none` (section 5.3).

**Further schema elements:**

| Element | Purpose | Needed by |
| --- | --- | --- |
| `read.max_registers` (default 125) | upper limit for each Modbus request; ranges must respect it | SDM630: at most 40 float parameters = 80 registers for each request |
| `product: [a, b]` | channel = product of two channels | Solis PV strings (power = voltage × current) |
| `sum: [paths]` | channel = sum of several values | Shelly 3EM Gen1 energy of each phase |
| `value_id: { ids: <endpoint>, id: <n> }` | find a value by ID in a parallel ID array (pvmon caches the IDs and refreshes them every hour and on a length mismatch) | WARP meters (`/meters/0/value_ids`) |
| `http.auth: basic \| digest` | device authentication; the HTTP transport supports digest (including SHA-256) | Shelly Gen2, WARP |
| role `phase` | blocks for each phase feed the netting check; no site meter | SDM630, Shelly |
| status kinds `list` (non-empty → fault), `bool` (false → fault), `nonzero`, map `default` | status from HTTP devices | Shelly, WARP, go-e |
| status `level` (bitfield without map: any set bit has this level); `text` (de, en) for `list`/`bool`/`nonzero` | status without a code map | Solis BMS, Shelly, WARP |
| status `label` (de, en) | register name on the device page | all built-in profiles |
| channel type `enum` with `values: {code: name}` (live only) | readable charger states | go-e, WARP |
| `endpoint` for each HTTP channel or status, endpoint `interval` (Go duration), block `category`, string `length` in registers | devices with several endpoints, slow ID lists, consumer defaults, text registers | WARP, go-e |

The profiles and their fixtures are in `profiles/`.

### 4.3 Connections and read modes (Modbus TCP and RTU)

- Devices with the same connection address share one connection and one FIFO request queue. The unit ID distinguishes them. RS485 and Modbus gateways allow only one request at a time.
- **`range`:** the profile defines ranges. Each range is at most the `max_registers` of the profile (default 125). The profile author verifies the ranges on a device. A poll reads, inside each range, only the spans that hold needed registers (section 2, only what is used). Spans less than 16 registers apart are joined into one request. A range without needed registers is skipped. Every channel address MUST lie inside a range (validated). Dumps read the whole ranges.
- **`direct`:** pvmon reads each needed channel with its own request, one after the other (1–4 registers).
- You MAY switch a profile with `range` to `direct` for a device in the config. You MUST NOT switch a profile with `direct` to `range`. This is a validation error, because the ranges of such a profile are not verified.
- A Modbus exception "illegal data address" on a range is logged once with the hint `read: direct`.
- Every request has the connection timeout (default 2s). A poll has an overall budget of interval − 100 ms.
- Recovery on a Modbus connection that was already open: a gateway can drop a request, or a socket can be half-open. In this case pvmon closes the connection and retries the request once at once on a fresh connection. The retry happens inside the same poll. Only a fresh connection that fails starts the backoff.
- HTTP tolerates one failed request before the backoff starts.
- The backoff doubles from 1 s to a maximum of 30 s. It resets on the first success.

### 4.4 Read-only guarantee and future write path

- The transport interface `Reader` has no write method. In v1 Modbus uses only function codes 03 and 04, and HTTP uses only `GET`.
- A separate interface `Commander` is reserved for later single commands (for example `max_soc`). Profiles will declare these commands explicitly with ranges (`commands: { max_soc: { addr: …, range: [10, 100] } }`). Execution will need the token scope `control`. Pvmon will write an audit log entry for it and will never run it in a loop. In v1 `Commander` is neither implemented nor registered.

### 4.5 Profile management

- The binary embeds all built-in profiles. No profile files are necessary at runtime. A device selects a profile by its ID (`profile: shelly/pro-3em`).
- A device MAY name a local profile file by its path (`profile: ./profiles/my-meter.yaml`). A value is a path when it ends in `.yaml` or `.yml`. Otherwise it MUST be a built-in ID (`<vendor>/<model>`, lower case). If it is not, validation fails.
- Pvmon resolves relative paths against the directory of `config.yaml`. For the UI configuration, it resolves them against the data directory.
- Pvmon loads the file at start and on every config change. It validates the file like a built-in profile. Only the devices that name the file use it. There is no implicit override directory.
- A local file can have a `profile` ID that equals a built-in ID. Then the file replaces the built-in profile for these devices only. Pvmon logs this once at `info`. The devices view shows the profile as "local file" with its path.
- An unreadable or invalid local file is a startup error (file layer) or a rejected UI change (HTTP 422). Pvmon never falls back silently to the built-in profile.
- Channel keys are stable. A rename needs `renamed_from`, so that the stored series continue. A profile update never changes stored data.
- `pvmon profile validate <file>` checks the schema, address collisions, ranges and maps, including all languages.
- `pvmon profile plan <file>` proposes ranges from the channel addresses (≤ 125 registers, small gaps bridged). The author verifies the ranges on a device.
- `pvmon profile probe --device <id>` does one read and prints the normalised values.
- `pvmon dump --device <id> [--count N --interval D]` records a fixture. For Modbus it records all ranges or channel addresses as raw words. For HTTP it records the response bodies. Pvmon removes MAC addresses before it writes. Profiles read no serial numbers.
- Every built-in profile MUST ship with a fixture and golden values. CI decodes the fixture and compares. A profile without a real dump carries `verified: false`. The devices view then shows "Profile untested – please contribute a dump".

### 4.6 v1 profiles

| Profile | Transport | Blocks / roles | Counters | Default netting | Source | Verified |
| --- | --- | --- | --- | --- | --- | --- |
| `solis/s6-hybrid` | Modbus TCP/RTU, FC 04, ranges 33035–33058, 33094–33150, 33162–33167, 33263–33286, 33596 | pv (total) with strings pv1–pv4 below it, battery, grid (external meter as the inverter sees it, like evcc; grid or inverter), backup | daily\_reset 0.1 kWh (PV, battery, backup), lifetime 0.01 kWh (grid) | `app` | solis register map CSV and solis `table.go` (verified on a device; `table.go` wins on conflicts) | yes; dump pending |
| `eastron/sdm630` | Modbus RTU/TCP, FC 04, float32, max 80 registers for each request, ranges 0x0000–0x004F and 0x015A–0x0165 | total (grid or consumer), l1–l3 (phase) | lifetime kWh (import 0x0048, export 0x004A) | `app` | Eastron SDM630 Modbus protocol V1.5 | no |
| `shelly/pro-3em` | HTTP Gen2 RPC `EM.GetStatus` + `EMData.GetStatus`, digest auth | total (grid or consumer), a–c (phase) | lifetime Wh (`total_act`, `total_act_ret`) | `meter` | Shelly Gen2 API docs | yes (the device nets the phases) |
| `shelly/3em` | HTTP Gen1 `/status`, basic auth | total (grid or consumer), l1–l3 | lifetime Wh, sum of the phases | `meter` (placeholder) | Gen1 docs and community sources | no |
| `goe/charger-v2` | HTTP API v2 `/api/status?filter=…` | charger (consumer / wallbox) | lifetime Wh (`eto`); power `nrg[11]` | – | go-e API v2 key list, evcc client | dump pending |
| `tinkerforge/warp3` | HTTP API (`/meters/0/values` by value ID, `/evse/state`), digest auth | charger (consumer / wallbox) | lifetime kWh (ID 209); power ID 74 | – | WARP docs, firmware `meter_value_id.csv` | dump pending |
| `opendtu/total` | HTTP `/api/livedata/status`, basic auth | pv (power + lifetime kWh `total.YieldTotal`), pv_power (power only, energy integrated ≈) | lifetime kWh | – | OpenDTU source (`WebApi_ws_live.cpp`), evcc template | no |

Open points for each device are signs, units and conflicts between sources. The profile file and its fixture document them. Dumps from real devices resolve them.

## 5. Data pipeline

Pvmon balances energy **for each meter** from its own counters. It then sums the results to site quantities. The live path assembles live power at each tick from the newest values. The live path only displays. Everything that pvmon stores and reports comes from counters.

### 5.1 Stages

1. **Scheduler (one goroutine for each connection):** polls its devices in turn. The next poll of a device starts one interval after the start of the previous poll. Polls never overlap. The injected clock gives the poll instant once. Pvmon uses it for everything that it derives from that poll.
2. **Normaliser:** the profile turns raw data into channels with unit and sign. The output is a `Sample{device, meter, channel, value, at}`.
3. **Counter tracker (for each energy channel):** turns counter readings into energy deltas (5.3).
4. **Integrator:** for meters without a counter, it integrates power with the trapezoid rule between two consecutive successful polls (5.3).
5. **Bucket accumulator:** keeps the open 15-minute bucket of each series in memory.
   - A gauge bucket holds avg, min, max and covered seconds.
   - An energy bucket holds Wh, last counter and flags.
   - The accumulator upserts every 60 s and finally at the end of the slot. It uses one transaction with `counter_state`.
6. **Live cache:** keeps the newest value of each channel with its timestamp. A value older than 3 × its device interval is `missing`.
7. **Site calculator:** runs when the cache changes, debounced by 250 ms. It computes the live site quantities (5.5). A missing required meter makes the dependent quantity `missing`, never wrong.
8. **Derive service:** computes day, month, year, lifetime, site quantities for each bucket and ratios on request, from buckets. It caches the results in `derived_cache` (section 7).

### 5.2 Time

- Buckets are slots of 900 s in UTC (`slot = unix_seconds / 900`). Pvmon forms local days, months and years only when it reads, with the `TZ` of the site. Every real time zone offset is a multiple of 15 min. Slots therefore never cross local boundaries. DST days have 92 or 100 slots.
- All timestamps come from the server clock, never from devices.
- A clock jump of more than 5 min (NTP correction) closes the open bucket. Pvmon logs it once. The next poll is a gap poll.
- No `time.Now()` inside the pipeline. Components receive the injected `Clock`.

### 5.3 Counter kinds: everything becomes energy deltas

Pvmon keeps neither lifetime values nor daily values internally. It keeps **energy for each interval (Δ Wh)**. One tracker for each counter kind produces deltas. From there on, there is one code path. Pvmon forms an app "lifetime" value when it reads: `carry + Σ wh`.

| Kind | Raw value | Δ per poll | Protection rules |
| --- | --- | --- | --- |
| `lifetime` | monotonic counter | current − previous | Pvmon detects a `u32` overflow. Pvmon holds a Δ that is larger than `max_power` × the time since the last reading that was not held + one counter step. A counter step is the resolution or one raw count of an integer register, whichever is larger. The held Δ is a glitch (for example swapped words). Pvmon checks it again on the next poll. Energy below one step at the previous reading is counted later. A clear decrease without a reset means that the meter was replaced. Pvmon then sets a new base and Δ = 0, logs it once, and the devices view asks the admin to confirm |
| `daily_reset` | daily counter that resets itself | current − previous; after a reset: current | See the rules below this table |
| `none` | power only | power × time (trapezoid) | only between two consecutive successful polls; otherwise `gap` |

**Rules for `daily_reset`:**

- A reset is a data event. Time never triggers it, because device clocks drift and often have no NTP and no DST.
- A drop of a member beyond its resolution is pending. It must persist for 15 min. Meanwhile pvmon holds the members of the group, and the device counter buffers the energy.
- Then every member of the `reset_group` (all counters of the device reset together) books its value since the reset plus any held pre-reset rise. The booking is spread from the poll before the drop. Nothing is lost or counted twice.
- After a gap, pvmon places the reset at its expected instant. A restart that already saw the drop counts as a gap. The expected instant is the wall-clock time of the last confirmed reset. Before the first reset it is the `rollover`. It is never before the last reading before the drop. The rest of the old day stays open and never becomes 0.
- Held polls book nothing. Their slots stay open until the release books them.
- A member can show the reset but keep its value. Pvmon logs this member as stuck, and it continues normally.
- A drop that vanishes earlier is a glitch.
- A false reset that lasts longer than 15 min ends in an implausible jump. Pvmon holds this jump. It becomes the new base without energy when the next reading confirms it.
- Pvmon never books the energy of a daily register before the last confirmed reset (`counter_state.reset`). Before the first reset, the limit is the configured `rollover`.
- Pvmon logs once when no reset occurred for 25 h.

**State for each input series (`counter_state`):**

- the last raw value
- the timestamp
- the remainder below 1 Wh (float64)
- the effective resolution
- the last confirmed reset of its group
- the last poll that read the series (`seen`, the restart point of a pending reset and of the outage check after a restart)
- the start of the first outage since the last raw value changed (`gap`, 0 = none), so a delta booked in a later run is still flagged

Pvmon writes the state in the same transaction as the bucket upsert. The raw base and the booked energy therefore always match after a crash.

**Effective resolution** = max(profile `resolution`, type precision at the current value). Example for f32 kWh: steps of 1 Wh up to about 16 384 kWh, 2 Wh above that, 4 Wh above 32 768 kWh.

**Gap poll.** A poll is a gap poll in two cases:

- At least one poll of that device failed since its last successful poll.
- The app was not running in between.

**Outage.** An outage is a gap poll more than the device interval + 5 min after the last successful poll. After a restart, the last successful poll is `seen`. One failed poll and a quick restart are slow cycles, not outages.

**Spreading a counter delta.** A counter delta covers the time since its base exactly. The base is the last reading that changed the value: an unchanged reading keeps it, for `lifetime` and `daily_reset` alike. After a restart, a `lifetime` base starts at `seen`: the power since its last change is not restored, and up to `seen` the counter stood still. A `daily_reset` base keeps its last change, because it limits where a reset is placed. A delta of 0 Wh is booked only since the last booking of the counter, so pvmon writes only the new slots. Netting books the net of a poll since the last successful poll: both counters were read there. Pvmon spreads the delta over the slots of that time:

- in proportion to the power of the same block in the direction of the counter (section 2), integrated per slot (trapezoid between polls, bridged over failed polls and outages). A coarse step (for example 0.1 kWh) then lands where the power flowed.
- evenly when the block has no power, the power does not cover the whole time (after a restart) or shows no flow in it.

Every slot of the time gets a row, also with 0 Wh.

The time can start after the base, for example at a daily reset. Pvmon then counts only the part of the first slot's power that is after the start, in proportion to the time. The power before the start belongs to the previous period.

**Estimates.** For `lifetime` and `daily_reset`, only the slots from the start of an outage to the poll that books its delta are flagged `estimated`. A delta that falls into one slot is exact and is not flagged. The total is exact, so the affected days stay complete and show ≈. For `none`, the gap stays a gap: pvmon flags the slots from the last successful poll to the current poll `gap`, so the affected days are incomplete.

### 5.4 Completeness and estimates

- A day of a series is complete when all its slots have energy without `gap` (DST: 92, 96 or 100 slots). The day of a site quantity is complete when the day of every contributing meter is complete.
- Pvmon marks incomplete periods (`n/m days`). It never shows them as 0 kWh. Missing periods stay empty in charts.
- Estimated contributions propagate. These are the `estimated` flag, integrated meters and shared-counter splits. When any input is estimated, the derived value is ≈.

### 5.5 Live site quantities

Pvmon computes live site quantities with the definitions of section 2 from the newest values. Battery and grid appear as source or sink, depending on the sign.

- `site/consumption` live = PV + grid import − grid export + battery discharge − battery charge.
- `site/house_rest` live = consumption − Σ direct consumers.

Pvmon never stores live values. The 15-minute gauges come from device channels.

## 6. Grid netting across phases

Many private meters count each phase separately. The utility meter nets all phases first. PV Monitor reproduces this netting exactly for each poll from the totals. It books the netted import and export.

**Problem.** A three-phase house can import on one phase and export on another at the same time. The utility meter sums the phases first. For example, 9 kW in on L1+L2 and 3 kW out on L3 is 6 kW import and 0 export. A meter for each phase books 9 kW import **and** 3 kW export. Both counters run too high by the same amount. [Solis documents this](https://solis-service.solisinverters.com/en/support/solutions/articles/44002494719-eastron-meter-soliscloud-export-and-import-discrepancies) for Solis with Eastron. Forum reports name most SDM630 versions.

**Why the app can fix it from totals.** Over any span, the import counter of a meter for each phase grows by Σ phase intake. Its export counter grows by Σ phase output. The difference is the netted energy of all phases. The excess is in both counters and cancels. The netted energy of each poll is therefore exact. Pvmon books a positive value as import and a negative value as export.

**What is lost.** Import and export cancel inside an interval only when the *sum* of all phases changes direction between two polls. The net stays exact. Import and export are both undercounted by the same small amount. This happens most often when a battery regulates the grid to about 0 W. To reduce the loss, poll the grid meter fast (1–2 s on its own connection). Always use counters, never power snapshots, when the counters are fine enough.

**Setting `netting` on the `grid` meter:**

| Value | Meaning | Use when |
| --- | --- | --- |
| `meter` (default) | pvmon uses the import and export counters of the device as they are | the meter nets itself |
| `app` | the app nets for each poll and books the result on import or export | the meter counts each phase separately |

**Profile defaults:**

- Shelly Pro 3EM: `meter` (checked on a real device: the totals match the utility meter closely).
- Shelly 3EM Gen1: `meter` (unverified).
- SDM630: `app`.
- Solis grid channels: `app`.

The config always wins. A model variant that behaves differently gets its own profile. Single-phase installations are not affected. `app` is harmless there.

**Input selection with `app` (automatic, best method first):**

1. **Fine counters.** These are total import and export counters, or counters for each phase with the profile `sum`. Use them while their effective resolution is ≤ 1 Wh. Net = Δimport − Δexport for each poll.
2. **Signed total grid power.** This is the signed sum of the meter, or the sum of the powers of each phase. Net = power × time for each poll. It uses the same integrator as `none`.

When the counters become too coarse, the device switches to method 2 and logs it once. Solis 0.1 kWh dailies and 0.01 kWh meter totals are coarse counters. They rarely move between 5 s polls. They step at *different* polls. Method 1 would then book both sides.

The difference of the two counters stays the exact net over a long span. Once each day, the app compares this difference with the net of its own totals and logs the drift. An automatic correction will be built only if measurements show that it is necessary.

**State.** With `app`, the device counters are inputs. They are the hidden series `grid_import_raw` and `grid_export_raw` (`series.hidden = 1`). They have their own `counter_state` and the counter rules of 5.3. They have no buckets. Pvmon commits them in the same transaction as the netted `energy_in` and `energy_out` buckets.

Method 1 also stores the hidden series `grid_cancelled` = Σ min(Δimport, Δexport) for each poll. Pvmon uses it for the hint "would have been over-counted".

```go
dImp := track(rawImp, imp, at) // counter tracker of the hidden input series
dExp := track(rawExp, exp, at)
var net float64
switch {
case useCounters: // method 1
    net = dImp - dExp
case gapPoll: // method 2, but no power to integrate across a gap
    net = dImp - dExp
default: // method 2
    net = power * at.Sub(prevAt).Hours() // W * h = Wh
}
switch {
case gapPoll:
    bookGapNet(net) // net spread over the gap's buckets; days incomplete
case net >= 0:
    imp.add(net)
default:
    exp.add(-net)
}
```

After an outage, only the net across the gap is known. Pvmon books it as net, and the days are incomplete. Pvmon never books raw counter values. `site/grid_net` is the same in both modes.

**Detect which mode a meter needs.** The devices view shows this check. It never changes the setting.

| Observation | Meter | Setting |
| --- | --- | --- |
| Σ import of the phases = total import, and the same for export | does not net | `app` |
| the totals are lower than the sums of the phases by the same amount on both sides | nets itself | `meter` |
| only totals: import and export rise in the same seconds while the total power never changed sign | does not net | `app` |

**Other causes of deviation from the utility meter.** The statements from forums are unverified.

| Cause | Typical size | Detectable | Handling |
| --- | --- | --- | --- |
| the meter does not net the phases | large | yes | `netting: app`; hint for Solis + SDM630MCT firmware CX0385 |
| the total flow changes direction between polls | estimate ≤ 0.6 kWh/day for each direction (±50 W battery oscillation, 5 s polls; arithmetic) | yes | hint from `grid_cancelled` |
| accuracy class (MID B ±1 %, CTs 1–3 %) | 1–3 % | only against readings | documented |
| CT reversed | large | yes (a phase exports at night with PV = 0) | hint; fix the installation ([Solis](https://solis-service.solisinverters.com/en/support/solutions/articles/44002852865-smart-meter-phase-rotation-and-ct-polarity-verification)) |
| CT and voltage on different phases | 10–150 % on that phase | yes (power factor far from 1) | hint; needs values for each phase in the profile |
| loads before the CT | any | only against readings | documented |
| wrong register (apparent energy, import+export total) | up to tens of % | partly | profile rule: active import and export only |
| currents below the starting threshold | small | hardly | documented |
| swapped words, spikes | absurd jumps | yes | `max_power` guard |
| comparison of different moments | ±1 kWh | – | documented |
| meter self-consumption, inverter standby | 10–50 Wh/day | – | documented |

Reference: German utility meters net the phases continuously and count into 1.8.0 and 2.8.0. The 15-minute gateway values are register readings. They are not a netting window ([akkudoktor forum](https://akkudoktor.net/t/ueber-welchen-zeitraum-integrieren-die-verschiedenen-zaehler-den-saldierten-verbrauch/40921)).

## 7. Storage

SQLite (WAL, one connection) stores only raw data in 15-minute buckets, plus master data. Pvmon computes every derived value on demand. It caches the value in a table that you can drop at any time. One retention applies to everything that depends on time.

| Class | Tables | Lost if deleted? |
| --- | --- | --- |
| Raw data | `b15_gauge`, `b15_energy`, `status_event` | yes |
| Master data | `series`, `counter_state`, `config_*`, `user`, `session`, `api_token`, `audit_log`, `meta` | yes |
| Carry | `carry` (energy of purged time, for lifetime totals) | yes |
| Cache | `derived_cache` | no, pvmon recomputes it |

### 7.1 Schema (v1)

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) WITHOUT ROWID;
-- schema_version, created_at, purged_before, import_source

CREATE TABLE series (
  id          INTEGER PRIMARY KEY,
  meter_id    TEXT    NOT NULL,          -- 'site' for site quantities
  channel     TEXT    NOT NULL,          -- power, soc, energy_in, energy_out, grid_import_raw ...
  kind        INTEGER NOT NULL,          -- 1 gauge, 2 energy
  origin      INTEGER NOT NULL,          -- 1 measured, 2 integrated, 3 derived share
  hidden      INTEGER NOT NULL DEFAULT 0,
  unit        TEXT    NOT NULL,
  active_from INTEGER NOT NULL,          -- slot
  active_to   INTEGER,                   -- NULL = active
  UNIQUE (meter_id, channel, active_from)
);

CREATE TABLE b15_gauge (
  series_id INTEGER NOT NULL, slot INTEGER NOT NULL,
  avg INTEGER NOT NULL, min INTEGER NOT NULL, max INTEGER NOT NULL,  -- 1/1000 unit
  cover INTEGER NOT NULL,                -- covered seconds 0..900
  PRIMARY KEY (series_id, slot)
) WITHOUT ROWID;

CREATE TABLE b15_energy (
  series_id INTEGER NOT NULL, slot INTEGER NOT NULL,
  wh INTEGER NOT NULL,                   -- mWh in this bucket (>= 0)
  counter INTEGER,                       -- raw counter at bucket end in mWh; NULL if integrated
  flags INTEGER NOT NULL DEFAULT 0,      -- 1 estimated, 2 gap, 4 reset_seen, 8 imported
  PRIMARY KEY (series_id, slot)
) WITHOUT ROWID;

CREATE TABLE counter_state (
  series_id INTEGER PRIMARY KEY,
  raw REAL, at INTEGER NOT NULL,         -- last raw value, unix ms
  remainder REAL NOT NULL DEFAULT 0,     -- below 1 Wh
  resolution REAL, reset INTEGER NOT NULL DEFAULT 0, -- unix ms of the last daily reset, 0 none
  seen INTEGER NOT NULL DEFAULT 0,       -- unix ms of the last poll that read it
  gap INTEGER NOT NULL DEFAULT 0         -- unix ms of the first outage since raw changed, 0 none
);

CREATE TABLE status_event (
  device_id TEXT NOT NULL, register TEXT NOT NULL, at INTEGER NOT NULL,
  code INTEGER NOT NULL,                 -- raw code; level and texts from the profile
  PRIMARY KEY (device_id, register, at)  -- register "@connection": 0 online, 1 offline, 2 stopped
) WITHOUT ROWID;

CREATE TABLE carry (
  key TEXT PRIMARY KEY,                  -- series id or derived key
  wh REAL NOT NULL, before_slot INTEGER NOT NULL
) WITHOUT ROWID;

CREATE TABLE derived_cache (
  key TEXT PRIMARY KEY,                  -- 'site/consumption|month|2026-10'
  config_rev INTEGER NOT NULL,
  value TEXT NOT NULL                    -- JSON: value, complete, estimated, missing_days
) WITHOUT ROWID;

CREATE TABLE config_object (
  kind TEXT NOT NULL, id TEXT NOT NULL,  -- connection | device | meter
  body TEXT NOT NULL,                    -- JSON; secrets as enc:v1:<base64>
  updated_at INTEGER NOT NULL, updated_by TEXT,
  PRIMARY KEY (kind, id)
) WITHOUT ROWID;
CREATE TABLE config_setting (key TEXT PRIMARY KEY, value TEXT NOT NULL) WITHOUT ROWID;
CREATE TABLE config_history (rev INTEGER PRIMARY KEY, at INTEGER NOT NULL, by TEXT, snapshot TEXT NOT NULL);

CREATE TABLE user (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  pw_hash TEXT NOT NULL, lang TEXT, created_at INTEGER NOT NULL, last_login INTEGER
);
CREATE TABLE session (
  token_hash BLOB PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL, last_seen INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE api_token (
  id TEXT PRIMARY KEY, secret_hash BLOB NOT NULL, name TEXT NOT NULL, scope TEXT NOT NULL,
  user_id INTEGER REFERENCES user(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL, expires_at INTEGER, last_used INTEGER
) WITHOUT ROWID;
CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY, at INTEGER NOT NULL, actor TEXT, action TEXT NOT NULL, detail TEXT
);
```

### 7.2 What is stored

- By default, pvmon stores the history of only `power`, `soc` and the energy counters of each meter. Diagnostic channels (string voltages, temperatures, frequency) are live only. To store them, list them in `devices.<id>.history`.
- Site quantities have no raw rows.
- Pvmon stores bucket values (`b15_gauge`, `b15_energy`) as fixed-point integers in 1/1000 of their unit (mWh, mW, 0.001 %). This is far below the resolution of any meter. It is about 25 % smaller than `REAL`, and sums are exact. Each flush rounds its share once (at most 0.0005 for each flush and value). Only `storage` converts between the fixed-point form and the unit.
- Everything else keeps full precision. Rounding for display happens only in the presentation layer.

### 7.3 Derived cache

- Pvmon caches only closed periods, at day, month and year level. It builds months from cached days and years from cached months. It computes today, the current month and the current year fresh (the current day has at most 96 rows for each series, plus the cached closed days).
- A period that ends before the first stored slot has no data and nothing missing. Pvmon answers it without SQL and never caches it.
- A write to a past slot (gap fill, import, data correction) deletes the cache keys of the day, month, year and lifetime that contain it. The deletion happens in the same transaction as the write.
- The cache keeps a generation that every invalidation bumps. A cache write is dropped when its computation started under an older generation. A value that was computed before a past-slot write is therefore not stored after it.
- A change of the site configuration increments `config_rev`. Pvmon recomputes stale entries lazily on read.
- After midnight, a background job warms the cache for the closed day, month and year.
- `pvmon backfill` drops the cache and warms it again.

### 7.4 Retention

`storage.retention` deletes whole local days from buckets, status events, audit log and cache. Before it deletes, pvmon adds the purged energy to `carry`. It does this for each raw series and for each derived site quantity, in the same transaction. Lifetime totals therefore stay app-lifetime. `meta.purged_before` records the boundary.

Retention never runs `VACUUM`. New buckets reuse the freed pages, so the file stays about the same size. To shrink the file, run the CLI job `pvmon vacuum` (13). Sessions expire by their own lifetime.

### 7.5 Write domains

Each writer has its own write domain. A writer MUST NOT write outside its domain. `storage` is the only owner of SQLite. It enforces the domains.

| Writer | May write | Through |
| --- | --- | --- |
| collector | `b15_gauge`, `b15_energy`, `counter_state`, `status_event`, `series` (new series) | collector store interface, `livecache` |
| derive service | `derived_cache`; `carry` and purges only during retention | derive store interface |
| config service | `config_*`, `audit_log` | config store interface |
| auth service | `user`, `session`, `api_token`, `audit_log` | auth store interface |
| CLI jobs (app stopped, flock) | everything, only after a verified backup | `storage` and `maintenance` |
| all other components | nothing | read-only interfaces |

- Storage rejects a write outside the domain of the caller with a typed error. A test covers this.
- A bucket upsert and its `counter_state` are written in one transaction. The raw base and the booked energy therefore always match after a crash (5.3).
- A write to a past slot deletes the affected `derived_cache` keys in the same transaction (7.3).
- Only `storage` converts bucket values to the fixed-point form (7.2).
- Sums run in SQL with explicit slot bounds and one grouped query for each period.
- Pvmon writes only the rows that changed.

### 7.6 Backups and migrations

- Online backups (`VACUUM INTO`) run every `backup_interval`. The interval is counted from the newest existing backup. Each backup is verified with `PRAGMA integrity_check`. Pvmon deletes unverified backups.
- A backup consists of the DB and `secret.key`. They are named `pvmon-<UTC YYYYMMDD-hhmmss.mmm>.db` and `.secret.key`.
- Migrations are versioned. They always run after a verified backup. Pvmon refuses a database that is newer than the binary.

### 7.7 Size budget

Example R1+R3+R4: about 15 gauge series and 12 energy series × 96 slots × 365 days give about 0.95 million rows each year. With `WITHOUT ROWID` and fixed-point values, this is about 22 MB each year. The measured size is about 0.85 MB for each gauge series and 0.8 MB for each energy series each year. With `REAL`, it was 1.25 MB and 0.9 MB. The derived cache adds well under 1 MB each year.

An uncached year of a site quantity reads about 350 000 rows in one grouped query. The target is less than 100 ms on a Raspberry Pi 4. This happens once for each period.

### 7.8 Import from solis

Command: `pvmon import-solis --db solis.db --map map.yaml`.

- Stop the app before the import.
- Pvmon opens the solis database read-only.
- A database holds at most one import. It is recorded as `meta.import_source`.

The import follows these rules:

- **Energy:** each daily value of solis for a mapped series becomes one energy bucket. The bucket is in the last slot of its local day (`estimated|imported`). A day with such a bucket counts as complete. Pvmon skips days from today on. It also skips days from the first local day for which pvmon already stored data. A running installation therefore keeps its own data, and the import fills only the history before it. Pvmon skips negative daily values and reports them.
- **Lifetime:** the solis total minus Σ all solis daily values of a series (baselines of purged days) becomes `carry` before the first imported slot. Pvmon reports a total that is below the daily sum. It never carries a negative value.
- **Months and years:** pvmon has only the daily values. A solis month or year can have a stored value that differs from its daily sum by more than 0.05 kWh. Causes are cutover offsets and days without daily rows. Pvmon lists such a month or year in the summary. A zero row without daily values means "no data". It is not a mismatch.
- **Status:** the status log of solis for mapped keys before the cutoff becomes `status_event` of one device.
- **Map:**
  - `energy` maps a solis key without level suffix (`pv_energy`) to `<meter>/<channel>` of an energy series of the site.
  - `status` names the device. It maps the solis status keys to status registers of its profile.
  - Pvmon does not import the consumption keys (`energy_consumption`, `household_energy`) and the net `grid_energy`. Pvmon derives consumption itself.
  - The template is `example/import-solis.map.yaml`.
- **Order:**
  1. Check the map against the site.
  2. Take the exclusive lock.
  3. Run the migration.
  4. Refuse a second import.
  5. Make a verified backup.
  6. Run one transaction: buckets, carry, status, `import_source`, derived cache dropped.
- **Output:**
  - the backup path
  - one line for each series (days, kWh, skipped days, carry)
  - unmapped keys
  - the number of status changes
  - every month or year mismatch

## 8. Backend architecture and lifecycle

One Go binary is the whole application. The `go:embed` directive puts these parts into the binary:

- the frontend (`frontend/dist`)
- Swagger UI and the OpenAPI document
- all built-in profiles
- the PDF fonts
- the list of common passwords
- the database migrations

At runtime the application needs only the binary, an optional `config.yaml` and the data directory. Local profile files are optional (section 4.5). The code never reads embedded files from disk.

The design follows solis, an earlier monitoring application for Solis inverters. These rules carry over from solis:

- constructor dependency injection (DI)
- no global state
- one composition root
- a supervisor with a restart budget
- an injected clock

New in pvmon: the application creates collectors **per connection**.

### 8.1 Packages

| Package | Responsibility | Instances |
| --- | --- | --- |
| `cmd` | entry, subcommand dispatch, signal context, exit codes, ldflags | – |
| `internal/app` | composition root: factories, config mapping, health adapters | 1 |
| `internal/config` | file/env/DB layering, structural validation; domain rules injected | 1 |
| `internal/site` | effective site model, series catalogue, capabilities | 1 |
| `internal/profile` | schema, loader, decoder, status maps | 1 |
| `internal/transport/{modbus,http}` | read bytes, reconnect, error classes | per connection |
| `internal/collector` | scheduler, normaliser, counter tracker, integrator, netting, bucket accumulator | per connection |
| `internal/livecache`, `internal/sitecalc` | newest values; live site quantities | 1 each |
| `internal/derive` | periods, site quantities, ratios, cache, warm-up, retention | 1 |
| `internal/storage`, `internal/database` | SQLite, write domains, migrations, backups | 1 |
| `internal/auth` | users, sessions, tokens, rate limit | 1 |
| `internal/configstore` | UI config persistence, history, secret encryption | 1 |
| `internal/health` | supervisor, component contract, snapshot per component and device | 1 |
| `internal/report` (+`csv`, `pdf`) | pure report model and renderers | – |
| `internal/http/{handler,middleware,router,server}`, `internal/ws` | REST, WebSocket | 1 |
| `internal/export` | interface only (future MQTT, webhooks) | 0 |

### 8.2 Supervision and health

Every goroutine has an owner. The owner starts, stops and waits for the goroutine. Waiting loops use the injected `util.Clock`. Every channel documents its owner (the sender closes it) and its behaviour when full.

The single supervisor is `internal/health`. Restartable components are each collector, the derive service, sitecalc and the WebSocket hub. Each implements this contract:

```go
Start(ctx context.Context) error // error only for unrecoverable conditions
Stop() error                      // idempotent, graceful, bounded
State() health.State              // Healthy | Recovering | Failed
LastBeat() time.Time              // atomic, updated by the component's own loop
```

- Only a `health.Factory` that the supervisor calls builds a component. The component receives its `health.Reporter` in the constructor.
- There is no `Restart()` method. No component restarts another component. A restart is Stop, then factory, then Start.
- A config change (`config_rev`) rebuilds the affected collectors in the same way.
- A component beats from its **loop**, not only after work. The reconnect loop beats on every attempt and while in backoff.
- The healthy grace is 5 × the longest device interval of that component.
- The collector handles ordinary failures internally as `Recovering`. Examples are a device timeout, a read error and a lost connection. It uses the transport recovery of section 4 (immediate reconnect, backoff 1 s → 30 s). Such failures never count as restarts.
- The restart budget is 3 per component. Restarts are at least one sweep apart. The counter resets only after a restarted instance kept beating for the healthy grace.
- A fatal condition raises `ErrHealthFatal`. It cancels the root context through `context.WithCancelCause`. These conditions are fatal:
  - the budget is exhausted
  - a `Stop()` exceeded its timeout
  - a non-restartable part (storage, HTTP server, event bus) failed
- Health per device is one of these values:
  - `online`
  - `offline` (no successful poll for 3 intervals)
  - `stale` (the device answers, but values are frozen: an energy counter is unchanged for 2 h while PV power > 0)
  - `unconfigured`
- `GET /health` answers 200 with `ok` or `degraded`. `degraded` means a device is offline or a component is recovering. It answers 503 in these cases:
  - a required part failed
  - the restart budget is exhausted
  - a meter marked `critical: true` is offline longer than `status.offline_after`
  - the snapshot is older than 3 sweeps

  The endpoint never blocks on a component.

### 8.3 Shutdown and exit codes

SIGINT and SIGTERM start a single-phase shutdown. This is the order:

1. HTTP stops accepting requests.
2. Collectors stop and flush their open buckets.
3. The application joins the derive and backup jobs.
4. Storage closes.

One hard deadline of 30 s applies. There is no second-signal force mode.

The exit code is 0 on a signal. The exit code is 1 on fatal health, a startup error, a panic or a shutdown timeout. The exit code is 2 on a CLI usage error. The process never restarts itself. The container runtime restarts it (`restart: unless-stopped`). `log.Fatal` and `os.Exit` are allowed only in `main`.

`main` builds the signal context. The `cmd` package sets exit codes.

## 9. HTTP API and WebSocket protocol

REST serves models, history, reports and administration. WebSocket serves live values only. All endpoints are versioned under `/api/v1`. They are documented in OpenAPI and served with Swagger UI at `/docs`. "Read" endpoints are open when `auth.public_read` is true. Otherwise they need a session or token.

### 9.1 Endpoints

| Method + path | Access | Purpose |
| --- | --- | --- |
| `GET /health` | open | health snapshot (8.2) |
| `GET /api/v1/version` | open | `{version, commit, build_date, go}` |
| `GET /api/v1/site` | read | site model for the UI (below) |
| `GET /api/v1/energy?series=a,b&level=bucket\|day\|month\|year\|total&from=&to=` | read | energy per period for any series and site quantity; one grouped query per period |
| `GET /api/v1/gauge?series=a,b&from=&to=` | read | 15-minute avg/min/max/cover |
| `GET /api/v1/report?from=&to=&compare=last_year\|previous\|none&granularity=&format=json\|csv\|pdf&lang=de\|en` | read | report (section 12) |
| `GET /api/v1/devices`, `GET /api/v1/devices/{id}` | read | devices, state, live channels, profile info, netting check |
| `GET /api/v1/devices/{id}/status?start=&end=` | read | status changes (newest 1000; with a range, plus the change active at its start) |
| `GET /api/v1/status?start=&end=` | read | generic level of every device over time (status page) |
| `GET /api/v1/profiles` | admin | available profiles |
| `GET /api/v1/config`, `GET\|PUT\|DELETE /api/v1/config/{kind}/{id}`, `GET\|PUT /api/v1/config/settings` | admin | UI configuration; file-owned objects return `locked: true` and reject writes with 409 |
| `PUT /api/v1/config` | admin | several object changes `{objects: [{kind, id, body}]}` as one revision (a `null` body deletes); validated as a whole, so the setup wizard saves a site that is only valid together |
| `POST /api/v1/config/probe` | admin | `{objects, device}`: read every meter block of a device once with the draft changes applied (nothing is stored), return normalised values; Modbus reads go through the connection of the running collector, only an address without a collector gets its own connection for the probe; an unreachable device is a 200 with `error` |
| `GET /api/v1/config/export` | admin | effective configuration as YAML, secrets masked |
| `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me` | open / session | session handling |
| `POST /api/v1/setup` | open only while no user exists, needs setup code | create the first admin |
| `GET\|POST /api/v1/users`, `DELETE /api/v1/users/{id}`, `PUT /api/v1/users/{id}/password` | admin | user management |
| `GET\|POST /api/v1/tokens`, `DELETE /api/v1/tokens/{id}` | admin | API tokens (secret returned once) |
| `GET /api/v1/audit?from=&to=` | admin | audit log |

Times are RFC 3339. Day-based parameters also accept local dates (`YYYY-MM-DD`, server `TZ`). The range limit is 3 660 days for the day level and 10 000 buckets for the bucket level.

**`GET /api/v1/site` (shape):**

```json
{
  "name": "Home", "tz": "Europe/Berlin", "config_rev": 42, "data_since": "2025-03-01",
  "capabilities": { "pv": 2, "battery": 1, "grid": true, "backup": true,
                    "loadpoints": { "wallbox": 2, "heatpump": 0, "heater": 0, "other": 0 } },
  "meters": [ { "id": "battery", "role": "battery", "label": "Battery", "device": "wr1",
                "parent": "site", "capacity_wh": 10000, "series": ["battery/power", "battery/soc", "battery/energy_in", "battery/energy_out"] } ],
  "devices": [ { "id": "wr1", "profile": "solis/s6-hybrid", "verified": true, "state": "online" } ],
  "site_series": ["site/pv", "site/consumption", "site/grid_in", "site/grid_out", "site/soc", "site/loadpoints/wallbox"]
}
```

**Energy value shape:** `{ "period": "2026-11-18", "wh": 6213.4, "complete": true, "estimated": false, "missing_days": 0, "missing": [] }`. `missing` lists the incomplete local days of the period (bucket level: the slot itself). `missing_days` is its length.

- JSON rounds to 2 decimals. Storage keeps 1/1000 of the unit (section 7.2).
- A ratio series (unit `%`) has its number in `pct` instead of `wh`.
- The series object carries `unit` (`Wh` or `%`).
- A period without data has the number `null`, never 0.

**Gauge shape:** `{ "series": [ { "name": "site/pv", "unit": "W", "points": [ { "at": "2026-11-19T12:00:00+01:00", "avg": 4120.5, "min": 3810, "max": 4630.2, "cover": 900 } ] } ] }`.

- The names are the visible gauge series of the meters, plus `site/pv` and `site/soc`.
- `site/pv` is the sum of the top-level PV power. Its min and max are the sums of the extremes of the parts. This gives a band that contains the true one.
- `site/soc` is weighted like the live SoC.
- A slot is listed only when every part has data.
- `at` is the slot start in server time.
- Site powers for the day view come from `GET /api/v1/energy?level=bucket` (avg power = Wh × 4). It already contains every site quantity.

**Devices.** `GET /api/v1/devices` answers `{ "devices": [ { "id", "profile", "vendor", "models", "transport", "verified", "state", "interval_s", "meters", "last_poll" } ] }`.

- `last_poll` has `at`, `took_ms` and `ok`. `took_ms` is the time from the first request to the decoded sample. `last_poll` is null before the first poll since start.
- `GET /api/v1/devices/{id}` adds these fields:
  - `netting` (of its grid meter)
  - `read` (Modbus read mode)
  - `status` (the status registers: `name`, and the profile `label` {de, en} when set)
  - `blocks` (per meter: `meter`, `label`, `role`, `block`, `channels[{name, key, unit}]`; the keys are for `/ws`)
- `GET /api/v1/devices/{id}/status?start=&end=` answers `{ "events": [ { "register", "at", "code", "level", "texts": [{ "de", "en" }] } ] }`. It lists the changes in the range, newest first, at most 1000. After them it lists the change of each register that was active at `start`. The default range is the last 90 days.
- The service evaluates levels and texts with the current profile.
- The register `@connection` carries the connection. Its levels are `ok` (online), `offline` and `none` (pvmon stopped). It has no texts.
- `GET /api/v1/status?start=&end=` answers `{ "devices": [ { "id", "changes": [ { "at", "level" } ] } ] }`. For each device it lists the generic level (`ok`, `notice`, `fault`, `offline`, `none`), oldest first, only where it changes. The first entry is the level active at `start`.
- The generic level is `offline` or `none` from the connection. Otherwise it is the worst level of the status registers.
- The API never returns addresses or credentials.

**Auth.**

- `POST /api/v1/auth/login` `{name, password}` and `POST /api/v1/setup` `{code, name, password, lang}` answer `{ "user": { "id", "name", "lang", "created_at", "last_login" } }` (200 / 201). They set the session cookie.
- Error answers:
  - wrong name or password: 401 `unauthorized`
  - wrong setup code: 403 `forbidden`
  - setup after the first user: 409 `conflict`
  - too many failures: 429 `rate_limited` with `Retry-After`
  - password against the policy: 422 `validation_failed` (`details[{path: "password", message: too_short|too_long|too_common}]`)
- `POST /api/v1/auth/logout` answers 204.
- `GET /api/v1/auth/me` is open. It answers `{ "user": { "id", "name", "via": "session"|"token" } | null, "setup_required", "public_read" }`. The UI uses it to choose between setup, login and dashboard.
- `GET /api/v1/users` answers `{ "users": [...] }`. `POST` `{name, password, lang}` answers 201.
- `PUT /api/v1/users/{id}/password` `{password}` answers 204 and ends the sessions of that user. Deleting the last user gives 409.
- `GET /api/v1/tokens` answers `{ "tokens": [ { "id", "name", "scope", "created_at", "expires_at", "last_used" } ] }`.
- `POST /api/v1/tokens` `{name, expires_days}` answers 201 with the same fields plus `token` (`pvm_<id>_<secret>`). The token is shown only here. `expires_days` 0 or absent means no expiry.
- `GET /api/v1/audit?from=&to=` takes dates (`to` is inclusive; the default is the last 30 days). It answers `{ "entries": [ { "id", "at", "actor", "action", "detail" } ] }`, newest first, at most 1000. `actor` is `user:<name>`, `token:<id>` or `cli`. `detail` is a JSON object without secrets.

**Configuration.** `kind` is `connection`, `device` or `meter`. The bodies are the objects in file form (section 3).

- `GET /api/v1/config` answers `{ "config_rev", "objects": [ { "kind", "id", "locked", "body", "updated_at", "updated_by", "unreadable" } ], "settings": { "<key>": { "value", "locked", "stored" } } }`.
- It lists every object of the effective configuration. File and env objects are locked.
- It lists every UI setting. `value` is the effective one. `stored` shows whether the UI layer sets it.
- `GET /api/v1/config/{kind}/{id}` answers `{ "config_rev", "object" }`. `PUT` with the body answers 200 with the same shape. `DELETE` answers 204.
- `GET /api/v1/config/settings` answers `{ "config_rev", "settings" }`. `PUT` `{ "<key>": value }` answers the same shape. A `null` value removes the key from the UI layer.
- Secrets come back as `***`. Sending `***` keeps the stored secret. The API refuses references (`${env:…}`, `${file:…}`). `unreadable` lists secrets that the user must enter again (secret.key lost).
- Errors:
  - unknown kind or object: 404 `not_found`
  - locked object or setting: 409 `locked_by_file`
  - a change saved concurrently: 409 `conflict`
  - an invalid result: 422 `validation_failed`, with every problem as `details[{path, message}]` (unknown keys included)
- `GET /api/v1/config/export` returns the configured layers without defaults as YAML (the `config.yaml` download). Secrets are masked. References are kept.
- `GET /api/v1/profiles` answers `{ "profiles": [ { "id", "vendor", "models", "transport", "verified", "netting", "blocks": [ { "name", "role", "roles_allowed", "category", "channels" } ] } ] }`.
- Every saved change writes the audit action `config_change` with `detail.changes[{kind, id, op: put|delete|settings, changed: {path: [old, new]}}]`. Secrets appear as `***`.

**Report.** `GET /api/v1/report` (section 12) answers JSON by default. The JSON has these fields:

- `from` and `to`: the reported days. `to` is yesterday when `today_excluded`.
- `granularity`
- `summary`:
  - `days`, `complete_days`
  - `total` and `per_day` in kWh
  - `self_sufficiency_pct`, `self_consumption_pct`, `grid_charge_est_kwh`
  - `sources` and `pv_split` as `solar|battery|grid_est_kwh` with `_pct`
  - `battery_ratio`, `balance_diff_kwh|pct`, `fallback_days`
- `figures`: `id`, `kind` (energy or ratio), `better`, `value`, `compared`, `base`, `change`, `change_pct`. Energy is in kWh. Ratios are in percent and their change is in points.
- `comparison`: `mode`, `from`, `to`, `days`, `complete_days`, `matched`, `matched_days`; or null.
- `buckets`
- `pv_sources`
- `batteries`
- `loadpoints` (`meters`, `categories`) or null
- `profile`: `mean_w` per slot of day, `peak_w`, `peak_at`
- `patterns`
- `missing_days` (at most 100)
- `quality` per meter: `missing_days`, `estimated_days`

Quantities that the site does not have are null. `format=csv` returns the buckets as the download `pvmon-report-<from>_<to>.csv`. `format=pdf` returns the A4 report in `lang` (de or en, default en) as `pvmon-report-<from>_<to>.pdf`.

### 9.2 Errors

All errors have one shape: `{ "error": "Bad Request", "code": "invalid_range", "message": "to must not be before from", "details": […] }`. The `code` values are stable and machine-readable. The client translates them. These codes exist:

- `invalid_param`
- `invalid_range`
- `range_too_long`
- `unknown_series`
- `not_found`
- `unauthorized`
- `forbidden`
- `csrf`
- `rate_limited`
- `locked_by_file`
- `validation_failed` (with `details[{path, message}]`)
- `conflict`
- `internal`

### 9.3 WebSocket `/ws` (public interface; changes are breaking)

```json
→ { "type": "subscribe", "keys": ["site/pv.power", "battery/soc"] }
← { "type": "snapshot", "values": { "site/pv.power": { "value": 2410, "unit": "W", "ts": "…" } } }
← { "type": "update", "ts": "…", "values": { "site/pv.power": { "value": 2398 } }, "removed": [] }
← { "type": "device_state", "device": "meter", "state": "offline" }
← { "type": "site_changed", "config_rev": 43 }
← { "type": "status", "device": "wr1", "register": "operating", "code": 4117, "level": "fault" }
→ { "type": "unsubscribe", "keys": ["battery/soc"] }
← { "type": "error", "code": "unknown_keys", "keys": ["foo"] }
→ { "type": "ping" }  ← { "type": "pong" }
```

- Keys are series IDs for live channels (`<meter>/<channel>`) and live site quantities (`site/<quantity>.power`). The state of charge is `site/soc`.
- Snapshot entries carry `value`, `unit` and `ts`. Update entries carry `value` and `unit` with the `ts` of the frame.
- `value` is a number (2 decimals) or a text. `approx: true` marks an estimate (SoC averaged without capacities).
- The server pushes only changed, subscribed keys, diffed per client. It coalesces pushes (about 75 ms) into one frame. `removed` lists subscribed keys that became missing.
- Unknown keys never drop the connection. The server answers them with `unknown_keys`. It answers a frame that is not a valid message with `invalid_message`. It answers a subscribe or unsubscribe with more than 256 keys with `too_many_keys`.
- The server disconnects a client that cannot keep up with its frames.
- The client sends a ping every 20 s and on tab focus. It reconnects if no frame arrives within 5 s.
- Same-origin check: the Origin host must equal the Host header. With `public_read: false` the upgrade needs a session cookie or `?token=`.
- Limits: 256 clients (the server closes the 257th with 1013), 256 keys per frame, 1 MiB per message.

## 10. Authentication and security

There is exactly one role: `admin`. Reading needs no login by default. Everything that changes state requires an admin session or an admin API token.

| Topic | Requirement |
| --- | --- |
| Role | `admin` only: configuration, users, tokens, audit. Several admin accounts are allowed. The last admin cannot be deleted |
| First admin | While no user exists, the UI opens the setup flow. It requires a one-time setup code (8 characters, Crockford base32). The code is printed in the startup log and is valid until used or until the process restarts. Alternative: `pvmon user add <name>` |
| Passwords | Argon2id (memory 19 MiB, iterations 2, parallelism 1, 16-byte salt, 32-byte key). The parameters are stored with the hash and upgraded on login. Logins and setup run one at a time. This bounds the hashing memory and lets the failure limit count every attempt of a parallel burst. Minimum length 12. The password is checked against an embedded list of common passwords. No composition rules, no forced rotation |
| Sessions | 256-bit random token from `crypto/rand`; stored only as SHA-256; cookie `pvmon_session`, `HttpOnly`, `SameSite=Strict`, `Path=/`, `Secure` when the request came via HTTPS (direct or `X-Forwarded-Proto` from a configured trusted proxy). Idle timeout 7 d, absolute timeout 30 d. Logout and password change delete sessions server-side |
| CSRF | `SameSite=Strict` plus an Origin (or Referer) check on every non-GET request. A mismatch returns 403 `csrf`. A request with neither header is accepted only with `Authorization: Bearer` (scripts with a token). A browser always sends Origin on POST |
| Brute force | Per IP and per account: 5 failures cause an exponential delay (1 s, doubling, max 15 min). Responses are uniform for an unknown user and a wrong password. The server logs and audits failures |
| API tokens | Format `pvm_<id>_<secret>` (id 8, secret 32 base62 characters). Shown once. Stored as SHA-256 of the secret. Scope `admin` (v1; `control` is reserved). Optional expiry. `last_used` is tracked. Revocable. Sent as `Authorization: Bearer` |
| Secrets in DB | Device credentials that the user enters in the UI are encrypted with XChaCha20-Poly1305. The 256-bit key is in `data/secret.key` (created at first start, mode 0600) as one line of base64. A key file of 32 raw bytes (pvmon up to v0.2.0) stays valid. The stored form is `enc:v1:<nonce+ciphertext>`. If the key is lost, the stored secrets are unreadable and the UI asks the user to enter them again |
| Audit | logins (success and failure), logout, config changes (diff without secrets), user and token changes, setup, imports. `storage.retention` controls retention |
| Transport | The app serves HTTP. External access is allowed only behind a reverse proxy with TLS. The app sends HSTS when it is served via HTTPS. Trusted proxies are configured explicitly (`app.trusted_proxies`) |
| Headers | `Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; connect-src 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, `X-Frame-Options: DENY` |
| Devices | Every outgoing request has a timeout. HTTP drivers do not follow redirects to other hosts. TLS to devices verifies certificates, unless `insecure_tls: true` is set per device |
| Privacy | No telemetry. No outgoing connections except to configured devices |

**Limitation.** With `public_read: true`, anyone on the LAN can read energy data. The WebSocket origin check does not prevent DNS rebinding. Exposure to the internet requires an authenticating reverse proxy or `public_read: false`.

**Platforms and connections.**

- pvmon runs on linux and darwin only. The server and the CLI jobs need `flock`.
- The UI loads no external fonts or CDNs.
- The application opens no outgoing connection except to configured devices.

## 11. Frontend

The UI is built entirely from `GET /api/v1/site`. It contains no hard-coded device, register or role list. A site with PV and grid only never shows a battery. The visual reference is the Playwright screenshots in `frontend/e2e/__screenshots__`. The theme is Graphite. The phone "Power now" card uses variant B with the bars of variant A.

### 11.1 Stack

- Vue 3 (Composition API, `<script setup>`, TypeScript strict), Vite, Vue Router, Pinia, vue-i18n, Tailwind 4.
- `@lucide/vue` is the only icon source.
- Apache ECharts is imported tree-shaken from `echarts/core` through an own composable. There is no `vue-echarts`.
- Inter is self-hosted via `@fontsource-variable/inter`.
- There is no VueUse and no date library. `matchMedia`, `ResizeObserver`, `Intl` and a small local-date helper cover these needs.
- Quality tools: `vue-tsc` with TypeScript 6.x, ESLint (incl. vue-i18n rules), knip, Vitest + Vue Test Utils + happy-dom, Playwright + axe.

### 11.2 Layout and responsiveness

- The **compact layout** applies when the pointer is coarse **or** the width is below 768 px. It has these parts:
  - no top bar; every page carries its title and starts a little below the status bar
  - safe-area insets for the notch and the home indicator
  - no page zoom by pinch or double tap, and no page scroll bar
  - a bottom navigation docked to the screen edge (full width, top edge only, items above the home indicator and inset from the rounded display corners)
  - 5 items: Overview, Day view, History, Reports, More
- Otherwise the desktop layout applies.
- **Desktop:** the sidebar is 248 px wide and has the full viewport height. It never scrolls. Navigation is at the top. The status card (devices online, version, admin login/logout) is at the bottom. Only the content area scrolls. Content has no maximum width.
- The **phone variant of the flow** applies only when the pointer is coarse **and** the width is below 768 px. Tablets get the desktop components in the compact shell.
- Every page renders inside one `Page` component (padding, title). Cards use one `Card` component.
- Breakpoints: 768 px (compact), 1024 px (two-column cards), 1440 px (three columns on the overview).

### 11.3 Pages

| Page | Content | Driven by |
| --- | --- | --- |
| Overview | Desktop: energy flow, KPI tiles today, energy table (today / month / year / total), 14-day PV vs consumption trend. Phone: "Power now" balance bars, 4 KPI tiles, energy card with period tabs, last 7 days list | capabilities |
| Day view | 15-minute power of one day. PV area (min/max band as an opt-in chip). Consumption and loadpoint lines (a category with several loadpoints can be split into one line per meter, from its power gauge). Grid bars (import up, export down). Battery bars (charge up, discharge down) with the SoC line on a second axis. Synced crosshair with aligned time axes. Date from the calendar picker. Day summary (per meter when split) and data quality | series catalogue |
| History | day / month / year bars per series with range picker (11.8) | series catalogue, `data_since` |
| Reports | period presets, one date range picker and comparison select, preview (the period table is folded until opened), PDF/CSV download as blob (errors shown in the page) | capabilities |
| Devices | List with state, profile, verified flag, interval and the duration of the last poll. Detail page with all live channels of the profile, grouped (AC, DC/strings, battery, temperature), and the netting check for grid meters. Status: the connection and every status register of the profile as day bars (registers shown or hidden per device and browser), the messages of a selected day (`?day=`) or the newest 50 | profile |
| Status | One row per device with its generic level per day (worst of `fault` > `offline` > `notice` > `ok` > `none`), the current level with its start time, and the live connection state. Hover on a day (desktop) shows how long each level lasted that day (the device page does the same per register message). Desktop shows 30 or 90 days (per browser, shared with the device page); the compact layout shows 30. A day opens the device page on that day (`/devices/{id}?day=…#status`) | devices |
| Settings (admin) | connections, devices (probe, profile choice), meters (role, category, parent, label, capacity, kWp), app settings, users, API tokens, language, audit log | config API |

The phone "More" tab contains Devices, Status, Settings, and the device count, version, language and theme.

### 11.4 Capability-driven widgets

```ts
defineWidget({
  id: 'loadpoint-energy',
  requires: (site) => Object.values(site.capabilities.loadpoints).some((n) => n > 0),
  slots: ['overview', 'reports'],
})
```

`requires` is a pure function of the site model. Tests run it against all reference setups. A widget whose meter is offline stays visible but greyed out. The widget order and the hidden widgets are stored per browser (`localStorage`, validated on load), never on the server.

### 11.5 Desktop energy flow

- The flow has at most one node per role (PV, Battery, Grid, House). It has **one node per loadpoint category** that exists ("Wallbox", "Heat pump"). A node never shows a count, however many loadpoints share the category.
- Missing roles have neither node nor edge.
- House is `site/house_rest`. `backup` meters have no node of their own. They show in the House breakdown, as part of it, wherever the loadpoints are wired.
- Slots are fixed: PV top, Grid left, Battery bottom. The consumer column is on the right in fixed order (House, Wallbox, Heat pump, Heater, Other), at most 5 rows. The column closes up when nodes are missing.
- A pure function `layout(capabilities, width) → nodes, edges` computes the geometry. Consumers hang off a vertical bus right of the hub, so edges never cross. The SVG scales with the card (`viewBox`) and never scrolls.
- A node is a rounded rectangle (radius 16). It has an icon in a tinted circle, a label, and a value with tabular numbers and fixed digits. Labels truncate with an ellipsis and show the full text in a tooltip. The battery node label includes the SoC.
- Edges are 3 px wide with animated dashes (CSS keyframes) in flow direction. They are static at 0 W. There is no animation with `prefers-reduced-motion`. Missing data greys the node.
- A click on a node opens a popover with the breakdown per meter (for example both wallboxes or both PV strings).

### 11.6 Phone "Power now"

- The card header has the title. If a battery exists, it also has a **SoC chip** (battery icon + capacity-weighted SoC). A tap on the chip opens the battery breakdown. The SoC appears nowhere else on the phone overview.
- The card has two rows. **"From"** shows the sources (PV, battery discharging, grid import). **"To"** shows the sinks (loadpoint categories, house, battery charging, grid export). Each row shows its total in kW.
- Each row is **one shared bar**, 28 px high and as wide as the card. The bar is rounded only at its outer ends (radius 8, `overflow: hidden`). Segments are flat role colours separated by 2 px gaps. They have no borders and no gradients.
- The segment width is its share of the row total in percent (`flex-grow = share × 100`). Both rows therefore always span 100 %. This holds for any number of segments and any power, including totals below 1 kW and single-segment rows.
- A segment shows only an icon (16 px, stroke in a dark tone of the role colour). A segment narrower than 26 px shows nothing.
- Below each bar, one wrapping line lists all items: dot, name, value. Every item is tappable (target ≥ 24 px) and opens the breakdown.
- Battery and grid switch rows with their sign. A value of 0 W is not shown as a segment, but it stays in the list as "0 W".
- Breakdowns of battery and grid show magnitudes with their direction ("charging" / "discharging", "import" / "export") instead of a sign. This matches the rows.
- A tap on a bar segment opens the breakdown of that item, like its list entry.
- The height is fixed. Each list keeps room for every item that it can ever show (battery and grid in both rows, widest value). The card therefore never changes height with the live values, and nothing below it moves.
- The title is a toggle (`aria-expanded`, chevron) that folds the card to the bars. Totals and lists slide away. The row titles "From" and "To" with their icons and the SoC chip stay. A second tap unfolds the card. Nothing else folds the card. The browser remembers the choice.
- A breakdown dialog takes the focus itself. When the user opens it by tap, it shows no focus ring.

### 11.7 Charts (ECharts)

- Every chart is interactive. Hover (mouse) or tap (touch) shows a tooltip with all visible series of that point. A second tap elsewhere moves the tooltip. A tap outside closes it.
- Day view: the crosshair is synchronised across all charts of the page. The tooltip shows the 15-minute slot with the averages (min–max of PV when its band is on).
- Tooltip rows carry the colour dot of their series.
- There is no chart zoom or drag-to-pan anywhere (desktop and phone). On phones, a horizontal swipe scrolls bar charts when the range does not fit. The minimum bar slot is 18 px for days, 26 px for months and 40 px for years. The initial position is at the newest value.
- Series toggles are chips. Hidden series do not count in axis scaling.
- Periods without data leave a gap and say "no data" in the tooltip, never 0.
- Overview trend: hover shows the day. A click or tap pins the day below the chart (the others are dimmed) with "Day view" and, for finished days, "Open day report". A tap never navigates directly. The tooltip holds no links.
- Colours come from role tokens. Loadpoint categories have their own tokens: wallbox `--ev`, `--heatpump`, `--heater`, `--other`. A theme switch re-registers the ECharts theme at runtime.
- Loadpoints are named by category only ("Wallbox"), never with a count. Only the live flow shows the count.
- All controls look the same. The user chooses every date in the themed calendar dialog (Monday first, keyboard navigation, bottom sheet in the compact layout). A date range uses one dialog: the range shows as a continuous band, with a preview up to the hovered or focused cell after the first pick. Every select uses the themed select field (button + listbox popup in the theme, keyboard and type-ahead, fixed-positioned so that cards never cover it). Selected toggles are tinted with the accent.
- Motion is short and subtle. It is off with `prefers-reduced-motion`. These motions exist:
  - pages fade in
  - popups and dialogs rise in
  - changed live values slide in (compact layout only)
  - flow dots run faster with more power
  - balance bars ease to their new share
- Touch targets are at least 24 × 24 px. In the compact layout, status day bars form one 44 px strip. The tap picks the day under the finger. The arrow keys move the selection.

### 11.8 History range picker

- The granularity tabs are days, months and years. The defaults are **30 days, 12 months, 5 years**, ending today.
- The range button shows the range ("21 Oct – 19 Nov 2026"). The arrows shift the range by its length. "Next" is disabled at today.
- The range button opens a bottom sheet (phone) or a popover (desktop). It offers these choices:
  - presets: days 7, 30, 90, 365; months 6, 12, 24, 36; years 3, 5, 10
  - a custom range: one range picker of the current granularity (days: the month calendar; months: the 12 months of a year; years: 12 years per page). Two picks set the two ends, in any order, and apply the range
- Periods without data or in the future are disabled. The UI does not store a custom range.
- Below the chart, a detail card shows the selected bar and the sum of the visible range, including "without data n of m".
- The browser stores the last used granularity and range preset.

### 11.9 Design system "Graphite"

| Token | Light | Dark |
| --- | --- | --- |
| `--bg` | `#f3f4f6` | `#0c0e12` |
| `--ink` / `--ink-2` | `#0b0f17` / `#566070` | `#e8eaee` / `#9aa3b2` |
| `--card` (glass) | `rgba(255,255,255,.78)` | `rgba(24,27,33,.78)` |
| `--tile` | `#ffffff` | `#1a1d23` |
| `--line` | `rgba(11,15,23,.08)` | `rgba(255,255,255,.08)` |
| `--well` | `rgba(11,15,23,.035)` | `rgba(255,255,255,.03)` |
| `--pv` | `#e49b0f` | `#fbbf24` |
| `--grid` | `#3b82f6` | `#60a5fa` |
| `--battery` | `#10b981` | `#34d399` |
| `--home` | `#8b5cf6` | `#a78bfa` |
| `--ev` (loadpoints) | `#06b6d4` | `#22d3ee` |
| `--accent` | `#2563eb` | `#60a5fa` |

**Rules (MUST):**

- The style is modern and calm: flat surfaces, one soft shadow (`0 12px 32px -18px`), 1 px hairline borders. Do not use gloss, highlights, bevels, inner shadows, gradients on controls or bars, or stacked shadows.
- Use glass only on cards and navigation (`backdrop-filter: blur(16px)`). Never put glass on glass. Use an opaque surface with `prefers-reduced-transparency` or without `backdrop-filter` support.
- **Rounded containers never get a coloured border.** Colour accents are flat fills or straight, inset lines.
- Use colours only from tokens, never hex in components. Use role colours for marks and icons, not for body text.
- Text contrast is at least 4.5:1 (3:1 from 24 px) against the worst background point.
- Type is Inter with tabular numbers everywhere. The scale is 12 / 13 / 14 / 16 / 22 / 28 px. Radii are 8 (bars, chips inside), 12 (controls), 16–20 (cards) and 999 (pills). Spacing is in steps of 4.
- The UI has a light and a dark theme. The default follows the OS. The browser stores the user choice (and the user profile when logged in).

### 11.10 Language and formatting

- vue-i18n provides `de` and `en` from day one, with one JSON file per language. A missing or unused key fails CI.
- The default language comes from the browser language, with fallback `en`. The browser or the user profile stores the choice.
- Numbers and dates use `Intl` with the locale of the **chosen language** (de: "4.512,3 kWh", en: "4,512.3 kWh"), never the browser region. Components do not use `toFixed` or `toLocaleString`. Power below 1 kW shows in whole watts.
- The UI translates API errors from their `code`. Profile status texts come in both languages. Reports take `lang`.

### 11.11 PWA and updates

- PWA: `manifest.webmanifest` has the name, short name, icons 192/512 (incl. maskable), `display: standalone`, theme colours, and shortcuts to Day view, History, Reports and Status. The app also has an Apple touch icon and meta tags. There is no push and no notification from the worker.
- Service worker (`sw.js`, built from `frontend/src/sw/`):
  - It needs HTTPS or `localhost`. On plain HTTP the browser runs no worker, and every start loads the UI of the server.
  - Each build precaches its files into its own cache.
  - A pointer names the **accepted** build. The worker always answers pages with the `index.html` of that build, so the UI never changes by itself.
  - A new build installs next to it and waits, also across app restarts. It becomes the served UI only when the user accepts the update banner.
  - First installs and `dev` builds take over at once.
- Offline behaviour:
  - Built files come from the cache.
  - API `GET`s are network-first (6 s). The worker keeps the last good answer (at most 300) and marks it with its fetch time. The UI then shows "Offline: data from …".
  - The worker never keeps `/api/v1/version`, `/health`, `/ws`, `/docs`, downloads (`format=`) and every non-`GET` request.
  - The server sends `sw.js` and the manifest with `no-cache`.
- Update check:
  - The client polls `/api/v1/version` every 5 minutes and on tab focus.
  - If the server version differs from the own version of the UI, a banner shows "New version x – reload". The server was updated and the open page is old. The user can dismiss the banner for that version.
  - The version is always the answer of the server and is never cached. A waiting or expired worker therefore never shows the banner.
  - When the user accepts the banner, the client fetches the newest worker. It reloads only when that worker carries a UI other than the open one. Otherwise it shows "could not be loaded yet". This prevents a reload loop.
  - "Check for updates" next to the version asks at once and answers in place (up to date, new version, development build, server not reachable).
  - The UI has no internet access. It does not look up new releases (privacy, section 14).
  - Binary, UI and docs share one version `<tag>-<short commit>` (AGENTS.md section 8). A `dev` build never reports an update.

### 11.12 Live data and notifications

- Live data comes only via WebSocket through `useSubscription(keys)` (ref-counted, re-subscribed on reconnect). History comes via REST.
- The status levels are:
  - `ok`
  - `notice` (shown as "warning": normal non-producing states, amber)
  - `fault` (red)
  - the generic `offline` (grey)
  - `none` (no data)
- Only faults count for the badge, the banner and notifications.
- Browser notifications are opt-in per browser. The app shows them only while it is open but not focused. It does not notify faults that are present at connect, or repeats after a reconnect.

### 11.13 Accessibility

- Use real buttons and links.
- Show a visible focus.
- Label all icon-only controls.
- Give charts an accessible summary and a table view.
- Primary controls have touch targets of at least 44 px.
- Turn motion off with `prefers-reduced-motion`.
- Never use colour as the only carrier of meaning (put labels next to every bar segment).

### 11.14 Setup and settings flow

1. **First start:** enter the setup code from the log, create the admin account and choose the language.
2. **Connections and devices:** enter the host or serial port. Try unit IDs. Choose a built-in profile or enter the path of a local profile file. "Probe" shows normalised live values before saving.
3. **Meters:** the wizard proposes meters from the profile (hybrid inverter → PV strings, battery, grid, backup). If a dedicated meter exists, the wizard proposes it as `grid`, and the inverter grid channel becomes diagnostic.
4. **Hierarchy and categories:** assign a category and a parent to each consumer (drag and drop or select).
5. **Plausibility check with live values:** "PV delivers 3.2 kW now – correct?". The balance PV + grid + battery ≈ consumption ± tolerance must hold. The user can select "Reverse direction" per meter. The wizard also runs the netting check for grid meters.
6. **Save:** the wizard saves to the DB layer. File-owned fields are locked and marked. `pvmon config export` produces the matching YAML at any time.

## 12. Reports, status and notifications

Reports keep the definitions of solis. They compute on site quantities from 15-minute buckets. A section appears only with the matching capability.

**Request rules:**

- Days are local dates of the server time zone.
- `to` is not later than today.
- A range has at most 3 660 days.
- A range that ends today is reported up to yesterday (`today_excluded`).
- Today alone gives a 400 error.

| Section | Shown when | Content |
| --- | --- | --- |
| Summary, energy flows | always | PV, consumption, import, export, self-sufficiency, self-consumption rate |
| Battery | ≥ 1 battery | charged, discharged, grid charging (≈), discharged ÷ charged (plain quotient, never called efficiency); per battery from 2 |
| PV sources | ≥ 2 `pv` meters | share and specific yield (kWh/kWp when `kwp` is set) |
| Loadpoints | ≥ 1 consumer under the site | energy and PV share per category and per loadpoint |
| Load profile | always | mean consumption per 15-minute slot of day, peak = highest 15-minute mean |
| Comparison | `compare` ≠ none | energy in kWh and %, ratios in points; only matched days are compared |
| Data quality | always | missing days and gaps per meter, estimated shares, device changes in the period |
| How this report is calculated | always (PDF) | data used, standard figures, estimates, rules; kept in sync with the code |

**Definitions:**

- Self-sufficiency = (consumption − import) / consumption. Self-consumption rate = (PV − export) / PV. Both come from summed energy, never from averaged ratios.
- **Time-resolved estimates:** per 15-minute bucket, solar share = clamp(1 − import / consumption, 0, 1). Loadpoint PV energy = loadpoint energy × solar share. Grid charging of the battery = min(charge, import) per bucket. The report sums these over the period and marks them ≈. A bucket with gaps falls back to the daily balance for that day. The report states this.
- The consumption sources (grid, battery, solar) are capped so that they add up to consumption. The residual is the "balance difference", never "losses".
- A day counts only with data for every series of the section. The report lists missing days and never counts them as 0 kWh. Ratios that need stable data (battery quotient, balance difference) appear only from 28 complete days.
- The report keeps standard figures apart from the estimates of this app (≈; CSV columns `_est_kwh`).

**Formats:** JSON preview, CSV and PDF (A4; summary, flows, patterns + comparison, table, method page). Rounding: API 2 decimals, PDF and UI 1 decimal, identical digits. The PDF palette is a print palette. It is independent of the UI theme and readable in greyscale. The language comes from `lang`.

**Status:**

- Profiles map device codes to `ok` / `notice` / `fault` with texts in de and en.
- App-level states per device:
  - `offline`: fault during daylight after `status.offline_after`, notice at night
  - `stale`: fault
- The collector stores a status change as `status_event` only on change, with the raw code. Level and texts come from the profile at read time, so a profile fix also corrects the history. A collector restores the last stored code per register at start, so a restart stores nothing unchanged.
- The collector stores the connection of each device in the same way under the register `@connection` (online, offline, and stopped when pvmon stops polling). The status page therefore shows offline times, and shows the time that pvmon did not run as no data.
- A profile status entry may carry a `label` {de, en}. It is the register name on the device page. Status keys are identifiers (`[a-z][a-z0-9_]*`).
- Notifications: browser notifications only (section 11.12). Webhooks and MQTT come later through the `export` interface.

## 13. CLI commands

All commands use the same configuration and the same database. Some commands are marked "stopped". These commands refuse to run while the server holds its shared lock (`flock` next to the DB). They always create and verify a backup first. If no backup exists, they write nothing and exit with code 1.

| Command | Purpose | Server |
| --- | --- | --- |
| `pvmon` / `pvmon serve [-config path]` | run the server | – |
| `pvmon version` | build info | any |
| `pvmon user add <name>` / `user reset-password <name>` / `user list` | manage admins | stopped |
| `pvmon token create --name n [--expires 90d]` / `token list` / `token revoke <id>` | API tokens | stopped |
| `pvmon config validate` / `config export` | validate the file, env and stored UI layers / print them as YAML without defaults (secrets masked); reads the DB read-only | any |
| `pvmon profile validate <file>` / `profile plan <file>` / `profile probe --device id` | profile authoring | probe: stopped (bus is exclusive) |
| `pvmon dump --device id [--count N --interval D] [-o file]` | record a fixture | stopped |
| `pvmon simulate [--setup R1..R7]` | run with fake transports for UI development and demos | – |
| `pvmon backfill` | drop and re-warm the derived cache | stopped |
| `pvmon vacuum` | shrink the DB file after a large purge (shorter retention, removed series, migration): verified backup, `VACUUM`, `PRAGMA integrity_check` | stopped |
| `pvmon import-solis --db path --map file` | import solis history before the own data of pvmon (section 7.8) | stopped |
| `pvmon data exclude --series s --from t --to t` | mark buckets as invalid (gap) | stopped |
| `pvmon data swap --meter m --from t --to t` | swap `energy_in`/`energy_out` (reversed CT) | stopped |
| `pvmon data reassign --from-series a --to-series b --from t --to t` | move buckets to the correct series | stopped |
| `pvmon restore <backup>` | restore DB and `secret.key` | stopped |

In this document, "solis" means an earlier monitoring application for Solis inverters. Section 17 explains what pvmon takes over from it.

`user add` and `user reset-password` read the password from the first line of stdin. Example: `printf '%s\n' "$PW" | pvmon user add admin`. These commands check the password policy before the backup. `token create` prints the token once on stdout.

Data correction commands print a before/after summary. They need `--yes` to write. They invalidate the affected cache keys and write an audit entry. Exit codes: 0 success, 1 failure, 2 usage error.

## 14. Operations and non-functional requirements

| Area | Requirement |
| --- | --- |
| Platforms | linux (amd64, arm64, armv7) and darwin (amd64, arm64) binaries; multi-arch `scratch` image on ghcr.io (`latest` only for stable tags). Windows is unsupported (no `flock`) |
| Deployment | single binary, no further runtime files (UI, profiles, fonts, docs embedded); Docker Compose templates for TCP and RTU (`devices:` for USB-RS485), `restart: unless-stopped`, `TZ` pinned, `data/` as volume (DB, backups, `secret.key`) |
| Resource budget | smallest target Raspberry Pi 3B+/4: < 60 MB RSS in steady state, < 2 % CPU average with 5 devices at 5 s, start < 3 s, frontend initial bundle < 250 KB gzip (ECharts lazy). Pinned by a CI benchmark |
| Bus budget | the scheduler measures the round-trip time per connection; above the smallest device interval the devices view warns ("bus overloaded: raise interval or use range mode"); polls are never skipped silently |
| Responsiveness | overview first paint < 1 s on LAN; any REST read of a closed period < 200 ms when cached, < 1 s uncached for a year |
| Durability | a crash loses at most 60 s of the open bucket; counters recover the energy on the next poll |
| Versioning | one version for binary, UI and docs: `<tag without v>-<short commit>` via ldflags and `VITE_APP_VERSION`; SemVer tags `vX.Y.Z`; `dev` without tag |
| Releases | goreleaser builds archives and images from a tag on `main` after the checks run again |
| Dependencies | Renovate is the only updater and tracks every pinned version (Go modules incl. tool directives, npm, Dockerfiles, compose, GitHub Actions, golangci-lint) |
| Privacy | no telemetry, no phone-home, no external fonts or CDNs at runtime (fonts are bundled) |
| Licence | MIT for code and profiles |
| Documentation | README (quick start, common options), user docs (installation, setups, netting, FAQ, security), profile guide (dump, write, golden test), Swagger UI for the API, AGENTS.md for contributors and agents |

**Dependencies (MUST stay minimal; stdlib first):** The toolchain is Go 1.27.4. AGENTS.md section 10 lists every Go module and npm package with version, reason and stdlib alternative. Routing (`net/http`), JSON paths, `flock` and tokens use the standard library. Tests use testify with mockery-generated mocks. These are test-only dependencies. A new dependency needs a row there in the same change.

## 15. Testing and quality gates

Unit tests use no network, no real clock and no sleeps. Deterministic fixtures pin every behaviour. Tests are table-driven with testify. Mocks come from mockery and are never edited by hand. `make check` runs the checks of CI and the simulation. It MUST pass before every merge.

| Level | Scope | Tool |
| --- | --- | --- |
| Profile golden | recorded or documentation-based fixture → expected normalised channels for every built-in profile | Go |
| Counter and netting | lifetime overflow, glitch hold, meter replacement, daily reset incl. reset group, integration across gaps, netting methods 1 and 2, resolution switch | Go, table-driven, fake clock |
| Replay | sample sequences of whole days incl. midnight, DST (Europe/Berlin) and outages → expected buckets and day values per series | Go |
| Simulation | a virtual device with random power runs profiles through the collector with device and pvmon faults; local only, not in CI | Go, fake clock |
| Reference setups | R1–R7 configs + replay → site quantities, `/api/v1/site`, reports | Go |
| Storage | write domains, cache invalidation on past writes, retention with carry, migrations, backup verification | Go + SQLite |
| Transport | Modbus TCP and RTU (virtual serial pair + simulator, shared bus with two unit IDs), HTTP drivers against recorded responses | Go |
| Auth | password hashing parameters, session expiry, CSRF, rate limit, token scope | Go |
| API | handlers with `httptest` and mockery mocks of the services; OpenAPI contract test | Go |
| UI logic | widget `requires`, flow `layout()` for all 64 combinations of PV/battery × category subsets at desktop and phone width (no overlapping boxes, texts inside the viewBox, minimum gaps), balance bars (always 100 % width, totals < 1 kW, single segment, 0 W), formatters per language | Vitest |
| UI visual | screenshots of overview, day view, history and settings for R1, R3+R4 and PV-only, light and dark, desktop and phone | Playwright |
| Accessibility | axe checks on every page | Playwright |

AGENTS.md section 9 gives the test rules and the CI gates.

## 16. Roadmap and acceptance gates

The sequence first proves that the generic model reproduces the numbers of solis exactly. Then it adds devices and comfort. No dates are set. Each phase ends at a gate that automated tests check.

| Phase | Name | Content | Ends at |
| --- | --- | --- | --- |
| 1 | Foundation | site model, profiles; Modbus TCP/RTU, HTTP; Solis profile; collector, buckets; simulator, `pvmon dump` | Gate 1 |
| 2 | Multi-device | shared connections; SDM630, Shelly, go-e; WARP3, grid netting; derive cache, REST, WS; Overview, i18n, PWA | Gate 2 |
| 3 | Parity with solis | history + range picker; reports (PDF, CSV); status per device; day view (15 min); `import-solis` | Gate 3 |
| 4 | Comfort, release 1.0 | setup wizard; device pages; loadpoint report; settings, users; API tokens, audit | Gate 4 |

| Gate | Acceptance criteria |
| --- | --- |
| Gate 1 | replay of recorded Solis samples yields the same daily energy as the solis database for every series it stores per device (±0.01 kWh; grid ±0.1 kWh, because solis stores the daily grid registers of the inverter at 0.1 kWh while pvmon books the lifetime counters of the meter; the replay books them with `netting: meter`, netting `app` is tested separately); buckets survive restart and crash without loss beyond 60 s; `pvmon dump` works on real devices |
| Gate 2 | reference setups R1, R3 and R4 green in replay; netting methods tested; overview (desktop and phone) renders from `/api/v1/site` for all three; i18n complete for these pages; PWA installable |
| Gate 3 | `import-solis` on a real solis DB: all historical daily values, monthly/yearly totals and lifetime totals match solis; history, reports and status reach solis feature parity |
| Gate 4 | R1–R7 green; setup wizard creates a working R3+R4 site from scratch; auth and token tests green; user docs and profile guide complete; resource budget met on a Raspberry Pi 4 |

Cross-cutting from Phase 2 on: i18n, the Graphite design system, accessibility checks and the minimal PWA are part of every new page.

## 17. Future work and decision log

**Future work (designed for, not in v1):**

| Item | Hook in v1 |
| --- | --- |
| MQTT import (transport) and export of normalised values | `transport` interface, `export` interface |
| Webhooks for faults (ntfy, Gotify, Home Assistant) | `export` interface |
| Single control commands (e.g. max SoC) | `Commander` interface, token scope `control` reserved |
| SunSpec discovery with a generic profile | profile schema, `modbus` transport |
| Utility meter via optical read head (Tasmota SML over HTTP JSON) | HTTP transport, `grid` role |
| Comparison with utility meter readings (1.8.0 / 2.8.0) entered by an admin | admin auth, report data-quality section |
| Admin UI for data correction | CLI `data` commands, cache invalidation |
| Automatic drift correction for coarse netting inputs | daily drift log (section 6) |

**Decision log (all decided 2026-11-19):**

| Topic | Decision |
| --- | --- |
| Name | PV Monitor, binary and repository `pvmon` |
| Scope | monitoring; never an EMS; control of single values later via `Commander` |
| Device integration | native transports + declarative YAML profiles; v1: Modbus TCP/RTU, HTTP JSON |
| Packaging | one binary with UI, docs and all built-in profiles embedded; a device names a built-in profile by ID or a local profile file by path |
| v1 devices | Solis S6 Hybrid, Eastron SDM630, Shelly 3EM / Pro 3EM, go-e, WARP3 (HTTP) |
| Modbus | shared connection per address; `range` or `direct`, config may only switch `range` → `direct` |
| Stack | Go + SQLite + Vite / Vue 3; charts Apache ECharts |
| Audience | one household per installation, open source, MIT |
| Storage | raw data only, 15-minute buckets (gauge avg/min/max, energy Wh); everything else derived on demand and cached; one retention with carry |
| Counters | every counter kind becomes Δ Wh per poll; lifetime formed when reading |
| Netting | `netting: meter \| app` on the grid meter, default from the profile |
| Configuration | file and UI at the same time, file wins; UI changes only with login |
| Auth | single `admin` role, read without login, Argon2id, sessions, API tokens |
| Language | i18n from day one, German and English |
| Loadpoint PV share | time-resolved per 15-minute bucket |
| UI theme | Graphite: flat, calm, light glass, no gloss; no coloured borders on rounded containers |
| Desktop layout | full-height fixed sidebar, scrolling content, scalable flow diagram |
| Flow nodes | one per role, loadpoints one per category |
| Phone "Power now" | balance bars "From" / "To", one shared full-width bar per row, icon-only segments, SoC chip in the header |
| Charts | interactive everywhere (tooltip on hover/tap, crosshair, horizontal scroll), no zoom |
| History | range picker; defaults 30 days, 12 months, 5 years |
| PWA | installable, offline with the last data, a new UI only after the user accepts; update check via version endpoint |
| Data correction | CLI only in v1 |
| Not included | cost/savings, metrics export, telemetry |

**Decisions from the device research and the dependency review (2026-11-19):**

| Section | Decision | Reason |
| --- | --- | --- |
| 4.2 / 4.3 | `read.max_registers` per profile (default 125) | the SDM630 accepts at most 40 float parameters (80 registers) per request |
| 4.2 | transform `product: [channel, channel]` | Solis has no power register per PV string; power = voltage × current |
| 4.2 | transform `value_id: { ids: <endpoint>, id: <n> }` | WARP meters report values by MeterValueID; the index depends on the meter type |
| 4.2 | HTTP auth `basic` or `digest` per profile; the transport supports digest (SHA-256) | Shelly Gen2 and WARP use digest authentication |
| 4.2 | `identify` block (model, serial with `redact`): removed 2026-11-23 | the wizard has the user choose the profile; profiles read no serials, so dumps contain none |
| 4.2 | role `phase` for per-phase blocks, role `comparison` for inverter consumption | per-phase values feed the netting check without becoming meters |
| 4.2 | status kinds `list` (non-empty → fault), `bool` (false → fault), `nonzero`, map `default` | Shelly errors, Shelly Gen1 `is_valid`, WARP error fields |
| 4.6 | v1 profile table with sources and verification state | result of the device research |
| 7.1 | `derived_cache.value` is JSON (TEXT) instead of CBOR | one dependency less, stdlib `encoding/json` |
| AGENTS 7 | logging stays zerolog (reverts a `log/slog` proposal) | proven library, less own code |
| 8.1 / 15 | tests with testify and mockery-generated mocks; CI checks mock drift (reverts a stdlib-only proposal) | fewer hand-made test helpers, stable and simple |
| 11.1 | frontend: no `vue-echarts`, no VueUse, no date-fns; icons from `@lucide/vue` | fewer dependencies; `lucide-vue-next` is deprecated |
| 4.1 | Modbus client `grid-x/modbus` instead of `simonvetter/modbus` | actively maintained, RTU-over-TCP for later |
| 3 / 14 | configuration loaded with koanf (DB map → file → env) | proven library instead of an own loader |
| 3 / AGENTS 7 | optional log file with timberjack rotation (`log.*` keys) | proven library instead of own rotation |
| 14 | Go toolchain pinned to 1.27.4 | decided |
| 4.5 / 8 / 14 | everything embedded in the binary; local profiles only by explicit path in `profile`, no override directory | only the binary is needed; explicit choice per device |

**Taken over from solis on purpose:**

- One composition root, constructor DI, no global state, a supervisor with a restart budget, an injected clock, write domains enforced by storage, and dangerous work only in CLI jobs.
- The report definitions: self-sufficiency, self-consumption rate, "balance difference", the plain discharged ÷ charged quotient and the matched-day comparison.
- The lint limits, testify and mockery, Renovate, goreleaser and the `scratch` image.

**Changed against solis on purpose:**

| solis | pvmon |
| --- | --- |
| viper | koanf |
| chi | `net/http` routing |
| simonvetter Modbus | `grid-x/modbus` |
| gorilla WebSocket | `coder/websocket` |
| CBOR | JSON |
| React | Vue |
| daily rows | 15-minute buckets |
