# Your first tournament

This page walks you through the fastest path from a running server to real-time results on a screen. Each step links to the full reference for that topic.

## 1. Install and start the server

Install bracket-creator on your computer by following the section of [Download](../install/download.md) for your computer (macOS, Windows or Linux), then start the tournament app:

```bash
bracket-creator mobile-app --folder ./tournament-data --bind 0.0.0.0
```

`--bind 0.0.0.0` lets the other devices in the hall (a scoreboard screen, a phone) reach the app; without it only this computer can. The operating system may ask whether to allow incoming connections for bracket-creator: allow them, or the other devices cannot connect. Refer to [Start the server](../organisers/run-tournament.md#start-the-server) for the server's flags. [Security notes](../organisers/operating-modes.md#security-notes) explains what opening the app to the hall network means for the admin password.

To run it in a container instead, use the [Docker option](../install/download.md#any-computer-with-docker). The image starts the tournament app by itself, already open to the other devices in the hall.

Open `http://localhost:8080` in a browser on the same computer. When the data folder is empty, the app prompts you to create a tournament. Fill in the name, date, venue, number of shiai-jo, and the admin password. Before you set the password, read [Operating modes](../organisers/operating-modes.md) to choose between officiated and self-run.

## 2. Create a competition

From the dashboard, click **+ Add competition**. Choose Individual or Team, then pick a format: **Knockout only**, **Pools + Knockout**, **League**, or **Swiss** (work in progress). Refer to [Formats](../organisers/formats.md) for a description of each format and when to use it.

The number of shiai-jo you entered in [Install and start the server](#1-install-and-start-the-server) is your venue's total. It can be anything from 1 to 16. Each competition is then assigned some of them. A competition that ends in a knockout must be assigned 1, 2, 4, 8 or 16. Its bracket splits into one block per shiai-jo, and those blocks merge in pairs. So a hall with three shiai-jo runs each competition on 1 or 2, and can run two competitions at once to use all three. Refer to [Assigning shiai-jo](../organisers/run-tournament.md#assigning-shiai-jo).

![Creating a competition: the name, type, format, pool-size, and shiai-jo options on the setup form.](../../screenshots/mobile-create-competition.png)

## 3. Add competitors

Open the competition setup and paste a newline-separated roster into the participant panel, then click **Apply changes**. The [Run a tournament](../organisers/run-tournament.md) guide covers import options and roster management in full.

![The participant panel: a line-numbered paste box holding a roster of names and dojos, with a CSV import area above it.](../../screenshots/mobile-add-participants.png)

## 4. Generate the draw and start

Click **Generate draw** to build the pools and bracket. Review the preview, then click **Start competition**. Matches now appear to scorers and to the public viewer page.

![The draw preview: pools of players with their result grids, ready to start or discard.](../../screenshots/mobile-draw-preview.png)

## 5. Enter a score

Go to the **Scores** tab, open a match, and record the result. Refer to [Scoring a match](../court-operators/scoring-a-match.md) for the full scoring workflow, including points, decisions, and team bouts.

![The score editor: Shiro and Aka fighters with ippon buttons, foul counters, and decision controls.](../../screenshots/mobile-score-editor.png)

!!! tip
    Open `http://<laptop address>:8080` on a second screen (a scoreboard TV or a phone on the same network) with no password, for example `http://192.168.1.10:8080`. The laptop's address is the IP address shown in its Wi-Fi or network settings (or printed by `ipconfig` on Windows and `ip addr` on Linux). Standings and brackets update in real time as a scorer records each result. Refer to [Following a tournament](../spectators/following.md) for the full viewer experience.

## If you print your brackets instead

Steps 1 to 5 run the whole tournament in the app. To run the day from paper instead, with no app at the courts, follow steps 1 to 4 up to **Generate draw** for every competition. Then print from the app instead of starting the competitions:

- **Export PDFs** on the dashboard renders print-ready PDFs from the draw you generated. They cover the registration sheets, name sheets, competitor tags, pool and match sheets, and bracket trees of every competition. The [sample printouts](../organisers/sample-printouts.md) show a page of each. A competition with no draw yet prints as empty places. It needs LibreOffice where the app runs: install it on the computer, or with Docker run the `ghcr.io/gitrgoliveira/bracket-creator-mobile-pdf` image.
- **Download blank template (.xlsx)** on a competition's **Export & print** tab gives the same draw as a workbook with linked formulas for scoring by hand. Before the draw it gives empty places only, sized from the expected number of participants. The tab warns if you have not shuffled the competitor list yet.

Neither export covers a Swiss competition; follow its live standings instead. Refer to [Export and print](../organisers/run-tournament.md#export-and-print).

On the day you need:

- **A3 printer**: for team and competitor name sheets
- **A4 printer**: for the bracket trees
- **Scoreboards, whiteboard markers, scissors, tasuki**: for keeping score at the courts and marking competitors

### Keep courts in sync

If you have multiple shiai-jo and run on printed brackets, share each competition's workbook online so every table works from the same document:

1. Download the blank template (.xlsx) from the competition's page, upload it to Google Drive and open it with Google Sheets. The workbook's formulas work in Google Sheets as they do in Excel.
2. Share the sheet with the person at each table, with permission to edit.
3. At each table, open the sheet on any device with a browser or the Google Sheets app. Type that court's results into its own matches: the points in the score cells, and an **X** in the centre column for a tie.

## Next steps

For the complete organiser workflow, including pools, seeding, and competition settings, refer to [Run a tournament](../organisers/run-tournament.md). Before the event, take a backup of the whole tournament: refer to [Backup and restore](../organisers/backup-and-restore.md).
