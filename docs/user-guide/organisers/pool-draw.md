# The pool draw

In a competition with a pool phase, generating the draw decides which pool every competitor fights in. This page explains how those places are worked out, so you can check a pool sheet before you publish it and answer questions about it at the desk.

The same rules apply to team competitions, with each team placed as one entrant. For what happens after the pools, which pool feeds which knockout slot, see [The knockout draw](knockout-draw.md).

## What the draw starts from

The draw itself is deterministic: the same entrants, in the same order, with the same settings, always produce the same pools. Any randomness happens before the draw, and you control it:

- The **tournament app** draws from the participant list as it stands. Use **Shuffle unseeded** in the participants panel to randomise unranked positions first if you want a random order; see [Adding participants](run-tournament.md#adding-participants). When at least one participant is checked in, only checked-in participants join the draw; see [Check-in workflow](run-tournament.md#check-in-workflow).
- The **command line** shuffles the entry list before drawing, so two runs over the same file produce different pools. Pass `--determined` to skip the shuffle and draw in file order. See [create-pools](../commands/create-pools.md).

## How many pools, and their sizes

**Pool size** in competition setup is either a maximum or a minimum, and the two modes divide differently:

- **Maximum players per pool**: the entrant count is divided up, rounding the pool count upwards, and the entrants are then spread as evenly as possible. With 22 entrants and a maximum of 4 you get six pools: four of 4 and two of 3.
- **Minimum players per pool**: the entrant count is divided down, so every pool reaches the minimum and no extra pool opens for the remainder. The players left over join existing pools, one each, so with 22 entrants and a minimum of 4 you get five pools: three of 4 and two of 5.

Where the differently sized pools end up on the sheet is decided by the shiai-jo arrangement in [Pools onto shiai-jo](#pools-onto-shiai-jo), not by size, so do not expect the larger pools to be listed first.

Two settings are refused rather than drawn. A minimum pool size larger than the entrant count cannot form a single pool, and a **Pools + Knockout** competition whose entrants would form only one pool is refused when you start it, because one pool with a two-competitor final is a league in all but name. Both refusals name the entrant count and the pool size, and both are resolved the same way: reduce the pool size, add entrants, or change the format to League.

The **Fit the knockout** qualifier option chooses its own pool count so that the qualifiers fill the bracket exactly; see [How many qualify from each pool](knockout-draw.md#how-many-qualify-from-each-pool). Which pool each competitor lands in still follows the rules below.

## Who lands in which pool

**Seeded competitors** are placed first, in rank order: in different pools and, as far as the shape allows, on different shiai-jo and at opposite ends of each shiai-jo's set of pools. Two seeds never share a pool; where a configuration cannot satisfy every constraint, the deepest one gives way and the draw warns you which. [Seeding](knockout-draw.md#seeding) describes how far apart the draw keeps them. Their dojos are recorded before anyone else is placed.

**Everyone else** is then placed one at a time, in the order the list arrives (shuffled upstream, as described at the top of this page); the draw does not reorder it. Before anyone is placed, the draw works out which branch of the knockout tree each pool's qualifiers will feed, and every placement is recorded per pool and per branch. Each competitor descends that tree: at every fork, the branch holding fewer of their dojo wins, then the one with more room, and the same rule picks the pool at the bottom. That descent is what keeps a dojo apart: its second member lands in the opposite half, its third and fourth in untouched quarters, without the draw ever needing to look ahead.

The dojo is the only thing the draw keeps apart. Sharing a name is not a conflict: two competitors can share a name only when their dojos differ, because a second entry with both the same name and the same dojo is refused as a duplicate of the same person. Namesakes are two different people, they may fight in the same pool, and the sheet tells them apart by dojo and by competitor number.

Once every competitor has a pool, the draw examines the finished result and exchanges competitors between pools where that keeps dojos apart longer, and only then. An exchange is taken only when it helps: no dojo's first meeting may get earlier because of one, pool sizes never change, and a seeded competitor is never moved. On a roster where no dojo can be kept apart any better, the pools come out exactly as placed.

### Watch a draw being made

The walk-through below applies the rules on this page to two example rosters. Step through it one competitor at a time, or press Play and watch the pools fill. At each fork it shows how many of that competitor's dojo already sit on either side of the bracket, and which way that sends them.

<div data-pool-draw-animation>
  <p>This walk-through needs JavaScript. Everything it demonstrates is described in the text above and below it.</p>
</div>

Both rosters, and the pools the draw produces from them, come from the application itself rather than from a hand-written example.

## When a dojo has more members than pools

Once a dojo has one member in every pool, some of its members must share, and the draw spreads them as evenly as the pool count allows. With 24 entrants, 10 of them from one dojo, in six pools of 4, that dojo's members land 2, 2, 2, 2, 1 and 1 to a pool: no pool is all one dojo, no pool holds more than one pairing from that dojo, and no spread with fewer shared pools exists.

Two of its members meeting in a pool is arithmetic, not a shortcoming: a dojo with more members than pools cannot avoid it, and the draw's job is to make it as rare as the numbers allow.

Setting seeds does not cost you any of this. Seeds take their places first, exactly where the seeding rules put them, and their dojos are counted from the start, so the rest of their dojo is spread around them. A roster spreads its dojos just as well seeded as unseeded.

## Dojo-mates and the knockout

Keeping dojo-mates out of one pool is only half the job: two members of one dojo in different pools can still be drawn to meet in the very first knockout match if their pools feed neighbouring slots. Because every placement is made against the knockout tree, the draw pushes that first dojo-mate meeting as late as the bracket allows, and in particular keeps it out of the first round wherever any arrangement of the pools could.

With two qualifiers per pool the second place crosses to a partner region, and a crossed second meeting a dojo-mate there is chance the draw cannot rule out. It still makes a best effort: where two pools serve a competitor equally well, the draw prefers the one whose crossing lands its qualifiers furthest from the rest of the dojo. Beyond that, the limits are the sheet's own arithmetic, and they are worth knowing at the desk. When a dojo's members qualify from pools that must feed neighbouring slots, because the pool counts leave no alternative, the early meeting stands; and when several dojos compete for the same few well-separated pool pairs, not every dojo can have one, so the draw settles the contest in favour of as few early meetings as possible overall.

## Pools onto shiai-jo

Pools are assigned to shiai-jo in contiguous blocks: with six pools on two shiai-jo, pools A, B and C fight on the first and pools D, E and F on the second. When the counts do not divide evenly, the earlier shiai-jo take one pool more. A shiai-jo with no pool to run would stand idle, so the draw steps the competition's shiai-jo count down when there are fewer pools than shiai-jo; see [How many shiai-jo a competition can use](knockout-draw.md#how-many-shiai-jo-a-competition-can-use).

Because the seeds are spread before the pools fill, the top seeds' pools sit on different shiai-jo wherever the counts allow, and their pool matches run in parallel rather than one after another on the same shiai-jo.

## Competitor numbers

Every competition has a number prefix. You can set your own in competition setup, or leave it blank: the app then derives one from the competition's name, for example "Kendo Open" becomes K, or KO if K is already used by another competition that day. The setup page always shows you the derived prefix as you type the name, before you save. The settings page shows it too, but only while the competition has no prefix stored yet; once one is saved, the field simply shows that stored value.

Generating the draw gives every competitor a number built from that prefix, and that number identifies them on the pool sheet, on the printed tags and at the desk. Numbers run in draw order straight through the pools, so with the prefix K and pools of four, K1 to K4 are the first pool, K5 to K8 the second, and so on.

Because the numbers follow the finished draw, they are only fixed once you generate it. Until then the participants panel shows a provisional number, which changes if the roster or the draw changes.

You can change the number prefix at any time, including after the draw is generated or the competition has started. Saving the new prefix renumbers every competitor immediately; it does not change who is in which pool or who plays whom. If you have already printed tags, reprint them after changing the prefix, because the numbers on the old tags no longer match.
