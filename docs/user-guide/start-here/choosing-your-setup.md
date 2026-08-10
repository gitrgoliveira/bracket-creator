# Choosing your setup

Two independent questions shape every tournament you run with bracket-creator. First, how digital your venue is: whether you print and score on paper, keep one device per shiai-jo, or run fully on-screen scoreboards with real-time mobile pages. Second, who runs and scores the matches: trained staff behind an admin password (officiated) or competitors scoring their own bouts with no password required (self-run). Because these questions are independent, you choose each one separately and combine them however suits your event.

## Digitization level

How far you digitize determines which surfaces you use on the day. See [Three ways to run a tournament](../../index.md#three-ways-to-run-a-tournament) for a full explanation of each level.

- **Offline.** Print the Excel brackets and score on paper. No networked devices are needed at ringside.
- **Partially connected.** One device per shiai-jo keeps courts in sync through the tournament app.
- **Fully digital.** On-screen scoreboards and real-time mobile pages show scores and standings to everyone in the hall.

## Operating model

The operating model controls who can record scores and advance matches. In **officiated** mode, staff authenticate with the admin password and run every match. In **self-run** mode, competitors report their own results with no password barrier. See [Operating modes](../organisers/operating-modes.md) for the full rules on both models, including guidance on when to choose each one.

## Choose by how you run the day

The following table maps common event configurations to the right starting point.

| Scenario | Digitization | Who scores | Start here |
|---|---|---|---|
| Print and run on paper | Offline | Staff | [Generate brackets](../organisers/web-ui.md) |
| Officiated event | Partial or full | Staff | [Run a tournament](../organisers/run-tournament.md) |
| Self-run event | Partial or full | Competitors | [Run a tournament](../organisers/run-tournament.md) and [Operating modes](../organisers/operating-modes.md) |

## Choose by your role

If you know your role at the event, the following table takes you directly to the guide written for you.

| Role | What you do | Start here |
|---|---|---|
| Organiser | Set up and run the event | [Run a tournament](../organisers/run-tournament.md) |
| Court operator | Score matches at a shiai-jo | [Scoring a match](../court-operators/scoring-a-match.md) |
| Competitor | Register and report your own results in self-run | [Self-run](../competitors/self-run.md) |
| Spectator | Follow scores and standings | [Following a tournament](../spectators/following.md) |

## Devices and screens

The app runs on several screens at the same time, and each surface is designed for a different one. Nothing needs installing on any of them: every surface is a web page served by the tournament app, so each device only needs a current browser and a way to reach the server.

| Surface | Who uses it | Device to plan for |
|---|---|---|
| Operator console | Court operators and organisers | A tablet or a laptop. For a tablet, plan on an iPad Air or better, or an Android tablet of equivalent size and age. Use it in landscape. |
| Court display | Everyone at the shiai-jo | A screen or TV at the court, showing `/display?court=A`. Read-only, so it needs no password. |
| Lobby display | People waiting to compete or watch | A screen in the lobby or waiting room, showing `/display?court=all` for every court at once. |
| Viewer pages | Spectators and competitors | Their own mobile phone. These pages are designed mobile-first. |

A few consequences worth planning around:

- **The operator console is the one surface with a real minimum.** Scoring screens put a full team encounter, its bout rows, and the controls on one screen, so a small phone is not a practical operator device. A laptop works equally well if you have one per shiai-jo.
- **Display screens are optional.** Without them you are at the partially connected level described above: results still reach phones, but you will still print scoreboards for the courts and tags for competitors.
- **The court display is usually driven from the operator's own machine** over an HDMI cable rather than from a separate device. The console and the board are then two tabs in one browser on one computer, so scores reach the board without a network hop and it keeps updating through a Wi-Fi drop. This is a client-side arrangement at the court and holds whichever way you host the server. See [Keep the court scoreboard alive on the same machine](../../architecture/infrastructure-architecture.md#keep-the-court-scoreboard-alive-on-the-same-machine-hdmi).
- **Spectator phones may be on cellular** rather than venue Wi-Fi when the app is cloud-hosted, so they do not add to your local network load.

See [Following a tournament](../spectators/following.md) for the full list of display and viewer URLs, including the streaming overlay variant.

## New to bracket-creator?

If this is your first time using the app, work through [Your first tournament](first-tournament.md) for the fastest path from an empty server to real-time results on a screen.
