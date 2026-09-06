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
//     -- no three-way rotation -- scored on a FOUR-TIER lexicographic
//     objective led by a total spread-cap excess delta, then the
//     WINNER-PATH metric (poolPairRounds fed winner-only slot lists, i.e.
//     slots[0] per pool, the same pre-reorder space the descent itself
//     uses), then an all-qualifier best-effort tie-break (see
//     improveDojoMeetings' own doc comment for the full tier list). This is what
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
//
// bc-pnum (int-id rewrite): the bijective pool-label fix
// (bc-dojo-least-conflicted-pool) restored 12 previously collided pools to
// improveDojoMeetings' own exchange pass, which is legitimate extra work,
// not a regression -- but a CPU profile of that pass afterwards showed
// improveDojoMeetings at 99.34% cumulative, earliestDojoMeeting at 86.86%,
// and mapaccess2_faststr (a map[string]* lookup) at 51%: the per-pool `counts`
// this pass and the descent's dojoNode.dojoCount both used were
// map[string]int keyed by the normalized dojo string, looked up on every
// candidate evaluated. dojoIDCache (tournament.go) interns each distinct
// normalized key to a dense int ONCE per draw, up front, so both structures
// became a plain []int/[][]int indexed by that id instead -- see
// dojoIDCache's own doc comment for the measured before/after numbers, and
// earliestDojoMeetingScan's (below) for the buffer-reuse fix that removed
// the remaining per-call allocation once the map lookup itself was gone.
//
// Follow-up (bc-pnum review): the id-conversion above still left every
// candidate's ak/bk resolved via ids.of(a.Dojo)/ids.of(b.Dojo) -- two map
// probes each (dojoKeyCache's raw->normalized lookup, then dojoIDCache's own
// normalized->id lookup) -- so a re-profile after the first pass still
// showed a measurable mapaccess2_faststr share. poolDojoIDs (a [][]int kept
// in lockstep with `counts` at the same two swap sites, improveDojoMeetings)
// removed that too: ak/bk are now a plain slice index. A CPU profile of
// BuildPoolPhaseTreeAware_256_16x16_Interleaved after BOTH fixes shows
// mapaccess2_faststr, dojoIDCache.of and dojoKeyCache.of absent entirely
// from a full (nodefraction=0) node listing -- not merely small, genuinely
// not sampled. What remains (~86% cumulative) is earliestDojoMeetingScan
// itself: the O(numPools) countIn scan collecting a dojo's occupied pools,
// plus the O(occupied^2) pairRound comparison over them -- real arithmetic
// over the extra pools the bijective-label fix legitimately restored to the
// exchange pass, not a lookup cost left to remove. See dojoIDCache's own
// doc comment for the measured before/after benchmark numbers this left.
//
// Output is unchanged: this only touches HOW the existing objective is
// computed, never what it computes, and `make examples` was re-run and
// diffed byte-identical after every pass of this rewrite.

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
//     result, scored on a FOUR-TIER lexicographic objective led by a total
//     spread-cap excess delta, then the winner-path metric, then an
//     all-qualifier best-effort tie-break: the descent commits each player
//     the moment it places them and cannot see a later dojo's needs, so on
//     a multi-dojo roster an early dojo can still box a later one into
//     pools whose winners meet in round 1. The exchange pass closes
//     exactly that residual (accepted only when it strictly improves,
//     never worsens any dojo's earliest winner-path meeting); it is a
//     no-op on an all-unique-dojo roster and on a single-dojo roster
//     already at the brute-force ceiling. See improveDojoMeetings'
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
// This is always STANDARD mode (QualifierModeStandard): a caller that knows
// its competition's real extra-qualifiers setting must call
// BuildPoolPhaseTreeAwareWithMode instead, or the distributor scores every
// candidate against the wrong knockout tree whenever that setting is not
// the default. Kept as its own entry point, for the many existing tests
// (this file's and pool_distribution_gate_test.go's) that call it by this
// exact 5-argument signature, pinned before mode-awareness existed -- but
// FOLDED into BuildPoolPhaseTreeAwareWithMode (bc-drwx item 11) rather than
// carrying its own duplicate poolTargetSizes+buildPoolPhaseTreeAwareCore
// call pair: qualifierMode.MinPoolSize is documented as unused in standard
// mode, so BuildPoolPhaseTreeAwareWithMode's own MinPoolSize derivation
// (which BuildPoolPhaseTreeAware never computed at all) cannot change this
// function's behaviour, only its implementation.
func BuildPoolPhaseTreeAware(players []Player, poolSize int, isMax bool, numCourts int, poolWinners int) ([]Pool, int, error) {
	return BuildPoolPhaseTreeAwareWithMode(players, poolSize, isMax, numCourts, poolWinners, QualifierModeStandard)
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
// for why the values are duplicated here rather than imported. Exported
// (bc-drwx item 13) so internal/state's own test suite -- which already
// imports internal/helper in production code (Competition.QualifiersForPool
// takes a helper.Pool) -- can pin state.ExtraQualifiers* equal to these by
// direct reference (TestExtraQualifiersConstantsMatchHelper,
// internal/state/extra_qualifiers_constants_test.go) instead of the two
// sets of string literals only ever being checked by convention.
const (
	QualifierModeStandard    = ""
	QualifierModeLargerPools = "larger-pools"
	QualifierModeFillBracket = "fill-bracket"
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
// uniform minSize row, spread by realTargetSizes' own remainder walk --
// see tournament.go's assignPlayersToPools doc comment, bc-drwx item 11),
// fill-bracket's poolWinners is always 1
// (state.ValidateExtraQualifiers' own gate), and the mode is
// QualifierModeFillBracket throughout -- everything else (seed placement,
// the remainder spread, the one-pass distribution, ReorderPoolsForCourts
// last) is the shared core BuildPoolPhaseTreeAware itself now uses.
func BuildPoolPhaseFillBracketTreeAware(players []Player, minSize int, numCourts int) ([]Pool, int, error) {
	// bc-drwx item 13, corrected (bc-drwx review): this guard is
	// MESSAGE-ONLY, not a safety necessity -- an earlier version of this
	// comment claimed it prevented an uncontrolled allocation in
	// buildQualifierSkeleton (minSize placeholder seats per pool), but
	// that allocation was never actually unbounded: FillBracketPoolCount's
	// own numPools is capped at n/minSize (its own maxP), so
	// numPools*minSize -- the exact quantity buildQualifierSkeleton
	// allocates -- can never exceed n, the caller's OWN roster length,
	// whatever minSize is. A minSize at or above MaxPoolSize left
	// unguarded here still cannot reach that allocation: FillBracketPoolCount
	// already refuses it, either via "fewer than the minimum pool size" (n
	// < minSize) or its own "no pool count fits" error for the cases where
	// n >= minSize but no valid pool count satisfies the draft-supply rule.
	// The guard is kept anyway because "pool size must be less than 1000"
	// names the real limit and the reason in one line, which is a clearer
	// operator-facing message than whichever of those two FillBracketPoolCount
	// errors would otherwise surface.
	if minSize >= MaxPoolSize {
		return nil, 0, fmt.Errorf("cannot create pools: pool size must be less than %d, got %d", MaxPoolSize, minSize)
	}
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
	return buildPoolPhaseTreeAwareCore(players, numPools, base, numCourts, 1, qualifierMode{ExtraQualifiers: QualifierModeFillBracket, MinPoolSize: minSize})
}

// ErrBlankDojoInDraw is the sentinel identifying a draw refused because the
// roster contains at least one player with an empty Dojo
// (bc-dojo-least-conflicted-pool FIX 1). Match it with errors.Is; the
// returned error's message additionally names every offending player so the
// operator knows exactly which row to repair.
//
// DISTINCT from state.ErrBlankDojo (internal/state/participants.go), the
// participant-WRITE floor: different packages, different sentinel values,
// on purpose -- errors.Is(drawErr, state.ErrBlankDojo) would silently never
// match this one despite the shared concept, which is exactly why this one
// carries the "InDraw" suffix rather than the same bare name.
//
// Every downstream signal in this file is defined only for non-blank
// dojos: without this pre-flight, a blank-dojo player would consume a real
// pool seat via the leastConflictedPool bypass without ever updating the
// tree's capacity accounting -- the descent's ONLY fullness signal, which
// lets a later descent overfill a pool past its target size -- and
// improveDojoMeetings' footprint/spread/meeting objective would count
// Dojo=="" as a phantom dojo that drives and vetoes real swaps. Per the
// operator's absolute "dojo must never be empty" rule (NO FALLBACKS), a
// blank-dojo roster is refused outright rather than tolerated by a
// blank-skipping accounting path.
//
// This is why every call site downstream (recordDojoOccupancy's callers,
// dojoFootprintOptimum's footprint builders, pickDojoTreeAwarePool) used to
// carry its OWN `dojo != ""` guard: this pre-flight did not always run
// first. Now that it does -- the ONE gate every entry point funnels
// through, before any of those call sites are ever reached -- those guards
// are unreachable, not merely redundant, and were removed (bc-drwx item
// 11) rather than kept as a second, silent floor a future change could
// drift from this one.
//
// Participant LOADING stays blank-tolerant on purpose (state.LoadParticipants
// and its CSV parser accept a blank dojo, so a legacy or hand-edited roster
// can still be loaded and the offending row repaired in the UI/CSV) -- only
// the DRAW itself refuses, at this one shared pre-flight.
var ErrBlankDojoInDraw = errors.New("cannot draw pools: every competitor must have a dojo")

// ValidateNoBlankDojo is the one pre-flight check shared by
// BuildPoolPhaseTreeAware, BuildPoolPhaseTreeAwareWithMode and
// BuildPoolPhaseFillBracketTreeAware (all three funnel through
// buildPoolPhaseTreeAwareCore, so this is called exactly once per draw
// attempt regardless of which entry point the caller used). Returns nil
// when every player has a non-blank Dojo. Trims before comparing (matching
// state.ErrBlankDojo's own write-floor check, saveParticipantsNoLock) so a
// future in-memory producer that hands this a whitespace-only Dojo without
// going through that floor first cannot slip "   " past this guard too.
//
// Exported (bc-drwx item 8) so internal/engine's runDrawPipeline can call it
// as ONE roster pre-flight covering every competition format, not just the
// pool-distributor formats (mixed/league) that reach it via
// buildPoolPhaseTreeAwareCore: a standalone playoffs or Swiss competition
// used to draw silently over a blank-dojo roster, since neither
// generatePlayoffs nor GenerateSwissRound ever passes through the
// distributor at all. The call INSIDE buildPoolPhaseTreeAwareCore stays --
// it is what makes this function true for a caller that reaches the
// distributor some OTHER way (a CLI/test caller of BuildPoolPhaseTreeAware*
// directly, bypassing the engine's own pre-flight entirely) -- but for the
// engine's own callers it is now the ASSERT this doc always claimed it was,
// never the operator-facing refusal: that already fired one layer up.
func ValidateNoBlankDojo(players []Player) error {
	var names []string
	for _, p := range players {
		if strings.TrimSpace(p.Dojo) == "" {
			names = append(names, p.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrBlankDojoInDraw, strings.Join(names, ", "))
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
	// ErrBlankDojoInDraw's own doc comment for the two mechanisms this closes.
	// Redundant with -- and now effectively an ASSERT behind -- the engine's
	// own runDrawPipeline pre-flight (bc-drwx item 8) for every caller that
	// reaches this through the engine; still the real, operator-facing
	// refusal for a caller (CLI, test) that reaches this function directly.
	if err := ValidateNoBlankDojo(players); err != nil {
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
	//
	// keys memoizes dojoKey by raw dojo string for the whole of this draw
	// (bc-drwx review fix): both this step and the exchange pass below
	// compare dojo identity in loops that scale with roster/pool size, and
	// NormalizeParticipantName (what dojoKey calls) is expensive real work
	// (NFD decompose, strip marks, re-NFC, lowercase, whitespace collapse)
	// that a draw's hot paths used to redo on every single comparison --
	// see dojoKeyCache's own doc comment (tournament.go) for the measured
	// cost.
	//
	// ids interns every distinct normalized dojo key across the WHOLE
	// roster to a dense int id, ONCE, here -- BEFORE assignUnseededByDojoTree
	// or improveDojoMeetings allocate a single []int/[][]int sized by it
	// (bc-pnum). This is what lets both of those hot loops index a plain
	// []int per pool/tree-node instead of paying a map[string]* lookup
	// (mapaccess2_faststr, profiled at 51% cumulative in
	// BuildPoolPhaseTreeAware_256_16x16_Interleaved) on every candidate
	// evaluated -- see dojoIDCache's own doc comment for why every id must
	// be minted before that sizing happens, and its own callers for the
	// measured before/after numbers. newDojoIDCacheFor is the one shared
	// body for this "intern the whole roster up front" block (tournament.go).
	ids, keys := newDojoIDCacheFor(players)
	qualifierSlots := treeAwareQualifierSlots(targetSizes, poolWinners, drawCourts, mode)
	if err := assignUnseededByDojoTree(pools, targetSizes, unseeded, qualifierSlots, keys, ids); err != nil {
		return nil, 0, err
	}

	// Step 4: the pairwise-exchange repair pass. The descent above is
	// greedy per player, and on a MULTI-dojo roster the dojos placed early
	// can still box a later dojo into pools whose own qualifiers meet in
	// round 1, because the descent commits each player the moment it
	// places them and cannot see a later dojo's needs -- measured before
	// this step existed (this bead's own sweep): 12 of 1596 multi-dojo
	// configs, all at poolWinners>=2, reached a worse winner-path result
	// through the descent alone than through repair. See
	// improveDojoMeetings' own doc comment for the exchange rule and its
	// four-tier objective (bc-drwx item 4: this comment used to restate
	// that objective itself, a copy this file already carried three other
	// times, so it now points at the one place that description lives
	// instead of drifting from it again). Single-dojo rosters are already
	// at their brute-force ceiling (the Phase 3 gate pins 180/180) and an
	// all-unique-dojo roster has no dojo spanning >=2 pools at all, so
	// this loop is a no-op in both cases, which is what keeps the gate
	// numbers and the unique-dojo identity contract exactly where they
	// were.
	improveDojoMeetings(pools, qualifierSlots, ids)

	return ReorderPoolsForCourts(pools, drawCourts), drawCourts, nil
}

// dojoCounter reports how many members of the dojo identified by DENSE ID id
// (dojoIDCache, bc-pnum) currently occupy pool poolIdx -- an int id, not a
// normalized dojo string, so the hot per-candidate lookups in
// improveDojoMeetings' exchange pass index a plain []int instead of paying a
// map[string]* lookup (mapaccess2_faststr, profiled at 51% cumulative in
// BuildPoolPhaseTreeAware_256_16x16_Interleaved even after dojoKeyCache
// memoized the normalization itself -- see dojoIDCache's own doc comment).
// earliestDojoMeeting takes one rather than a bare []Pool so its caller can
// choose the cheapest available source of truth: a standalone caller with no
// other state wraps countDojoInPool directly (O(poolSize) per call, given
// the id's original dojo string), while improveDojoMeetings' own exchange
// pass wraps its incrementally-maintained per-pool `counts` [][]int instead
// (O(1) per call, indexed by the SAME id this type's caller already has).
type dojoCounter func(poolIdx int, id int) int

// earliestDojoMeeting (bc-pnum review H9: moved to
// pool_distribution_tree_aware_test.go, its one caller) is the reference
// oracles' entry point into earliestDojoMeetingScan -- a thin,
// throwaway-scratch-slice wrapper kept only because the tests and reference
// oracles that call it by this signature do not run often enough for the
// scratch allocation to matter. Every production caller
// (improveDojoMeetings' hot exchange loop) calls earliestDojoMeetingScan
// directly instead, with a buffer it reuses across the whole function.

// earliestDojoMeetingScan is earliestDojoMeeting's body, factored out
// (bc-pnum) so a hot caller can hand it a REUSABLE scratch slice instead of
// paying a fresh make([]int, 0, len(pools)) on every call: profiling
// BuildPoolPhaseTreeAware_256_16x16_Interleaved after the mapaccess2_faststr
// fix (dojoIDCache) still showed earliestDojoMeeting itself, plus GC
// scanning/mallocgc together, over half of cumulative time -- this
// function's own per-call slice was the source. *occupied is reset to
// length 0 (keeping its capacity) on every call rather than reallocated, and
// left holding the current dojo's occupied-pool indices on return purely as
// a side effect of the reset-and-refill; callers never read it directly.
//
// Collects dojo's occupied-pool indices ONCE (O(P*poolSize)) instead of
// rediscovering pool j's membership inside the pair loop for every i (the
// old code called countDojoInPool(pools[j], ...) up to once per (i, j)
// pair, i.e. O(P^2*poolSize)). The pair loop below then only ever visits
// the k <= P pools that actually hold the dojo, via direct pairRound
// lookups (k^2), which is where the real savings are: k is typically small
// even when P (pool count) is large.
//
// count (bc-drwx review fix, then bc-pnum's int-id rewrite) replaces a raw
// countDojoInPool(pools[i], dojo) call: improveDojoMeetings' own exchange
// pass calls this function on the order of numPools^2*poolSize^2 times, and
// an O(poolSize) re-scan of pool.Players per query at that scale multiplied
// the whole pass by poolSize for nothing once dojo identity itself is
// already a cached int id (dojoIDCache) -- see improveDojoMeetings' own
// `counts`/`countIn` doc comment for the O(1) replacement it passes here. A
// standalone caller (this file's own tests) can still pass a
// countDojoInPool-backed closure and get exactly the old behaviour.
func earliestDojoMeetingScan(pools []Pool, pairRound [][]int, id int, count dojoCounter, occupied *[]int) int {
	buf := (*occupied)[:0]
	for i := range pools {
		if i >= len(pairRound) {
			continue
		}
		if count(i, id) > 0 {
			buf = append(buf, i)
		}
	}
	*occupied = buf
	earliest := math.MaxInt
	for oi := 0; oi < len(buf); oi++ {
		for oj := oi + 1; oj < len(buf); oj++ {
			i, j := buf[oi], buf[oj]
			if r := pairRound[i][j]; r < earliest {
				earliest = r
			}
		}
	}
	return earliest
}

// cachedDojoMeeting is earliestDojoMeeting memoized by dense dojo id
// (dojoIDCache, bc-pnum -- two spellings of one dojo already share the one
// id, matching earliestDojoMeeting's own count-derived answer, which is
// already spelling-insensitive). known/cache are a matched pair of
// numDojos()-sized slices rather than a map (bc-pnum: this cache is cleared
// every pass -- see the call site's own comment -- and clearing a slice is
// both cheaper and avoids the int-map lookup a map[int]int cache would still
// pay). Takes the id pre-resolved, not a raw dojo string, since every call
// site in this file already has one on hand (a.Dojo/b.Dojo resolved once at
// the ai/bi level) -- see dojoCounter's own doc comment for why re-resolving
// here would undo that. occupied is threaded straight through to
// earliestDojoMeetingScan -- see that function's own doc comment. Callers
// (improveDojoMeetings) hold cache valid only across a span of calls during
// which pools' CONTENTS are known not to change -- see the call site's own
// comment for why that span is exactly "one pass" there. Getting that
// invariant wrong would silently serve a stale round for a dojo whose pool
// membership already moved, so this stays a private helper rather than
// something a new caller could reach for without re-deriving the same
// guarantee.
func cachedDojoMeeting(pools []Pool, pairRound [][]int, id int, known []bool, cache []int, count dojoCounter, occupied *[]int) int {
	if known[id] {
		return cache[id]
	}
	v := earliestDojoMeetingScan(pools, pairRound, id, count, occupied)
	known[id] = true
	cache[id] = v
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

// dojoFootprintOptimum returns a per-dojo (dense id) optimum function:
// ceil(totalMembers/numPools), the per-pool spread cap every dojo-count
// comparison in this file is measured against (bc-drwx item 11: shared by
// improveDojoMeetings' exchange pass and assignUnseededByDojoTree's forward
// descent, which used to keep two independently-built footprint maps that
// could drift on what "how many of this dojo, in total" means). totalMembers
// is counted from `pools`' current occupants plus `extra` -- non-nil only
// for assignUnseededByDojoTree, whose still-to-place unseeded slice has not
// reached `pools` yet at the point it needs the optimum; improveDojoMeetings
// runs after every player already has a pool, so its own call passes nil.
//
// The footprint is a []int indexed by dense dojo id (bc-pnum: was a
// map[string]int keyed by dojoKey, in the hot excessOf/optimum call path of
// improveDojoMeetings' exchange loop, so its lookup was part of the
// mapaccess2_faststr cost that motivated dojoIDCache -- see that type's own
// doc comment). ids must already have interned every dojo in `pools` and
// `extra` BEFORE this call: footprint is sized to ids.numDojos() once, up
// front, and a caller that lets ids mint a NEW id while this function's own
// two loops run would index past the end of it. Every production caller
// satisfies this by interning the whole roster at buildPoolPhaseTreeAwareCore
// entry, before pool formation starts.
//
// Two spellings of one dojo ("Mumeishi"/"mumeishi") already resolve to the
// SAME id via ids.of (see dojoIDCache's own doc comment), so they still
// accumulate into one footprint entry rather than two half-sized ones.
//
// No blank-dojo guard (bc-drwx item 11): every caller of this function is
// reached only after buildPoolPhaseTreeAwareCore's ValidateNoBlankDojo
// pre-flight has already refused the whole roster if any player's Dojo is
// blank, so a blank entry here is unreachable, not merely rare.
func dojoFootprintOptimum(pools []Pool, extra []Player, numPools int, ids dojoIDCache) func(id int) int {
	footprint := make([]int, ids.numDojos())
	for i := range pools {
		for _, pl := range pools[i].Players {
			footprint[ids.of(pl.Dojo)]++
		}
	}
	for _, p := range extra {
		footprint[ids.of(p.Dojo)]++
	}
	return func(id int) int {
		if numPools <= 0 {
			return 0
		}
		return (footprint[id] + numPools - 1) / numPools
	}
}

// improveDojoMeetings is the multi-dojo repair loop described at its call
// site: a WINNER-PATH-ONLY pairwise exchange pass over the pools the
// descent (assignUnseededByDojoTree) already placed. pairRound is built
// from qualifierSlots reduced to each pool's WINNER (rank-1) leaf alone
// (winnerSlots below) rather than the full qualifierSlots a pool with
// poolWinners>1 also carries runner-up/crossed-in leaves for -- the same
// metric the descent itself optimises, and the one the operator ruled the
// ship/keep decision on (a same-dojo collision through runner-up CROSSING
// is accepted chance, not a defect either stage owes a fix for).
//
// Objective is FOUR-tier lexicographic (bc-drwx item 7 corrected this doc,
// which used to describe only the middle two tiers and a flat per-pool cap
// precondition the code had already replaced with tier (a)'s delta check --
// see `better`'s own doc comment for the authoritative statement): (a) total
// spread-cap excess (sum over every (pool, dojo) of how far that pool sits
// over the dojo's per-pool optimum ceil(total/numPools)) must never
// increase; (b) the number of multi-pool dojos whose earliest WINNER-PATH
// meeting is round 1; (c) the negated sum of finite WINNER-PATH meeting
// rounds; (d) the all-qualifier best-effort (allQualPairRound), a PURE
// TIE-BREAK that only ever decides a comparison where (a)-(c) are exactly
// tied and can never veto a swap those tiers already prefer. Strictly
// decreasing (or, for tier (d), non-increasing) on every accepted exchange,
// so the loop terminates.
//
// An exchange moves one unseeded player of a round-1 dojo out of one of its
// pools in return for an unseeded player of a different dojo, and is legal
// only when afterwards neither exchanged dojo's earliest WINNER-PATH
// meeting got EARLIER -- the one hard precondition alongside the objective
// itself (the operator's ruling; the all-qualifier tier (d) does NOT get
// the same hard veto, see `better`'s call site). NO three-way rotation: a
// pairwise stall simply stops the loop (see the file-level doc comment for
// the measured dominance this simplification still achieves over the old
// pairwise+rotation repair it replaces).
//
// Takes only `pools` and `qualifierSlots` (bc-drwx item 11): the
// `targetSizes` parameter this function used to also take was never read in
// its body, and the `roster []Player` parameter it used to build its own
// footprint map from is redundant with `pools` -- by the time this runs
// (buildPoolPhaseTreeAwareCore, AFTER assignUnseededByDojoTree has placed
// every player), `pools`' membership already IS the whole roster.
// dojoFootprintOptimum derives the same optimum from `pools` alone.
func improveDojoMeetings(pools []Pool, qualifierSlots [][]int, ids dojoIDCache) {
	// rosterSize replaces the old `roster []Player` parameter's len() for
	// map pre-allocation sizing and the pass-count cap below; both are
	// sizing hints only, so summing pool membership (rather than a real
	// player list) is exactly as good.
	rosterSize := 0
	for i := range pools {
		rosterSize += len(pools[i].Players)
	}
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
	numPools := len(pools)
	optimum := dojoFootprintOptimum(pools, nil, numPools, ids)

	// numDojos is the dense id space's size: `ids` must already have
	// interned every dojo in `pools` before this point (the caller,
	// buildPoolPhaseTreeAwareCore, interns the whole roster up front -- see
	// dojoIDCache's own doc comment), so this is stable for the rest of the
	// function and every []int/[][]int below can be sized to it once.
	numDojos := ids.numDojos()

	// counts[i][id] is pool i's current member count for dojo id, maintained
	// incrementally by every speculative and accepted swap below (bc-drwx
	// review fix, then bc-pnum's int-id rewrite: this was a
	// []map[string]int, and cAi/cAj/cBi/cBj plus every earliestDojoMeeting
	// call's own occupied-pool scan paid a map[string]* lookup
	// (mapaccess2_faststr) per candidate on top of the identity lookup
	// dojoKeyCache already memoized -- profiled at 51% cumulative in
	// BuildPoolPhaseTreeAware_256_16x16_Interleaved. A plain []int indexed
	// by dense id removes that lookup entirely, leaving integer indexing).
	// countIn below is the O(1) replacement threaded through both the direct
	// cAi/cAj/cBi/cBj reads and every earliestDojoMeeting/cachedDojoMeeting
	// call in this function.
	//
	// poolDojoIDs[i][k] is the dense dojo id of pools[i].Players[k] (bc-pnum
	// follow-up): the int-id rewrite above still called ids.of(a.Dojo) once
	// per ai and ids.of(b.Dojo) once per CANDIDATE -- on the order of
	// numPools^2*poolSize^2 times for bk alone -- and dojoIDCache.of does
	// TWO map probes per call (dojoKeyCache.of's raw->normalized lookup,
	// then its own normalized->id lookup), which a follow-up profile still
	// showed as a measurable cost even after the per-pool tallies stopped
	// being map-keyed. poolDojoIDs removes it: built once here alongside
	// counts, and moved together with it by the ONE `exchange` closure
	// below (bc-pnum review: this used to be a hand-mirrored update at each
	// of two call sites, which is exactly the shape that let a half-update
	// slip through the whole test suite green -- see exchange's own doc
	// comment), a candidate's ak/bk become a plain slice index instead of a
	// cache lookup.
	counts := make([][]int, numPools)
	poolDojoIDs := make([][]int, numPools)
	for i := range pools {
		counts[i] = make([]int, numDojos)
		poolDojoIDs[i] = make([]int, len(pools[i].Players))
		for k, pl := range pools[i].Players {
			id := ids.of(pl.Dojo)
			counts[i][id]++
			poolDojoIDs[i][k] = id
		}
	}
	countIn := dojoCounter(func(poolIdx int, id int) int {
		return counts[poolIdx][id]
	})

	// exchange swaps the players CURRENTLY at (i, ai) and (j, bi), moving
	// counts and poolDojoIDs with them (bc-pnum review, G1): pools, counts
	// and poolDojoIDs must always move together, and a hand-written pair of
	// call sites -- one for "do the swap", a second, separately typed-out
	// one for "undo it" -- is exactly the shape that lets a half-update
	// (updating only one of the three structures, or only one side of a
	// pair) slip through unnoticed: the whole helper suite stayed green
	// under a mutation that dropped one poolDojoIDs write, because nothing
	// short of an end-of-function invariant check ever re-derives
	// poolDojoIDs from `pools` to catch the drift.
	//
	// Self-inverse BY CONSTRUCTION, not by a hand-mirrored copy of the
	// deltas: it reads whatever ids currently occupy the two slots (x, y)
	// FIRST, then swaps, so calling it a second time on the SAME (i, ai, j,
	// bi) reads back the post-swap ids and swaps again, restoring every one
	// of the three structures to exactly its pre-call state. The revert
	// below is therefore the identical call, not a second, independently
	// maintained mirror of it -- there is only one place this operation is
	// written down.
	exchange := func(i, ai, j, bi int) {
		x, y := poolDojoIDs[i][ai], poolDojoIDs[j][bi]
		pools[i].Players[ai], pools[j].Players[bi] = pools[j].Players[bi], pools[i].Players[ai]
		poolDojoIDs[i][ai], poolDojoIDs[j][bi] = y, x
		counts[i][x]--
		counts[i][y]++
		counts[j][y]--
		counts[j][x]++
	}

	// excessOf is dojo id's contribution to tier (a) from a single pool
	// holding `count` of it: how far over its per-pool optimum that one
	// pool sits, floored at 0 (a pool at or under optimum contributes
	// nothing -- only OVER-cap pools count, never under).
	excessOf := func(id int, count int) int {
		if over := count - optimum(id); over > 0 {
			return over
		}
		return 0
	}

	// totalExcess is tier (a): sum over every (pool, dojo) pair of that
	// pool's excess for that dojo. This is the operator's spread tier --
	// see this function's own doc comment for why it must lead the other
	// three tiers, and dojoNode's doc comment (pool_distribution_tree_aware.go)
	// for the descent-side guard this backstops for the one placement
	// order the descent's own forward-only guard cannot reach (the
	// roster's literal last remaining seat). Reads `counts` directly
	// (bc-drwx review fix) instead of re-scanning every pool's Players --
	// `counts` is already the authoritative per-pool tally, kept in sync by
	// every swap below, so re-deriving it here via dojoKey per player was
	// pure waste. Walks every id in 0..numDojos (bc-pnum: `counts[i]` is now
	// a dense []int rather than a map with one entry per dojo ACTUALLY IN
	// this pool), which excessOf(id, 0) correctly scores as 0 excess for
	// every dojo absent from pool i -- this only runs once per PASS, not per
	// candidate, so the extra zero-entries are not the loop this bead
	// targeted.
	totalExcess := func() int {
		total := 0
		for i := range pools {
			for id, c := range counts[i] {
				total += excessOf(id, c)
			}
		}
		return total
	}

	// scanBuf is earliestDojoMeetingScan's reusable occupied-pool scratch
	// slice (bc-pnum), shared across every call EITHER objective() or the
	// exchange loop below makes: the exchange loop alone calls
	// earliestDojoMeetingScan on the order of numPools^2*poolSize^2 times,
	// and a fresh make([]int, 0, numPools) per call (earliestDojoMeeting's
	// own throwaway-buffer behaviour) was, once dojoIDCache had already
	// removed the map[string]* lookup, the next-largest measured cost --
	// mostly paid back as GC scanning rather than the allocation itself.
	// Declared here, ABOVE objective (bc-pnum follow-up: objective() used
	// to call the throwaway-buffer earliestDojoMeeting wrapper instead,
	// which is what its own doc comment used to describe as having no
	// production caller -- wrong, since objective() itself is one, twice
	// per DISTINCT DOJO per pass (guarded by seen[id] below, not twice per
	// pass flat), so objective() can share it too. Every call is
	// synchronous and objective() always runs to completion BEFORE the
	// exchange loop below starts for that pass (never interleaved with
	// it), so no two scans are ever in flight on this buffer at once, and
	// nothing reads *occupied after the call that filled it returns --
	// one growing slice is safe to reuse for the whole function.
	var scanBuf []int

	// objective() itself calls earliestDojoMeetingScan directly (sharing
	// scanBuf above) rather than the cachedDojoMeeting wrapper (bc-drwx
	// item 13, noted rather than changed: it runs at the TOP of each pass,
	// before winnerMeetCache/allQualMeetCache are cleared a few lines below
	// at the call site, so routing it through THAT cache would need the
	// clear() calls moved AHEAD of this call instead of after it -- a real
	// reordering, not the loop-invariant code motion applied to cAi/cAj
	// above, and the measured cost here is one whole-roster pass per OUTER
	// iteration of the climb (bounded by the number of accepted swaps, not
	// by candidate count), a much smaller multiplier than the
	// candidate-scan cost the wave-2 slotBest memo (seed.go) was written to
	// fix. Left alone rather than restructured for a marginal win.
	objective := func() (excess, roundOnes, negSum, allQualNegSum int) {
		excess = totalExcess()
		seen := make([]bool, numDojos)
		for i := range pools {
			// id read from poolDojoIDs (bc-pnum review), not
			// ids.of(pl.Dojo): poolDojoIDs is already in scope and kept in
			// lockstep with pools' membership by every exchange (see
			// exchange's own doc comment above), so this is what makes "no
			// map probe anywhere in the exchange pass" a structural
			// property of the code rather than something only true by
			// measurement on the shapes profiled so far.
			for k := range pools[i].Players {
				id := poolDojoIDs[i][k]
				if seen[id] {
					continue
				}
				seen[id] = true
				if m := earliestDojoMeetingScan(pools, pairRound, id, countIn, &scanBuf); m != math.MaxInt {
					if m <= 1 {
						roundOnes++
					}
					negSum -= m
				}
				if m := earliestDojoMeetingScan(pools, allQualPairRound, id, countIn, &scanBuf); m != math.MaxInt {
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

	// Per-pass memoization for earliestDojoMeeting, keyed by dense dojo id --
	// see the cache-clearing comment inside the pass loop for why a single
	// pair of slices reused (and cleared) across passes is safe here.
	// known/cache are matched pairs (bc-pnum: were map[string]int) so a
	// miss is a bool check rather than a second map probe, and clear() on a
	// bool slice is cheaper than clearing a map of the same size.
	winnerMeetKnown := make([]bool, numDojos)
	winnerMeetCache := make([]int, numDojos)
	allQualMeetKnown := make([]bool, numDojos)
	allQualMeetCache := make([]int, numDojos)

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
	for pass := 0; pass < rosterSize*numPools+1; pass++ {
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
		clear(winnerMeetKnown)
		clear(allQualMeetKnown)
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
				// ak (bc-drwx review fix, then bc-pnum's int-id rewrite,
				// then the poolDojoIDs follow-up): a's dense id read
				// straight out of poolDojoIDs rather than re-resolved via
				// ids.of(a.Dojo) -- see poolDojoIDs' own doc comment
				// (above, where `counts` is built) for why the cache
				// lookup itself was still a measurable cost. Reused for
				// every countIn/cachedDojoMeeting call below that needs a's
				// identity -- see dojoCounter's own doc comment for why
				// passing the raw string through instead would have put an
				// O(numPools) run of cache lookups back inside
				// earliestDojoMeeting's occupied-pool scan.
				ak := poolDojoIDs[i][ai]
				beforeA := cachedDojoMeeting(pools, pairRound, ak, winnerMeetKnown, winnerMeetCache, countIn, &scanBuf)
				beforeAQA := cachedDojoMeeting(pools, allQualPairRound, ak, allQualMeetKnown, allQualMeetCache, countIn, &scanBuf)
				hasMeetingSignal := beforeA != math.MaxInt
				// cAi (bc-drwx item 13: hoisted, same reasoning as beforeA/
				// beforeAQA above -- it depends only on pools[i] and a.Dojo,
				// both loop-invariant across the whole (j, bi) scan below,
				// so recomputing it per candidate b bought nothing). Reused
				// directly for hasExcessSignal rather than calling
				// countDojoInPool a second time for the same answer.
				cAi := countIn(i, ak) // includes a
				hasExcessSignal := excessOf(ak, cAi) > 0
				if !hasMeetingSignal && !hasExcessSignal {
					continue // nothing an exchange could ever improve for this dojo, from THIS pool
				}
				for j := 0; j < numPools && !improved; j++ {
					if j == i {
						continue
					}
					// cAj (bc-drwx item 13: hoisted to the j level -- it
					// depends on pools[j] and a.Dojo, both loop-invariant
					// across the bi scan below for this j, unlike cBj/cBi
					// which depend on b.Dojo and must stay per-candidate).
					cAj := countIn(j, ak) // a not in j yet
					for bi := 0; bi < len(pools[j].Players) && !improved; bi++ {
						b := pools[j].Players[bi]
						if b.Seed > 0 {
							continue
						}
						// bk (bc-drwx review fix, then bc-pnum's int-id
						// rewrite, then the poolDojoIDs follow-up): b's
						// dense id read straight out of poolDojoIDs,
						// mirroring ak above -- this is the read that used
						// to run once per CANDIDATE (numPools^2*poolSize^2
						// scale), so it is the one this follow-up mattered
						// most for.
						bk := poolDojoIDs[j][bi]
						if bk == ak {
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
						cBj := countIn(j, bk) // includes b
						cBi := countIn(i, bk) // b not in i yet
						beforeExc := excessOf(ak, cAi) + excessOf(ak, cAj) + excessOf(bk, cBi) + excessOf(bk, cBj)
						afterExc := excessOf(ak, cAi-1) + excessOf(ak, cAj+1) + excessOf(bk, cBi+1) + excessOf(bk, cBj-1)
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
						beforeB := cachedDojoMeeting(pools, pairRound, bk, winnerMeetKnown, winnerMeetCache, countIn, &scanBuf)
						beforeAQB := cachedDojoMeeting(pools, allQualPairRound, bk, allQualMeetKnown, allQualMeetCache, countIn, &scanBuf)
						// exchange moves pools/counts/poolDojoIDs together,
						// speculatively (bc-pnum review, G1 -- see that
						// closure's own doc comment for why this is ONE
						// call rather than a hand-mirrored update): every
						// read below this point (afterA/afterB/afterAQA/
						// afterAQB, via countIn, and any LATER candidate's
						// ak/bk read via poolDojoIDs) must see the
						// POST-swap tally/identity, and the revert a few
						// lines down is the SAME call, self-inverse.
						exchange(i, ai, j, bi)
						afterA := earliestDojoMeetingScan(pools, pairRound, ak, countIn, &scanBuf)
						afterB := earliestDojoMeetingScan(pools, pairRound, bk, countIn, &scanBuf)
						afterAQA := earliestDojoMeetingScan(pools, allQualPairRound, ak, countIn, &scanBuf)
						afterAQB := earliestDojoMeetingScan(pools, allQualPairRound, bk, countIn, &scanBuf)
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
						// bc-drwx item 7: the all-qualifier (tier d) "never
						// earlier" terms used to be ANDed in here as a HARD
						// PRECONDITION alongside the winner-path guard,
						// which let tier (d) VETO a swap tiers (a)-(c)
						// preferred (repro: a swap moving dojo X's
						// winner-path meeting from round 1 to 3 was refused
						// because dojo Y's all-qualifier crossing moved
						// from round 6 to 1, even though nothing about
						// tiers (a)-(c) objected). Tier (d) is a PURE
						// TIE-BREAK (this function's own doc comment: "it
						// never overrides (a)-(c): a swap tier (c) already
						// prefers is taken regardless of what tier (d)
						// thinks of it") and better() already encodes that
						// -- newAQ only ever decides a comparison where
						// tiers (a)-(c) are EXACTLY tied. The winner-path
						// "never earlier" guard (afterA/afterB) stays a
						// hard precondition: that one IS the ruling,
						// unlike the all-qualifier best-effort.
						if afterA >= beforeA && afterB >= beforeB &&
							better(newExc, newR1, newNS, newAQ, curExc, curR1, curNS, curAQ) {
							improved = true
							break
						}
						// revert: exchange is self-inverse (see its own doc
						// comment), so calling it again on this same
						// (i, ai, j, bi) undoes pools, counts AND
						// poolDojoIDs together -- not a second,
						// hand-mirrored copy of the deltas above.
						exchange(i, ai, j, bi)
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
	case QualifierModeLargerPools:
		overrides := extraQualifierOverridesFromSizes(postSizes, mode.MinPoolSize, poolWinners)
		postSlots = poolQualifierPathsPerPool(postSizes, poolWinners, overrides, numCourts)
	case QualifierModeFillBracket:
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

// QualifiersForOversizedPool is the larger-pools "oversized pool sends one
// extra qualifier" rule (bc-qual): a pool sends poolWinners+1 qualifiers
// exactly when it is OVERSIZED (size > minPoolSize), else poolWinners
// unchanged. minPoolSize <= 0 means there is no minimum to be over, so no
// pool is ever oversized (degrades to the uniform poolWinners count) rather
// than marking every pool oversized.
//
// Shared (bc-drwx item 13) by state.Competition.QualifiersForPool
// (internal/state/models.go, POST-placement, from a real pool's
// participant count) and extraQualifierOverridesFromSizes below
// (PRE-placement, from a target size alone) so the two -- previously two
// independent implementations of the identical rule, one per package --
// can never drift on what "oversized" means. internal/state already
// imports internal/helper (Competition.QualifiersForPool's own helper.Pool
// parameter), so this is the one direction sharing can go; helper cannot
// import state back (see qualifierMode's own doc comment).
func QualifiersForOversizedPool(size, minPoolSize, poolWinners int) int {
	if minPoolSize > 0 && size > minPoolSize {
		return poolWinners + 1
	}
	return poolWinners
}

// extraQualifierOverridesFromSizes applies QualifiersForOversizedPool
// pre-placement, from target SIZES alone: pool i is oversized when
// sizes[i] > minPoolSize. This is exact, not an approximation --
// QualifiersForPool's own oversized test only ever reads a pool's
// participant COUNT, which is exactly what sizes[i] already promises pool i
// will end up holding once distribution finishes (both the old fill+repair
// pipeline and this one guarantee every pool's FINAL size equals its target
// size; nobody is ever short or over).
//
// minPoolSize <= 0 returns nil (no minimum to be over) rather than an
// empty-but-present map: QualifiersForOversizedPool would never actually
// populate one in that case either (every pool degrades to the uniform
// poolWinners, so the `w != poolWinners` check below never fires), but the
// early return states the "no minimum" case as itself rather than relying
// on that being the loop's incidental behaviour.
func extraQualifierOverridesFromSizes(sizes []int, minPoolSize, poolWinners int) map[int]int {
	if minPoolSize <= 0 {
		return nil
	}
	var overrides map[int]int
	for i, sz := range sizes {
		if w := QualifiersForOversizedPool(sz, minPoolSize, poolWinners); w != poolWinners {
			if overrides == nil {
				overrides = make(map[int]int, len(sizes))
			}
			overrides[i] = w
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
	// dojoCount is indexed by dense dojo id (dojoIDCache, bc-pnum: was a
	// map[string]int). recordDojoOccupancy/chooseDojoTreePool are called
	// once per placed player times its root-to-leaf depth, so a
	// map[string]* lookup (mapaccess2_faststr) per node visited was real,
	// measured cost -- a plain []int removes it. Sized to numDojos at build
	// time (buildDojoTree/buildDojoTreeRec) for every node, leaf and
	// internal alike, since every node in the tree can in principle see
	// every dojo.
	dojoCount []int
	capacity  int
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
//
// numDojos sizes every node's dojoCount []int (bc-pnum): the caller's
// dojoIDCache must already have interned every dojo that will ever be
// recorded against this tree (buildPoolPhaseTreeAwareCore interns the whole
// roster before this is called) -- see dojoIDCache's own doc comment for why
// minting an id after this sizing would index past the end of it.
func buildDojoTree(qualifierSlots [][]int, targetSizes []int, placed []int, numDojos int) (*dojoNode, int) {
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
	root := buildDojoTreeRec(poolAtSlot, targetSizes, placed, totalBits, 0, numDojos)
	return root, totalBits
}

// buildDojoTreeRec recurses MSB-first: bitsLeft counts down from totalBits
// to 0 as prefix accumulates the winner slot's bits high-to-low, so the
// FIRST split (root's two children) fixes the slot number's highest bit --
// exactly the bit dojoMeetRound (seed.go) says two slots must differ in to
// meet only in the final -- and the split floor (bitsLeft == 0) lands on one
// specific slot number, i.e. at most one real pool.
func buildDojoTreeRec(poolAtSlot map[int]int, targetSizes, placed []int, bitsLeft, prefix, numDojos int) *dojoNode {
	if bitsLeft == 0 {
		n := &dojoNode{poolIdx: -1, dojoCount: make([]int, numDojos)}
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
	left := buildDojoTreeRec(poolAtSlot, targetSizes, placed, bitsLeft-1, prefix<<1, numDojos)
	right := buildDojoTreeRec(poolAtSlot, targetSizes, placed, bitsLeft-1, (prefix<<1)|1, numDojos)
	poolIndices := make([]int, 0, len(left.poolIndices)+len(right.poolIndices))
	poolIndices = append(poolIndices, left.poolIndices...)
	poolIndices = append(poolIndices, right.poolIndices...)
	return &dojoNode{
		left: left, right: right, poolIdx: -1,
		dojoCount:   make([]int, numDojos),
		capacity:    left.capacity + right.capacity,
		poolCount:   left.poolCount + right.poolCount,
		roomPools:   left.roomPools + right.roomPools,
		poolIndices: poolIndices,
	}
}

// recordDojoOccupancy walks root-to-leaf along winnerSlot's own bit path
// (the identical path buildDojoTreeRec assigned that slot to) and updates
// every node on the way: dojoCount[id]++ always, capacity += capacityDelta.
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
//
// id is the caller's already-resolved dense dojo id (dojoIDCache, bc-pnum):
// this used to take the raw dojo string and a dojoKeyCache and normalize
// here, but every call site already has an id on hand by the time it calls
// this (assignUnseededByDojoTree resolves it once per player), so passing
// the string through again would have cost a second cache lookup for no
// reason. Two spellings of one dojo ("Mumeishi"/"mumeishi") already resolve
// to the SAME id (dojoIDCache.of), so they still accumulate into the SAME
// node count instead of two separate, half-sized ones -- chooseDojoTreePool
// and pickDojoTreeAwarePool read the same id back.
func recordDojoOccupancy(root *dojoNode, id, winnerSlot, totalBits, capacityDelta int) {
	node := root
	bitsLeft := totalBits
	for {
		node.dojoCount[id]++
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
//
// id is the caller's already-resolved dense dojo id (dojoIDCache, bc-pnum),
// matching recordDojoOccupancy's own write -- see that function's doc
// comment for why the resolution happens once at the caller rather than
// here.
func chooseDojoTreePool(root *dojoNode, id int, qualifierSlots [][]int, dojoPoolIndices []int) int {
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
			lc, rc := l.dojoCount[id]*r.poolCount, r.dojoCount[id]*l.poolCount
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
// A dojo with NOBODY placed anywhere yet (root == nil, or
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
func pickDojoTreeAwarePool(pools []Pool, targetSizes []int, root *dojoNode, dojo string, id int, qualifierSlots [][]int, keys dojoKeyCache) int {
	// id is the caller's already-resolved dense dojo id (dojoIDCache,
	// bc-pnum), matching recordDojoOccupancy's own write. dojo (the raw
	// string) is still needed alongside it: leastConflictedPool and
	// countDojoInPool below stay string-based, since they are not on this
	// bead's hot path (see those functions' own doc comments).
	//
	// No `dojo == ""` guard (bc-drwx item 11): every caller is reached only
	// after buildPoolPhaseTreeAwareCore's ValidateNoBlankDojo pre-flight has
	// already refused a roster with any blank dojo, so a blank dojo here is
	// unreachable -- see ErrBlankDojoInDraw's own doc comment.
	if root == nil || root.dojoCount[id] == 0 {
		return leastConflictedPool(pools, targetSizes, dojo, keys)
	}
	dojoPoolIndices := make([]int, 0, 4)
	for idx := range pools {
		if countDojoInPool(pools[idx], dojo, keys) > 0 {
			dojoPoolIndices = append(dojoPoolIndices, idx)
		}
	}
	if best := chooseDojoTreePool(root, id, qualifierSlots, dojoPoolIndices); best >= 0 {
		return best
	}
	// The tree found no room (a bye-heavy corner, or a pool this roster's
	// mode left out of the tree entirely, per buildDojoTree's own doc
	// comment) -- leastConflictedPool still has the full, real pool list to
	// fall back on.
	return leastConflictedPool(pools, targetSizes, dojo, keys)
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
// No blank-dojo guards (bc-drwx item 11: this function used to carry three
// -- around the seed-occupancy recording loop, the per-player cap check,
// and the live-placement recordDojoOccupancy call -- one per p.Dojo/pl.Dojo
// read below). Every caller is reached only after
// buildPoolPhaseTreeAwareCore's ValidateNoBlankDojo pre-flight has already
// refused a roster with any blank dojo, so a blank dojo anywhere in `pools`
// or `unseeded` is unreachable -- see ErrBlankDojoInDraw's own doc comment.
//
// ids (bc-pnum) must already have interned every dojo in `pools` and
// `unseeded` before this call -- see dojoIDCache's own doc comment for why;
// buildPoolPhaseTreeAwareCore interns the whole roster before calling this.
// A per-player id is resolved ONCE per loop iteration below (mirroring the
// hoisting already used throughout this file) and threaded into
// pickDojoTreeAwarePool/recordDojoOccupancy/dojoOptimum instead of each of
// those re-resolving it from the raw string.
func assignUnseededByDojoTree(pools []Pool, targetSizes []int, unseeded []Player, qualifierSlots [][]int, keys dojoKeyCache, ids dojoIDCache) error {
	placed := make([]int, len(pools))
	for i := range pools {
		placed[i] = len(pools[i].Players)
	}
	root, totalBits := buildDojoTree(qualifierSlots, targetSizes, placed, ids.numDojos())
	if root != nil {
		for i := range pools {
			if i >= len(qualifierSlots) || len(qualifierSlots[i]) == 0 {
				continue
			}
			for _, pl := range pools[i].Players {
				recordDojoOccupancy(root, ids.of(pl.Dojo), qualifierSlots[i][0], totalBits, 0)
			}
		}
	}

	numPools := len(pools)
	// dojoFootprintOptimum (bc-drwx items 3 and 11, then bc-pnum's int-id
	// rewrite): shared with improveDojoMeetings so the two can never
	// independently drift on what "how many of this dojo, in total" means,
	// and indexed by dense id so two spellings of one dojo count toward the
	// SAME cap. `unseeded` is the extra slice: at this point those players
	// are not in `pools` yet.
	dojoOptimum := dojoFootprintOptimum(pools, unseeded, numPools, ids)

	for _, p := range unseeded {
		id := ids.of(p.Dojo)
		best := pickDojoTreeAwarePool(pools, targetSizes, root, p.Dojo, id, qualifierSlots, keys)
		if best < 0 {
			// Cannot happen when sum(targetSizes) == len(players), which
			// realTargetSizes guarantees; kept as a defensive error rather
			// than a panic or a silently dropped player.
			return fmt.Errorf("cannot place player %s: no pool has room", p.Name)
		}
		if countDojoInPool(pools[best], p.Dojo, keys) >= dojoOptimum(id) {
			if alt := poolUnderDojoCap(pools, targetSizes, p.Dojo, dojoOptimum(id), keys); alt >= 0 {
				best = alt
			}
		}
		p.PoolPosition = int64(len(pools[best].Players) + 1)
		pools[best].Players = append(pools[best].Players, p)
		if root != nil && best < len(qualifierSlots) && len(qualifierSlots[best]) > 0 {
			recordDojoOccupancy(root, id, qualifierSlots[best][0], totalBits, -1)
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
func poolUnderDojoCap(pools []Pool, targetSizes []int, dojo string, dojoCap int, keys dojoKeyCache) int {
	masked := append([]int(nil), targetSizes...)
	for i := range pools {
		if countDojoInPool(pools[i], dojo, keys) >= dojoCap {
			masked[i] = len(pools[i].Players)
		}
	}
	return leastConflictedPool(pools, masked, dojo, keys)
}
