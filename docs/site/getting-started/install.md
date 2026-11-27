# Installation

pvmon is one program file. It uses a data directory and a configuration file,
`config.yaml`. The configuration file is optional.

pvmon operates on Linux (amd64, arm64, armv7) and macOS (amd64, arm64). It does not
operate on Windows.

## Time zone

Set the time zone with the environment variable `TZ`. pvmon uses it to calculate days,
months and years.

## Docker Compose

1. Copy the [example directory](https://github.com/dombyte/pvmon/tree/main/example) to
   your host.
2. Set the addresses of your devices in `config.yaml`.
3. Set your time zone in `TZ` in `compose.yaml`.
4. Start pvmon:

   ```bash
   docker compose up -d
   ```

5. Open `http://<host>:8080`.

The image is `ghcr.io/dombyte/pvmon`. The volume at `/app/data` contains the database, the
backups and the encryption key.

## Program file

1. Download the archive for your platform from the
   [releases page](https://github.com/dombyte/pvmon/releases).
2. Extract the archive.
3. Write a `config.yaml`. Refer to [Configuration](/guides/configuration).
4. Start pvmon:

   ```bash
   TZ=Europe/Berlin ./pvmon -config config.yaml
   ```

After a fatal error, pvmon stops with exit code 1. It does not start again. Use a
supervisor to start it again, for example systemd or Docker `restart: unless-stopped`.

## RS485 adapter

For a Modbus RTU adapter on the host, do these steps:

1. Give the container access to the device in `compose.yaml`:

   ```yaml
   services:
     pvmon:
       devices:
         - /dev/ttyUSB0:/dev/ttyUSB0
   ```

2. Add the connection to `config.yaml`:

   ```yaml
   connections:
     rs485: { address: "rtu:///dev/ttyUSB0", speed: 9600, parity: N, stop_bits: 1 }
   ```

## Simulated devices

You can start pvmon without hardware:

```bash
pvmon simulate --setup R3
```

The command writes to `./data/simulate.db`. Refer to [Example setups](/guides/setups).
