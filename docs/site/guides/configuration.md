# Configuration

You can configure pvmon in the web interface, in `config.yaml` and with environment
variables. You can use all three together.

## Priority

pvmon reads the sources in this sequence. A later source replaces an earlier source.

1. Default values
2. Web interface (stored in the database)
3. `config.yaml`
4. Environment variables

The web interface cannot change an object or a setting that comes from the file or from the
environment. pvmon uses the changes from the web interface immediately. After you change
`config.yaml`, start pvmon again.

## Minimum file

```yaml
site:
  name: Home
  interval: 5s

connections:
  lan-solis: { address: "tcp://10.0.0.20:502", timeout: 2s }

devices:
  wr1:   { connection: lan-solis, unit: 1, profile: solis/s6-hybrid }
  meter: { address: "http://10.0.0.31", profile: shelly/pro-3em, interval: 2s }

meters:
  pv:      { device: wr1, source: pv, kwp: 10, label: Roof }
  battery: { device: wr1, source: battery, capacity: 10kWh }
  grid:    { device: meter, source: total }
```

A device uses a `connection`, or it has its own `address`. The key `source` selects a meter
block of the profile. Refer to [Supported devices](/reference/devices).

## Rules

- A configuration with meters must have exactly one meter with the role `grid`.
- An ID starts with a lowercase letter. It can contain `a-z`, `0-9` and `-`. The maximum is
  32 characters.
- Durations, sizes and energy values must have a unit: `5s`, `30d`, `50MB`, `10kWh`.
- Devices with the same connection address use one connection. Give each Modbus device a
  different unit ID.

## Secrets

Do not write passwords into the file. Use a reference:

```yaml
devices:
  warp:
    address: "http://10.0.0.41"
    profile: tinkerforge/warp3
    auth: { user: admin, password: "${env:WARP_PASSWORD}" }
```

The reference `${file:/run/secrets/name}` reads the secret from a file.

## Environment variables

A variable name starts with `PVMON_`. Two underscores separate the levels of a key:

```bash
PVMON_APP__LOG_LEVEL=debug
PVMON_DEVICES__WR1__INTERVAL=10s
```

An environment variable cannot set an object with `-` in its ID.

## Check

```bash
pvmon config validate
pvmon config export      # prints the configuration as YAML, without secrets
```

pvmon shows each error with the path of the key. At the start, it gives a warning for each
unknown key.
