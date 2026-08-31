package helper

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strings"
)

// The region-aware pool distributor (bc-dojo). Built BESIDE the old
// fill+repair pipeline in Phase 2 so the Phase 3 decision-gate scorecard
// (pool_distribution_gate_test.go) had something to measure it against;
// Phase 4 is the swap this file now IS: BuildPoolPhase and
// BuildPoolPhaseFillBracket (tournament.go) both delegate here, so this is
// production code on both cutting paths, not a shadow implementation.
//
// bc-dojo-least-conflicted-pool (this file's own follow-up bead)
// SUPERSEDED Phase 4's own step 3 + repair (a footprint-sorted greedy fill,
// scored against ALL of a pool's qualifiers, plus a pairwise-exchange
// repair with a three-way-rotation fallback) with a two-stage pipeline
// that is measured to DOMINATE it:
//
//  1. assignUnseededByDojoTree (bottom of this file) places every unseeded
//     player, IN ROSTER ORDER (no re-sort: the roster arrives pre-shuffled
//     by design, sorting here would fight that), by descending a tree of
//     halves/quarters/.../pools built over each pool's WINNER (rank-1)
//     qualifier leaf ONLY -- it never looks at a pool's runner-up/
//     crossed-in qualifiers, even though poolWinners>=2 sends a SECOND
//     qualifier that CROSSES to a different, non-mirrored region of the
//     bracket (R4a/b/c, draw.go). At each node: fewer of this dojo per
//     real pool wins (dojoNode's own doc comment covers the per-pool
//     normalisation and the roomPools breadth tie-break this needed to
//     reach zero per-pool-optimum failures); a dojo nobody has placed yet
//     bypasses the tree entirely (leastConflictedPool), which is what
//     keeps the unique-dojo round-robin identity contract intact.
//  2. improveDojoMeetings (below) then runs a PAIRWISE-ONLY exchange pass
//     -- no three-way rotation -- scored on the WINNER-PATH metric alone
//     (poolPairRounds fed winner-only slot lists, i.e. slots[0] per pool,
//     the same pre-reorder space the descent itself uses). This is what
//     closes the one gap the descent alone could not: on a MULTI-dojo
//     roster the dojos the descent places early can still box a later
//     dojo into pools whose own qualifiers meet in round 1, because the
//     descent commits each player the moment it places them and cannot
//     see a later dojo's needs. The exchange pass fixes exactly that class
//     of residual, and nothing else -- it is a no-op on an all-unique-dojo
//     roster (no dojo ever spans >=2 pools, so curR1 is 0 from the first
//     objective() call) and on a single-dojo config already at the
//     brute-force ceiling (TestTreeAwareGateScorecard's 180/180 sweep).
//
// GATE METRIC, operator ruling: a same-dojo collision arising from
// runner-up CROSSING is accepted chance ("that's life"), not a defect
// either stage owes a fix for -- holistic (all-qualifier) prevention was
// explicitly ruled out. The decision gate is the WINNER-PATH metric
// (earliestDojoWinnerMeetingRound, pool_distribution_gate_test.go: a
// dojo's earliest meeting counting only pools' winner leaves), not
// earliestDojoMeetingRound's all-qualifier number.
//
// MEASURED (the multi-dojo on/off sweep the original repair was justified
// by, 1596 configs, winner-path metric, lexicographic round-1-count then
// sum): descent+exchange vs the OLD all-qualifier-scored fill+repair
// pipeline it replaced -- 402 better, 1194 same, 0 worse; vs the descent
// ALONE (no exchange) -- 12 better, 1584 same, 0 worse, and the exchange
// pass fired in EXACTLY those 12 configs (including the shape that
// motivated the original repair's three-way rotation: 5 pools of 3, four
// 3-member dojos, winners 2 -- see TestDojoTreeDescent_PoolWinnersTwoRegression,
// which now pins the exchange CLOSING that gap rather than leaving it
// open). Dominates both alternatives; wired into buildPoolPhaseTreeAwareCore
// below. See assignUnseededByDojoTree's and improveDojoMeetings' own doc
// comments for the placement/repair mechanism and dojoNode's for the
// per-pool-optimum and single-pool-funnelling defects found and closed
// while building the descent.
//
// Phase 4 also made the distributor MODE-AWARE: production runs three
// different knockout builders depending on
// state.Competition.ExtraQualifiers (standard/uniform, "larger-pools",
// "fill-bracket"), each cutting a differently-shaped tree, and a
// distributor that always scored against the standard tree would optimise
// for a bracket the competition does not actually draw whenever a
// non-standard mode is in play. treeAwareQualifierSlots dispatches on a
// qualifierMode value to the matching skeleton builder
// (poolQualifierPaths / poolQualifierPathsPerPool /
// poolQualifierPathsFillBracket, draw_qualifier_paths.go) so the tree
// scored against is always the tree that mode actually cuts. The descent
// and the exchange pass both only ever read a pool's WINNER leaf out of
// whichever tree that dispatch selects, so mode-awareness is unchanged by
// this bead.

// BuildPoolPhaseTreeAware is BuildPoolPhase's region-aware sibling. It
// returns the same (pools, drawCourts, error) shape, for the same reasons
// (see BuildPoolPhase's own doc comment on why the steps are ordered), but
// replaces the fill-then-repair body with a single forward pass that can see
// the whole knockout tree before it places anyone:
//
//  1. Pool count and target sizes come from poolTargetSizes -- the exact
//     arithmetic CreatePools uses, reused rather than copied.
//  2. Seeds are placed FIRST, by placeSeedIndices -- the exact arithmetic
//     PoolSeeding uses for its own seeded pass, extracted so the two can
//     never drift (pinned byte-identical by
//     TestSeedPlacementEquality_OldVsTreeAware). Each seed's dojo is folded
//     into that pool's occupancy immediately, which is what "recording
//     their dojo" means operationally: the unseeded pass below sees seeded
//     occupants exactly as it sees earlier unseeded ones.
//  3. Every unseeded player is then placed in ONE PASS, IN ROSTER ORDER (no
//     re-sort -- the roster arrives pre-shuffled by design, sorting here
//     would fight that), by assignUnseededByDojoTree descending a tree of
//     halves/quarters/.../pools built over each pool's WINNER (rank-1)
//     qualifier leaf: at each node, fewer of this dojo per real pool wins,
//     ties broken by room then by leastConflictedPool's existing rule. See
//     assignUnseededByDojoTree's own doc comment for the placement
//     mechanism.
//  4. improveDojoMeetings then runs a PAIRWISE-ONLY exchange pass over the
//     result, scored on the winner-path metric alone: the descent commits
//     each player the moment it places them and cannot see a later dojo's
//     needs, so on a multi-dojo roster an early dojo can still box a later
//     one into pools whose winners meet in round 1. The exchange pass
//     closes exactly that residual (accepted only when it strictly
//     improves, never worsens any dojo's earliest winner-path meeting);
//     it is a no-op on an all-unique-dojo roster and on a single-dojo
//     roster already at the brute-force ceiling. See improveDojoMeetings'
//     own doc comment.
//  5. ReorderPoolsForCourts runs last, exactly as BuildPoolPhase's does.
//
// numCourts is used ONLY in step 1 (to derive drawCourts, the modulus seed
// placement and ReorderPoolsForCourts both need) and step 5 (the actual
// court assignment). Steps 2 through 4 -- the whole of WHO goes WHERE --
// never read a court index: the knockout tree's region/crossing structure
// is the same shape whatever the shiaijo count (poolQualifierPaths, Phase
// 1), so distribution is computed once and courts are laid onto the result
// afterwards, never the other way round.
//
// poolWinners is the one addition to BuildPoolPhase's own parameter list:
// the tree shape poolQualifierPaths reads depends on how many qualifiers
// each pool sends up (a pool crossing 2 qualifiers routes its 2nd to the
// partner block; crossing 1 crosses nothing), so the scorer needs it up
// front. BuildPoolPhase's signature and behaviour are untouched by this
// function's existence.
//
// This is always STANDARD mode (qualifierModeStandard): a caller that knows
// its competition's real extra-qualifiers setting must call
// BuildPoolPhaseTreeAwareWithMode instead, or the distributor scores every
// candidate against the wrong knockout tree whenever that setting is not
// the default. Kept as its own entry point (rather than folded into the
// mode-aware one with a fixed argument) because every one of this file's
// and pool_distribution_gate_test.go's existing tests call it by this exact
// 5-argument signature, pinned before mode-awareness existed.
func BuildPoolPhaseTreeAware(players []Player, poolSize int, isMax bool, numCourts int, poolWinners int) ([]Pool, int, error) {
	numPools, targetSizes, err := poolTargetSizes(len(players), poolSize, isMax)
	if err != nil {
		return nil, 0, err
	}
	return buildPoolPhaseTreeAwareCore(players, numPools, targetSizes, numCourts, poolWinners, qualifierMode{ExtraQualifiers: qualifierModeStandard})
}

// defaultPoolWinners is the pool-winners count BuildPoolPhase falls back to
// when it has no real competition to read one from: the documented default
// both state.Competition.EffectivePoolWinners() and
// engine.ResolveQualifiedPools use. helper/estimate.go's EstimateMatchCounts
// falls back to this same const for the same reason (a synthetic estimate
// has no real competition either).
const defaultPoolWinners = 2

// qualifierMode selects which of production's three knockout-draw skeleton
// builders treeAwareQualifierSlots scores candidate placements against
// (bc-dojo Phase 4): the shape of the tree the distributor sees must be the
// shape production actually cuts for that mode, or the region metric it
// optimises does not correspond to the real draw.
//
// ExtraQualifiers mirrors state.Competition.ExtraQualifiers's three values
// ("" / "larger-pools" / "fill-bracket") as a plain string rather than an
// imported type: internal/state imports internal/helper (Competition.
// QualifiersForPool takes a helper.Pool), so helper importing state back
// for this one type would cycle. The three qualifierMode* constants below
// are declared with the IDENTICAL string values on purpose, so a caller
// holding a state.Competition can pass comp.ExtraQualifiers straight
// through with no conversion and no risk of the two vocabularies drifting
// apart silently (a typo'd fourth string here is simply treated as
// standard, exactly as ValidateExtraQualifiers would have already rejected
// it before this seam is ever reached in production).
type qualifierMode struct {
	ExtraQualifiers string
	// MinPoolSize is the minimum per-pool size (state.Competition.PoolSize
	// under minimum-players-per-pool sizing) both non-standard modes key
	// their "oversized"/"has a 2nd to draft" tests off of. Unused in
	// standard mode.
	MinPoolSize int
	// SeedPoolIndex maps a seed's RANK to the pool index it lands in
	// (PRE-reorder space, the same space targetSizes is in), as already
	// computed by this call's own seed-placement step. Only fill-bracket
	// mode reads it (SelectFillBracketDrafts drafts seeded pools in seed
	// order); the other two modes ignore it.
	SeedPoolIndex map[int]int
}

// qualifierMode.ExtraQualifiers values, mirroring
// state.Competition.ExtraQualifiers* -- see qualifierMode's own doc comment
// for why the values are duplicated here rather than imported.
const (
	qualifierModeStandard    = ""
	qualifierModeLargerPools = "larger-pools"
	qualifierModeFillBracket = "fill-bracket"
)

// BuildPoolPhaseTreeAwareWithMode is BuildPoolPhaseTreeAware's mode-aware
// sibling (bc-dojo Phase 4): the real production entry point for a caller
// that knows its competition's actual pool-winners count and
// extra-qualifiers setting (internal/engine/pools.go,
// cmd/create-pools.go), as opposed to BuildPoolPhaseTreeAware's and
// BuildPoolPhase's own fixed defaultPoolWinners/standard-mode behaviour.
//
// The minimum-mode pool size qualifierMode.MinPoolSize needs (state.
// Competition.PoolSize when extraQualifiers is larger-pools or
// fill-bracket, ignored for standard mode) is fully derivable from this
// function's own poolSize/isMax parameters -- it is poolSize under
// minimum-mode sizing (isMax false) and 0 (irrelevant) under max-mode
// sizing, exactly poolTargetSizes' own isMax contract -- so it is computed
// here rather than taken as a caller-supplied parameter every caller would
// otherwise have to hand-derive identically. isMax must be false whenever
// extraQualifiers is non-standard (state.ValidateExtraQualifiers enforces
// this on every real caller before formation runs); this function does not
// re-validate it, matching poolTargetSizes' own trust-the-caller contract.
func BuildPoolPhaseTreeAwareWithMode(players []Player, poolSize int, isMax bool, numCourts int, poolWinners int, extraQualifiers string) ([]Pool, int, error) {
	numPools, targetSizes, err := poolTargetSizes(len(players), poolSize, isMax)
	if err != nil {
		return nil, 0, err
	}
	minPoolSize := 0
	if !isMax {
		minPoolSize = poolSize
	}
	return buildPoolPhaseTreeAwareCore(players, numPools, targetSizes, numCourts, poolWinners, qualifierMode{ExtraQualifiers: extraQualifiers, MinPoolSize: minPoolSize})
}

// BuildPoolPhaseFillBracketTreeAware is BuildPoolPhaseFillBracket's
// region-aware body (bc-dojo Phase 4), mirroring BuildPoolPhaseTreeAware's
// relationship to BuildPoolPhase: the pool COUNT and BASE target sizes come
// from this function's own FillBracketPoolCount formation objective (a
// uniform minSize row, exactly what CreatePoolsForCount cuts to before its
// own remainder spreads), fill-bracket's poolWinners is always 1
// (state.ValidateExtraQualifiers' own gate), and the mode is
// qualifierModeFillBracket throughout -- everything else (seed placement,
// the remainder spread, the one-pass distribution, ReorderPoolsForCourts
// last) is the shared core BuildPoolPhaseTreeAware itself now uses.
func BuildPoolPhaseFillBracketTreeAware(players []Player, minSize int, numCourts int) ([]Pool, int, error) {
	// Same rule 4 supply-side read as BuildPoolPhaseFillBracket's own
	// pre-Phase-4 body: FillBracketPoolCount needs the roster's seed RANKS,
	// not just a count, to know which pool counts a gapped seed set can
	// actually supply drafts for.
	seeded, _ := partitionSeeded(players)
	seedRanks := make([]int, len(seeded))
	for i, p := range seeded {
		seedRanks[i] = p.Seed
	}
	numPools, _, err := FillBracketPoolCount(len(players), minSize, seedRanks)
	if err != nil {
		return nil, 0, err
	}
	base := make([]int, numPools)
	for i := range base {
		base[i] = minSize
	}
	return buildPoolPhaseTreeAwareCore(players, numPools, base, numCourts, 1, qualifierMode{ExtraQualifiers: qualifierModeFillBracket, MinPoolSize: minSize})
}

// ErrBlankDojo is the sentinel identifying a draw refused because the
// roster contains at least one player with an empty Dojo
// (bc-dojo-least-conflicted-pool FIX 1). Match it with errors.Is; the
// returned error's message additionally names every offending player so the
// operator knows exactly which row to repair.
//
// Every downstream signal in this file is defined only for non-blank
// dojos: recordDojoOccupancy is guarded on `p.Dojo != ""` (so a blank-dojo
// player consumes a real pool seat via the leastConflictedPool bypass
// without ever updating the tree's capacity accounting -- the descent's
// ONLY fullness signal -- which lets a later descent overfill a pool past
// its target size) and improveDojoMeetings' footprint/spread/meeting
// objective would otherwise count Dojo=="" as a phantom dojo that drives
// and vetoes real swaps. Per the operator's absolute "dojo must never be
// empty" rule (NO FALLBACKS), a blank-dojo roster is refused outright
// rather than tolerated by a new blank-skipping accounting path.
//
// Participant LOADING stays blank-tolerant on purpose (state.LoadParticipants
// and its CSV parser accept a blank dojo, so a legacy or hand-edited roster
// can still be loaded and the offending row repaired in the UI/CSV) -- only
// the DRAW itself refuses, at this one shared pre-flight.
var ErrBlankDojo = errors.New("cannot draw pools: every competitor must have a dojo")

// validateNoBlankDojo is the one pre-flight check shared by
// BuildPoolPhaseTreeAware, BuildPoolPhaseTreeAwareWithMode and
// BuildPoolPhaseFillBracketTreeAware (all three funnel through
// buildPoolPhaseTreeAwareCore, so this is called exactly once per draw
// attempt regardless of which entry point the caller used). Returns nil
// when every player has a non-blank Dojo.
func validateNoBlankDojo(players []Player) error {
	var names []string
	for _, p := range players {
		if p.Dojo == "" {
			names = append(names, p.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrBlankDojo, strings.Join(names, ", "))
}

// buildPoolPhaseTreeAwareCore is BuildPoolPhaseTreeAware's and
// BuildPoolPhaseFillBracketTreeAware's shared body (bc-dojo Phase 4): given
// the pool COUNT and BASE target sizes already resolved by the caller's own
// formation objective, it places seeds, spreads whatever remainder the base
// sizes leave (realTargetSizes -- a no-op for max-mode or an
// exact-multiple roster, the only shapes every sweep in this package
// exercised before this was added), distributes the unseeded in one pass
// against the mode's own knockout skeleton, and reorders for courts.
func buildPoolPhaseTreeAwareCore(players []Player, numPools int, baseTargetSizes []int, numCourts int, poolWinners int, mode qualifierMode) ([]Pool, int, error) {
	// FIX 1 (bc-dojo-least-conflicted-pool): refuse a blank-dojo roster up
	// front, before any seed/pool arithmetic runs, rather than let one
	// silently corrupt the tree-aware capacity accounting below. See
	// ErrBlankDojo's own doc comment for the two mechanisms this closes.
	if err := validateNoBlankDojo(players); err != nil {
		return nil, 0, err
	}

	drawCourts := EffectiveDrawCourts(numPools, numCourts)

	pools := make([]Pool, numPools)
	for i := range pools {
		pools[i].PoolName = poolPositionName(i)
	}

	targetSizes := realTargetSizes(baseTargetSizes, len(players))

	seeded, unseeded := partitionSeeded(players)

	// Step 2: seeds first, byte-identical to today's pipeline (see
	// placeSeedIndices' own doc comment for how that is verified).
	seedIdx := placeSeedIndices(seeded, numPools, clampCourts(drawCourts), len(players))
	// SeedPoolIndex is only ever consulted by fill-bracket mode
	// (poolQualifierPathsFillBracket), but it is cheap enough (at most one
	// entry per seed) to build unconditionally rather than threading a
	// mode check through this loop too.
	seedPoolIdx := make(map[int]int, len(seeded))
	for si, idx := range seedIdx {
		// idx is only ever negative when placeSeedIndices ran out of roster
		// slots to place a seed in at all (more seeds than players); numPools
		// is guaranteed > 0 here whenever idx >= 0, since placeSeedIndices
		// returns every index as -1 up front when numPools <= 0.
		if idx < 0 {
			continue
		}
		p := seeded[si]
		poolIdx := idx % numPools
		p.PoolPosition = int64(len(pools[poolIdx].Players) + 1)
		pools[poolIdx].Players = append(pools[poolIdx].Players, p)
		if p.Seed > 0 {
			seedPoolIdx[p.Seed] = poolIdx
		}
	}
	mode.SeedPoolIndex = seedPoolIdx

	// Step 3: one pass over the unseeded, IN ROSTER ORDER (the caller's
	// list is already shuffled upstream -- CLI shuffle / the mobile app's
	// "Shuffle unseeded" control -- so re-sorting here would fight that
	// decision rather than help it), by descending the dojo tree built
	// over the mode's own knockout skeleton. See assignUnseededByDojoTree's
	// own doc comment for the placement mechanism.
	qualifierSlots := treeAwareQualifierSlots(targetSizes, poolWinners, drawCourts, mode)
	if err := assignUnseededByDojoTree(pools, targetSizes, unseeded, qualifierSlots); err != nil {
		return nil, 0, err
	}

	// Step 4: winner-path pairwise-exchange repair. The descent above is
	// greedy per player, and on a MULTI-dojo roster the dojos placed early
	// can still box a later dojo into pools whose own qualifiers meet in
	// round 1, because the descent commits each player the moment it
	// places them and cannot see a later dojo's needs -- measured before
	// this step existed (this bead's own sweep): 12 of 1596 multi-dojo
	// configs, all at poolWinners>=2, reached a worse winner-path result
	// through the descent alone than through repair. The operator's rule
	// is absolute -- a first match against a dojo-mate must not happen
	// where any assignment avoids it -- so the finished pools get a
	// repair loop: unseeded-for-unseeded exchanges, accepted only when
	// they strictly improve (fewer dojos meeting in round 1 BY WINNER
	// PATH, then a later winner-path meeting-sum), never worsen ANY
	// dojo's earliest winner-path meeting, never move a seed and never
	// break a dojo's per-pool optimum. Single-dojo rosters are already at
	// their brute-force ceiling (the Phase 3 gate pins 180/180) and an
	// all-unique-dojo roster has no dojo spanning >=2 pools at all, so
	// this loop is a no-op in both cases, which is what keeps the gate
	// numbers and the unique-dojo identity contract exactly where they
	// were.
	improveDojoMeetings(pools, targetSizes, qualifierSlots, players)

	return ReorderPoolsForCourts(pools, drawCourts), drawCourts, nil
}

// earliestDojoMeeting returns the earliest knockout round two of dojo's pools
// are drawn to meet, or math.MaxInt when dojo occupies fewer than two pools.
// pairRound is the precomputed pool-pair meeting matrix (poolPairRounds):
// the repair loop evaluates hundreds of thousands of candidate exchanges,
// and recomputing each pool pair's slot pairing per candidate multiplied the
// whole test suite's runtime by three before the matrix existed.
func earliestDojoMeeting(pools []Pool, pairRound [][]int, dojo string) int {
	// Collect dojo's occupied-pool indices ONCE (O(P*poolSize)) instead of
	// rediscovering pool j's membership inside the pair loop for every i
	// (the old code called countDojoInPool(pools[j], ...) up to once per
	// (i, j) pair, i.e. O(P^2*poolSize)). The pair loop below then only
	// ever visits the k <= P pools that actually hold the dojo, via direct
	// pairRound lookups (k^2), which is where the real savings are: k is
	// typically small even when P (pool count) is large.
	occupied := make([]int, 0, len(pools))
	for i := range pools {
		if i >= len(pairRound) {
			continue
		}
		if countDojoInPool(pools[i], dojo) > 0 {
			occupied = append(occupied, i)
		}
	}
	earliest := math.MaxInt
	for oi := 0; oi < len(occupied); oi++ {
		for oj := oi + 1; oj < len(occupied); oj++ {
			i, j := occupied[oi], occupied[oj]
			if r := pairRound[i][j]; r < earliest {
				earliest = r
			}
		}
	}
	return earliest
}

// cachedDojoMeeting is earliestDojoMeeting memoized by dojo name in cache.
// Callers (improveDojoMeetings) hold cache valid only across a span of calls
// during which pools' CONTENTS are known not to change -- see the call
// site's own comment for why that span is exactly "one pass" there. Getting
// that invariant wrong would silently serve a stale round for a dojo whose
// pool membership already moved, so this stays a private helper rather than
// something a new caller could reach for without re-deriving the same
// guarantee.
func cachedDojoMeeting(pools []Pool, pairRound [][]int, dojo string, cache map[string]int) int {
	if v, ok := cache[dojo]; ok {
		return v
	}
	v := earliestDojoMeeting(pools, pairRound, dojo)
	cache[dojo] = v
	return v
}

// poolPairRounds precomputes earliestPairing for every pool pair.
func poolPairRounds(qualifierSlots [][]int) [][]int {
	n := len(qualifierSlots)
	m := make([][]int, n)
	for i := range m {
		m[i] = make([]int, n)
		for j := range m[i] {
			if i == j {
				m[i][j] = math.MaxInt
				continue
			}
			m[i][j] = earliestPairing(qualifierSlots[i], qualifierSlots[j])
		}
	}
	return m
}

// earliestPairing is the WORST (earliest) dojoMeetRound over every pair of
// slots two pools could send to the knockout: the pessimistic meeting round
// two same-dojo qualifiers from these pools could be forced into, since
// either pool's ACTUAL finisher for a given rank is not known yet. Fed
// winner-only single-element slot lists by improveDojoMeetings (see that
// function's own doc comment for why), this degenerates to exactly
// dojoMeetRound(winnerA, winnerB) -- still correct, just with nothing left
// to be pessimistic ABOUT once there is only one slot per side.
func earliestPairing(a, b []int) int {
	worst := math.MaxInt
	for _, sa := range a {
		for _, sb := range b {
			if r := dojoMeetRound(sa, sb); r < worst {
				worst = r
			}
		}
	}
	return worst
}

// improveDojoMeetings is the multi-dojo repair loop described at its call
// site: a WINNER-PATH-ONLY pairwise exchange pass over the pools the
// descent (assignUnseededByDojoTree) already placed. pairRound is built
// from qualifierSlots reduced to each pool's WINNER (rank-1) leaf alone
// (winnerSlots below) rather than the full qualifierSlots a pool with
// poolWinners>1 also carries runner-up/crossed-in leaves for -- the same
// metric the descent itself optimises, and the one the operator ruled the
// ship/keep decision on (a same-dojo collision through runner-up CROSSING
// is accepted chance, not a defect either stage owes a fix for). Objective,
// lexicographic and strictly decreasing on every accepted exchange (so the
// loop terminates): first the number of multi-pool dojos whose earliest
// WINNER-PATH meeting is round 1, then the negated sum of finite
// WINNER-PATH meeting rounds. An exchange moves one unseeded player of a
// round-1 dojo out of one of its pools in return for an unseeded player of
// a different dojo, and is legal only when afterwards (a) neither
// exchanged dojo's earliest WINNER-PATH meeting got EARLIER, (b) neither
// pool holds more of either dojo than the dojo's per-pool optimum
// ceil(total/numPools) allows, so the spread invariants the gate pins
// survive by construction. NO three-way rotation: a pairwise stall simply
// stops the loop (see the file-level doc comment for the measured
// dominance this simplification still achieves over the old
// pairwise+rotation repair it replaces).
func improveDojoMeetings(pools []Pool, targetSizes []int, qualifierSlots [][]int, roster []Player) {
	winnerSlots := make([][]int, len(qualifierSlots))
	for i, s := range qualifierSlots {
		if len(s) > 0 {
			winnerSlots[i] = s[:1]
		}
	}
	pairRound := poolPairRounds(winnerSlots)
	// allQualPairRound is the SAME matrix, but over the FULL qualifierSlots
	// (winner and runner-up/crossed-in leaves both) -- tier (d)'s own data,
	// and the operator's best-effort answer to "is there also a best
	// effort when there are 2 qualifiers from the pool?": yes, as a final
	// tie-break, never a trade against the winner-path tiers ahead of it.
	allQualPairRound := poolPairRounds(qualifierSlots)
	footprint := make(map[string]int, len(roster))
	for _, p := range roster {
		footprint[p.Dojo]++
	}
	numPools := len(pools)
	optimum := func(dojo string) int {
		return (footprint[dojo] + numPools - 1) / numPools
	}

	// excessOf is dojo's contribution to tier (a) from a single pool
	// holding `count` of it: how far over its per-pool optimum that one
	// pool sits, floored at 0 (a pool at or under optimum contributes
	// nothing -- only OVER-cap pools count, never under).
	excessOf := func(dojo string, count int) int {
		if over := count - optimum(dojo); over > 0 {
			return over
		}
		return 0
	}

	// totalExcess is tier (a): sum over every (pool, dojo) pair of that
	// pool's excess for that dojo. This is the operator's spread tier --
	// see this function's own doc comment for why it must lead the other
	// two tiers, and dojoNode's doc comment (pool_distribution_tree_aware.go)
	// for the descent-side guard this backstops for the one placement
	// order the descent's own forward-only guard cannot reach (the
	// roster's literal last remaining seat).
	totalExcess := func() int {
		total := 0
		counts := map[string]int{}
		for i := range pools {
			for k := range counts {
				delete(counts, k)
			}
			for _, pl := range pools[i].Players {
				counts[pl.Dojo]++
			}
			for d, c := range counts {
				total += excessOf(d, c)
			}
		}
		return total
	}

	objective := func() (excess, roundOnes, negSum, allQualNegSum int) {
		excess = totalExcess()
		seen := map[string]bool{}
		for i := range pools {
			for _, pl := range pools[i].Players {
				if seen[pl.Dojo] {
					continue
				}
				seen[pl.Dojo] = true
				if m := earliestDojoMeeting(pools, pairRound, pl.Dojo); m != math.MaxInt {
					if m <= 1 {
						roundOnes++
					}
					negSum -= m
				}
				if m := earliestDojoMeeting(pools, allQualPairRound, pl.Dojo); m != math.MaxInt {
					allQualNegSum -= m
				}
			}
		}
		return excess, roundOnes, negSum, allQualNegSum
	}

	// better is FOUR-tier lexicographic (operator ruling): tier (a) total
	// cap excess leads, so a swap that reduces excess is preferred over
	// one that does not, EVEN WHEN IT IS MEETING-NEUTRAL (both round-1
	// count and sum unchanged) -- the descent's own forward-only cap guard
	// (assignUnseededByDojoTree's dojoOptimum/poolUnderDojoCap) cannot see
	// past the roster's own placement order, and the shape it cannot
	// reach (a dojo occupying exactly half the roster, zero slack
	// anywhere, adversarial interleave) is fixable by AFTER-THE-FACT
	// exchange even though no candidate pool was ever free during
	// placement. Tiers (b) round-1 count and (c) meeting sum are WINNER
	// PATH, unchanged from before tier (a) existed. Tier (d) is the
	// all-qualifier best-effort (same operator ruling as
	// chooseDojoTreePool's own tie-break): once (a)-(c) are all tied, a
	// swap that reduces the ALL-QUALIFIER meeting sum -- i.e. pushes a
	// runner-up/crossed-in collision later -- is still preferred over one
	// that does not, so a meeting-neutral-and-spread-neutral swap is not
	// wasted when it could reduce a crossing collision instead. It never
	// overrides (a)-(c): a swap tier (c) already prefers is taken
	// regardless of what tier (d) thinks of it.
	better := func(ea, r1a, nsa, aqa, eb, r1b, nsb, aqb int) bool {
		if ea != eb {
			return ea < eb
		}
		if r1a != r1b {
			return r1a < r1b
		}
		if nsa != nsb {
			return nsa < nsb
		}
		return aqa < aqb
	}

	// Per-pass memoization for earliestDojoMeeting, keyed by dojo name --
	// see the cache-clearing comment inside the pass loop for why a single
	// map reused (and cleared) across passes is safe here.
	winnerMeetCache := make(map[string]int, len(roster))
	allQualMeetCache := make(map[string]int, len(roster))

	// Bounded belt-and-braces cap; the lexicographic strict improvement is
	// the real termination argument. Runs to a FULL fixpoint of the
	// lexicographic objective -- excess first, then round-1 count, then
	// meeting sum, each tier searched even when the ones ahead of it are
	// already at floor: a dojo whose earliest meeting is round 2 while a
	// strictly-later placement exists is still an improving move on tier
	// (c), and a dojo sitting one-over-cap in a pool that has room
	// elsewhere is still an improving move on tier (a), regardless of
	// what tiers (b)/(c) do. Measured: tier (c) alone (this loop before
	// tier (a) existed) is what closes the 12-of-1596 configs (all
	// winners=2, all already at round-1 count 0) where the descent alone
	// landed one dojo's winner-path sum short of what a single exchange
	// could still buy it; tier (a) is what returns
	// dojo/deep-oversubscription (draw_shapes_golden_test.go) to
	// MaxSameDojoCount 2.
	for pass := 0; pass < len(roster)*numPools+1; pass++ {
		curExc, curR1, curNS, curAQ := objective()
		// winnerMeetCache/allQualMeetCache memoize earliestDojoMeeting by
		// dojo name for the duration of THIS pass: pools are only ever
		// mutated by an ACCEPTED swap below, which immediately breaks every
		// loop in this pass via the "&& !improved" guards, so every "before"
		// read in between reflects the exact same pool contents this pass
		// started with. Cleared at the top of every pass (an accepted swap
		// always starts a new one), which is equivalent to clearing on
		// accept -- nothing in THIS pass reads the cache again once a swap
		// is accepted. "After" values are never cached: they are the
		// post-swap state of one specific candidate and are never reused.
		clear(winnerMeetCache)
		clear(allQualMeetCache)
		improved := false
		for i := 0; i < numPools && !improved; i++ {
			for ai := 0; ai < len(pools[i].Players) && !improved; ai++ {
				a := pools[i].Players[ai]
				if a.Seed > 0 {
					continue
				}
				// beforeA/beforeAQA depend only on a.Dojo and the pass-start
				// pool contents (see the cache comment above), so they are
				// loop-invariant across the entire (j, bi) scan below --
				// hoisted here instead of being recomputed per candidate b,
				// and reused directly for hasMeetingSignal rather than
				// calling earliestDojoMeeting a second time for the same
				// answer.
				beforeA := cachedDojoMeeting(pools, pairRound, a.Dojo, winnerMeetCache)
				beforeAQA := cachedDojoMeeting(pools, allQualPairRound, a.Dojo, allQualMeetCache)
				hasMeetingSignal := beforeA != math.MaxInt
				hasExcessSignal := excessOf(a.Dojo, countDojoInPool(pools[i], a.Dojo)) > 0
				if !hasMeetingSignal && !hasExcessSignal {
					continue // nothing an exchange could ever improve for this dojo, from THIS pool
				}
				for j := 0; j < numPools && !improved; j++ {
					if j == i {
						continue
					}
					for bi := 0; bi < len(pools[j].Players) && !improved; bi++ {
						b := pools[j].Players[bi]
						if b.Seed > 0 || b.Dojo == a.Dojo {
							continue
						}
						// Tier (a) delta, re-derived from the four (pool,
						// dojo) cells the swap actually touches, REPLACING
						// the old flat "receiving pool must not exceed
						// cap" precheck: that precheck rejected every
						// excess-reducing swap whose receiving side was
						// itself already at or near cap, which is exactly
						// the fix path (move a member OUT of an over-cap
						// pool into one that is merely at-cap-minus-one).
						// A swap is only ever rejected here for making
						// TOTAL excess WORSE, never for a single pool's
						// count in isolation.
						cAi := countDojoInPool(pools[i], a.Dojo) // includes a
						cAj := countDojoInPool(pools[j], a.Dojo) // a not in j yet
						cBj := countDojoInPool(pools[j], b.Dojo) // includes b
						cBi := countDojoInPool(pools[i], b.Dojo) // b not in i yet
						beforeExc := excessOf(a.Dojo, cAi) + excessOf(a.Dojo, cAj) + excessOf(b.Dojo, cBi) + excessOf(b.Dojo, cBj)
						afterExc := excessOf(a.Dojo, cAi-1) + excessOf(a.Dojo, cAj+1) + excessOf(b.Dojo, cBi+1) + excessOf(b.Dojo, cBj-1)
						deltaExc := afterExc - beforeExc
						if deltaExc > 0 {
							continue
						}
						// Only the two exchanged dojos' meetings can move,
						// so the objective is updated by their delta rather
						// than recomputed over every dojo (which made the
						// 2048-config sweep ~6x slower for no extra
						// information). Both the winner-path pair (tiers
						// b/c) and the all-qualifier pair (tier d) are
						// captured before AND after, since the "no dojo
						// gets earlier" guard applies to BOTH -- the
						// best-effort crossing tier must never backfire by
						// accepting a swap that makes an all-qualifier
						// meeting earlier even while tiers (a)-(c) look
						// neutral or better. beforeA/beforeAQA are the
						// ai-level hoisted values (a.Dojo and the pool
						// contents are both unchanged since then); beforeB/
						// beforeAQB come from the pass-scoped cache, since
						// b.Dojo recurs across many (i, ai, j, bi)
						// candidates within one pass.
						beforeB := cachedDojoMeeting(pools, pairRound, b.Dojo, winnerMeetCache)
						beforeAQB := cachedDojoMeeting(pools, allQualPairRound, b.Dojo, allQualMeetCache)
						pools[i].Players[ai], pools[j].Players[bi] = b, a
						afterA := earliestDojoMeeting(pools, pairRound, a.Dojo)
						afterB := earliestDojoMeeting(pools, pairRound, b.Dojo)
						afterAQA := earliestDojoMeeting(pools, allQualPairRound, a.Dojo)
						afterAQB := earliestDojoMeeting(pools, allQualPairRound, b.Dojo)
						newExc, newR1, newNS := curExc+deltaExc, curR1, curNS
						for _, d := range [2][2]int{{beforeA, afterA}, {beforeB, afterB}} {
							bef, aft := d[0], d[1]
							if bef != math.MaxInt {
								if bef <= 1 {
									newR1--
								}
								newNS += bef
							}
							if aft != math.MaxInt {
								if aft <= 1 {
									newR1++
								}
								newNS -= aft
							}
						}
						newAQ := curAQ
						for _, d := range [2][2]int{{beforeAQA, afterAQA}, {beforeAQB, afterAQB}} {
							bef, aft := d[0], d[1]
							if bef != math.MaxInt {
								newAQ += bef
							}
							if aft != math.MaxInt {
								newAQ -= aft
							}
						}
						if afterA >= beforeA && afterB >= beforeB &&
							afterAQA >= beforeAQA && afterAQB >= beforeAQB &&
							better(newExc, newR1, newNS, newAQ, curExc, curR1, curNS, curAQ) {
							improved = true
							break
						}
						pools[i].Players[ai], pools[j].Players[bi] = a, b // revert
					}
				}
			}
		}
		if !improved {
			break // pairwise exchanges stalled; no rotation fallback (see this function's own doc comment)
		}
	}

	// Exchanges reorder members inside pools; renumber the display positions
	// so they stay 1..n in pool order, exactly as the fill assigns them.
	for i := range pools {
		for k := range pools[i].Players {
			pools[i].Players[k].PoolPosition = int64(k + 1)
		}
	}
}

// treeAwareQualifierSlots is poolQualifierPaths (Phase 1's seam) called
// correctly for a distributor that places players by PRE-reorder pool
// position while the real knockout draw is built from POST-reorder
// position, which are two DIFFERENT index spaces:
//
//   - PRE-reorder: the order CreatePools' straight fill builds pools in,
//     which is the order seed placement's arithmetic (placeSeedIndices) is
//     computed against (index i lands in pool i%numPools) -- BuildPoolPhase
//     itself only reorders as its LAST step, after every player has a pool.
//     This is the space `pools` (and `targetSizes`) is in throughout steps
//     2 and 3 of BuildPoolPhaseTreeAware.
//   - POST-reorder: the CONTIGUOUS-BLOCK-BY-COURT order ReorderPoolsForCourts
//     produces (all court-0 pools first, then court-1's, ...), which is what
//     AssignPoolsToCourts -- and through it BuildKnockoutDraw -- assumes a
//     pool array is already in.
//
// Calling poolQualifierPaths directly on PRE-reorder targetSizes silently
// scores every candidate against the WRONG topology whenever ReorderPoolsForCourts
// would actually move anything (its own activation condition, numPools >
// numCourts): measured in testing (bc-dojo Phase 2/3) to relabel a same-dojo
// pairing that the real draw puts in round 2 as a round-1 pairing instead,
// because pool position 2 pre-reorder can land adjacent to position 0
// post-reorder while looking two slots apart before the permutation.
//
// This round-trips through the POST-reorder order (matching what the real
// draw will use) and permutes the per-pool answer back into PRE-reorder
// index space, which is the space every caller in this file operates in.
//
// mode (bc-dojo Phase 4) selects WHICH of the three skeleton builders does
// that scoring: production runs a different knockout builder per
// state.Competition.ExtraQualifiers, and the tree scored against must be
// the tree that mode actually cuts (see the file-level comment). overrides
// for larger-pools mode, and the seed-pool map for fill-bracket mode, are
// both derived from POST-reorder sizes/indices here, matching what
// production's own real-pool derivation (extraQualifierOverrides,
// SelectFillBracketDrafts) reads off the ACTUAL stored pools -- which are
// always POST-reorder, since BuildPoolPhase/BuildPoolPhaseFillBracket
// return only after ReorderPoolsForCourts has run.
//
// A mode's skeleton builder returning nil (BuildKnockoutDrawPerPool /
// BuildKnockoutDrawFillBracket refusing an out-of-scope shape) degrades to
// nil here too, which the dojo-tree descent (buildDojoTree,
// pickDojoTreeAwarePool) reads as "no region information at all": every
// placement falls back to plain leastConflictedPool. Production's own
// draw-build step reaches the identical out-of-scope refusal independently
// (buildPoolFedDraw), so the operator is told regardless of how the pools
// were formed.
func treeAwareQualifierSlots(targetSizes []int, poolWinners, numCourts int, mode qualifierMode) [][]int {
	numPools := len(targetSizes)
	post := reorderPositions(numPools, numCourts)

	postSizes := make([]int, numPools)
	for preIdx, postIdx := range post {
		postSizes[postIdx] = targetSizes[preIdx]
	}

	var postSlots [][]int
	switch mode.ExtraQualifiers {
	case qualifierModeLargerPools:
		overrides := extraQualifierOverridesFromSizes(postSizes, mode.MinPoolSize, poolWinners)
		postSlots = poolQualifierPathsPerPool(postSizes, poolWinners, overrides, numCourts)
	case qualifierModeFillBracket:
		postSeedPoolIdx := make(map[int]int, len(mode.SeedPoolIndex))
		for rank, preIdx := range mode.SeedPoolIndex {
			if preIdx >= 0 && preIdx < numPools {
				postSeedPoolIdx[rank] = post[preIdx]
			}
		}
		postSlots = poolQualifierPathsFillBracket(postSizes, mode.MinPoolSize, postSeedPoolIdx, numCourts)
	default:
		postSlots = poolQualifierPaths(postSizes, poolWinners, numCourts)
	}
	if postSlots == nil {
		return nil
	}
	preSlots := make([][]int, numPools)
	for preIdx, postIdx := range post {
		preSlots[preIdx] = postSlots[postIdx]
	}
	return preSlots
}

// extraQualifierOverridesFromSizes mirrors
// state.Competition.QualifiersForPool's larger-pools rule (a pool sends
// poolWinners+1 exactly when it is OVERSIZED, i.e. len(pool.Players) >
// PoolSize) but pre-placement, from target SIZES alone: pool i is oversized
// when sizes[i] > minPoolSize. This is exact, not an approximation --
// QualifiersForPool's own oversized test only ever reads a pool's
// participant COUNT, which is exactly what sizes[i] already promises pool i
// will end up holding once distribution finishes (both the old fill+repair
// pipeline and this one guarantee every pool's FINAL size equals its target
// size; nobody is ever short or over).
//
// minPoolSize <= 0 returns nil (no minimum to be over, matching
// QualifiersForPool's own degrade-to-uniform behaviour for that case) rather
// than marking every pool oversized.
func extraQualifierOverridesFromSizes(sizes []int, minPoolSize, poolWinners int) map[int]int {
	if minPoolSize <= 0 {
		return nil
	}
	var overrides map[int]int
	for i, sz := range sizes {
		if sz > minPoolSize {
			if overrides == nil {
				overrides = make(map[int]int, len(sizes))
			}
			overrides[i] = poolWinners + 1
		}
	}
	return overrides
}

// reorderPositions reports the same pre-to-post-reorder permutation
// ReorderPoolsForCourts (helper.go) produces, but as a bare index mapping
// instead of a []Pool: post[preIdx] is the position pre-reorder index
// preIdx lands at once ReorderPoolsForCourts actually runs, including its
// own no-op condition (numCourts <= 1 || numPools <= numCourts).
//
// Rather than re-deriving ReorderPoolsForCourts' own i%numCourts grouping
// arithmetic by hand -- a second copy that could silently drift from the
// real function -- this SIMULATES the real thing (the same pattern
// realTargetSizes uses for its own remainder spread): it builds numPools
// placeholder pools, each holding one uniquely-identifiable marker player
// (the player's Seed field carries its pre-reorder index; PoolName can't be
// the marker, since ReorderPoolsForCourts overwrites it as part of
// reordering), runs the REAL ReorderPoolsForCourts over them, and reads the
// permutation back off where each marker ended up. This can never drift
// from ReorderPoolsForCourts because it IS ReorderPoolsForCourts.
func reorderPositions(numPools, numCourts int) []int {
	markers := make([]Pool, numPools)
	for i := range markers {
		markers[i] = Pool{Players: []Player{{Seed: i}}}
	}
	reordered := ReorderPoolsForCourts(markers, numCourts)
	post := make([]int, numPools)
	for postIdx, p := range reordered {
		post[p.Players[0].Seed] = postIdx
	}
	return post
}

// ---------------------------------------------------------------------
// The dojo-tree descent (bc-dojo-least-conflicted-pool): step 3 of
// buildPoolPhaseTreeAwareCore. Places every unseeded player, IN ROSTER
// ORDER, by descending a tree of halves/quarters/.../pools built over each
// pool's WINNER qualifier leaf -- no repair pass of its own; the residual
// gap this alone could not close (12 of 1596 multi-dojo configs, all at
// poolWinners>=2) is closed by improveDojoMeetings' winner-path pairwise
// exchange immediately after it runs (see the file-level doc comment for
// the measured numbers). Exercised directly (in isolation, without the
// exchange pass) by pool_distribution_dojo_tree_test.go, which is also
// where the exchange's own contribution is pinned.
// ---------------------------------------------------------------------

// dojoNode is one node of the tree of halves built over every pool's WINNER
// qualifier leaf (buildDojoTree). An internal node has both left and right
// set and poolIdx == -1; a leaf has left == right == nil and poolIdx either
// >= 0 (a real pool occupies this knockout leaf) or -1 (a bye: no pool sends
// its winner here at all, e.g. a non-power-of-two pool count). dojoCount and
// capacity are aggregates over every REAL pool beneath this node, maintained
// incrementally by recordDojoOccupancy rather than recomputed per query, so
// a placement is a single root-to-leaf walk.
//
// poolCount (the number of REAL, non-bye pools beneath this node) is fixed
// at build time and never changes. It exists because a bracket with a
// non-power-of-two pool count produces UNEVEN branches -- e.g. a 6-pool
// standard draw splits 4 real pools on one side and 2 on the other -- and
// comparing raw dojoCount (or raw capacity) between branches of different
// sizes is misleading: a 2-pool branch already holding one of this dojo in
// EACH of its two pools (count 2, fully saturated, no room to add this dojo
// without doubling up) looks falsely "safer" than a 4-pool branch holding
// three (count 3, but with an entirely dojo-free pool still open) purely
// because 2 < 3. Comparing dojoCount/poolCount (and capacity/poolCount)
// instead -- as a cross-multiplied integer ratio, never a float -- fixes
// this: 2/2 (=1.0) vs 3/4 (=0.75) correctly prefers the 4-pool branch.
// Verified against TestTreeAwareGateScorecard's per-pool-optimum sweep,
// which regressed hundreds of configs under a raw-count comparison before
// this normalisation was added (see chooseDojoTreePool's own doc comment).
//
// roomPools (the number of real pools beneath this node that currently have
// ANY remaining capacity at all, of any dojo) is a SEPARATE aggregate from
// poolCount, and changes as placements consume the last seat in a pool:
// poolCount answers "how many real pools exist here", roomPools answers
// "how many of them can still take anyone". Comparing average capacity
// alone (capacity/poolCount) can still funnel a dojo's members into the
// SAME single pool repeatedly: once every pool in a branch already holds
// exactly one of this dojo, a lone still-roomy pool in a 1-pool branch can
// keep outscoring a 2-pool branch (also one each, tied on normalised
// dojoCount) purely on its own per-pool capacity, even though the 2-pool
// branch offers a SECOND distinct option for the NEXT member while the
// 1-pool branch has only the one -- repeat that tie enough times and every
// later member of that dojo funnels into the same lone pool. roomPools
// breaks that tie by breadth (more pools still open beats more seats in
// fewer pools) before capacity ever gets a vote. See
// TestPoolSeeding_DojoSpreadFallback (interleaved-roster 10-of-24 dojo),
// which pinned exactly this failure mode before roomPools was added.
type dojoNode struct {
	left, right *dojoNode
	poolIdx     int
	poolCount   int
	roomPools   int
	dojoCount   map[string]int
	capacity    int
	// poolIndices lists every REAL pool index beneath this node (fixed at
	// build time, like poolCount). Used only by the all-qualifier
	// best-effort tie-break in chooseDojoTreePool: a genuine tie on every
	// preceding tier is rare, so this is the one place gathering the
	// FULL, un-aggregated pool list (rather than the incrementally
	// maintained counters every other comparison uses) is worth its cost.
	poolIndices []int
}

// buildDojoTree builds the tree of halves over qualifierSlots' WINNER (rank
// 1) leaf for every pool, keyed by the pool's PRE-reorder index (the same
// index targetSizes and pools are in throughout this file). placed[i] is how
// many players already occupy pool i (seeds, at the point this is called)
// so each leaf's starting capacity already accounts for them.
//
// The tree's shape comes directly from the ACTUAL winner-leaf slot numbers
// the mode's real skeleton builder assigned (treeAwareQualifierSlots), never
// from an idealised balanced tree recomputed here: a non-power-of-two pool
// count already shows up as byes and uneven branch sizes in those slot
// numbers, and the tree built from them inherits that shape automatically.
//
// Returns (nil, 0) when qualifierSlots carries no usable winner data at all
// (mode.ExtraQualifiers's skeleton builder refused this shape, or there are
// no pools) -- the caller degrades entirely to leastConflictedPool in that
// case, exactly as the region-scoring predecessor of this function degraded
// when it had no region information (see treeAwareQualifierSlots' own doc
// comment on this same nil case).
func buildDojoTree(qualifierSlots [][]int, targetSizes []int, placed []int) (*dojoNode, int) {
	if len(qualifierSlots) == 0 {
		return nil, 0
	}
	poolAtSlot := make(map[int]int, len(qualifierSlots))
	maxSlot := 0
	for i, slots := range qualifierSlots {
		if len(slots) == 0 {
			continue // this pool has no usable winner slot; left out of the
			// tree entirely, reachable only via the flat leastConflictedPool
			// fallback (same degrade as an unsupported mode/shape).
		}
		winner := slots[0]
		poolAtSlot[winner] = i
		if winner > maxSlot {
			maxSlot = winner
		}
	}
	if len(poolAtSlot) == 0 {
		return nil, 0
	}
	totalBits := bits.Len(uint(maxSlot))
	root := buildDojoTreeRec(poolAtSlot, targetSizes, placed, totalBits, 0)
	return root, totalBits
}

// buildDojoTreeRec recurses MSB-first: bitsLeft counts down from totalBits
// to 0 as prefix accumulates the winner slot's bits high-to-low, so the
// FIRST split (root's two children) fixes the slot number's highest bit --
// exactly the bit dojoMeetRound (seed.go) says two slots must differ in to
// meet only in the final -- and the split floor (bitsLeft == 0) lands on one
// specific slot number, i.e. at most one real pool.
func buildDojoTreeRec(poolAtSlot map[int]int, targetSizes, placed []int, bitsLeft, prefix int) *dojoNode {
	if bitsLeft == 0 {
		n := &dojoNode{poolIdx: -1, dojoCount: map[string]int{}}
		if i, ok := poolAtSlot[prefix]; ok {
			n.poolIdx = i
			n.poolCount = 1
			n.poolIndices = []int{i}
			if room := targetSizes[i] - placed[i]; room > 0 {
				n.capacity = room
				n.roomPools = 1
			}
		}
		return n
	}
	left := buildDojoTreeRec(poolAtSlot, targetSizes, placed, bitsLeft-1, prefix<<1)
	right := buildDojoTreeRec(poolAtSlot, targetSizes, placed, bitsLeft-1, (prefix<<1)|1)
	poolIndices := make([]int, 0, len(left.poolIndices)+len(right.poolIndices))
	poolIndices = append(poolIndices, left.poolIndices...)
	poolIndices = append(poolIndices, right.poolIndices...)
	return &dojoNode{
		left: left, right: right, poolIdx: -1,
		dojoCount:   map[string]int{},
		capacity:    left.capacity + right.capacity,
		poolCount:   left.poolCount + right.poolCount,
		roomPools:   left.roomPools + right.roomPools,
		poolIndices: poolIndices,
	}
}

// recordDojoOccupancy walks root-to-leaf along winnerSlot's own bit path
// (the identical path buildDojoTreeRec assigned that slot to) and updates
// every node on the way: dojoCount[dojo]++ always, capacity += capacityDelta.
//
// capacityDelta is 0 when recording a SEED's occupancy: a seed's pool
// already had its room removed from the leaf's starting capacity (via
// buildDojoTree's own `placed` argument), so this call exists only to make
// the seed's dojo VISIBLE to the tree, not to spend room a second time.
// capacityDelta is -1 when recording a live unseeded placement, which is
// the one and only place this pass actually spends a seat -- and the one
// case that can exhaust a leaf's LAST seat, in which case roomPools is
// decremented all the way back up to the root too (see dojoNode's own doc
// comment on why roomPools is tracked separately from capacity/poolCount).
func recordDojoOccupancy(root *dojoNode, dojo string, winnerSlot, totalBits, capacityDelta int) {
	node := root
	bitsLeft := totalBits
	for {
		node.dojoCount[dojo]++
		node.capacity += capacityDelta
		if bitsLeft == 0 {
			break
		}
		bitsLeft--
		if (winnerSlot>>bitsLeft)&1 == 0 {
			node = node.left
		} else {
			node = node.right
		}
	}
	// node is now the leaf. capacityDelta < 0 only for a live placement;
	// capacity == 0 here means this exact decrement was the one that spent
	// the leaf's last seat (capacity only ever decreases by exactly 1 per
	// call, so 0 is reached exactly once).
	if capacityDelta < 0 && node.capacity == 0 {
		node = root
		bitsLeft = totalBits
		for {
			node.roomPools--
			if bitsLeft == 0 {
				return
			}
			bitsLeft--
			if (winnerSlot>>bitsLeft)&1 == 0 {
				node = node.left
			} else {
				node = node.right
			}
		}
	}
}

// allQualifierWorstMeeting is the operator's best-effort crossing tie-break
// ("is there also a best effort when there are 2 qualifiers from the
// pool?" -- yes: "avoided as a best effort by placing in the correct pool
// of the other side"): the WORST (i.e. earliest) dojoMeetRound over every
// pair of slots a candidate pool COULD send up against every slot an
// already-placed dojo-mate's pool could send up, reading the FULL
// qualifierSlots (winner AND runner-up/crossed-in) on both sides -- unlike
// every other comparison in this file, which deliberately reads the
// winner leaf alone. Returns math.MaxInt when either side is empty (no
// data, never used to decide anything by itself).
func allQualifierWorstMeeting(candidatePools []int, qualifierSlots [][]int, dojoPools []int) int {
	worst := math.MaxInt
	for _, p := range candidatePools {
		if p < 0 || p >= len(qualifierSlots) {
			continue
		}
		for _, q := range dojoPools {
			if q < 0 || q >= len(qualifierSlots) || q == p {
				continue
			}
			if r := earliestPairing(qualifierSlots[p], qualifierSlots[q]); r < worst {
				worst = r
			}
		}
	}
	return worst
}

// chooseDojoTreePool descends root to a leaf for a NEW member of dojo: at
// every internal node, take the child holding FEWER of dojo PER REAL POOL
// beneath it (a child with zero remaining capacity is never eligible at
// all, regardless of its count); ties broken by the child offering MORE
// STILL-OPEN pools (roomPools); ties broken by the child with MORE
// remaining capacity per real pool; ties broken by the ALL-QUALIFIER
// best-effort (allQualifierWorstMeeting: the child whose full slot sets --
// runners-up included -- stay LATEST against this dojo's already-placed
// pools' full slot sets); ties broken by the lower/leftmost child. Returns
// -1 when the reached leaf has no real pool or no room (a bye, or every
// real pool beneath root is already full -- the latter should not arise
// given this file's own sum(targetSizes) == len(players) invariant, but is
// reported rather than panicking).
//
// dojoCount and capacity are compared PER REAL POOL (dojoCount/poolCount,
// capacity/poolCount), cross-multiplied to stay in integers, never raw
// sums -- see dojoNode's own doc comment for why an uneven branch split
// (any non-power-of-two pool count) makes a raw-sum comparison actively
// wrong. roomPools is compared as a raw count (breadth, not a ratio) --
// see dojoNode's own doc comment for why capacity-per-pool alone can still
// funnel repeated placements into one lone pool.
//
// The all-qualifier tier is a PURE tie-break, reached only when every tier
// ahead of it is EXACTLY equal: it can never override a decision the
// winner-path tiers already made, only resolve what would otherwise fall
// to the arbitrary lower/leftmost rule. dojoPoolIndices (every pool this
// dojo already occupies, by index) is computed ONCE by the caller and
// threaded through unchanged for the whole descent, since it does not
// change mid-walk.
func chooseDojoTreePool(root *dojoNode, dojo string, qualifierSlots [][]int, dojoPoolIndices []int) int {
	node := root
	for node.left != nil {
		l, r := node.left, node.right
		switch {
		case l.capacity <= 0 && r.capacity <= 0:
			return -1
		case l.capacity <= 0:
			node = r
		case r.capacity <= 0:
			node = l
		default:
			lc, rc := l.dojoCount[dojo]*r.poolCount, r.dojoCount[dojo]*l.poolCount
			lcap, rcap := l.capacity*r.poolCount, r.capacity*l.poolCount
			switch {
			case lc < rc:
				node = l
			case rc < lc:
				node = r
			case l.roomPools > r.roomPools:
				node = l
			case r.roomPools > l.roomPools:
				node = r
			case lcap > rcap:
				node = l
			case rcap > lcap:
				node = r
			default:
				lWorst := allQualifierWorstMeeting(l.poolIndices, qualifierSlots, dojoPoolIndices)
				rWorst := allQualifierWorstMeeting(r.poolIndices, qualifierSlots, dojoPoolIndices)
				switch {
				case lWorst > rWorst:
					node = l
				case rWorst > lWorst:
					node = r
				default:
					node = l // tie -> lower/leftmost
				}
			}
		}
	}
	if node.poolIdx < 0 || node.capacity <= 0 {
		return -1
	}
	return node.poolIdx
}

// pickDojoTreeAwarePool chooses a pool for a new member of dojo: the tree
// descent (chooseDojoTreePool) when the tree has something to say, plain
// leastConflictedPool otherwise.
//
// A dojo with NOBODY placed anywhere yet (root == nil, dojo == "", or
// root.dojoCount[dojo] == 0 -- no seed and no earlier unseeded member of
// this dojo) has no tree signal to route on at all: every branch would tie
// 0-vs-0 at every level, all the way down, and the decision would then rest
// entirely on branch CAPACITY -- which tracks winner-SLOT-number branching,
// not pool INDEX, and is uneven whenever the knockout draw's own byes make
// one branch hold more real pools than the other (the common case for a
// non-power-of-two pool count). Falling through to leastConflictedPool
// instead (fewest of dojo -- tied at 0 -- then fewest players, then lowest
// index, scanning pools in INDEX order) is what keeps
// TestPoolDistribution_UniqueDojoIdentity's round-robin deal intact: an
// all-unique-dojo roster has EVERY placement hit this exact bypass, so the
// deal is produced by leastConflictedPool's own index-ordered scan, exactly
// as it always has been, never by the tree. Once a dojo has at least one
// member placed somewhere (a seed, or an earlier unseeded member via this
// same bypass), root.dojoCount[dojo] is no longer 0 and every later member
// of that dojo genuinely does have a branch to avoid, so the tree descent
// takes over from there.
func pickDojoTreeAwarePool(pools []Pool, targetSizes []int, root *dojoNode, dojo string, qualifierSlots [][]int) int {
	if root == nil || dojo == "" || root.dojoCount[dojo] == 0 {
		return leastConflictedPool(pools, targetSizes, dojo)
	}
	dojoPoolIndices := make([]int, 0, 4)
	for idx := range pools {
		if countDojoInPool(pools[idx], dojo) > 0 {
			dojoPoolIndices = append(dojoPoolIndices, idx)
		}
	}
	if best := chooseDojoTreePool(root, dojo, qualifierSlots, dojoPoolIndices); best >= 0 {
		return best
	}
	// The tree found no room (a bye-heavy corner, or a pool this roster's
	// mode left out of the tree entirely, per buildDojoTree's own doc
	// comment) -- leastConflictedPool still has the full, real pool list to
	// fall back on.
	return leastConflictedPool(pools, targetSizes, dojo)
}

// assignUnseededByDojoTree places every unseeded player, IN ROSTER ORDER --
// the participant list arrives pre-shuffled (the CLI shuffle, the mobile
// app's explicit "Shuffle unseeded" control), so re-sorting by dojo size
// here would fight that upstream decision rather than help it; order
// sensitivity is upstream's responsibility by design.
//
// Each placement is one call to pickDojoTreeAwarePool against a SHARED,
// incrementally-updated dojo tree (see that function's and buildDojoTree's
// own doc comments for the mechanism and the one bypass case). Seeds are
// folded into the tree's dojo counts before the loop starts -- their room
// is already spent (baked into `placed` when the tree was built), so
// recordDojoOccupancy is called with capacityDelta 0 for them and -1 for
// every live unseeded placement, the one place this pass actually spends a
// seat.
//
// dojoOptimum guards the tree's pick against the one failure mode pure
// relative comparisons cannot see: when a dojo's LAST few members are all
// that remains to be placed and every pool the tree descent would consider
// is tied, the descent can be forced -- by simple seat exhaustion elsewhere
// in the roster, nothing to do with THIS dojo at all -- into the one pool
// that still has physical room, even when that pool already holds the
// dojo's per-pool optimum (ceil(footprint/numPools), footprint counting
// seeds too, the same arithmetic checkAbsoluteInvariants' gate enforces).
// A roster where one dojo is exactly HALF the field (footprint == numPools
// * optimum, zero slack anywhere) reaches this in an adversarial roster
// order -- see the "dojo/deep-oversubscription" shape in
// draw_shapes_golden_test.go. Whenever the tree's own pick would exceed the
// cap AND a different pool exists that is both under the cap and has room,
// that pool is used instead (poolUnderDojoCap, leastConflictedPool's own
// tie-break applied to the capped candidate set); only when EVERY pool
// with room is already at or over the cap does the tree's original
// (unavoidable) pick stand -- which the same 24-entrant, half-dojo roster
// still reaches at its very last placement, when literally one seat
// remains in the entire roster and it happens to sit in an at-cap pool; no
// forward-only, no-repair pass can route around that, since nothing short
// of a different EARLIER placement (requiring foresight this pass does not
// have) could have left a different pool as the last one standing.
func assignUnseededByDojoTree(pools []Pool, targetSizes []int, unseeded []Player, qualifierSlots [][]int) error {
	placed := make([]int, len(pools))
	for i := range pools {
		placed[i] = len(pools[i].Players)
	}
	root, totalBits := buildDojoTree(qualifierSlots, targetSizes, placed)
	if root != nil {
		for i := range pools {
			if i >= len(qualifierSlots) || len(qualifierSlots[i]) == 0 {
				continue
			}
			for _, pl := range pools[i].Players {
				if pl.Dojo == "" {
					continue
				}
				recordDojoOccupancy(root, pl.Dojo, qualifierSlots[i][0], totalBits, 0)
			}
		}
	}

	numPools := len(pools)
	footprint := make(map[string]int, len(unseeded))
	for i := range pools {
		for _, pl := range pools[i].Players {
			if pl.Dojo != "" {
				footprint[pl.Dojo]++
			}
		}
	}
	for _, p := range unseeded {
		if p.Dojo != "" {
			footprint[p.Dojo]++
		}
	}
	dojoOptimum := func(dojo string) int {
		if numPools <= 0 {
			return 0
		}
		f := footprint[dojo]
		return (f + numPools - 1) / numPools
	}

	for _, p := range unseeded {
		best := pickDojoTreeAwarePool(pools, targetSizes, root, p.Dojo, qualifierSlots)
		if best < 0 {
			// Cannot happen when sum(targetSizes) == len(players), which
			// realTargetSizes guarantees; kept as a defensive error rather
			// than a panic or a silently dropped player.
			return fmt.Errorf("cannot place player %s: no pool has room", p.Name)
		}
		if p.Dojo != "" && countDojoInPool(pools[best], p.Dojo) >= dojoOptimum(p.Dojo) {
			if alt := poolUnderDojoCap(pools, targetSizes, p.Dojo, dojoOptimum(p.Dojo)); alt >= 0 {
				best = alt
			}
		}
		p.PoolPosition = int64(len(pools[best].Players) + 1)
		pools[best].Players = append(pools[best].Players, p)
		if root != nil && p.Dojo != "" && best < len(qualifierSlots) && len(qualifierSlots[best]) > 0 {
			recordDojoOccupancy(root, p.Dojo, qualifierSlots[best][0], totalBits, -1)
		}
	}
	return nil
}

// poolUnderDojoCap is leastConflictedPool restricted to pools that hold
// FEWER than dojoCap of dojo already: every pool at or over the cap is
// masked as already full (its target size set to its current length), so
// leastConflictedPool's own room check and tie-break (fewest of dojo,
// fewest players, lowest index) narrows the search to the capped
// candidates while still returning a true original index. Returns -1 when
// no pool is both under cap and has room, matching leastConflictedPool's
// own sentinel.
func poolUnderDojoCap(pools []Pool, targetSizes []int, dojo string, dojoCap int) int {
	masked := append([]int(nil), targetSizes...)
	for i := range pools {
		if countDojoInPool(pools[i], dojo) >= dojoCap {
			masked[i] = len(pools[i].Players)
		}
	}
	return leastConflictedPool(pools, masked, dojo)
}
