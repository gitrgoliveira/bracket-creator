# Mobile / Live Tournament App

The `mobile-app` command starts a live tournament management server for use **on the day of the tournament**. Any device on the same network can open the URL to view live results or — with the admin password — manage pools and scores.

## Starting the server

```bash
bracket-creator mobile-app --folder ./tournament-data
```

The `--folder` and `--port` flags can also be supplied via environment variables — useful for systemd units, Docker, or any deploy that doesn't run via `make`:

```bash
TOURNAMENT_DATA_DIR=/path/to/data PORT=8082 bracket-creator mobile-app
```

| Flag | Env var | Default |
|------|---------|---------|
| `--folder` / `-f` | `TOURNAMENT_DATA_DIR` | `.` |
| `--port` / `-p` | `PORT` | `8080` |
| `--bind` / `-b` | `BIND_ADDRESS` | `localhost` |

An explicit flag always wins over the env var.

Or with the Makefile:

```bash
make run-mobile
PORT=8082 make run-mobile                        # custom port
TOURNAMENT_DATA_DIR=/path/to/data make run-mobile
```

Open [http://localhost:8080](http://localhost:8080) in your browser (or share the LAN address with scorers).

## Public viewer

The default view requires no password. It shows:

- The full match schedule across all shiai-jo, filterable by player or team.
- Pool standings updated in real time as scores are entered.
- The elimination bracket as it fills in.

## Admin console

Click **Admin** and enter the tournament password to access the admin console. The server runs in one of two authentication modes — see [Admin authentication](#admin-authentication) below.

### Admin authentication

**File mode** (default — local / private LAN use):

The admin password lives plaintext in `tournament-data/tournament.md`. Set it during the **Create tournament** flow, or edit the file directly. If you forget the password, browse to `http://<host>/reset` from any device on the same network and choose a new one — no old password required. The reset endpoint is intentionally unauthenticated and is the documented recovery path; for trusted networks this is convenient, and for internet-exposed deployments use locked mode below.

**Locked mode** (recommended for any deployment reachable over the internet):

```bash
# 1) generate a bcrypt hash for your chosen password.
# hash-password reads from stdin without a prompt or echo masking —
# pipe from a secrets manager or here-doc rather than typing interactively.
printf '%s' "$MY_ADMIN_SECRET" | bracket-creator hash-password
# (the hash is printed on stdout — copy it)

# 2) start the server with --lock-password and the hash in the env
TOURNAMENT_PASSWORD_HASH='$2a$10$...' \
  bracket-creator mobile-app --lock-password -f ./tournament-data
```

In locked mode:

- The on-disk password in `tournament.md` is ignored.
- `POST /api/tournament/reset` returns 404. The SPA's `/reset` page still loads (it's part of the embedded SPA bundle) but renders an "operator-disabled" message instead of the form, and the AuthModal hides the "Forgot password?" link.
- Authentication compares the `X-Tournament-Password` header against the env-var bcrypt hash.
- Rotating the credential requires restarting the server with a new hash; the runtime never reads the env var twice.
- If `--lock-password` is set but `TOURNAMENT_PASSWORD_HASH` is empty or malformed, the server **refuses to start** — fail-closed, so a misconfigured deployment can't silently fall through to file mode.

The public `GET /api/auth-config` endpoint reports `{mode: "file"|"locked", resetEnabled: bool}` so SPAs and external monitoring can see the active mode without authenticating.

### Operational notes

- **No rate limiting on `/reset`.** The reset endpoint is unauthenticated by design and the server does not throttle calls to it. On a trusted LAN this is fine (the legitimate operator is the only person at the keyboard); on any network where untrusted clients can reach the server, an attacker can grief the deployment by repeatedly POSTing new passwords and locking the operator out. **Always run with `--lock-password` for internet-exposed deployments**, or front the server with a reverse proxy that rate-limits `/api/tournament/reset`.
- **Mode switching preserves the stored password.** When you switch from file mode to locked mode, the password on disk in `tournament.md` is **not** erased — it's just ignored at auth time. If you later switch back to file mode (drop `--lock-password`), the original password authenticates again. Treat this as a feature for rollback experimentation, but be aware that the on-disk credential remains discoverable by anyone with filesystem access. If you want to fully retire a file-mode password, run `POST /api/tournament/reset` to a value you don't intend to use before switching to locked mode.

### Dashboard

The dashboard lists all competitions. Each card shows the competition type, number of participants, format, and current status. Click a card to manage that competition.

### Setting up a competition

Each competition goes through a **Setup → Pools → Playoffs** lifecycle.

1. **Participants & seeds** — Paste or upload a CSV. The participant textarea shows numbered lines for easy error spotting. Format:
   - Without zekken: `Name, Dojo[, Dan grade]`
   - With zekken: `Name, Zekken display name, Dojo[, Dan grade]`

   Enable **Use Zekken display name** in Settings first when using the 4-column format.

   After pasting, click **Apply** to save. Optionally import a seeds CSV or type seed numbers per row.

2. **Settings** — Adjust pool size, winners per pool, shiai-jo assignment, start time, round-robin mode, and zekken display.

3. **Start competition** — Click **Start competition →** to draw pools. The status moves to `pools`.

### Pools (live)

Once pools are drawn, the **Pools** tab shows all pools and their current standings. Scorers can use the **Scores — edit** tab or the dedicated score editor to record match results.

### Bracket (live)

After all pool matches are complete, advance the pool winners to the elimination bracket. The bracket updates live as scores come in.

### Export & print

Download the `.xlsx` bracket file at any time via **Export & print**.

## Data format

State is stored as plain files in the data folder:

```
tournament-data/
  tournament.md                 ← YAML: name, date, venue, courts, password
  competitions/
    <id>/
      config.md                 ← YAML: kind, format, pool settings, courts, …
      participants.csv          ← name[, zekken][, dojo][, dan]
```

The files can be hand-edited between rounds if needed.

## Tournament schedule

The **Tournament schedule** view (accessible from the admin dashboard) lets you set start times and minutes-per-match per competition, then auto-schedule all pool matches across the assigned shiai-jo.
