# Naginata competitions

Naginata competitions run on the same tools as kendo (pools, brackets, Swiss, and
the scoring console), with naginata-specific rules the app handles for you.
Naginata has two competition forms:

- **Combat** (shiai): armoured matches scored by ippon, with one extra target,
  Sune (a strike to the shin). Naginata divisions conventionally decide a
  single 3rd place with a bronze match instead of kendo's two joint 3rds. Refer to
  [Third-place match](#third-place-match) for how that is set.
- **Engi-kyogi**: the kata form, in which a pair perform choreographed forms
  scored by referee flags rather than ippon. The app runs it as an
  [Engi (pair kata) competition](engi.md), the same setting any other kata
  competition uses.

You pick the form with two checkboxes on a competition's **Settings** tab, both
locked once the draw is generated:

| Checkbox | What it does | Available for |
|--------|--------------|---------------|
| **Naginata competition** | Turns on the combat rules: the Sune ippon button | Individual and team |
| **Engi (kata competition)** | Flag-count scoring for pair kata in any art; refer to [Engi](engi.md) | Individual only |

The 3rd-place rule (joint vs. single) is a separate setting, shared with kendo
competitions; refer to [Third-place match](#third-place-match).

A naginata engi-kyogi division usually has both on: **Engi** for the flag
scoring and **Naginata competition** for the single 3rd place. A kata
competition for another art ticks **Engi** alone.

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

## Third-place match

Whether a competition awards two joint 3rd places or decides a single one with
a bronze match is its own setting: the **Award two joint 3rd places**
checkbox on the competition's **Settings** tab. It is independent of the **Naginata competition** and **Engi (kata competition)** checkboxes. It applies to knockout, pools-then-knockout, and
league competitions; it is not shown for Swiss, which has no bracket.

Kendo awards two equal 3rd places to the beaten semi-finalists and plays no
bronze match; that is the default for every new competition. Naginata
conventionally awards a single 3rd instead, so turning on **Naginata
competition** also unticks **Award two joint 3rd places** for you. You can tick it back on afterwards if your event wants naginata to share 3rd place
too. The same applies in reverse: a kendo (non-naginata) competition can turn
the checkbox off to decide a single 3rd with a bronze match, for example for a
selection event that needs exactly one bronze medal.

When a single 3rd place is in effect and both semi-finals are complete, the
app adds the bronze match to the knockout bracket, labelled **3rd Place**. By
convention it runs on the same shiai-jo as the final and immediately before
it, so it also appears in that court's queue. Score it like any other bout:

- The winner takes 3rd place.
- The loser finishes 4th and does not appear on the awards podium.

The public podium reflects this automatically. Refer to
[Awards and winners](run-tournament.md#results-and-awards) for the full
podium behaviour, including how the same setting works for league standings.
