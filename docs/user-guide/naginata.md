# Naginata competitions

Naginata tournaments run on the same tools as kendo (pools, brackets, Swiss, and
the live scoring console), with a few rule differences the app handles for you:

- An extra scoring target, Sune (a strike to the shin).
- A single 3rd place decided by a playoff, instead of kendo's two joint 3rds.
- Engi-kyogi, a separate kata-demonstration format scored by referee flags
  rather than ippons.

There are two independent switches on a competition, both on its **Settings** tab
and both locked once the draw is generated:

| Switch | What it does | Available for |
|--------|--------------|---------------|
| **Naginata competition** | Adds the Sune ippon and the 3rd-place playoff | Individual and team |
| **Engi (kata demonstration)** | Replaces ippon scoring with flag counting | Individual only |

You can turn on either one alone, or both together for a naginata kata division.

## Sune ippon

Turn on **Naginata competition** and the score editor's waza buttons gain an **S**
(Sune) alongside the kendo set, so the row reads **M K D T S H**. Award it the
same way as any other waza:

- On screen: tap **S** under the side that scored.
- By keyboard: lowercase **`s`** awards Sune to **Shiro** (white, left), and
  **`Shift`+`S`** awards it to **Aka** (red, right). This mirrors the other waza
  keys, where a lowercase letter scores for Shiro and holding Shift scores for
  Aka.

Everything else about a naginata shiai bout (time, hikiwake, kiken, encho) works
exactly as it does for kendo.

## Third-place playoff

Kendo awards two equal 3rd places to the beaten semi-finalists and plays no
bronze match. Naginata instead plays a 3rd-place playoff between the two
semi-final losers, and only the top three positions receive medals.

Once both semi-finals are complete, the app adds the playoff to the knockout
bracket, labelled **3rd Place**. By convention it runs on the same shiaijo as the
final and immediately before it, so it also appears in that court's queue. Score
it like any other bout:

- The winner of the playoff takes 3rd place.
- The loser finishes 4th and does not appear on the awards podium.

The public podium reflects this automatically: a naginata competition shows a
single 3rd place. Refer to [Awards and winners](mobile-app.md#awards-and-winners)
for the full podium behaviour.

## Engi-kyogi (kata demonstration)

Engi-kyogi is a choreographed demonstration performed by a pair of competitors,
judged by a panel of referees who each raise a flag for the side they judge
superior. It is scored entirely differently from a combat (shiai) bout: there are
no ippons, no time on the clock, and no draws.

Turn on **Engi (kata demonstration)** on an individual competition's Settings
tab to switch its score editor to flag counting. You can combine it with the
**Naginata competition** switch for a naginata kata division.

!!! note "Quick-score and overrides are off for Engi"
    Because every Engi result comes from the flag editor, the kendo shortcuts,
    quick-score, manual winner overrides, and daihyosen are disabled for Engi
    competitions.

### Add Engi pairs

An Engi competitor is a pair: two people from the same dojo or team, scored
together as one unit. There are no individual bouts inside a pair. Enter each
pair as a single participant row with both member names and the shared dojo:

```
Name 1, Name 2, Dojo
```

The two names display stacked on one side of the match, and the pair counts as
one entry in the draw and one line in the standings.

### Score with flags

The score editor becomes a flag counter with one column per side, Aka (red)
and Shiro (white):

- Use the **+ / -** buttons under each side, or the keyboard: **`a`** adds a flag
  to **Aka**, **`s`** adds a flag to **Shiro** (either key works with or without
  Shift), **`Backspace`** undoes the last flag you added, and **`Enter`** saves.
- A bout's flags must total 1, 3, or 5. The panel is always an odd size, so
  there is always a majority and a bout can never end in a draw. The editor
  flags any other total and will not let you save it.
- The side with more flags wins the bout.

### Standings

In Engi pools and leagues, pairs are ranked by the following criteria, in order:

1. Total wins.
2. Total flags accumulated across all bouts, as the tie-break when wins are equal.

Both the winning and the losing side's flags count toward that side's own tally,
so a pair that loses by three flags to two still keeps its two flags.
