# Configuration keys

The column **UI** shows the settings that you can change in the web interface.

## Application

| Key | Default | UI | Function |
| --- | --- | --- | --- |
| `site.name` | `Home` | yes | Name of the site |
| `site.interval` | `5s` | yes | Poll interval of the devices without their own interval, minimum 1 s |
| `app.port` | `8080` | | HTTP port |
| `app.log_level` | `info` | | `debug`, `info`, `warn` or `error` |
| `app.request_timeout` | `30s` | | Time limit for HTTP requests and database reads, minimum 1 s |
| `app.trusted_proxies` | empty | | Addresses or networks of the trusted reverse proxies |
| `auth.public_read` | `true` | yes | Read access without a login |
| `auth.session_idle` | `7d` | | A session stops after this time without use |
| `auth.session_max` | `30d` | | Maximum life of a session |
| `storage.path` | `./data/pvmon.db` | | Database file |
| `storage.retention` | `10y` | yes | Retention time of the data. No function in this version |
| `storage.backup_interval` | `24h` | yes | Interval of the backups |
| `storage.max_backups` | `3` | yes | Number of backups that pvmon keeps |
| `status.offline_after` | `15m` | yes | A device that is offline in daylight for longer than this time has a fault |

## Log file

| Key | Default | Function |
| --- | --- | --- |
| `log.file` | empty | Log file in addition to stdout |
| `log.max_size` | `50MB` | Size at which pvmon starts a new file |
| `log.max_backups` | `5` | Number of old files that pvmon keeps |
| `log.max_age` | `30d` | Maximum age of old files |
| `log.rotate_at` | empty | Times of day to start a new file, for example `["00:00"]` |
| `log.compression` | `gzip` | `gzip`, `zstd` or `none` |

## `connections.<id>`

| Key | Default | Function |
| --- | --- | --- |
| `address` | | `tcp://host:port`, `rtu:///dev/ttyUSB0` or `http://host` |
| `timeout` | `2s` | Time limit of one request |
| `speed` | `9600` | Serial speed |
| `data_bits` | `8` | Serial data bits |
| `parity` | `N` | Serial parity |
| `stop_bits` | `1` | Serial stop bits |

## `devices.<id>`

| Key | Default | Function |
| --- | --- | --- |
| `connection` | | ID of a connection |
| `address` | | Address, as an alternative to `connection` |
| `unit` | `1` | Modbus unit ID |
| `profile` | | ID of a built-in profile, or the path of a profile file |
| `interval` | `site.interval` | Poll interval, minimum 1 s |
| `timeout` | | Time limit of one request |
| `read` | from the profile | `range` or `direct`. You cannot set `range` for a `direct` profile |
| `rollover` | `00:00` | Local time at which the device resets its daily counters |
| `netting` | from the profile | `meter` or `app`. Refer to [Grid netting](/concepts/netting) |
| `history` | empty | Channels to store in addition to power, state of charge and energy |
| `insecure_tls` | `false` | Accept a TLS certificate without a check |
| `auth.user`, `auth.password` | empty | Login of the device. Use a secret reference |

## `meters.<id>`

| Key | Default | Function |
| --- | --- | --- |
| `device` | | ID of the device |
| `source` | | Meter block of the profile |
| `role` | from the profile | A role that the profile permits |
| `category` | from the profile | `wallbox`, `heatpump`, `heater` or `other` |
| `parent` | | `site` or the ID of a meter |
| `label` | | Name in the web interface |
| `kwp` | | Peak power of a PV meter in kWp |
| `capacity` | | Capacity of a battery, for example `10kWh` |
| `critical` | `false` | `GET /health` gives an error if this meter is offline too long |
