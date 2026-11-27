# WebSocket

The WebSocket `/ws` sends live values. For history, use the
[REST API](/api/).

## Messages

The arrow `→` shows a message to the server. The arrow `←` shows a message from the server.

```text
→ { "type": "subscribe", "keys": ["site/pv.power", "battery/soc"] }
← { "type": "snapshot", "values": { "site/pv.power": { "value": 2410, "unit": "W", "ts": "…" } } }
← { "type": "update", "ts": "…", "values": { "site/pv.power": { "value": 2398 } }, "removed": [] }
← { "type": "device_state", "device": "meter", "state": "offline" }
← { "type": "site_changed", "config_rev": 43 }
← { "type": "status", "device": "wr1", "register": "operating", "code": 4117, "level": "fault" }
→ { "type": "unsubscribe", "keys": ["battery/soc"] }
← { "type": "error", "code": "unknown_keys", "keys": ["foo"] }
→ { "type": "ping" }
← { "type": "pong" }
```

## Keys and values

- A key is a live channel (`<meter>/<channel>`) or a live site quantity
  (`site/<quantity>.power`). The key of the site state of charge is `site/soc`.
- `value` is a number with 2 decimals, or a text.
- `approx: true` shows an estimate.
- The server sends only the changed values of the subscribed keys.
- The server collects changes for approximately 75 ms and sends one message.
- `removed` contains the subscribed keys that have no value.

## Errors

| Code | Cause |
| --- | --- |
| `unknown_keys` | A key does not exist. The connection stays open |
| `invalid_message` | A message does not have a correct format |
| `too_many_keys` | A message contains more than 256 keys |

## Limits

- The server accepts 256 clients. It closes the next connection with the code 1013.
- The maximum size of a message is 1 MiB.
- The server disconnects a client that is too slow.

## Connection check

1. Send `ping` every 20 seconds.
2. If no message arrives in 5 seconds, connect again.

## Access

The host in the `Origin` header must be the same as the `Host` header. If
`auth.public_read` is `false`, send a session cookie or the parameter `?token=<token>`.
