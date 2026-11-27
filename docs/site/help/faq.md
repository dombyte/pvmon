# Troubleshooting

## The setup code is lost

Start pvmon again. If no account exists, pvmon writes a new code to the log.

## A password is lost

1. Stop pvmon.
2. Set a new password:

   ```bash
   printf '%s\n' "$PASSWORD" | pvmon user reset-password admin
   ```

## The web interface cannot change a setting

`config.yaml` or an environment variable defines the setting. Change it there.

## pvmon does not start

1. Read the log. pvmon shows each configuration error with the path of the key, for example
   `meters.grid.device: unknown device "meterr"`.
2. Correct the errors.
3. Run `pvmon config validate`.

## A Modbus device reports "illegal data address"

Set `read: direct` for this device.

## A Modbus device is offline frequently

Many gateways and RS485 buses can answer only one request at a time.

- Give all devices of one bus the same connection.
- Increase the poll interval.
- Make sure that no other program reads the device.

## Import and export are higher than on the utility meter

Refer to [Grid netting](/concepts/netting).

## A day shows `n/m days` or the mark ≈

Refer to [Energy and data quality](/concepts/energy).

## The browser cannot install the app

The browser must use HTTPS or `localhost`.
