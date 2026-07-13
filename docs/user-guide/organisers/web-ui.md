# Legacy Web UI

The legacy web UI is the **organiser's** tool for the days before the event: turn a roster into a print-ready bracket, no command line required. It is a standalone one-shot bracket generator, separate from the [tournament app](run-tournament.md), which takes over on the day for scorers, competitors, and spectators.

Start it with the [`serve` command](../commands/serve.md):

```bash
bracket-creator serve
```

Then open [http://localhost:8080](http://localhost:8080) in your browser.

## Main screen

Configure the tournament on the main screen. Choose the format, either Pools and Playoffs or Playoffs (Knockout Tournament), then set the number of courts, pool sizes, and other options. Upload your participant CSV directly from the browser.

![Web UI main screen](../../screenshots/webui-main.png)

## Participant list

After uploading a CSV the participant list is shown for review before generating the bracket.

![Participant list](../../screenshots/webui-player-list.png)

## Seeding

Click **Seed Participants** to open the seeding modal. Type a seed number into each participant's rank field. Seeds control which players are kept apart in the early rounds.

![Seeding modal](../../screenshots/webui-seeding-modal.png)

After confirming, the assigned seeds are shown inline next to each participant name.

![Seeds assigned](../../screenshots/webui-seeds-assigned.png)

## Download the bracket

Click **Create Tournament** to produce the Excel file. The browser downloads the `.xlsx` directly (no server-side storage, no account needed).
