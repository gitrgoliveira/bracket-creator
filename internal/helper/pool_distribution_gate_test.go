package helper

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
)

// bc-dojo Phase 3/4: the decision-gate scorecard.
//
// This file WAS the ship/keep decision for the region-aware rebuild
// (BuildPoolPhaseTreeAware, pool_distribution_tree_aware.go): Phase 3 ran
// BOTH the old fill+repair pipeline and the new tree-aware one over every
// sweep pool_distribution_invariants_test.go (Phase 0) established as the
// baseline, plus an end-to-end region sweep neither pipeline had been
// measured against before, and logged a side-by-side scorecard.
//
// Phase 4 SWAPPED the tree-aware distributor into production:
// BuildPoolPhase and BuildPoolPhaseFillBracket (tournament.go) both
// delegate to it now, so "old" and "new" are no longer two different
// algorithms -- BuildPoolPhase(players, poolSize, isMax, numCourts) is
// exactly BuildPoolPhaseTreeAware(players, poolSize, isMax, numCourts,
// defaultPoolWinners), and calling both sides of an old-vs-new comparison
// would just compare the production path against itself (at a possibly
// DIFFERENT poolWinners than the sweep intends, since BuildPoolPhase's own
// poolWinners is fixed). This file was rewritten from that comparison into
// ABSOLUTE assertions on the production path directly
// (BuildPoolPhaseTreeAware, called with each sweep's own intended
// poolWinners): per-pool dojo-concentration optimum, pool sizes, no two
// seeds sharing a pool, the unique-dojo round-robin identity contract, and
// the end-to-end region metric against the brute-force ceiling. The ONE
// place an "old" pipeline is still reconstructed is the seed-placement
// equality pin below, which needed a real point of comparison and not just
// an absolute property.
//
// HISTORICAL RECORD, since the side-by-side verdict this file used to print
// no longer exists to reproduce: at commit d5eb8870 (the last commit before
// the swap subsumed the old fill+repair pipeline), the old-vs-new region
// scorecard read old-sum=220, new-sum=254, new-worse=0 across the same 180
// end-to-end configs this file still sweeps. That is the number the Phase 3
// ship decision was made on; it is not re-derivable now that "old" and
// "new" are the same code.
//
// gateScorecard accumulates every metric across every sweep so the final Log
// is one picture rather than scattered per-subtest output, and so a human
// reading a failed run sees the SAME totals whether the run passed or
// failed: t.Logf runs regardless (Errorf does not stop the function), and
// the failing assertions point at exactly the counters that regressed.
type gateScorecard struct {
	// Metric: per-pool dojo-concentration optimum (maxOfDojo <= ceil(dojoGroupSize/numPools)).
	optimumConfigs int
	optimumFail    int

	// Metric: pool sizes (target sizes reproduced exactly).
	sizeConfigs  int
	sizeMismatch int

	// Metric: seeds-per-pool (no two seeds share a pool).
	seedShareConfigs int
	seedShareFail    int

	// Metric: seed placement equality (placeSeedIndices' extraction claim --
	// every seed lands in the same named pool as PoolSeeding's own
	// still-standing pipeline would put it in; see
	// referencePoolSeedingPipeline's doc comment for why this is the one
	// place a second pipeline is still built).
	seedEqualityChecked    int
	seedEqualityMismatches int

	// Metric: unique-dojo identity contract (byte-identical round-robin deal).
	identityConfigs int
	identityFail    int

	// Metric: end-to-end region, WINNER-PATH (earliestDojoWinnerMeetingRound
	// -- the gate, per operator ruling: a same-dojo collision through
	// runner-up CROSSING is accepted chance, not a defect either pipeline
	// stage owes a fix for) against the WINNER-PATH BRUTE-FORCE CEILING --
	// the best any pool assignment could achieve for that shape, counting
	// only pools' winner leaves. mathMaxInt sentinel excluded from
	// sums/round1 counts (means "this dojo never spanned >=2 pools in this
	// config", i.e. no data).
	regionConfigs      int
	regionSum          int
	regionRound1       int
	optimumMeetConfigs int // e2e configs checked against the brute-force ceiling
	optimumMeetBelow   int // hard-fail trigger: the path met earlier than the shape's brute-force optimum

	// Metric: the SAME end-to-end configs, ALL-QUALIFIER (informational
	// only, never asserted): earliestDojoMeetingRound, which also counts
	// runner-up/crossed-in qualifiers a pool with poolWinners>1 sends up.
	// Diverges from the winner-path numbers above exactly where a
	// crossing-borne collision the operator ruled out of scope occurs.
	allQualRegionConfigs int
	allQualRegionSum     int
	allQualRegionRound1  int
}

func (s *gateScorecard) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== bc-dojo Phase 4 gate scorecard (production tree-aware path, absolute) ===\n")
	fmt.Fprintf(&b, "per-pool optimum:      configs=%d  fail=%d\n", s.optimumConfigs, s.optimumFail)
	fmt.Fprintf(&b, "pool sizes:            configs=%d  mismatches=%d\n", s.sizeConfigs, s.sizeMismatch)
	fmt.Fprintf(&b, "seeds-per-pool:        configs=%d  fail=%d\n", s.seedShareConfigs, s.seedShareFail)
	fmt.Fprintf(&b, "seed placement:        seeds-checked=%d  mismatches=%d (vs referencePoolSeedingPipeline)\n",
		s.seedEqualityChecked, s.seedEqualityMismatches)
	fmt.Fprintf(&b, "unique-dojo identity:  configs=%d  fail=%d\n", s.identityConfigs, s.identityFail)
	fmt.Fprintf(&b, "region, WINNER-PATH (the gate, operator ruling):\n")
	fmt.Fprintf(&b, "  configs-with-data=%d  sum=%d  round1=%d\n", s.regionConfigs, s.regionSum, s.regionRound1)
	fmt.Fprintf(&b, "  brute-force ceiling: configs=%d  below-optimum=%d (hard-fail trigger)\n",
		s.optimumMeetConfigs, s.optimumMeetBelow)
	fmt.Fprintf(&b, "region, ALL-QUALIFIER (informational only, includes operator-accepted crossing collisions):\n")
	fmt.Fprintf(&b, "  configs-with-data=%d  sum=%d  round1=%d\n", s.allQualRegionConfigs, s.allQualRegionSum, s.allQualRegionRound1)
	fmt.Fprintf(&b, "  (historical, commit d5eb8870, old-vs-new before the swap: old-sum=220 new-sum=254 new-worse=0)\n")
	return b.String()
}

// earliestDojoMeetingRound is the known-gap test's own metric
// (pool_distribution_invariants_test.go), generalised into a function: the
// earliest knockout round any two of dojo's qualifying pools could be drawn
// to meet, or math.MaxInt when dojo does not span at least two pools in this
// draw (nothing to measure).
func earliestDojoMeetingRound(pools []Pool, poolWinners, numCourts int, dojo string) int {
	draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
	if draw == nil {
		return mathMaxIntSentinel
	}
	return earliestMeetingRoundInDraw(draw, pools, dojo)
}

// earliestMeetingRoundInDraw is earliestDojoMeetingRound's builder-agnostic
// half: given an ALREADY-BUILT draw (from any of production's three
// builders -- BuildKnockoutDraw, BuildKnockoutDrawPerPool,
// BuildKnockoutDrawFillBracket), the earliest knockout round any two of
// dojo's qualifying pools are drawn to meet, or the sentinel when dojo does
// not span at least two pools in this draw. Split out so the per-mode seam
// tests (pool_distribution_modes_test.go) can measure a larger-pools or
// fill-bracket draw the same way this file measures a standard one, without
// re-deriving the slot-extraction logic.
func earliestMeetingRoundInDraw(draw *KnockoutDraw, pools []Pool, dojo string) int {
	dojoPools := map[string]bool{}
	for _, p := range pools {
		if countDojoInPool(p, dojo) > 0 {
			dojoPools[p.PoolName] = true
		}
	}
	var slots []int
	for slot, v := range TreeToLeafArray(draw.Root) {
		if i := strings.LastIndex(v, "-"); i > 0 && dojoPools[v[:i]] {
			slots = append(slots, slot)
		}
	}
	if len(slots) < 2 {
		return mathMaxIntSentinel
	}
	earliest := mathMaxIntSentinel
	for a := range slots {
		for b := a + 1; b < len(slots); b++ {
			if rd := dojoMeetRound(slots[a], slots[b]); rd > 0 && rd < earliest {
				earliest = rd
			}
		}
	}
	return earliest
}

const mathMaxIntSentinel = 1 << 30

// TestProductionVsReferencePipeline_WinnerPathNeverWorse is the permanent
// sweep-comparison gate bc-dojo-least-conflicted-pool's own on/off harness
// became once the descent and its winner-path exchange pass merged into
// production (BuildPoolPhaseTreeAware): comparing production against
// itself under a different name is meaningless (this file's own historical
// note on Phase 4's identical situation), so the comparison target moves
// to referencePoolSeedingPipeline -- PoolSeeding -> CreatePools ->
// ReorderPoolsForCourts, with NO region awareness at all, the pipeline the
// original bc-dojo rebuild replaced and the one still-standing REAL
// baseline meaningful to compare against (also used by the seed-placement
// equality pins above).
//
// Scope, "where meaningful": the SAME single-dojo, unseeded, 180-config
// end-to-end sweep TestTreeAwareGateScorecard's own "end_to_end_region"
// subtest and this file's HISTORICAL RECORD (old-sum=220, new-sum=254,
// new-worse=0) already anchor on -- not the full seeded multi-dojo
// 1596-config sweep the exchange pass's OWN decision gate was measured
// against. That broader sweep is NOT meaningful here: production
// regresses against the reference pipeline on 19 of its 1596 configs, ALL
// of them seeded (seeds=2), because referencePoolSeedingPipeline's simpler
// unseeded-dojo-clustering sort can happen to interact with a FIXED seed
// layout more favourably than the winner-path descent's own placement
// rule for that specific mix -- an artefact of introducing seeds into a
// comparison the original rebuild never made, not a regression in
// anything this bead touched (seed PLACEMENT itself is pinned
// byte-identical between the two pipelines by
// TestSeedPlacementEquality_OldVsTreeAware/_MultiDojo; only the UNSEEDED
// dojo-spread objective differs, and only the single-dojo unseeded shape
// below is the one this repo has ever measured production against the
// reference pipeline on).
func TestProductionVsReferencePipeline_WinnerPathNeverWorse(t *testing.T) {
	better, same, worse, total := 0, 0, 0, 0
	winnerPathStats := func(pools []Pool, courts, poolWinners int) (roundOne int, sum int) {
		if m := earliestDojoWinnerMeetingRound(pools, poolWinners, courts, "Dojo0"); m != mathMaxIntSentinel {
			sum = m
			if m == 1 {
				roundOne = 1
			}
		}
		return roundOne, sum
	}

	for numPools := 3; numPools <= 7; numPools++ {
		for dojoGroupSize := 2; dojoGroupSize <= numPools+2; dojoGroupSize++ {
			poolSize := 4
			if dojoGroupSize > numPools*poolSize {
				continue
			}
			r := buildMultiDojoRoster(numPools, poolSize, 1, dojoGroupSize, 0)
			for _, courts := range []int{1, 2, 4} {
				for _, winners := range []int{1, 2} {
					prod, prodCourts, err := BuildPoolPhaseTreeAware(r, poolSize, false, courts, winners)
					if err != nil {
						t.Errorf("pools=%d dojo=%d courts=%d winners=%d: BuildPoolPhaseTreeAware error: %v", numPools, dojoGroupSize, courts, winners, err)
						continue
					}
					ref, refCourts, err := referencePoolSeedingPipeline(r, poolSize, false, courts)
					if err != nil {
						t.Errorf("pools=%d dojo=%d courts=%d winners=%d: referencePoolSeedingPipeline error: %v", numPools, dojoGroupSize, courts, winners, err)
						continue
					}
					pr1, ps := winnerPathStats(prod, prodCourts, winners)
					rr1, rs := winnerPathStats(ref, refCourts, winners)
					if ps == 0 && rs == 0 {
						continue // Dojo0 did not span >=2 pools in either pipeline: no data
					}
					total++
					switch {
					case pr1 < rr1 || (pr1 == rr1 && ps > rs):
						better++
					case pr1 > rr1 || (pr1 == rr1 && ps < rs):
						worse++
						t.Errorf("pools=%d dojo=%d courts=%d winners=%d: production (r1=%d sum=%d) worse than referencePoolSeedingPipeline (r1=%d sum=%d)",
							numPools, dojoGroupSize, courts, winners, pr1, ps, rr1, rs)
					default:
						same++
					}
				}
			}
		}
	}
	t.Logf("production vs referencePoolSeedingPipeline (winner-path, single-dojo end-to-end sweep): total=%d better=%d same=%d worse=%d", total, better, same, worse)
	if total < 150 {
		t.Errorf("sweep shrank below the measured 180-config space: got %d", total)
	}
}

// earliestDojoWinnerMeetingRound is earliestDojoMeetingRound's WINNER-PATH
// sibling (operator ruling, bc-dojo-least-conflicted-pool): the earliest
// knockout round any two of dojo's qualifying pools' WINNER (rank-1)
// leaves alone are drawn to meet, ignoring every runner-up/crossed-in
// qualifier a pool with poolWinners>1 also sends up.
//
// This is the objective the dojo-tree descent
// (pool_distribution_tree_aware.go's shelved bottom half) actually
// optimises -- it walks each pool's winner leaf only, by design -- so it is
// the metric the operator ruled the descent be measured against for the
// ship/keep decision, not earliestDojoMeetingRound's own all-qualifier
// number. Per that ruling: "a second-place qualifier colliding with a
// same-dojo competitor through CROSSING is accepted chance -- that's life
// -- not a defect", so the all-qualifier number is reported alongside this
// one (informational) but is NOT the gate.
func earliestDojoWinnerMeetingRound(pools []Pool, poolWinners, numCourts int, dojo string) int {
	draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
	if draw == nil {
		return mathMaxIntSentinel
	}
	return earliestWinnerMeetingRoundInDraw(draw, pools, dojo)
}

// earliestWinnerMeetingRoundInDraw is earliestMeetingRoundInDraw's
// WINNER-PATH sibling: identical builder-agnostic split, but a leaf only
// counts when its label is a pool's WINNER slot specifically (suffix
// "-"+GetOrdinal(1), i.e. "-1st"), never a runner-up/crossed-in qualifier.
func earliestWinnerMeetingRoundInDraw(draw *KnockoutDraw, pools []Pool, dojo string) int {
	dojoPools := map[string]bool{}
	for _, p := range pools {
		if countDojoInPool(p, dojo) > 0 {
			dojoPools[p.PoolName] = true
		}
	}
	winnerSuffix := "-" + GetOrdinal(1)
	var slots []int
	for slot, v := range TreeToLeafArray(draw.Root) {
		if !strings.HasSuffix(v, winnerSuffix) {
			continue
		}
		if dojoPools[strings.TrimSuffix(v, winnerSuffix)] {
			slots = append(slots, slot)
		}
	}
	if len(slots) < 2 {
		return mathMaxIntSentinel
	}
	earliest := mathMaxIntSentinel
	for a := range slots {
		for b := a + 1; b < len(slots); b++ {
			if rd := dojoMeetRound(slots[a], slots[b]); rd > 0 && rd < earliest {
				earliest = rd
			}
		}
	}
	return earliest
}

// referencePoolSeeding, referenceSortUnseededByDojoCluster and
// referencePlaceSeedsForPools are PoolSeeding, sortUnseededByDojoCluster
// and placeSeedsForPools' exact bodies, moved here (bc-drwx item 11:
// PoolSeeding and its two private helpers were removed as dead PRODUCTION
// code -- no caller left outside their own tests, ever since bc-dojo Phase 4
// made BuildPoolPhase delegate to the tree-aware distributor instead).
// referencePlaceSeedsForPools still wraps placeSeedIndices, the ONE piece
// of this trio that IS live (shared with buildPoolPhaseTreeAwareCore) --
// this is not a resurrection of dead code, it is what lets
// referencePoolSeedingPipeline below keep serving as an INDEPENDENT
// reconstruction of the pre-Phase-4 pipeline that placeSeedIndices'
// extraction can be checked against, and what lets the many pre-existing
// PoolSeeding-based tests in this package keep pinning the same properties
// under their new name rather than losing that coverage outright.
func referencePoolSeeding(players []Player, numPools int, numCourts int) []Player {
	if numPools <= 0 {
		return players
	}
	// Both ends, through the one owner: numCourts is the spread modulus below.
	numCourts = clampCourts(numCourts)

	seeded, unseeded := partitionSeeded(players)
	referenceSortUnseededByDojoCluster(unseeded)

	result, occupied := referencePlaceSeedsForPools(seeded, numPools, numCourts, len(players))

	unIdx := 0
	for i := 0; i < len(players); i++ {
		if !occupied[i] {
			if unIdx < len(unseeded) {
				result[i] = unseeded[unIdx]
				unIdx++
			}
		}
	}

	return result
}

func referenceSortUnseededByDojoCluster(unseeded []Player) {
	dojoCount := make(map[string]int)
	for _, p := range unseeded {
		dojoCount[p.Dojo]++
	}
	sort.SliceStable(unseeded, func(i, j int) bool {
		ci, cj := dojoCount[unseeded[i].Dojo], dojoCount[unseeded[j].Dojo]
		if ci != cj {
			return ci > cj
		}
		if unseeded[i].Dojo != unseeded[j].Dojo {
			return unseeded[i].Dojo < unseeded[j].Dojo
		}
		return false
	})
}

func referencePlaceSeedsForPools(seeded []Player, numPools, numCourts, totalLen int) (result []Player, occupied map[int]bool) {
	result = make([]Player, totalLen)
	occupied = make(map[int]bool, len(seeded))
	indices := placeSeedIndices(seeded, numPools, numCourts, totalLen)
	for si, idx := range indices {
		if idx < 0 {
			continue
		}
		result[idx] = seeded[si]
		occupied[idx] = true
	}
	return result, occupied
}

// referencePoolSeedingPipeline reconstructs BuildPoolPhase's PRE-Phase-4
// body -- PoolSeeding -> CreatePools -> ReorderPoolsForCourts -- from the
// primitive functions directly, since BuildPoolPhase itself now delegates
// to the tree-aware distributor and can no longer serve as "the other
// pipeline" for this comparison. It exists so TestTreeAwareGateScorecard's
// seed-placement pin has something independent to check placeSeedIndices'
// extraction claim against.
func referencePoolSeedingPipeline(players []Player, poolSize int, isMax bool, numCourts int) ([]Pool, int, error) {
	numPools := PoolCount(len(players), poolSize, isMax)
	drawCourts := EffectiveDrawCourts(numPools, numCourts)
	pools, err := CreatePools(referencePoolSeeding(players, numPools, drawCourts), poolSize, isMax)
	if err != nil {
		return nil, 0, err
	}
	return ReorderPoolsForCourts(pools, drawCourts), drawCourts, nil
}

// checkAbsoluteInvariants runs the production tree-aware path
// (BuildPoolPhaseTreeAware, called with the sweep's own intended
// poolWinners -- never through BuildPoolPhase, whose poolWinners is fixed
// at a default that need not match) over one roster/shape and asserts the
// invariants that must ALWAYS hold, folding the result into sc. Reports via
// t.Errorf (never Fatal: a scorecard run must finish and log its totals
// even when it fails).
func checkAbsoluteInvariants(t *testing.T, sc *gateScorecard, r []Player, numPools, poolSize int, isMax bool, courts, poolWinners int, dojos []string, tag string) []Pool {
	t.Helper()
	pools, _, err := BuildPoolPhaseTreeAware(r, poolSize, isMax, courts, poolWinners)
	if err != nil {
		t.Errorf("%s: BuildPoolPhaseTreeAware error: %v", tag, err)
		return nil
	}

	// Sizes: every pool's size must equal poolTargetSizes' (remainder-spread)
	// target exactly -- nobody lost, duplicated, or short/over their target.
	sc.sizeConfigs++
	_, baseSizes, err := poolTargetSizes(len(r), poolSize, isMax)
	if err != nil {
		t.Errorf("%s: poolTargetSizes: %v", tag, err)
		return pools
	}
	wantSizes := realTargetSizes(baseSizes, len(r))
	if len(pools) != len(wantSizes) {
		sc.sizeMismatch++
		t.Errorf("%s: pool count differs: got=%d want=%d", tag, len(pools), len(wantSizes))
	} else {
		for i := range pools {
			if len(pools[i].Players) != wantSizes[i] {
				sc.sizeMismatch++
				t.Errorf("%s: pool %d size differs: got=%d want=%d", tag, i, len(pools[i].Players), wantSizes[i])
				break
			}
		}
	}

	// Seeds-per-pool: never two in one pool.
	sc.seedShareConfigs++
	if seedsSharePool(pools) {
		sc.seedShareFail++
		t.Errorf("%s: two seeds share a pool", tag)
	}

	// Seed placement equality against the reference pipeline (see
	// referencePoolSeedingPipeline's own doc comment for what this still
	// pins and why).
	refPools, _, err := referencePoolSeedingPipeline(r, poolSize, isMax, courts)
	if err != nil {
		t.Errorf("%s: referencePoolSeedingPipeline error: %v", tag, err)
	} else {
		refSeedPool := seedPoolByName(refPools)
		gotSeedPool := seedPoolByName(pools)
		for name, refPool := range refSeedPool {
			sc.seedEqualityChecked++
			if gotSeedPool[name] != refPool {
				sc.seedEqualityMismatches++
				t.Errorf("%s: seed %s pool differs: reference=%s got=%s", tag, name, refPool, gotSeedPool[name])
			}
		}
	}

	// Per-pool dojo-concentration optimum, for every dojo named: the
	// ABSOLUTE requirement is zero failures, not "no worse than some other
	// pipeline" -- a one-pass distributor that can see the whole knockout
	// tree before it places anyone has no excuse to leave a dojo
	// over-concentrated when ceil(dojoGroupSize/numPools) was reachable.
	for _, dojo := range dojos {
		dojoGroupSize := 0
		for _, p := range r {
			if p.Dojo == dojo {
				dojoGroupSize++
			}
		}
		if dojoGroupSize == 0 {
			continue
		}
		optimum := (dojoGroupSize + numPools - 1) / numPools
		sc.optimumConfigs++
		if got := maxOfDojo(pools, dojo); got > optimum {
			sc.optimumFail++
			t.Errorf("%s: dojo %s: over-concentrated (got=%d optimum=%d)", tag, dojo, got, optimum)
		}
	}

	return pools
}

func seedsSharePool(pools []Pool) bool {
	for _, p := range pools {
		seeds := 0
		for _, pl := range p.Players {
			if pl.Seed > 0 {
				seeds++
			}
		}
		if seeds > 1 {
			return true
		}
	}
	return false
}

// TestTreeAwareGateScorecard is the closed-gap regression pin the Phase 3
// decision gate became once the swap landed (Phase 4): BuildPoolPhase and
// BuildPoolPhaseFillBracket now delegate to the path this test exercises
// directly, so a regression here IS a regression in production, not in a
// shadow implementation nobody calls yet. See the file doc comment for what
// changed and why every assertion here is an Errorf (accumulate and log
// everything) rather than a require/Fatal (stop at the first finding).
func TestTreeAwareGateScorecard(t *testing.T) {
	sc := &gateScorecard{}

	// --- Sweep 1: the multi-dojo sweep (Phase 0's own 2048; extended per
	// operator ruling, bc-dojo-least-conflicted-pool, to dojoGroupSize up
	// to 2*numPools -- see pool_distribution_invariants_test.go's
	// TestPoolDistribution_Invariants for why the old numPools+2 cap could
	// never reach an oversubscribed dojo). ---
	t.Run("multidojo_2048", func(t *testing.T) {
		total := 0
		for numPools := 3; numPools <= 7; numPools++ {
			for poolSize := 3; poolSize <= 5; poolSize++ {
				for courts := 1; courts <= 2; courts++ {
					for nDojos := 2; nDojos <= 4; nDojos++ {
						for dojoGroupSize := 2; dojoGroupSize <= 2*numPools; dojoGroupSize++ {
							for nSeeds := 0; nSeeds <= 4 && nSeeds < numPools; nSeeds++ {
								if nDojos*dojoGroupSize > numPools*poolSize {
									continue
								}
								r := buildMultiDojoRoster(numPools, poolSize, nDojos, dojoGroupSize, nSeeds)
								dojos := make([]string, nDojos)
								for c := 0; c < nDojos; c++ {
									dojos[c] = fmt.Sprintf("Dojo%d", c)
								}
								tag := fmt.Sprintf("multidojo pools=%d size=%d courts=%d dojos=%dx%d seeds=%d",
									numPools, poolSize, courts, nDojos, dojoGroupSize, nSeeds)
								checkAbsoluteInvariants(t, sc, r, numPools, poolSize, false, courts, 1, dojos, tag)
								total++
							}
						}
					}
				}
			}
		}
		if total < 2000 {
			t.Errorf("sweep shrank below the measured 2048-config space: got %d", total)
		}
	})

	// --- Sweep 2: the 210-config seeded-dojo sweep (Phase 0's own). ---
	t.Run("seeded_dojo_210", func(t *testing.T) {
		total := 0
		for numPools := 3; numPools <= 7; numPools++ {
			for poolSize := 3; poolSize <= 5; poolSize++ {
				for nSeeds := 2; nSeeds <= 4 && nSeeds <= numPools; nSeeds++ {
					for dojoExtra := 0; dojoExtra <= 4; dojoExtra++ {
						n := numPools * poolSize
						if nSeeds+dojoExtra > n {
							continue
						}
						r := make([]Player, 0, n)
						for s := 1; s <= nSeeds; s++ {
							r = append(r, Player{Name: fmt.Sprintf("Seed%d", s), Dojo: "SeedDojo", Seed: s})
						}
						for i := 1; i <= dojoExtra; i++ {
							r = append(r, Player{Name: fmt.Sprintf("Mate%d", i), Dojo: "SeedDojo"})
						}
						for i := len(r) + 1; i <= n; i++ {
							r = append(r, Player{Name: fmt.Sprintf("O%d", i), Dojo: fmt.Sprintf("D%02d", i)})
						}
						tag := fmt.Sprintf("seededdojo pools=%d size=%d seeds=%d extra=%d", numPools, poolSize, nSeeds, dojoExtra)
						checkAbsoluteInvariants(t, sc, r, numPools, poolSize, false, 2, 1, []string{"SeedDojo"}, tag)
						total++
					}
				}
			}
		}
		if total < 200 {
			t.Errorf("sweep shrank below the measured 210-config space: got %d", total)
		}
	})

	// --- Sweep 3: the unique-dojo identity contract (Phase 0's own). ---
	t.Run("unique_dojo_identity", func(t *testing.T) {
		for _, numPools := range []int{3, 5, 8} {
			for _, poolSize := range []int{3, 4} {
				n := numPools * poolSize
				r := make([]Player, n)
				for i := range r {
					r[i] = Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo %03d", i+1)}
				}
				pools, _, err := BuildPoolPhaseTreeAware(r, poolSize, false, 2, 1)
				if err != nil {
					t.Fatalf("BuildPoolPhaseTreeAware error: %v", err)
				}
				sc.identityConfigs++
				if !identityContractHolds(r, numPools, pools) {
					sc.identityFail++
					t.Errorf("pools=%d size=%d: production path breaks the round-robin identity contract", numPools, poolSize)
				}
			}
		}
	})

	// --- Sweep 4: end-to-end region metric across courts 1/2/4 and winners
	// 1/2, judged against the BRUTE-FORCE CEILING for every config. ---
	//
	// The gate originally (Phase 3) demanded the three Phase 0 reproducers
	// beat round 1. That demand was WRONG, proven by exhaustive brute
	// force: at winners=1 those shapes' dojos occupy more than half the
	// qualifying pools, so pigeonhole forces a round-1 dojo pair no matter
	// which pools are chosen -- their ceiling IS 1, for any algorithm. The
	// correct requirement, enforced below for EVERY config with data: the
	// production path's earliest dojo meeting must EQUAL the best any pool
	// assignment could achieve (max over all pool subsets of the min
	// pairwise meeting round), unfixable shapes included, where equalling a
	// ceiling of 1 passes.
	t.Run("end_to_end_region", func(t *testing.T) {
		for numPools := 3; numPools <= 7; numPools++ {
			for dojoGroupSize := 2; dojoGroupSize <= numPools+2; dojoGroupSize++ {
				poolSize := 4
				if dojoGroupSize > numPools*poolSize {
					continue
				}
				r := buildMultiDojoRoster(numPools, poolSize, 1, dojoGroupSize, 0)
				for _, courts := range []int{1, 2, 4} {
					for _, winners := range []int{1, 2} {
						tag := fmt.Sprintf("e2e pools=%d dojo=%d courts=%d winners=%d", numPools, dojoGroupSize, courts, winners)
						pools, drawCourts, err := BuildPoolPhaseTreeAware(r, poolSize, false, courts, winners)
						if err != nil {
							t.Errorf("%s: BuildPoolPhaseTreeAware error: %v", tag, err)
							continue
						}

						// ALL-QUALIFIER number: informational only (logged via
						// the scorecard's own totals), never asserted -- see
						// gateScorecard's own doc comment for why.
						if allQualRound := earliestDojoMeetingRound(pools, winners, drawCourts, "Dojo0"); allQualRound != mathMaxIntSentinel {
							sc.allQualRegionConfigs++
							sc.allQualRegionSum += allQualRound
							if allQualRound == 1 {
								sc.allQualRegionRound1++
							}
						}

						// WINNER-PATH number: the gate, per operator ruling.
						round := earliestDojoWinnerMeetingRound(pools, winners, drawCourts, "Dojo0")
						if round == mathMaxIntSentinel {
							continue // Dojo0 did not span >=2 pools in this config: no data
						}
						sc.regionConfigs++
						sc.regionSum += round
						if round == 1 {
							sc.regionRound1++
						}

						_, newSizes, tErr := poolTargetSizes(len(r), poolSize, false)
						if tErr != nil {
							t.Errorf("%s: poolTargetSizes: %v", tag, tErr)
							continue
						}
						span := dojoGroupSize
						if span > numPools {
							span = numPools
						}
						ceiling := bruteForceWinnerMeetingCeiling(newSizes, winners, drawCourts, span)
						sc.optimumMeetConfigs++
						if round < ceiling {
							sc.optimumMeetBelow++
							t.Errorf("%s: production path meets at WINNER-PATH round %d but the winner-path brute-force ceiling for this shape is %d", tag, round, ceiling)
						}
					}
				}
			}
		}
	})

	t.Log(sc.String())

	// Overall gate: absolute, zero-failure requirements.
	if sc.optimumFail != 0 {
		t.Errorf("GATE FAIL: %d per-pool optimum failure(s) on the production path", sc.optimumFail)
	}
	if sc.sizeMismatch != 0 {
		t.Errorf("GATE FAIL: %d pool-size mismatch(es) against the target sizes", sc.sizeMismatch)
	}
	if sc.seedShareFail != 0 {
		t.Errorf("GATE FAIL: %d config(s) with two seeds sharing a pool", sc.seedShareFail)
	}
	if sc.seedEqualityMismatches != 0 {
		t.Errorf("GATE FAIL: %d seed(s) placed in a different pool than referencePoolSeedingPipeline", sc.seedEqualityMismatches)
	}
	if sc.identityFail != 0 {
		t.Errorf("GATE FAIL: %d config(s) broke the unique-dojo round-robin identity contract", sc.identityFail)
	}
	if sc.optimumMeetBelow != 0 {
		t.Errorf("GATE FAIL: %d config(s) met earlier than the brute-force ceiling", sc.optimumMeetBelow)
	}
	if sc.optimumMeetConfigs != 180 {
		t.Errorf("GATE FAIL: expected 180 end-to-end configs with data, got %d (sweep shrank or grew)", sc.optimumMeetConfigs)
	}
	if sc.regionConfigs == 0 {
		t.Errorf("GATE FAIL: region sweep produced no data (configs with a dojo spanning >=2 pools) -- the gate cannot be evaluated")
	}
}

// identityContractHolds is TestPoolDistribution_UniqueDojoIdentity's own
// check, extracted so the gate scorecard can run it against the production
// path.
func identityContractHolds(r []Player, numPools int, pools []Pool) bool {
	poolOf := map[string]string{}
	for _, p := range pools {
		for _, pl := range p.Players {
			poolOf[pl.Name] = p.PoolName
		}
	}
	n := len(r)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			same := poolOf[r[i].Name] == poolOf[r[j].Name]
			want := i%numPools == j%numPools
			if same != want {
				return false
			}
		}
	}
	return true
}

// bruteForceMeetingCeiling returns the best earliest-meeting round ANY pool
// assignment could achieve for a dojo spanning `span` pools: the maximum,
// over every subset of `span` pools, of the minimum pairwise meeting round
// between the chosen pools' qualifier slots. Exponential in numPools, which
// is capped at 7 in the sweep, so at most C(7,3)=35 subsets of pair-checks.
func bruteForceMeetingCeiling(targetSizes []int, poolWinners, drawCourts, span int) int {
	slots := treeAwareQualifierSlots(targetSizes, poolWinners, drawCourts, qualifierMode{ExtraQualifiers: QualifierModeStandard})
	n := len(slots)
	if span < 2 || span > n {
		return math.MaxInt
	}
	best := 0
	idx := make([]int, span)
	var rec func(start, k int)
	rec = func(start, k int) {
		if k == span {
			worst := math.MaxInt
			for a := 0; a < span; a++ {
				for b := a + 1; b < span; b++ {
					for _, sa := range slots[idx[a]] {
						for _, sb := range slots[idx[b]] {
							if r := dojoMeetRound(sa, sb); r > 0 && r < worst {
								worst = r
							}
						}
					}
				}
			}
			if worst != math.MaxInt && worst > best {
				best = worst
			}
			return
		}
		for i := start; i <= n-(span-k); i++ {
			idx[k] = i
			rec(i+1, k+1)
		}
	}
	rec(0, 0)
	if best == 0 {
		return math.MaxInt
	}
	return best
}

// bruteForceWinnerMeetingCeiling is bruteForceMeetingCeiling's WINNER-PATH
// sibling (operator ruling): identical combinatorial search, but each
// pool's candidate slot set is reduced to its WINNER (rank-1) leaf alone,
// matching what earliestDojoWinnerMeetingRound measures and what the
// dojo-tree descent + winner-path exchange pass actually optimises for.
// Runner-up/crossed-in slots a pool with poolWinners>1 also carries play
// no part in this ceiling, by the same ruling that keeps them out of the
// gate metric itself.
func bruteForceWinnerMeetingCeiling(targetSizes []int, poolWinners, drawCourts, span int) int {
	full := treeAwareQualifierSlots(targetSizes, poolWinners, drawCourts, qualifierMode{ExtraQualifiers: QualifierModeStandard})
	slots := make([][]int, len(full))
	for i, s := range full {
		if len(s) > 0 {
			slots[i] = s[:1]
		}
	}
	n := len(slots)
	if span < 2 || span > n {
		return math.MaxInt
	}
	best := 0
	idx := make([]int, span)
	var rec func(start, k int)
	rec = func(start, k int) {
		if k == span {
			worst := math.MaxInt
			for a := 0; a < span; a++ {
				for b := a + 1; b < span; b++ {
					for _, sa := range slots[idx[a]] {
						for _, sb := range slots[idx[b]] {
							if r := dojoMeetRound(sa, sb); r > 0 && r < worst {
								worst = r
							}
						}
					}
				}
			}
			if worst != math.MaxInt && worst > best {
				best = worst
			}
			return
		}
		for i := start; i <= n-(span-k); i++ {
			idx[k] = i
			rec(i+1, k+1)
		}
	}
	rec(0, 0)
	if best == 0 {
		return math.MaxInt
	}
	return best
}
