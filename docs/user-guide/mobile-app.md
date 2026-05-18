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

Click **Admin** and enter the tournament password (set in `tournament.md`) to access the admin console.

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
