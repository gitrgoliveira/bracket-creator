# Run a tournament on the day

This page is the operational hub for tournament day: start the tournament app, manage competitions, score matches, and export results. If you have not set up your tournament data yet, follow the quickstart at [First tournament](../start-here/first-tournament.md) before continuing here.

## Start the server

Run the following command from your terminal:

```bash
bracket-creator mobile-app --folder ./tournament-data
```

Then open `http://localhost:8080` in a browser. The server binds to `localhost` by default, so other devices cannot reach it yet. To let helpers on the same network connect, start the server with `--bind 0.0.0.0` (or a specific LAN interface), then share your machine's LAN address, for example, `http://192.168.1.10:8080`.

The following flags and environment variables control the server:

| Flag | Short | Env var | Default | Description |
|---|---|---|---|---|
| `--folder` | `-f` | `TOURNAMENT_DATA_DIR` | `.` | Path to the data folder |
| `--port` | `-p` | `PORT` | `8080` | HTTP port to listen on |
| `--bind` | `-b` | `BIND_ADDRESS` | `localhost` | Network address to bind |

An explicit flag always takes precedence over the matching environment variable.

The `make` targets let you launch quickly without typing flags:

```bash
make run-mobile                            # Default port 8080, ./tournament-data folder
PORT=8082 make run-mobile                  # Different port
TOURNAMENT_DATA_DIR=/path make run-mobile  # Different data folder
```

Two environment variables tune the API rate limiter for large events:

| Env var | Default | Description |
|---|---|---|
| `API_RATE_LIMIT` | `5000` | Sustained requests per second |
| `API_RATE_LIMIT_BURST` | `10000` | Peak burst size |

!!! tip
    For events with hundreds of simultaneous spectators, consider raising both rate-limit values before the tournament starts.

## The admin console

Click **Admin** in the navigation bar and enter the admin password. The rules for who can access which features depend on your tournament's operating mode. See [Operating modes](operating-modes.md) for the full access-control rules.

## Dashboard

The dashboard lists all competitions for the tournament. Each card shows the competition type, participant count, bracket format, and current status. Click a card to manage that competition.

![Admin dashboard](../../screenshots/mobile-dashboard.png)

## Tournament details and the public info page

Open **Edit details** from the dashboard to fill in the public information your attendees see:

- Venue address and a map link
- Opening and closing times
- A website link and an awards note
- Free-text info notes (rules, transportation, access details)
- Contact entries

Set the **Public URL** field to the externally reachable address of your app (for example, `https://my-tournament.example.com`). Setting this field enables QR codes on competitor tags and makes every shareable link work. The public URL also populates the public info page in the viewer and on spectator display screens.

!!! note
    For guidance on making the app reachable over the internet, see [Hosting](../install/hosting.md).

## Branding and sponsors

The same **Edit details** page also has branding and sponsor fields, below tournament details. All fields are optional; the default kendo theme applies when nothing is configured.

- **Logo**: upload an image file shown on the viewer, the lobby displays, and the admin screens.
- **Accent colours**: set a primary accent colour and a soft background tint; the viewer and display screens adopt them across the whole site.
- **Sponsors**: upload full-width images that appear on the public viewer page only. Sponsor images do not appear on the TV lobby boards or scoring displays.

## Announcements

Click **Announce** from the dashboard to broadcast a short message to every viewer. Choose a duration of 5, 10, 15, or 30 minutes; the message clears itself automatically when the time expires. It appears as an overlay on the viewer and display screens. Spectators who allow browser notifications can receive it in the background.

## Registration desk

Open **Registration desk** from the dashboard to access the cross-competition check-in surface for the welcome table. This view lists every competitor across all competitions so a registration helper can mark participants present as they arrive. It complements the per-competition check-in described in [Set up a competition](#set-up-a-competition).

## Set up a competition

A competition moves through three phases:

1. **Setup**: configure participants, seeding, and optional check-in.
2. **Draw preview** (`draw-ready`): review the generated pools, bracket, or first Swiss round. The roster is locked during this phase.
3. **Match play**: competitions with a pool phase start in `pools` status; knockout-only formats start in `playoffs`.

![Competition setup overview](../../screenshots/mobile-participants.png)

### Adding participants

The participant setup view has two panels.

The **Participant list** panel (labelled **Team list** for team competitions) contains a line-numbered paste box. Paste newline-separated rows in one of these formats:

- Without display name: `Name, Dojo[, Dan grade]`
- With display name (zekken): `Name, Zekken display name, Dojo[, Dan grade]`

Click **Paste clipboard** to read a tab-separated selection from the clipboard and convert it automatically. Click **Apply changes** to save the list.

![Participant setup panels](../../screenshots/mobile-participant-setup.png)

The **Check-in & Seeding** panel (labelled **Seeding** when check-in is disabled) shows the working roster. From here you can:

- Drag rows to assign seeds, or type a rank number directly.
- Click **Shuffle unseeded** to randomise unranked positions.
- Click **Import seeds (CSV)** to load a seed file, or **Clear seeds** to remove all ranks.

#### Editing a single competitor

Click the pencil icon on any row to open the edit modal for that competitor. You can change the name, dojo, dan grade, and display name during setup. Once the draw is generated, the pencil icon is disabled; discard the draw to re-enable editing.

### Check-in workflow

Enable check-in in **Settings** for the competition. When enabled, each row in the seeding panel gains a check-in checkbox. A **Show unchecked / Show all** toggle filters the list, and **Check in all** marks every participant at once.

The check-in rule is opt-in: when you click **Generate draw**, if at least one participant is checked in, only checked-in participants join the draw and unchecked participants are excluded (their seeds are dropped). If nobody is checked in, everyone is included.

### Draw preview

Click **Generate draw** to produce the bracket. The competition enters `draw-ready` status and shows an interactive preview:

- Pools competitions show pool assignments.
- Knockout competitions show the bracket tree.
- Swiss competitions show round 1.

You can still toggle individual check-in status during `draw-ready`, but roster edits (add, remove, reorder) are locked.

When the preview looks correct, click **Start competition** to move to match play. To make roster changes instead, click **Discard draw** to delete the draft and return to setup.

<!-- Raw HTML is copied verbatim by MkDocs (only markdown image paths get
     rewritten), so this src must be relative to the BUILT page URL
     (/user-guide/organisers/run-tournament/): three levels up, not two. -->
<figure class="bc-fig">
  <video controls loop muted playsinline preload="metadata" width="900" height="580" aria-label="Generating the draw: the pools preview appears after clicking Generate draw.">
    <source src="../../../screenshots/draw-generation.mp4" type="video/mp4">
  </video>
  <figcaption>Generating the draw. Press play to watch.</figcaption>
</figure>

## Pools and bracket

The **Pools** tab shows standings for every pool. Ranks are computed automatically from match results; operators do not edit them by hand, with one exception: chusen (drawing lots), the last-resort tie-break for a consequential team-pool tie that a daihyosen cannot settle (see [Recording decisions](../court-operators/recording-decisions.md)). When a daihyosen settles a tie that determines pool advancement, the winning side carries a **DH** badge in the standings.

After all pool matches are complete, advance pool winners to the elimination bracket from the Pools tab. The bracket updates in real time as results come in.

![Pools view](../../screenshots/mobile-pool-standings.png)

For the four competition formats and the Swiss round-by-round flow, see [Formats](formats.md).

For team lineups and team scoring rules, see [Team tournaments](team-tournaments.md).

For how to enter scores and navigate between matches, see [Scoring a match](../court-operators/scoring-a-match.md).

For kiken, fusenpai, daihyosen, and other special decisions, see [Recording decisions](../court-operators/recording-decisions.md).

For naginata and Engi-kyogi divisions, see [Naginata](naginata.md).

## Results and awards

The public viewer shows a competition's podium when it finishes, and a provisional ranking while it is still in progress:

- **Kendo knockout** (default): 1st place, 2nd place, and two equal 3rd places. There is no bronze match; both semi-final losers share third.
- **Naginata**: a single 3rd place is decided by a playoff. See [Naginata](naginata.md) for naginata-specific configuration.
- **Mixed format** (still in its pool phase): the viewer shows a provisional cross-pool ranking until the knockout decides the final places.

Operators see an all-competition winners view from the dashboard. You can also record optional **fighting-spirit** (敢闘賞) awards as free text; these appear on the viewer for all spectators. Saving awards requires the destructive-ops password in self-run mode; see [Operating modes](operating-modes.md#destructive-ops-password).

**League competitions** derive the podium from final standings. In an individual league, any tie within the top three places triggers a short ippon-shobu tie-breaker automatically, so the competition never closes with an unearned tie. The one exception is 3rd place: with the **Award two joint 3rd places** option enabled (the default for kendo), competitors tied entirely for third share the position instead, with no decider. In a team league, the operator chooses whether to run a tie-breaker or accept a tie at any position; see [Team standings and tie-breaks](team-tournaments.md#team-standings-and-tie-breaks).

Set the **Award two joint 3rd places** option during setup, before you generate the draw. Once the draw exists, the option is locked; discard the draw to change it.

## Export and print

### Excel

Two Excel downloads are available from the competition page:

- **Download results (.xlsx)**: a workbook with played scores, pool standings, winners, and decisions filled in. Covers pools, league, and knockout formats. Swiss competitions have no static bracket; follow the current standings instead.
- **Download blank template (.xlsx)**: an empty bracket workbook with linked formulas for hand scoring at events without a network connection.

### PDF

PDF exports (competitor tags, name sheets, and bracket trees) are available to admins only. Rendering requires LibreOffice:

- Use the `ghcr.io/gitrgoliveira/bracket-creator-mobile-pdf:latest` container image, which includes LibreOffice.
- Or install LibreOffice on the host and ensure `soffice` is on the system path.

The lean container image omits LibreOffice and returns a clear message when a PDF is requested.

When the **Public URL** is set and competitors have assigned numbers, each printed tag includes a QR code that opens that competitor's public page. See [Hosting](../install/hosting.md) for guidance on setting the public URL.

## Data format

Tournament state is stored as plain files inside the data folder you specified with `--folder`. You can hand-edit these files between rounds when a correction is needed:

- `tournament.md`: YAML front-matter with the tournament name, date, venue, court count, and the admin password and destructive-ops password.
- `competitions/<id>/config.md`: YAML front-matter with competition kind, format, pool settings, and courts.
- `competitions/<id>/participants.csv`: one participant per line with name, optional display name (zekken), dojo, and optional dan grade.

!!! warning
    Edit data files only between rounds, not while the server is actively processing match results. Concurrent writes can produce inconsistent state.

## Tournament schedule

Open **Tournament schedule** from the dashboard to configure timing for each competition. Set start times and minutes-per-match per competition, then click **Auto-schedule competition** to distribute all pool matches across the assigned shiai-jo automatically. The view shows an estimated end time per court based on match duration and the number of assigned matches.
