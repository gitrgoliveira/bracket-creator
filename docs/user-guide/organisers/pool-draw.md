# The pool draw

In a competition with a pool phase, generating the draw decides which pool every competitor fights in. This page explains how those places are worked out, so you can check a pool sheet before you publish it and answer questions about it at the desk.

The same rules apply to team competitions, with each team placed as one entrant. For what happens after the pools, which pool feeds which knockout slot, see [The knockout draw](knockout-draw.md).

## What the draw starts from

The draw itself is deterministic: the same entrants, in the same order, with the same settings, always produce the same pools. Any randomness happens before the draw, and you control it:

- The **tournament app** draws from the participant list as it stands. Use **Shuffle unseeded** in the participants panel to randomise unranked positions first if you want a random order; see [Adding participants](run-tournament.md#adding-participants). When at least one participant is checked in, only checked-in participants join the draw; see [Check-in workflow](run-tournament.md#check-in-workflow).
- The **command line** shuffles the entry list before drawing, so two runs over the same file produce different pools. Pass `--determined` to skip the shuffle and draw in file order. See [create-pools](../commands/create-pools.md).

## How many pools, and their sizes

**Pool size** in competition setup is either a maximum or a minimum, and the two modes divide differently:

- **Maximum players per pool**: the entrant count is divided up, rounding the pool count upwards, and the entrants are then spread as evenly as possible. With 22 entrants and a maximum of 4 you get 6 pools: four pools of 4 and two pools of 3.
- **Minimum players per pool**: the entrant count is divided down, so every pool reaches the minimum and no extra pool opens for the remainder. The players left over join existing pools, one each, so with 22 entrants and a minimum of 4 you get 5 pools: three of 4 and two of 5.

Where the differently sized pools end up on the sheet is decided by the shiai-jo arrangement described below, not by size, so do not expect the larger pools to be listed first.

The **Fit the knockout** qualifier option chooses its own pool count so that the qualifiers fill the bracket exactly; see [How many qualify from each pool](knockout-draw.md#how-many-qualify-from-each-pool). Which pool each competitor lands in still follows the rules below.

## Who lands in which pool

Before the pools are filled, the entry list is arranged:

1. **Seeded competitors** come first, in rank order. They are placed so the top seeds land in different pools, on different shiai-jo, and at opposite ends of each shiai-jo's set of pools. [Seeding](knockout-draw.md#seeding) describes how far apart the draw keeps them.
2. **Everyone else** is grouped by dojo, largest dojo first. This grouping is what keeps members of the same dojo apart: they occupy consecutive positions in the arranged list, and consecutive positions start their placement at different pools.

The pools then fill one competitor at a time. Each competitor starts at a different pool, in rotation, and takes the first pool with space that holds nobody from their dojo and nobody with the same name. Keeping exact name matches apart means two entrants who happen to share a name never land in the same pool, where the sheet could not tell them apart.

## When a dojo has more members than pools

Once a dojo has one member in every pool, no conflict-free pool exists for the rest of its members. Each of those members is placed in the pool currently holding the **fewest members of that dojo** among the pools with space; on a tie, the pool with fewer players, and then the earliest pool.

The overflow therefore spreads as evenly as the remaining space allows instead of piling into the first pool with room. With 24 entrants, 10 of them from one dojo, in six pools of 4, that dojo's members land 2, 2, 2, 2, 1 and 1 to a pool: no pool is all one dojo, and no pool holds more than one pairing from that dojo.

Two limits are worth knowing at the desk:

- Once a dojo outnumbers the pools, some matches between its members are unavoidable. The draw minimises them; it cannot remove them.
- The spread is only as good as the space left. If the last entrants to place are from one dojo and only one pool has room, they share that pool.

## Pools onto shiai-jo

Pools are assigned to shiai-jo in contiguous blocks: with six pools on two shiai-jo, pools A, B and C fight on the first and pools D, E and F on the second. When the counts do not divide evenly, the earlier shiai-jo take one pool more. A shiai-jo with no pool to run would stand idle, so the draw steps the competition's shiai-jo count down when there are fewer pools than shiai-jo; see [How many shiai-jo a competition can use](knockout-draw.md#how-many-shiai-jo-a-competition-can-use).

Because the seeds are spread before the pools fill, the top seeds' pools sit on different shiai-jo wherever the counts allow, and their pool matches run in parallel rather than one after another on the same shiai-jo.
