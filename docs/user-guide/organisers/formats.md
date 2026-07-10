# Tournament formats

bracket-creator supports four competition formats. The format determines how competitors are paired and how the final standings are reached. The choice between individual and team play is separate, made at competition creation; all four formats support both. See [Team tournaments](team-tournaments.md) for team-specific behaviour.

## Choosing a format

The table below summarises when each format fits best.

| Format | Best for |
|--------|----------|
| Playoffs | Small fields or qualification rounds where a straight knockout is enough |
| Mixed | Most events with a moderate-to-large field; pools thin the field before the knockout |
| League | Small fields where every match matters and standings tell the full story |
| Swiss | Large individual fields where you want many rounds but a full round-robin is impractical |

## Playoffs

Playoffs is a direct single-elimination (knockout) format. Each match eliminates one competitor; the winner advances until one remains. There are no pools and no preliminary phase.

Use playoffs when the field is small, or when you are running a dedicated knockout stage after a separate qualifying event.

## Mixed

Mixed runs two phases. First, competitors are divided into pools and play a round-robin within each pool. Then the top finishers from each pool advance to a knockout bracket.

Use mixed for most events. Pools give every competitor several matches before the knockout begins, and the bracket rewards pool performance through seeding.

## League

League is a full round-robin: every competitor meets every other competitor. Final standings come from the accumulated results across all matches; there is no knockout stage.

Use league for small fields (typically eight or fewer competitors) where you want standings that reflect the complete head-to-head record rather than a single bracket run.

## Swiss

Swiss is a tournament system that runs over a fixed number of rounds. No one is eliminated; instead, each round pairs competitors with similar win records against each other, so results stay competitive throughout.

Use Swiss for large individual fields where a full round-robin would require too many rounds but you still want every competitor to play multiple well-matched bouts.

!!! note
    Swiss is a format, not a status. Swiss competitions run live under the same pools phase as mixed and league; the difference is that rounds are generated one at a time rather than all at once.

### Swiss round flow

Before play begins, set the number of rounds at competition creation. Then follow these steps for each round:

1. **Start the competition.** Round 1 pairings are generated automatically. If seeds are set, round 1 uses fold pairing (1 vs N, 2 vs N-1, and so on). Without seeds, the pairing is deterministic-random. From round 2 onward, players with similar win records face each other.

2. **Record match results.** Scorers enter results in the live app. All matches in the current round must be completed before you can advance.

3. **Review the standings.** Standings update live from wins, points scored, and head-to-head records. The standings page is public and visible to competitors and spectators.

4. **Generate the next round.** Once all matches in the current round are complete, use the admin panel to generate the next round's pairings.

Repeat steps 2 through 4 until all rounds are done.

## Competition lifecycle

All formats share the same competition lifecycle: setup, draw preview, and live play. See [Running a live competition](run-live.md) for the full setup and draw-preview steps.
