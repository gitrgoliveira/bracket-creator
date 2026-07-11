# Web UI

This is the **organiser's** tool for the days before the event: turn a roster into a print-ready bracket, no command line required. On the day, the [tournament app](run-tournament.md) takes over for scorers, competitors, and spectators.

The web UI lets you generate brackets without using the command line. Start it with:

```bash
bracket-creator serve
```

Then open [http://localhost:8080](http://localhost:8080) in your browser.

## Main screen

Configure the tournament on the main screen: choose the format (Pools & Playoffs or Playoffs Only), set the number of courts, pool sizes, and other options. Upload your participant CSV directly from the browser.

![Web UI main screen](../../screenshots/webui-main.png)

## Participant list

After uploading a CSV the participant list is shown for review before generating the bracket.

![Participant list](../../screenshots/webui-player-list.png)

## Seeding

Click **Assign Seeds** to open the seeding modal. Drag competitors into ranked positions or type seed numbers directly. Seeds control which players are kept apart in the early rounds.

![Seeding modal](../../screenshots/webui-seeding-modal.png)

After confirming, the assigned seeds are shown inline next to each participant name.

![Seeds assigned](../../screenshots/webui-seeds-assigned.png)

## Download the bracket

Click **Generate** to produce the Excel file. The browser downloads the `.xlsx` directly (no server-side storage, no account needed).
