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
# Generate the hash (pipe the secret — bare invocation waits for stdin with no
# prompt or echo-off, so always use printf/pipe to avoid shell history leakage)
printf '%s' "$MY_ADMIN_SECRET" | bracket-creator hash-password

# Start the server
TOURNAMENT_PASSWORD_HASH='$2a$10$...' \
  bracket-creator mobile-app --lock-password -f ./tournament-data
```

- `POST /api/tournament/reset` returns 404. The `/reset` SPA route still serves the embedded page but renders an "operator-disabled" message instead of the form, and the AuthModal hides the "Forgot password?" link.
- `GET /api/auth-config` returns `{"mode": "locked", "resetEnabled": false}`.
- The server **refuses to start** if the env var is empty or malformed (fail-closed; no silent fallback to file mode).
- Rotation requires restarting with a new hash. The hash is read once at startup.

**Recommended for any deployment reachable over the internet.** The plaintext-in-file pattern of file mode is intended for trusted-network use only.

#### Operational caveats

- **`POST /api/tournament/reset` has no rate limiting.** The endpoint is unauthenticated and the server does not throttle calls. For internet-exposed deployments, run with `--lock-password` (which 404s `POST /api/tournament/reset`) OR front the server with a reverse proxy that rate-limits that path.
- **Locked mode has no bcrypt brute-force protection.** The server runs a full bcrypt comparison (`bcrypt.DefaultCost` ≈ 50–100 ms) on every `X-Tournament-Password` header, but does not throttle repeated failed attempts. For internet-exposed locked-mode deployments, add a reverse proxy that rate-limits authenticated routes (e.g. nginx's `limit_req`) in addition to `POST /api/tournament/reset`.
- **Mode switching preserves the stored password.** Switching from file mode to locked mode does NOT erase `tournament.md`'s `password` field — auth just stops consulting it. A later switch back to file mode resurrects the original password. This is a deliberate rollback feature, but anyone with filesystem access can still read the value. To fully retire a file-mode credential before going locked, `POST /api/tournament/reset` it to a one-time throwaway first.

## Examples

```bash
# Local LAN — file mode, default port
bracket-creator mobile-app -f ./tournament-data

# Bind to all interfaces, custom port
bracket-creator mobile-app -f ./tournament-data -b 0.0.0.0 -p 8082

# Locked mode for a public deployment.
# Note: `hash-password` reads ONE line from stdin without prompting or
# disabling terminal echo — pipe the password in from a secrets manager
# or a here-doc rather than typing it directly, so it never lands in
# shell history or the terminal scrollback.
HASH=$(printf '%s' "$MY_ADMIN_SECRET" | bracket-creator hash-password)
TOURNAMENT_PASSWORD_HASH="$HASH" \
  bracket-creator mobile-app --lock-password -f /var/lib/bracket-creator -b 0.0.0.0
```
