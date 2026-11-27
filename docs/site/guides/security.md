# Security

- pvmon supplies only HTTP. For access from the internet, use a reverse proxy with TLS and
  authentication.
- By default, all persons in your network can read your energy data. To require a login,
  set `auth.public_read` to `false`.
- Keep the file `data/secret.key`. pvmon uses it to encrypt the device passwords that you
  enter in the web interface. Without this key, you must enter the passwords again.
- pvmon connects only to your devices.
- pvmon does not write to your devices.

## Reverse proxy

1. Add the address of the proxy to `app.trusted_proxies`. pvmon then accepts the headers
   `X-Forwarded-For` and `X-Forwarded-Proto` from this proxy.
2. Configure the proxy to forward WebSocket connections to `/ws`.
3. Configure the proxy to keep the `Host` header.

pvmon compares the `Host` header with the origin of each request that changes data.

## Accounts

- All accounts have the role `admin`.
- You cannot delete the last account.
- After 5 incorrect logins, pvmon delays the subsequent attempts.
- The audit log records logins and changes.

## API tokens

A script uses an API token:

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/config
```

pvmon shows a token only one time. You can set an expiry date. You can revoke a token.
