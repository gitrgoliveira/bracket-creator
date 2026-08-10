# 007: EKC-aligned pool-to-knockout draw

**Bead:** bc-draw
**Status:** Phase 0. This document is the definition of record for rules R1-R9 and
pinned defaults D1-D7. Once agreed, divergence found later is fixed in **code**,
never by quietly rewriting this spec. Phases 1-5 are checked against it.

**Source of truth:** the DESCRIPTION section of bead `bc-draw`. Where the bead's
append-only NOTES disagree with its description, the description wins (several notes
are explicitly superseded by the 2026-08-09 plan review).

## Problem

A mixed competition (pools then knockout) draws its bracket by flattening the pool
finishers into one list and halving it recursively:

1. `GenerateFinals` emits `poolWinners` passes over the pool list; on pass `r`, pool
   `p` contributes the finisher of rank `(p + r) % poolWinners`. Adjacent slots
   therefore hold **adjacent pools**.
2. `CreateBalancedTree` splits that flat list at the midpoint, recursively.
3. `treeAdjustment` then repairs placement with a **two-node local swap** inside each
   two-level subtree, so that a "-1st" sits above a "-2nd".

`AssignPoolsToCourts` allocates pools to courts in contiguous blocks. Combining that
with step 1 gives four concrete divergences from the reference format:

- **Round-1 pairings are within-court at 2 qualifiers.** Adjacent pools share a
  shiaijo, so our 2-qualifier draw pairs two pools of the same court in round 1, where
  the reference crosses every 2nd place to a partner court.
- **No shiaijo is a contiguous region.** Every court's qualifiers are split across both
  halves of the draw. Measured: 4 pools x 2 qualifiers on 2 courts produces
  `[A-1st B-2nd C-1st D-2nd | B-1st A-2nd D-1st C-2nd]` with pools A,B on court 1 and
  C,D on court 2. Tree page 1 is titled "Shiaijo A" and its roster overlay lists pools
  A and B, but the bracket printed on that page contains C-1st and D-2nd. The page
  title is already wrong today; no page **can** be right until regions exist.
- **Byes fall wherever the recursive halving leaves an odd leaf.** Pool size is never
  considered, and seeding is not consulted. For 7 pools x 2 qualifiers we bye pools A
  and B; the reference byes pools 3 and 7. At 3+ qualifiers the local swap breaks down
  entirely: measured across 2-12 pools, byes land on 2nd and 3rd places while pool
  WINNERS play a round-1 match (at 2, 6, 7, 8, 9, 11 and 12 pools), and a pool's own
  qualifiers can meet as early as round 2.
- **Tree pages do not map onto shiaijo.** `TreePageLayout` only ever returns a power of
  two, and `SubdivideTree` cannot represent a non-power-of-two page count at all: asked
  for 3 pages on a 12-leaf tree it returns `[left half, right half, WHOLE TREE]`, so
  page 3 duplicates every match on pages 1 and 2. Asked for 5 it returns four quarters
  plus the whole tree; for 6, `[Q1, Q2, top half, Q3, Q4, bottom half]`. This path is
  unreachable today only because `TreePageLayout` never asks for a non-power-of-two.

The bracket **shapes** we produce are correct single-elimination brackets. What is
wrong is the pool-to-slot mapping, the bye allocation, and the page-to-shiaijo
correspondence. R3, R4, R6 and R8 are violated today (R5 additionally at 3+
qualifiers); R9 is unenforced.

## Reference draws (34th EKC 2026, Podgorica)

All three official draw sheets were decoded. All three run on **4 courts**, so none of
them settles a non-power-of-two court count; see the pinned defaults for that.

Round-1 pairings and byes below are transcribed exactly as recorded in the bead. They
are the observed sheet layout, not a derivation.

### A. Junior Individual Male

18 pools, **1 qualifier** per pool. Courts: **A**(1-5) **B**(6-10) **C**(11-14)
**D**(15-18).

| Region | Home pools | Round-1 |
|---|---|---|
| Court A | 1, 2, 3, 4, 5 | **P1 byes**, P2 v P3, P4 v P5 |
| Court B | 6, 7, 8, 9, 10 | **P6 byes**, P7 v P8, P9 v P10 |
| Court C | 11, 12, 13, 14 | P11 v P12, P13 v P14 |
| Court D | 15, 16, 17, 18 | P15 v P16, P17 v P18 |

**Demonstrates:**

- **R3 (court blocks).** Each shiaijo's pools occupy exactly one contiguous region.
- **R4 at 1 qualifier: nothing crosses.** Every round-1 match is between two pools of
  the SAME court (P2 v P3 are both court A). This is the case that refutes any blanket
  "round-1 opponents must come from different courts" rule: cross-court pairing is
  carried by the **crossing qualifiers** (2nds and 3rds), not by round-1 adjacency.
- **R6 (byes are region-local and are a structural consequence).** Courts A and B hold
  5 pools each (odd) and each grants exactly one round-1 bye; courts C and D hold 4
  each and grant none. No pool of court C or D receives a bye whatever its precedence.
- **The region-depth asymmetry is real even at 4 courts.** A 5-pool region needs three
  rounds to produce a court champion; a 4-pool region needs two. The A/B half is one
  round deeper than the C/D half.

### B. Junior Individual Female

7 pools, **1 qualifier** per pool. Courts: **A**(1,2) **B**(3) **C**(4,5) **D**(6,7).
Seeds marked on pools **1, 3 and 4** (three seeds, not four).

- **F1:** P1 v P2 (court A); the winner meets P3 in F4.
- **P3** (court B, its court's ONLY pool, seeded) **byes** to F4.
- **F2:** P4 v P5 (court C); **F3:** P6 v P7 (court D); the winners meet in F5.
- F4 and F5 are the semifinals; their winners contest the final.

Halves: {A, B} and {C, D}. Half 1 holds 3 entrants, half 2 holds 4.

**Demonstrates:**

- **The only reference with unequal pools per court** (2/1/2/2).
- **The region-depth default (D2).** Court B has the fewest pools (1) and got the
  shallowest region: its occupant reaches the half-final having played nothing, while
  court A's occupant plays one match first.
- **R6 criterion 1 (seeded pools first).** The one structural bye in the A/B half went
  to court B's seeded pool.
- Seeds on pools 1, 3, 4 sit on courts A, B, C respectively: **one seeded pool per
  court**, consistent with R2, though with only three seeds this draw does not pin the
  4-seeds-on-4-courts case.

### C. Junior Team

7 pools, **2 qualifiers** per pool. Courts: **A**(1,2) **B**(3,4) **C**(5,6) **D**(7).

| Region | Round-1 | Occupants |
|---|---|---|
| Q1 (court A) | P1#1 v P5#2, P2#1 v P6#2 | 4 |
| Q2 (court B) | **P3#1 byes**, P4#1 v P7#2 | 3 |
| Q3 (court C) | P5#1 v P1#2, P6#1 v P2#2 | 4 |
| Q4 (court D) | **P7#1 byes**, P3#2 v P4#2 | 3 |

**Demonstrates:**

- **R4 (crossing) at 2 qualifiers.** Every **1st** stays in its own court's region;
  every **2nd** crosses to the **partner** court. Partners here are **A-C** and
  **B-D**: court A's pools 1,2 send their 2nds to Q3, court C's pools 5,6 send theirs
  to Q1; court B's pools 3,4 send theirs to Q4, court D's pool 7 sends its 2nd to Q2.
- **R5 (separation) at 2 qualifiers.** Halves are (Q1,Q2) and (Q3,Q4), so partner
  courts sit in **opposite halves** and a pool's 1st and 2nd can only meet in the
  final. Verify on P1: P1#1 in Q1 (half 1), P1#2 in Q3 (half 2).
- **Structure beats preference (R4, last clause).** Court D has one home pool, so Q4 is
  short of home 1sts and must be filled by crossed-in occupants. Two of them come from
  the same source court and meet each other in round 1: **P3#2 v P4#2**, two court-B
  seconds. The construction does not avoid this; it accepts it.
- **R6 (byes) with seeded pools.** The two 3-occupant regions each carry one round-1
  bye, and both went to a **home 1st of a seeded pool**: P3#1 and P7#1. The seeded
  pools' winners were forced into the two odd regions by the court allocation.

### What the three references pin together

| Property | Male | Female | Team |
|---|---|---|---|
| Court region is contiguous (R3) | yes | yes | yes |
| Nothing crosses at 1 qualifier (R4) | yes | yes | n/a |
| 2nds cross to the partner court (R4) | n/a | n/a | yes |
| Partner courts in opposite halves (R5) | n/a | n/a | yes |
| Byes region-local, only where structural (R6/R7) | yes | yes | yes |
| Bye to a seeded pool's 1st (R6-1) | not shown | yes | yes |
| Shallow region to the fewest-pools court (D2) | n/a | yes | n/a |
| Unequal pools per court | no | yes | yes |
| 3 qualifiers (R4c, D3) | never | never | never |

Round-1 byes per region across all three references: court occupancy 1 -> 1 bye,
2 -> 0, 3 -> 1, 4 -> 0, 5 -> 1. All five are `q mod 2`, and `q = 5` is the observation
that discriminates between the two candidate region constructions; **D4** decides it.

## Rules

Normative keywords are MUST / MUST NOT / SHOULD in the RFC sense. "Region" means the
contiguous subtree of the knockout bracket that belongs to one shiaijo (R3).
"Occupant" means a qualifier placed in a region, whether it is a home 1st or crossed in
under R4.

### R1 Seeds

The operator MUST choose both the seeded competitors/teams and their **order** (ranks
1-4). The tool MUST NOT derive seeds from previous results, and MUST NOT accept tied
ranks: the existing distinct-rank seed model is sufficient and both
`domain.ValidateSeedAssignments` and `helper.ApplySeeds` already reject duplicates.

*Rationale:* seeding is a competitive-protection decision that belongs to the
organising committee, not to a heuristic over data the tool does not hold.

### R2 Seed placement

Seeded pools MUST be distinct, and their qualifiers MUST be spread as widely as the
configuration allows. The **normative statement of the rule is D6**, which is a single
constraint covering every court count; the per-court-count bullets below are worked
examples derived from it, not independent rules:

- **4 or more courts:** one seeded pool per court, each in a different quarter of the
  draw.
- **2 courts:** two seeded pools per court. Seeds **1 and 3** share one half, seeds
  **2 and 4** share the other, each in its own quarter, so the semifinals are **1 v 3**
  and **2 v 4** when the seeds hold (operator decision, 2026-08-09).
- **1 court:** the seeds go in different quarters of the single region.
- **Fewer than 4 pools:** the effective seed count is capped at the pool count, and the
  surplus seed ranks are ignored **with a warning**. It MUST NOT be an error.

Fewer than 4 seeds, **including none at all**, is a normal configuration, not a
degenerate one. The rule applies to whichever ranks are present, in rank order; with
zero seeds it is vacuous and placement falls through to R4 and R6 alone. When the
configuration cannot satisfy every constraint at once, D7 gives the order in which they
give way.

*Reference:* the Female draw places its **three** seeds on three different courts
(A, B, C) and gives one of them the half's only bye. It is a 3-seed draw, so it
corroborates "distinct courts, distinct quarters" but does not pin the rank-to-court
mapping.

### R3 Court blocks

Each shiaijo's pools MUST occupy exactly ONE contiguous region of the draw, and that
region MUST **be a subtree** of the bracket, not merely a contiguous span of leaves.

*Rationale:* a tree page must be a genuine subtree to print as a bracket (R8). A flat
leaf list halved recursively cannot guarantee a court's region is a subtree for
arbitrary court counts, which is why Phase 4 builds the tree **court-first**: one
subtree per shiaijo over that court's occupants, then combine the court subtrees into
the full bracket. R3 then holds by construction rather than by arithmetic coincidence.

*Reference:* all three draws. Each court block is one region in every one of them.

### R4 Crossing

Courts MUST form **partner pairs** that sit half the bracket apart: A-C and B-D on four
courts, each other on two. Within a pool:

- **(a)** The 1st place MUST stay in its own court's region.
- **(b)** The 2nd place MUST cross to the **partner** court's region.
- **(c)** The 3rd place MUST cross to the **other court pair's** regions, landing in a
  different quarter from both the pool's 1st and its 2nd. At fewer than 4 courts this
  degrades to "a different quarter than both", balancing region sizes. EKC never shows
  3 qualifiers, so this is our extrapolation (operator decision, 2026-08-09); the exact
  placement is pinned in **D3** below, and **D5** continues the same rotation for 4th
  and further qualifiers.
- **(d)** At **1 qualifier** per pool nothing crosses and round-1 matches are
  within-court, exactly as both EKC individual draws show.
- **(e)** A **1-court** competition emulates the structure: the court's pools split into
  two half-blocks that act as partner courts, 1sts staying in their block's half, 2nds
  crossing to the other (operator decision, 2026-08-09). The draw's shape is then
  identical whether an event runs on 1 court or several.
- **(f) Structure beats preference.** A region short of home 1sts MAY host crossed-in
  qualifiers meeting each other. This is not a defect to be avoided.

*Reference:* the Team draw for (a), (b) and (f) (Q4 = P3#2 v P4#2); both individual
draws for (d).

### R5 Separation

A pool's qualifiers MUST be separated maximally.

- **At 2 qualifiers:** opposite halves, so they can only meet in the final. This is
  **guaranteed** by R4 plus the partner-courts-in-opposite-halves property, not merely
  attempted.
- **At 3 or more qualifiers:** two of them necessarily share a half (pigeonhole), so
  the rule becomes: **no two qualifiers of one pool in the same quarter**, and the
  earliest possible same-pool reunion pushed as late as the structure allows.
- **Beyond one qualifier per quarter:** the no-two-in-a-quarter guarantee is itself
  pigeonhole-limited. A draw has four quarters, so at **5 or more** qualifiers per pool
  two of them must share one and R5 degrades to the reunion clause alone. **D5** states
  the placement rotation and where the guarantee stops.

*Reference:* the Team draw, P1#1 in Q1 against P1#2 in Q3.

### R6 Byes

Bye slots are **REGION-LOCAL**. Each structural bye belongs to the shiaijo region that
owns the slot and MUST be granted to one of **that region's actual occupants**,
including qualifiers that crossed in under R4. The 3rd-place routing therefore shifts
which region's ladder a qualifier competes in, and its bye eligibility with it
(operator requirement, 2026-08-09: R6 is occupant-based, not pool-based).

Within a region, precedence for each structural bye slot:

1. **Home 1st places of seeded pools**, in operator seed order (1, 2, 3, 4).
2. **Home 1st places of oversized pools** (pools whose qualifier plays more pool
   matches than the lightest pool's qualifier; see **D1** for the exact metric), in
   descending load, ties by pool order.
3. **Remaining home 1st places**, in pool order.
4. **Crossed-in 2nd places**, ranked by their own pool's precedence (seed, then size,
   then pool order); then **crossed-in 3rd places** likewise, then any further
   crossed-in ranks in rank order.

Precedence is a **preference, not a guarantee**: R3, R4 and R5 win. A region with no
structural bye MUST grant none regardless of precedence. **How many byes a region has
is D4**; this list only decides who gets them.

*Rationale for criterion 2:* in a round-robin pool of n every competitor fights n-1
matches, so a 4-pool winner reaches the knockout on 3 matches against a 3-pool winner's
2. A knockout bye evens both the total match count and the rest interval. This is
**fatigue compensation**, which is why it ranks BELOW seeding (**competitive
protection**) rather than above it.

*Reference:* the Male draw is the criterion-3/structural case (courts A and B, 5 pools
each, one bye each to P1 and P6; courts C and D, 4 pools each, none). The Female and
Team draws are the criterion-1 case (byes to seeded pools' winners).

### R7 Degradation ladder

When a region's structure needs more byes than it has home 1st places, the excess MUST
flow down the R6 list: crossed-in 2nds, then crossed-in 3rds. Bye arithmetic is **PER
REGION** under the court-first construction, not global: a region of `q` occupants
carries `NextPow2(q) - q` empty slots.

*Note, and this supersedes the earlier framing of R7:* `NextPow2(q) - q` counts a
region's empty **leaf slots**. It is NOT the number of named byes. Most of those empty
slots pair with each other and are never seen. The number of occupants that receive a
named round-1 bye is `q mod 2`, pinned in **D4**, which is what R6 allocates over. Quote
the leaf-slot formula only when talking about slot arithmetic, never when talking about
byes.

### R8 Tree pages

The Excel tree page count MUST be a multiple of the shiaijo count: a **power of two of
pages per shiaijo** (1, 2 or 4), the smallest that fits `MaxPlayersPerTree` (16) per
page. A 2-page court prints its region's two child subtrees; a 4-page court prints its
four grandchildren. Oversized regions get **more pages, never an error** (operator
decision, 2026-08-09). The explicit `--single-tree` override still wins and forces one
page.

*Rationale:* a page must be a genuine subtree to print as a bracket, and an operator
running shiaijo C must be able to pick up the pages for shiaijo C. Today neither
`TreePageLayout` nor `SubdivideTree` can express this (see Problem), so Phase 4
replaces `SubdivideTree`'s count-based split with a court-block splitter and makes
`TreePageLayout` return `numCourts x {1,2,4}`.

### R9 Shiaijo count

A competition MUST be allocated **1 shiaijo or an EVEN number** of shiaijo. An odd
allocation greater than 1 (3, 5, 7, ...) MUST be rejected. A single-shiaijo competition
is explicitly allowed and gets the R4(e) half-block draw.

The constraint is on the **COMPETITION's** allocation, not the venue total: a 5-shiaijo
tournament MAY run one competition on 4 and another on 1.

*Rationale:* R4 requires courts to pair up; with an odd count one court has no partner
and its runners-up have nowhere to cross to. Even allocation is also what guarantees
R5's opposite-halves property.

Enforcement MUST land at all four entry points together, so no path can produce an
unpairable allocation: the HTTP API, the engine, the operator UI, and the CLI
`--courts` flag (which today enforces only the 26-court A-Z label cap). Existing data
is validated **on write only**: a competition already saved with an odd allocation
keeps running and shows a persistent warning on its settings screen, because blocking
the settings PUT would lock the operator out of unrelated edits.

## Pinned defaults

D1-D3 are the three the bead required to be decided here. D4-D7 close the four items
that a first draft of this spec left open; they were ruled on by the operator on
2026-08-10. All seven are rules, not open questions. Where a default goes beyond what
the EKC sheets show it is labelled an extrapolation and listed again at the end of this
document.

### D1. R6 criterion 2 under `poolFormat: "partial"`

**Rule.** A pool is **oversized** when its qualifier plays MORE pool matches than the
qualifier of the competition's lightest pool. The load metric is defined per pool
format:

- **`poolFormat: "full"`** (round robin, the default, and the `roundRobin` legacy
  path): the pool's **participant count**. In a round robin of n every competitor
  fights n-1 matches, so participant count and per-competitor load induce the same
  ordering.
- **`poolFormat: "partial"`**: the **number of matches generated for that pool**, read
  from the drawn pool (`len(pool.Matches)`), not recomputed from its size.

Criterion 2 ranks by that metric **descending**, ties by pool order. Only pools whose
metric strictly exceeds the competition minimum qualify; when every pool has the same
metric, criterion 2 contributes nothing and precedence falls through to R6-3.

**Verified against the code.** `CreatePartialPoolMatches`
(`internal/helper/pool_partial.go:17`) emits an **adjacent-neighbour path graph**:
for a pool of N players it appends exactly **N-1** matches, `(0,1), (1,2), ...,
(N-2,N-1)`, skipping pools with fewer than 2 players. So under `partial` the generated
match count is `len(Players) - 1` and the generated-count ordering is **identical** to
the participant-count ordering. The two branches of this rule can never disagree about
which pools are oversized.

**Why still read the generated count rather than derive it.** The count is the thing
the rule is actually about. Reading `len(pool.Matches)` stays correct if the partial
pairing shape ever changes (a different graph, a round cap, an operator-tuned format),
whereas a size-derived shortcut would silently keep ranking by a proxy that no longer
tracks match load.

**Honest limitation to record.** Under `partial` each competitor fights 1 match (the
two endpoints of the path) or 2 (everyone else) regardless of pool size, so the
fatigue rationale that justifies criterion 2 is weaker there than under a round robin.
The criterion remains deterministic and total, and matches the operator's pinned
definition; it simply compensates less because there is less to compensate for.

### D2. Region depth

**An even court count does not make every draw symmetric.** With 6 courts each half
holds 3 regions, and 3 regions cannot merge symmetrically: one court's occupants reach
the half-final a round earlier than the other two. The same applies at 10, 12 and 14.
Only 1, 2, 4, 8 and 16 courts are fully symmetric. Unequal pools per court produces the
same asymmetry at **any** court count, including 4 (see the Female draw).

**Rule.** Whenever two or more regions that merge into the same parent node have
different depths, the **shallower** slot is allocated to the court with:

1. the **fewest pools**; then
2. the **fewest entrants** across those pools; then
3. the **earliest court order** (A before B before C ...).

This is deterministic and computed at draw time. It is NOT an operator decision per
event and MUST NOT be exposed as one, and it is not chusen (a random draw).

*Rationale:* the shallower region is a region-level bye, so it is a real competitive
advantage. Giving it to the court with the fewest pools makes it a compensation rather
than a windfall: that court's occupants had the smallest field to qualify from but also
the fewest structural bye slots inside their own region, and the shallow slot evens the
total match count on the path to the final. The entrant-count tiebreak refines the same
argument when two courts hold the same number of pools; court order is a last resort so
the result is reproducible.

*Corroboration:* the Female draw. Court B holds 1 pool, the fewest of any court, and it
is court B that got the shallow region: P3 byes straight into the half-final F4 while
court A's occupants play F1 first. The same draw also shows the bye landing on that
court's seeded pool, which is R6-1 doing its job inside the shallow region.

### D3. Third-place quarter assignment (R4c)

**EKC never shows 3 qualifiers per pool. This is our extrapolation, not a decoded
reference.** It is stated here as a rule so an implementer can code from it and so
Phase 1's golden file (which sweeps 1-4 qualifiers) has something to be checked
against.

The construction places 1st places (R4a) and 2nd places (R4b) first, then 3rd places by
the rule below, then any further qualifiers (see Open).

**Rule at 4 or more courts.** Courts are paired as `i` with `i + k`, where `2k` is the
court count; pairs are indexed by their lower court. Half 1 is courts `0..k-1`, half 2
is courts `k..2k-1`. For a pool on court `c`, processed in pool order:

1. **Candidate pairs** are all pairs other than `c`'s own pair. At exactly 4 courts
   (`k = 2`) there is exactly one, so the pair is forced.
2. **Prefer the half that does NOT contain the pool's 1st place.** The 1st is on `c`;
   the 2nd is on `c`'s partner, in the other half. Preferring the non-1st half keeps
   the pool's strongest qualifier as the only one of its pool-mates in its own half for
   as long as the structure allows.
3. Among the candidate regions in the preferred half, take the one with the **fewest
   current occupants**; tie broken by **lower court order**.
4. **Structure beats preference (R4f):** if no candidate region in the preferred half
   has capacity, fall back to the candidate regions in the 1st place's half by the same
   fewest-occupants-then-court-order rule.

At exactly 4 courts this reduces to a fixed involution: **A -> D, B -> C, C -> B,
D -> A**. A court-A pool's 1st is in region A, its 2nd in region C (partner), its 3rd
in region D: three distinct quarters, so R5's no-two-in-a-quarter holds, and each
region receives 3rd places from exactly one other court, so region sizes stay balanced.

**Rule at 2 courts.** There is a single pair {A, B}. Each court's region IS a half of
the draw, and that region's two child subtrees are the quarters: A1, A2, B1, B2 in draw
order. For a pool on court A, its 1st is in A1 or A2 and its 2nd is in B1 or B2. Its
3rd goes to **the other quarter of the partner half**, that is, the quarter of B
holding neither its 1st (which is not in B at all) nor its 2nd. Symmetrically for a
court-B pool: 3rd goes to the quarter of A that does not hold its 2nd. If that quarter
is full while a quarter of the pool's own half still has capacity, the 3rd falls back
there, by fewest occupants then quarter order (R4f again).

This is the same rule as the 4+ court case with "quarter of the partner half" standing
in for "region of the other pair": the 1st place is alone in its half, and the 2nd and
3rd share the other half in different quarters, meeting no earlier than that half's
decider. Both candidate quarters give the same earliest-reunion round, so R5 does not
discriminate between them and region-size balance is the operative criterion, exactly
as R4c says.

**At 1 court**, R4e's half-blocks stand in for partner courts and the 2-court rule
applies unchanged: the 3rd goes to the other half-block's other quarter.

### D4. Region internal shape, and how many empty slots become named byes

**Rule.** A region of `q` occupants is built **GREEDILY**. Its round-1 layer holds
`floor(q/2)` real matches and, when `q` is odd, exactly **ONE** named bye. Every
remaining empty slot pairs with another empty slot, forming a phantom match that is
already dropped downstream and never printed or displayed. So:

> **Round-1 byes per region = `q mod 2`**, and the named bye goes to the region's
> highest-precedence occupant under R6.

**Derivation.** EKC male court A has `q = 5` in an 8-slot region, so 3 empty slots. The
sheet shows **P1 bye, P2 v P3, P4 v P5**: two round-1 matches and ONE named bye, with
the other two empties consumed by a phantom pair and W(P4vP5) taking a round-2 bye. The
alternative construction, "pad to `NextPow2` and spread the empties", would have
produced **three** named byes and one round-1 match, which is not what the sheet shows.
All five observed occupancies (`q` = 1, 2, 3, 4, 5 across the three draws) are
consistent with `q mod 2`, and `q = 5` is the only one that discriminates between the
two constructions. This supersedes the R7 framing: `NextPow2(q) - q` counts empty leaf
slots, not named byes.

The parity always works out, which is why the greedy layout is realisable inside a
power-of-two region: for `q >= 2`, `NextPow2(q)` is even, so `NextPow2(q) - q` has the
same parity as `q`. Exactly `q mod 2` empty slots are left unpaired and the rest form
phantom pairs.

**Deeper layers.** The same greedy rule applies layer by layer: a layer of `x` survivors
holds `floor(x/2)` matches and, when `x` is odd, one bye. Byes above round 1 fall to
whichever slot the phantom pairs leave, and they are taken by **match winners, not
pools**, so R6 does not allocate them and there is nothing for the operator to choose.
Court A of the male draw is the worked case: 5 -> 3 -> 2 -> 1, four matches in three
rounds, with the round-2 bye going to W(P4vP5).

**Phantom matches are already handled downstream** and need no new code:
`computeBracketDisplayMetadata` (`internal/engine/bracket.go:414`) marks
non-real matches `Hidden` via its `isReal` predicate (`:427`),
`buildBracketFromLeaves` invokes it at `:263` before numbering, and the frontend's
`buildDisplayModel` (`web-mobile/js/bracket.jsx:660`) does the equivalent.

**Implementation warning: `CreateBalancedTree` does NOT produce this shape.** Phase 4
MUST construct a region's round-1 layer explicitly rather than delegating to
`CreateBalancedTree`. Simulating the exact current path
(`CreateBalancedTree` -> `TreeToLeafArray` -> pad to `NextPow2`, which is what
`internal/engine/bracket.go:124` `buildBracketFromLeaves` consumes) for `q` = 1..24
gives:

| `q` | greedy | `CreateBalancedTree` | verdict |
|---|---|---|---|
| 1, 2, 3, 4, 7, 8, 15, 16 | as above | identical, bye on occupant 1 | agrees |
| **5** | 2 matches + 1 bye | 2 matches + 1 bye, **bye on occupant 3** | count agrees, **wrong occupant** |
| **9** | 4 matches + 1 bye | 4 matches + 1 bye, **bye on occupant 7** | count agrees, **wrong occupant** |
| **17** | 8 matches + 1 bye | 8 matches + 1 bye, **bye on occupant 15** | count agrees, **wrong occupant** |
| **6** | 3 matches + 0 byes | **2 matches + 2 byes** | disagrees |
| **10** | 5 matches + 0 byes | **4 matches + 2 byes** | disagrees |
| **11** | 5 matches + 1 bye | **4 matches + 3 byes** | disagrees |
| **12** | 6 matches + 0 byes | **4 matches + 4 byes** | disagrees |
| **13** | 6 matches + 1 bye | **5 matches + 3 byes** | disagrees |
| **14** | 7 matches + 0 byes | **6 matches + 2 byes** | disagrees |
| **18-24** | | disagrees at every value | disagrees |

Counts agree only at `q` in {1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17} and the disagreement
set is unbounded (every `q` in 18..24 disagrees, worsening as `q` grows: at `q = 24`
recursive halving gives 8 matches + 8 byes where greedy gives 12 matches + 0 byes).
Where the counts DO agree, the placement can still be wrong: at `q = 2^k + 1` (5, 9, 17)
the bye lands on occupant `q - 2` instead of occupant 1, while at `q = 2^k - 1`
(1, 3, 7, 15) it correctly lands on occupant 1.

`ApplyPoolAdjustments` cannot repair this. `treeAdjustment`
(`internal/helper/tree.go:96`) swaps only when `leftPos > rightPos`, comparing the
**rank ordinal** parsed off the placeholder. In a 1-qualifier competition every occupant
is a "-1st", so the comparison is never strictly greater and no swap ever fires. The
`q = 5` wrong-occupant case IS the EKC male court-A case, and the existing fix-up
provably cannot reach it.

### D5. Qualifiers beyond 3rd place

**This is an extrapolation on the same footing as D3. EKC never shows more than 2
qualifiers per pool.**

**Rule.** R4's crossing generalises as a **rotation over regions**, so that a pool's
qualifiers occupy distinct quarters for as long as distinct quarters exist.

At **4 courts**, a pool on court A places: **1st -> A** (own), **2nd -> C** (partner),
**3rd -> D**, **4th -> B**. Four qualifiers, four distinct quarters. The full rotation
is symmetric:

| Pool's court | 1st | 2nd | 3rd | 4th |
|---|---|---|---|---|
| A | A | C | D | B |
| B | B | D | C | A |
| C | C | A | B | D |
| D | D | B | A | C |

Each region therefore receives crossed-in qualifiers from **exactly one source court per
rank**, so region sizes stay balanced. The 3rd-place column is exactly the A->D, B->C,
C->B, D->A involution D3 derives independently, so the two defaults agree.

At **2 courts** the four quarters are A1, A2, B1, B2: **1st** goes to a quarter of its
own court's half, **2nd** to a quarter of the partner half, **3rd** to the partner
half's OTHER quarter, **4th** to the pool's own half's other quarter. Where a rank has a
choice of two quarters (the 2nd, and the 1st when its half is empty), take the one with
fewest current occupants, ties by quarter order, which is D3's tiebreak chain reused
unchanged.

**Beyond 4 qualifiers per pool** the same rotation continues over regions in D3's
candidate order. R5's no-two-in-a-quarter guarantee is pigeonhole-impossible from the
5th qualifier onward (a draw has four quarters), so from there R5 degrades to "as late a
reunion as the structure allows" and there is **no additional guarantee**.
`EffectivePoolWinners` (`internal/state/models.go:601`) is unbounded above, so this is
reachable configuration, not a hypothetical.

**Consequence for Phase 1.** The golden file sweeps 1-4 qualifiers, so its 4-qualifier
cases are **RECORDED, not normative**, until Phase 4 implements D5. A Phase 3 diff
against them still proves behaviour preservation; a Phase 4 diff against them proves
nothing until they are regenerated.

### D6. R2 seed placement, generalised

**Rule, covering every court count, and the normative form of R2:**

> Seeds **1 and 3** fall in one half of the draw and seeds **2 and 4** in the other,
> each of the four in a **distinct quarter**, and, subject to that, on **distinct
> courts** and in **distinct pools**.

This preserves the operator's 2026-08-09 decision (semifinals **1 v 3** and **2 v 4**
when the seeds hold) and generalises it. At 4 courts it yields **seed 1 -> court A,
seed 3 -> court B, seed 2 -> court C, seed 4 -> court D**, from which "one seeded pool
per court, each in a different quarter" follows as a **consequence** rather than as a
separate rule. At 6 or more courts a quarter spans more than one region, so the
half/quarter constraint is the operative one and "distinct courts" becomes the tiebreak
that spreads seeds within a quarter.

**This deliberately differs from the conventional seeding convention.** Under both
conventions seeds 1 and 2 are in opposite halves and can only meet in the final, and
seeds 3 and 4 are placed to meet them in the semifinals. What differs is the **pairing**:
the conventional bracket groups 4 with 1 and 3 with 2, giving semifinals 1 v 4 and
2 v 3; the operator chose to group **3 with 1 and 4 with 2**, giving semifinals 1 v 3
and 2 v 4. Anyone comparing our output against a standard seeding table will see this
difference immediately and it is intended, not a defect.

**Fewer than four seeds.** Apply the rule to the ranks that exist, in rank order: seed 1
takes half X quarter 1, seed 2 takes half Y quarter 1, seed 3 takes half X quarter 2,
seed 4 takes half Y quarter 2. With three seeds, quarter (Y, 2) is simply unoccupied by
a seed; with two, seeds 1 and 2 sit in opposite halves; with one, the rule fixes only
its quarter; with **none** it is vacuous and R4 plus R6 place everything. Zero seeds
MUST be a normal, warning-free configuration.

*Reference:* the Female draw, three seeds on three distinct courts (A, B, C), one of
them taking the half's only bye. It corroborates the distinctness constraints but not
the rank-to-court mapping, since the bead records which pools are seeded and not their
ranks.

### D7. R2 when the constraints cannot all be satisfied

**Rule.** D6's constraints are a preference ordered exactly like R6's. In order:

1. **Distinct halves** for the 1+3 / 2+4 grouping.
2. **Distinct quarters.**
3. **Distinct courts.**
4. **Distinct pools.**

Drop **only the deepest unsatisfiable constraint**, and only for the seed that cannot be
placed under it, keeping every constraint that is still satisfiable for the other seeds.
When more than one seed cannot be placed, the **numerically largest seed rank gives way
first**: seed 4 before seed 3, seed 3 before seed 2, and seed 1 never gives way while
any lower-priority seed still can. Seeding exists to protect the top seed most.

**Constraint 4 is not droppable.** Two seeds MUST never share a pool. A configuration
that cannot give all four seeds distinct pools instead **caps the effective seed count
at the pool count and ignores the surplus ranks with a warning** (R2, last bullet). That
is the failure mode for constraint 4, and it is why the ladder in practice degrades
across 1-3 only.

**It MUST NEVER be an error.** Every configuration produces a draw. A competition with
4 seeds, 2 courts and 5 pools split 3/2 is legal and must draw; it simply cannot give
all four seeds their own quarter, so the deepest failing constraint gives way for the
lowest-priority seed and the operator sees a warning describing what was relaxed.

*Rationale:* a seeding rule that can refuse to draw is worse than a seeding rule that
degrades predictably. The operator can always inspect the result and move a seed by
hand; they cannot proceed at all against a hard error, and a live event has no time for
one.

## What changes in the code

Verified at commit `be378413` (main, PR #382). Every line below was opened and matched
to its expected content.

### The draw itself (R2-R7)

| Carries | Location | Phase 4 change |
|---|---|---|
| Flat pool-finisher list, adjacent-pool pairing | `internal/helper/tree.go:173` `GenerateFinals` | Rewritten: emit per-region occupant lists (home 1sts + crossed-in 2nds/3rds) instead of one flat list |
| Recursive midpoint halving | `internal/helper/tree.go:39` `CreateBalancedTree` | Kept for building a single region; the whole-tree build becomes court-first (region subtrees combined) |
| Two-node local placement swap | `internal/helper/tree.go:96` `treeAdjustment` | Replaced by the region-local R6/R7 bye allocation |
| Whole-tree placement pass (engine path) | `internal/helper/tree.go:347` `ApplyPoolAdjustments` | Becomes the single placement pass both paths call (Phase 3 hoists it) |
| Per-page placement (Excel path) | `internal/helper/tree.go:58` `PrintLeafNodes`, inline `treeAdjustment` call at `:65` | Phase 3 makes this a **pure renderer** that no longer mutates the tree |
| Tree to pow2 leaf array | `internal/helper/tree.go:325` `TreeToLeafArray` | Unchanged contract; its input shape changes |
| Contiguous pool-to-court blocks | `internal/helper/helper.go:230` `AssignPoolsToCourts` | Unchanged; it is already the R3 allocation |
| Pool deinterleave for court balance | `internal/helper/helper.go:277` `ReorderPoolsForCourts` | Unchanged; Phase 2a makes the **engine** call it (the CLI already does) |
| Court-aware seed spreading | `internal/helper/seed.go:236` `PoolSeeding` (doc note at `:235`) | Unchanged signature; Phase 2a fixes the engine's argument (see below) |
| Engine passes `PoolSize` where the pool COUNT is expected | `internal/engine/pools.go:29` | Phase 2a. The CLI passes them separately: `cmd/create-pools.go:183` (`numPools`) and `:185` (`activePoolSize`) |
| Engine draw entry point | `internal/engine/pools.go:13` `generatePools` (courts at `:104`) | Phase 2a adds the missing `ReorderPoolsForCourts` |
| Preview/live knockout construction | `internal/engine/bracket.go:84` `generatePoolPreviewBracket` (`GenerateFinals` at `:95`, `ApplyPoolAdjustments` at `:105`) | Consumes the new court-first build |
| Leaf array to persisted bracket | `internal/engine/bracket.go:124` `buildBracketFromLeaves` (court derivation at `:178`) | Court derivation becomes exact under R3/R8 |
| Stale comment: "1st-place finishers get byes" (false today at 3+ qualifiers) | `internal/engine/tiebreaker.go:125` | Boy Scout: correct it when R6 lands |

### Pagination (R8)

| Carries | Location | Phase 4 change |
|---|---|---|
| Page count, power of two only | `internal/helper/tree.go:394` `TreePageLayout` | Return `numCourts x {1,2,4}` |
| Count-based subtree split | `internal/helper/tree.go:301` `SubdivideTree` | Replace with a court-block splitter |
| Page-to-court label | `internal/helper/helper.go:257` `SubtreeCourtIndex` | Remove the then-dead overflow clamp at `:267-269` |
| Page roster overlay slice | `internal/helper/pool_bounds.go:13` `PoolBoundsForSubtree` | Remove the then-dead "last court absorbs the remainder" branch at `:32-36` |
| Render orchestration | `internal/helper/excel_tree.go:118` `RenderKnockoutPages` (`TreePageLayout` at `:119`, `SubdivideTree` at `:123`) | Its documented ordering constraint is **void** once placement moves upstream in Phase 3; update the comment and any test relying on it |
| CLI page entry points | `cmd/create-pools.go:244`, `cmd/create-playoffs.go:175`; `--single-tree` flags at `cmd/create-pools.go:56` and `cmd/create-playoffs.go:48` | `--single-tree` still wins |

### Shiaijo count (R9, Phase 2b)

| Enforcement point | Location |
|---|---|
| API, competition create | `internal/mobileapp/handlers_competition.go:426` |
| API, competition PUT | `internal/mobileapp/handlers_competition.go:757` |
| The validator both call | `internal/mobileapp/handlers_tournament.go:117` `validateCompetitionCourts` (today it only delegates to `validateCourtLabels`) |
| Engine | `internal/engine/court_validation.go:21` `ValidateCourtCount` (today only the idle-court cap) |
| CLI `--courts` | `internal/helper/helper.go:93` `ValidateCourts` (today only the 26-court A-Z cap, `MaxCourts` at `internal/helper/constants.go:57`). **Verified: both commands do route through it**, `cmd/create-pools.go:86` and `cmd/create-playoffs.go:75`, so one change covers both. **But `create-pools` re-clamps AFTER validating**: `:225-226` sets `o.courts = numPools` when courts exceed the pool count, so a legal `--courts 4` with 3 pools silently becomes an illegal **3**. R9 must be re-checked on the clamped value there. `create-playoffs`'s clamp (`:167-168`) is safe: it clamps to `RoundToPowerOf2`, always a power of two |
| UI blocker + legacy warning | `web-mobile/js/admin_competition_settings.jsx:632-659`, the "Assigned shiaijo (courts)" field: label at `:633`, court pills at `:640-644`, the existing hard-cap and suggested-court hints at `:645-658`. Requires a rebuild (`//go:embed`) and a browser screenshot of the blocked state |

### Pool composition inputs (context for R6-2)

| Carries | Location |
|---|---|
| Pool sizing; max mode gives the extra player to the FIRST `rem` pools | `internal/helper/tournament.go:241` `CreatePools`, balancing at `:264-273` |
| Partial-format match generation (N-1 path graph) | `internal/helper/pool_partial.go:17` `CreatePartialPoolMatches`; called from `internal/engine/pools.go:74` and `cmd/create-pools.go:236` |
| `poolFormat` values | `internal/state/models.go:308` (field), `:634-635` (`PoolFormatFull` / `PoolFormatPartial`) |
| Qualifiers per pool | `internal/state/models.go:601` `EffectivePoolWinners` (defaults to 2) |

### Characterization (Phase 1)

Existing coverage is placeholder-only and balanced-only:
`TestBracketCrossPoolMatching` (`internal/helper/tree_test.go:1442`) is restricted to
power-of-2 pool counts, and `TestTreeAdjustmentByeAllocation`
(`internal/helper/tree_test.go:1484`) sweeps only 2 qualifiers per pool.
`TestBracketIdentity_MixedComp` (`internal/engine/bracket_identity_test.go:406`)
compares the engine against a MODEL of Excel written inside the test rather than the
real `RenderKnockoutPages -> SubdivideTree -> PrintLeafNodes` path (Phase 5 replaces
it).

## Migration hazard: `ResolveQualifiedPools`

**This is the one item in bc-draw that can corrupt a live event. Phase 4 MUST NOT ship
without one of the two fixes below.**

### The hazard

For a mixed competition the preview bracket **is** the live in-place knockout: it is
written at draw time with pool-origin placeholder sides
(`internal/engine/bracket.go:84`), and `bracket.Preview` flips to false the first time
a pool resolves (`internal/engine/knockout.go:316`). Resolution replaces those
placeholder strings **in place**, so once a pool completes, the bracket no longer
records which placeholder each slot originally held.

`ResolveQualifiedPools` (`internal/engine/knockout.go:173`) therefore reconstructs that
information on **every call**, at `internal/engine/knockout.go:230-233`:

```go
finals := helper.GenerateFinals(pools, poolWinners)
tree := helper.CreateBalancedTree(finals)
helper.ApplyPoolAdjustments(tree)
template, terr := e.buildBracketFromLeaves(comp, helper.TreeToLeafArray(tree))
```

and then resolves the live bracket **by position** against that template
(`bracket.Rounds[ri][mi]` against `template.Rounds[ri][mi]`). The doc comment's
justification is that pools and `PoolWinners` are fixed after the draw, so the
template's shape is identical to the running bracket's. That justification silently
assumes **the algorithm is also fixed**. If the placement algorithm changes between a
competition's draw and its pool completion, the recomputed template no longer matches
the persisted bracket, and qualifiers are written into the **wrong slots** of a live
knockout. Nothing detects this: the structural-mismatch guards at `:246` and `:250`
only break on differing round/match COUNTS, which a placement change does not alter.

The trigger is not "operator upgraded mid-match". It is "operator upgraded between the
draw and the end of the pool phase", which for a two-day event is an ordinary thing to
do.

### Requirement

Phase 4 MUST ship with either (a) an algorithm version stamped into `bracket.json` at
draw time, or (b) the placeholder template persisted alongside the bracket.

### Recommendation: (b), persist the template, as per-match fields

`state.Bracket` (`internal/state/models.go:1054`) carries only `Rounds`, `Preview` and
`ThirdPlaceMatch`; `state.BracketMatch` (`:978`) has no record of its original
placeholder. Add additive, `omitempty` per-match fields, written by
`buildBracketFromLeaves` when the leaves are pool-origin placeholders:

```go
PlaceholderA      string `json:"placeholderA,omitempty"`
PlaceholderB      string `json:"placeholderB,omitempty"`
PlaceholderWinner string `json:"placeholderWinner,omitempty"`
```

`ResolveQualifiedPools` then **deletes** the recompute at `:230-233` and reads
`m.PlaceholderA/B/Winner` in place of `tpl.SideA/SideB/Winner`.

**Why per-match rather than one sibling template object.** `ThirdPlaceMatch` is the
precedent for a sibling field, but a sibling positional array would re-introduce
exactly the positional-alignment assumption this fix exists to remove: it would still
be correct only as long as nothing renumbers or reorders matches. A placeholder that
rides ON its match cannot drift out of alignment with it.

**Why (b) over (a).** A version stamp keeps the hazard alive and pays for it forever:
it requires a **frozen, still-callable, still-tested copy** of the entire v1 draw
(`GenerateFinals` + `CreateBalancedTree` + `treeAdjustment`/`ApplyPoolAdjustments` +
`buildBracketFromLeaves`'s ID and court assignment), plus a new frozen copy for every
future placement change, plus a dispatch branch that someone must remember to add. It
also protects only the changes someone thought to version. Persisting the template
removes the dependency entirely: the resolver stops being a function of the draw
algorithm at all, for this transition and every future one. It also removes a full
bracket rebuild from a path that runs on **every pool-match completion**.

**Legacy brackets.** The absence of the persisted placeholders IS the version marker,
so no separate version field is needed: a bracket that still holds pool placeholders in
its live sides and carries no `placeholderA/B` was drawn pre-Phase-4. Handle it one of
two ways, decided when Phase 4 is designed:

- **Preferred:** keep ONE frozen `generateFinalsV1` template builder for a single
  release, used only when the persisted placeholders are absent, and backfill-and-save
  the fields on first load. Bounded, and the frozen copy is deleted the release after.
- **Acceptable fallback:** refuse to resolve such a bracket with an actionable error
  telling the operator to discard the draw (`DELETE /api/competitions/:id/draw`) and
  regenerate. Acceptable only because bc-draw already requires Phase 4 to land
  **between events**; it is a bad outcome for anyone who upgrades mid-event.

Note that Phase 3 is behaviour-preserving (zero golden-file diff), so this hazard does
**not** apply to it. It applies to Phase 4 alone, which is why bc-draw groups every
shape-affecting rule into that single phase: the migration is paid exactly once.

## Extrapolations the operator should confirm

Nothing here is open: every rule in this document is decided and implementable. These
five defaults are the ones that go **beyond** what the three EKC sheets show, so they
are the ones where a later operator ruling is most likely. Each line states what would
change if the operator rules differently.

- **D3, 3rd-place placement.** EKC never shows 3 qualifiers; if the operator prefers a
  different quarter for the 3rd, the 4-court involution (A->D, B->C, C->B, D->A) and the
  2-court partner-half rule change, and D5's rotation table changes with them.
- **D4, greedy region shape.** If the operator wants the pad-to-`NextPow2`-and-spread
  construction instead, a `q = 5` region prints 1 round-1 match and 3 named byes rather
  than 2 matches and 1 bye, and R6 then allocates three byes per odd region instead of
  one.
- **D5, 4th and later qualifiers.** EKC never shows more than 2 qualifiers; a different
  rotation changes only draws with `poolWinners >= 3`, and would require the Phase 1
  golden file's 3- and 4-qualifier cases to be regenerated rather than merely rerecorded.
- **D6, 1+3 / 2+4 seed grouping.** Reverting to the conventional 1+4 / 2+3 grouping
  changes the semifinal pairings from 1 v 3 and 2 v 4 to 1 v 4 and 2 v 3, and swaps
  which court seeds 3 and 4 land on; nothing else in the draw moves.
- **D7, constraint-drop order.** If the operator would rather relax "distinct halves"
  before "distinct quarters", or protect the lowest seed instead of the highest, only
  under-constrained configurations change; every configuration that can satisfy D6 in
  full is unaffected.

## Out of scope

- **Deriving seeds from previous-edition results.** The operator supplies the 4 seeds
  and their order (R1).
- **Tied seed ranks (joint 3rd).** `domain.ValidateSeedAssignments`
  (`internal/domain/seed.go:41`) and `helper.ApplySeeds` (`internal/helper/seed.go:420`,
  rejection at `:444`) both reject duplicate ranks; with the operator choosing the
  order this stays as it is.
- **Playoffs-only (non-pool) brackets**, which use `StandardSeeding` and are unaffected
  by R2-R7. R8 and R9 **do** apply to them and must be checked, since `TreePageLayout`,
  `SubdivideTree` and the court allocation are shared.
- **Pool Draw and Pool Matches sheet layout.** Pool composition logic is unchanged;
  Phase 2a realigns the engine to the CLI's existing convention rather than inventing a
  new one.
