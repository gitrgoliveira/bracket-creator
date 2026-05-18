# mobile-app

Starts the live tournament management server (Preact UI + REST/SSE backend). Used **on the day of the tournament** for drawing pools, recording scores, and pushing live updates to spectator screens.

```
bracket-creator mobile-app [flags]
```

See the [Mobile / Live Tournament App guide](../mobile-app.md) for a full walkthrough of the UI.

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--folder` | `-f` | `.` | Folder containing `tournament.md` and `competitions/`. Created on first save. |
| `--port` | `-p` | `8080` (or `$PORT`) | Port to listen on |
| `--bind` | `-b` | `localhost` (or `$BIND_ADDRESS`) | Address to bind to. Use `0.0.0.0` to reach the server from other devices on the LAN. |
| `--lock-password` | — | unset (or `$LOCK_PASSWORD=true`) | Switch to locked authentication mode. Requires `TOURNAMENT_PASSWORD_HASH`. See [Authentication](#authentication) below. |

## Environment variables

| Variable | Used by | Description |
|----------|---------|-------------|
| `PORT` | `--port` default | Initial value for `--port` if the flag isn't set. |
| `BIND_ADDRESS` | `--bind` default | Initial value for `--bind` if the flag isn't set. |
| `LOCK_PASSWORD` | `--lock-password` default | `true` enables locked mode without passing the flag. |
| `TOURNAMENT_PASSWORD_HASH` | locked mode auth | Bcrypt hash of the admin password. Required when `--lock-password` is set. Generate with [`hash-password`](hash-password.md). |

## Authentication

The server has two authentication modes for the admin console; viewer routes are always public.

### File mode (default)

The admin password is stored plaintext in `tournament-data/tournament.md` and compared by exact-string match. Set the password during the in-app **Create tournament** flow, or edit `tournament.md` directly.

- `POST /api/tournament/reset` is enabled and unauthenticated — visit `/reset` in a browser to set a new password if you've forgotten the current one.
- `GET /api/auth-config` returns `{"mode": "file", "resetEnabled": true}`.

### Locked mode (`--lock-password`)

The on-disk password is ignored. Authentication compares the `X-Tournament-Password` header against a bcrypt hash supplied via the `TOURNAMENT_PASSWORD_HASH` environment variable.

```bash
# Generate the hash once (stdin or arg)
bracket-creator hash-password

# Start the server
TOURNAMENT_PASSWORD_HASH='$2a$10$...' \
  bracket-creator mobile-app --lock-password -f ./tournament-data
```

- `/reset` is disabled (returns 404). The SPA hides the "Forgot password?" link.
- `GET /api/auth-config` returns `{"mode": "locked", "resetEnabled": false}`.
- The server **refuses to start** if the env var is empty or malformed (fail-closed; no silent fallback to file mode).
- Rotation requires restarting with a new hash. The hash is read once at startup.

**Recommended for any deployment reachable over the internet.** The plaintext-in-file pattern of file mode is intended for trusted-network use only.

## Examples

```bash
# Local LAN — file mode, default port
bracket-creator mobile-app -f ./tournament-data

# Bind to all interfaces, custom port
bracket-creator mobile-app -f ./tournament-data -b 0.0.0.0 -p 8082

# Locked mode for a public deployment
HASH=$(bracket-creator hash-password)   # interactive
TOURNAMENT_PASSWORD_HASH="$HASH" \
  bracket-creator mobile-app --lock-password -f /var/lib/bracket-creator -b 0.0.0.0
```
