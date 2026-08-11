# Tournament formats

bracket-creator supports four competition formats. The format determines how competitors are paired and how the final standings are reached. The choice between individual and team play is separate, made at competition creation; all four formats support both. See [Team tournaments](team-tournaments.md) for team-specific behaviour.

## Choosing a format

The following table summarises when each format fits best.

| Format | Best for |
|--------|----------|
| Playoffs | Small fields or qualification rounds where a straight knockout is enough |
| Mixed | Most events with a moderate-to-large field; pools thin the field before the knockout |
| League | Small fields where every match matters and standings tell the full story |
| Swiss | Large individual fields where you want many rounds but a full round-robin is impractical |

!!! note
    In the tournament app's competition setup, these formats are labelled **Knockout only** (Playoffs), **Pools + Knockout** (Mixed), **League**, and **Swiss**.

## Playoffs

Playoffs is a direct single-elimination (knockout) format. Each match eliminates one competitor; the winner advances until one remains. There are no pools and no preliminary phase.

Use playoffs when the field is small, or when you are running a dedicated knockout stage after a separate qualifying event.

A playoffs competition splits its bracket across shiai-jo just as a mixed one does, so it must be assigned **1, 2, 4, 8 or 16 shiai-jo (courts)**. See [Mixed](#mixed) below for the reason, or [How many shiai-jo a competition can use](knockout-draw.md#how-many-shiai-jo-a-competition-can-use) for the full explanation.

## Mixed

Mixed runs two phases. First, competitors are divided into pools and play a round-robin within each pool. Then the top finishers from each pool advance to a knockout bracket.

Use mixed for most events. Pools give every competitor several matches before the knockout begins.

The knockout half is not a fresh random draw. Each shiai-jo gets its own block of the bracket; a pool's winner stays in that block and its other qualifiers cross into a partner shiai-jo's block, which keeps two competitors from the same pool apart for as long as the bracket allows. Byes are worked out within each block, and the seeds you set decide which quarter of the draw the top competitors land in. See [The knockout draw](knockout-draw.md) for the full rules and worked examples.

Because those blocks merge in pairs, a mixed competition must be assigned **1, 2, 4, 8 or 16 shiai-jo (courts)**. Two blocks produce one survivor, those survivors pair off again, and so on, so the count has to halve cleanly all the way down to a single final. Being even is not enough on its own: six blocks pair off into three, and three cannot pair off again, so 6 and 10 are refused just as 3, 5 and 7 are. A single shiai-jo is always allowed: its block splits into two halves that act as partner shiai-jo.

This applies to the competition's own allocation, not to the venue. A hall with three shiai-jo is normal and stays as it is: it runs each of its competitions on 1 or 2 of the three, and can run two competitions side by side to keep all three busy. Playoffs competitions follow the same rule, because they also split their bracket across shiai-jo. League and Swiss competitions produce no bracket and can use any number. See [How many shiai-jo a competition can use](knockout-draw.md#how-many-shiai-jo-a-competition-can-use).

## League

League is a full round-robin: every competitor meets every other competitor. Final standings come from the accumulated results across all matches; there is no knockout stage.

Use league for small fields (typically eight or fewer competitors) where you want standings that reflect the complete head-to-head record rather than a single bracket run.

## Swiss

Swiss is a tournament system that runs over a fixed number of rounds. No one is eliminated; instead, each round pairs competitors with similar win records, so results stay competitive throughout.

Use Swiss for large individual fields where a full round-robin would require too many rounds, but you still want every competitor to play multiple well-matched bouts.

!!! note
    Swiss is a format, not a status. A Swiss competition runs through the same lifecycle as mixed and league, but its rounds are generated one at a time rather than all at once.

### Swiss round flow

Before play begins, set the number of rounds in the competition settings. You can change this number later, including after rounds have started; the next round you generate uses the updated number. Then follow these steps for each round:

1. **Start the competition.** Round 1 pairings are generated automatically. If seeds are set, round 1 uses fold pairing (1 vs N, 2 vs N-1, and so on). Without seeds, the pairing is deterministic-random. From round 2 onward, players with similar win records face each other.

2. **Record match results.** Scorers enter results in the tournament app. All matches in the current round must be completed before you can advance.

3. **Review the standings.** Standings update in real time and are ranked by wins, then the scoring detail for the competition kind (points scored for individual, the full [team tie-break chain](team-tournaments.md#team-standings-and-tie-breaks) for teams, accumulated flags for [Engi](naginata.md#standings)), then head-to-head. The standings page is public and visible to competitors and spectators.

4. **Generate the next round.** Once all matches in the current round are complete, use the admin panel to generate the next round's pairings.

Repeat steps 2 through 4 until all rounds are done.

## Competition lifecycle

All formats share the same competition lifecycle: setup, draw preview, and match play. See [Running a tournament](run-tournament.md) for the full setup and draw-preview steps.
