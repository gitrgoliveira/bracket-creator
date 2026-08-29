package helper

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// bc-dojo Phase 3: the decision-gate scorecard.
//
// This file is the ship/keep decision for the region-aware rebuild
// (BuildPoolPhaseTreeAware, pool_distribution_tree_aware.go): it runs BOTH
// pipelines over every sweep pool_distribution_invariants_test.go (Phase 0)
// established as the baseline, plus an end-to-end region sweep neither
// pipeline was measured against before, and logs a single scorecard. Nothing
// in this file calls the new path from production code -- the swap, if this
// gate passes, is a separate, later change.
//
// gateScorecard accumulates every metric across every sweep so the final Log
// is one picture rather than scattered per-subtest output, and so a human
// reading a failed run sees the SAME totals whether the run passed or
// failed: t.Logf runs regardless (Errorf does not stop the function), and
// the failing assertions point at exactly the counters that regressed.
type gateScorecard struct {
	// Metric: per-pool club-concentration optimum (maxOfDojo <= ceil(clubSize/numPools)).
	optimumConfigs         int
	optimumOldFail         int
	optimumNewFail         int
	optimumNewWorseThanOld int // new failed where old passed: the hard-fail trigger

	// Metric: pool sizes (target sizes reproduced exactly).
	sizeConfigs  int
	sizeMismatch int

	// Metric: seeds-per-pool (no two seeds share a pool).
	seedShareConfigs int
	seedShareOldFail int
	seedShareNewFail int

	// Metric: seed placement equality (every seed in the identical named pool).
	seedEqualityChecked    int
	seedEqualityMismatches int

	// Metric: unique-dojo identity contract (byte-identical round-robin deal).
	identityConfigs int
	identityOldFail int
	identityNewFail int

	// Metric: end-to-end region (earliest knockout round a club's own
	// qualifiers could meet each other), the metric the whole rebuild exists
	// to move. mathMaxInt sentinel excluded from sums/round1 counts (means
	// "this club never spanned >=2 pools in this config", i.e. no data).
	regionConfigs      int
	regionOldSum       int
	regionNewSum       int
	regionOldRound1    int
	regionNewRound1    int
	regionNewWorse     int // hard-fail trigger: new < old for one config
	regionNewBetter    int
	regionNewSame      int
	optimumMeetConfigs int // e2e configs checked against the brute-force ceiling
	optimumMeetBelow   int // hard-fail trigger: new path met earlier than the shape's brute-force optimum
}

func (s *gateScorecard) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== bc-dojo Phase 3 gate scorecard (old fill+repair vs new tree-aware) ===\n")
	fmt.Fprintf(&b, "per-pool optimum:      configs=%d  old-fail=%d  new-fail=%d  (new-worse-than-old=%d)\n",
		s.optimumConfigs, s.optimumOldFail, s.optimumNewFail, s.optimumNewWorseThanOld)
	fmt.Fprintf(&b, "pool sizes:            configs=%d  mismatches=%d\n", s.sizeConfigs, s.sizeMismatch)
	fmt.Fprintf(&b, "seeds-per-pool:        configs=%d  old-fail=%d  new-fail=%d\n",
		s.seedShareConfigs, s.seedShareOldFail, s.seedShareNewFail)
	fmt.Fprintf(&b, "seed placement:        seeds-checked=%d  mismatches=%d\n",
		s.seedEqualityChecked, s.seedEqualityMismatches)
	fmt.Fprintf(&b, "unique-dojo identity:  configs=%d  old-fail=%d  new-fail=%d\n",
		s.identityConfigs, s.identityOldFail, s.identityNewFail)
	fmt.Fprintf(&b, "region (earliest club-meeting round in the knockout):\n")
	fmt.Fprintf(&b, "  configs-with-data=%d  old-sum=%d  new-sum=%d  (higher is later/better)\n",
		s.regionConfigs, s.regionOldSum, s.regionNewSum)
	fmt.Fprintf(&b, "  old-round1=%d  new-round1=%d  new-better=%d  new-same=%d  new-worse=%d (hard-fail trigger)\n",
		s.regionOldRound1, s.regionNewRound1, s.regionNewBetter, s.regionNewSame, s.regionNewWorse)
	fmt.Fprintf(&b, "  brute-force ceiling: configs=%d  new-below-optimum=%d (hard-fail trigger)\n",
		s.optimumMeetConfigs, s.optimumMeetBelow)
	return b.String()
}

// earliestClubMeetingRound is the known-gap test's own metric
// (pool_distribution_invariants_test.go), generalised into a function: the
// earliest knockout round any two of dojo's qualifying pools could be drawn
// to meet, or math.MaxInt when dojo does not span at least two pools in this
// draw (nothing to measure).
func earliestClubMeetingRound(pools []Pool, poolWinners, numCourts int, dojo string) int {
	draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
	if draw == nil {
		return mathMaxIntSentinel
	}
	clubPools := map[string]bool{}
	for _, p := range pools {
		if countDojoInPool(p, dojo) > 0 {
			clubPools[p.PoolName] = true
		}
	}
	var slots []int
	for slot, v := range TreeToLeafArray(draw.Root) {
		if i := strings.LastIndex(v, "-"); i > 0 && clubPools[v[:i]] {
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

// checkOptimumAndSizesAndSeeds runs the three "must not regress" metrics
// shared by every sweep in this file against one roster/shape, folding the
// result into sc, and reports the failures via t.Errorf (never Fatal: a
// scorecard run must finish and log its totals even when it fails).
func checkOptimumAndSizesAndSeeds(t *testing.T, sc *gateScorecard, r []Player, numPools, poolSize int, isMax bool, courts, poolWinners int, dojos []string, tag string) (oldPools, newPools []Pool) {
	t.Helper()
	oldPools, _, err := BuildPoolPhase(r, poolSize, isMax, courts)
	if err != nil {
		t.Errorf("%s: BuildPoolPhase error: %v", tag, err)
		return nil, nil
	}
	newPools, _, err = BuildPoolPhaseTreeAware(r, poolSize, isMax, courts, poolWinners)
	if err != nil {
		t.Errorf("%s: BuildPoolPhaseTreeAware error: %v", tag, err)
		return nil, nil
	}

	// Sizes.
	sc.sizeConfigs++
	if len(oldPools) != len(newPools) {
		sc.sizeMismatch++
		t.Errorf("%s: pool count differs: old=%d new=%d", tag, len(oldPools), len(newPools))
	} else {
		for i := range oldPools {
			if len(oldPools[i].Players) != len(newPools[i].Players) {
				sc.sizeMismatch++
				t.Errorf("%s: pool %d size differs: old=%d new=%d", tag, i, len(oldPools[i].Players), len(newPools[i].Players))
				break
			}
		}
	}

	// Seeds-per-pool.
	sc.seedShareConfigs++
	oldSeedFail := seedsSharePool(oldPools)
	newSeedFail := seedsSharePool(newPools)
	if oldSeedFail {
		sc.seedShareOldFail++
	}
	if newSeedFail {
		sc.seedShareNewFail++
		t.Errorf("%s: two seeds share a pool in the NEW path", tag)
	}

	// Seed placement equality.
	oldSeedPool := seedPoolByName(oldPools)
	newSeedPool := seedPoolByName(newPools)
	for name, oldPool := range oldSeedPool {
		sc.seedEqualityChecked++
		if newSeedPool[name] != oldPool {
			sc.seedEqualityMismatches++
			t.Errorf("%s: seed %s pool differs: old=%s new=%s", tag, name, oldPool, newSeedPool[name])
		}
	}

	// Per-pool club-concentration optimum, for every dojo named.
	for _, dojo := range dojos {
		clubSize := 0
		for _, p := range r {
			if p.Dojo == dojo {
				clubSize++
			}
		}
		if clubSize == 0 {
			continue
		}
		optimum := (clubSize + numPools - 1) / numPools
		sc.optimumConfigs++
		oldFail := maxOfDojo(oldPools, dojo) > optimum
		newFail := maxOfDojo(newPools, dojo) > optimum
		if oldFail {
			sc.optimumOldFail++
		}
		if newFail {
			sc.optimumNewFail++
		}
		if newFail && !oldFail {
			sc.optimumNewWorseThanOld++
			t.Errorf("%s: dojo %s: new path over-concentrated (new=%d old=%d optimum=%d) where old was not",
				tag, dojo, maxOfDojo(newPools, dojo), maxOfDojo(oldPools, dojo), optimum)
		}
	}

	return oldPools, newPools
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

// checkRegionMetric folds the end-to-end region metric for one dojo/config
// into sc: the earliest knockout round dojo's own qualifiers could meet,
// under both pipelines, hard-failing if the new path is ever WORSE than the
// old.
func checkRegionMetric(t *testing.T, sc *gateScorecard, oldPools, newPools []Pool, poolWinners, drawCourts int, dojo, tag string) {
	t.Helper()
	oldRound := earliestClubMeetingRound(oldPools, poolWinners, drawCourts, dojo)
	newRound := earliestClubMeetingRound(newPools, poolWinners, drawCourts, dojo)
	if oldRound == mathMaxIntSentinel && newRound == mathMaxIntSentinel {
		return // dojo did not span >=2 pools under either pipeline: no data
	}
	sc.regionConfigs++
	if oldRound != mathMaxIntSentinel {
		sc.regionOldSum += oldRound
		if oldRound == 1 {
			sc.regionOldRound1++
		}
	}
	if newRound != mathMaxIntSentinel {
		sc.regionNewSum += newRound
		if newRound == 1 {
			sc.regionNewRound1++
		}
	}

	switch {
	case newRound > oldRound:
		sc.regionNewBetter++
	case newRound == oldRound:
		sc.regionNewSame++
	default: // newRound < oldRound, including new having NO data (sentinel) where old did
		sc.regionNewWorse++
		t.Errorf("%s: dojo %s: new path's earliest meeting (%d) is WORSE than old's (%d)", tag, dojo, newRound, oldRound)
	}
}

// TestTreeAwareGateScorecard is the decision gate: BuildPoolPhaseTreeAware
// ships only if this passes. See the file doc comment for what "passes"
// means and why every assertion here is an Errorf (accumulate and log
// everything) rather than a require/Fatal (stop at the first finding).
//
// NOTE on the per-pool-optimum failures this surfaces (multiclub_2048 and
// seeded_club_210 below, ~17 of ~6060 optimum checks): every measured
// instance is the SAME mechanism, root-caused during Phase 3 development,
// not a scattering of unrelated bugs. sortUnseededByDojoCluster --
// PoolSeeding's OWN existing dojo-clustering sort, reused verbatim per the
// Phase 2 brief -- ranks a dojo by its UNSEEDED member count, because that
// is the only count PoolSeeding's own sort has ever needed (its seeded
// members are handled by an entirely separate code path). When most of a
// club's members happen to be seeded, its unseeded residual can be tiny
// (sometimes just one player) and sorts to the BACK of the one pass, by
// which point every pool but one is already full -- so that residual is
// placed wherever room is left, dojo-conflict or not, since the design
// (bc-dojo) is explicitly "one pass, no repair". The old pipeline tolerates
// the identical clustering order fine because rebalanceDojosAcrossPools
// fixes exactly this after the fact; the new one has no such pass by
// design. Reusing PoolSeeding's sort was the brief's explicit instruction
// ("the existing clustering order from PoolSeeding's dojo sort"), so this
// is left as specified rather than swapped for a total-footprint sort
// (seeded+unseeded) that was NOT authorised here -- see the final report
// for that as a candidate follow-up.
func TestTreeAwareGateScorecard(t *testing.T) {
	sc := &gateScorecard{}

	// --- Sweep 1: the 2048-config multi-club sweep (Phase 0's own). ---
	t.Run("multiclub_2048", func(t *testing.T) {
		total := 0
		for numPools := 3; numPools <= 7; numPools++ {
			for poolSize := 3; poolSize <= 5; poolSize++ {
				for courts := 1; courts <= 2; courts++ {
					for nClubs := 2; nClubs <= 4; nClubs++ {
						for clubSize := 2; clubSize <= numPools+2; clubSize++ {
							for nSeeds := 0; nSeeds <= 4 && nSeeds < numPools; nSeeds++ {
								if nClubs*clubSize > numPools*poolSize {
									continue
								}
								r := buildClubRoster(numPools, poolSize, nClubs, clubSize, nSeeds)
								dojos := make([]string, nClubs)
								for c := 0; c < nClubs; c++ {
									dojos[c] = fmt.Sprintf("Club%d", c)
								}
								tag := fmt.Sprintf("multiclub pools=%d size=%d courts=%d clubs=%dx%d seeds=%d",
									numPools, poolSize, courts, nClubs, clubSize, nSeeds)
								checkOptimumAndSizesAndSeeds(t, sc, r, numPools, poolSize, false, courts, 1, dojos, tag)
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

	// --- Sweep 2: the 210-config seeded-club sweep (Phase 0's own). ---
	t.Run("seeded_club_210", func(t *testing.T) {
		total := 0
		for numPools := 3; numPools <= 7; numPools++ {
			for poolSize := 3; poolSize <= 5; poolSize++ {
				for nSeeds := 2; nSeeds <= 4 && nSeeds <= numPools; nSeeds++ {
					for clubExtra := 0; clubExtra <= 4; clubExtra++ {
						n := numPools * poolSize
						if nSeeds+clubExtra > n {
							continue
						}
						r := make([]Player, 0, n)
						for s := 1; s <= nSeeds; s++ {
							r = append(r, Player{Name: fmt.Sprintf("Seed%d", s), Dojo: "SeedClub", Seed: s})
						}
						for i := 1; i <= clubExtra; i++ {
							r = append(r, Player{Name: fmt.Sprintf("Mate%d", i), Dojo: "SeedClub"})
						}
						for i := len(r) + 1; i <= n; i++ {
							r = append(r, Player{Name: fmt.Sprintf("O%d", i), Dojo: fmt.Sprintf("D%02d", i)})
						}
						tag := fmt.Sprintf("seededclub pools=%d size=%d seeds=%d extra=%d", numPools, poolSize, nSeeds, clubExtra)
						checkOptimumAndSizesAndSeeds(t, sc, r, numPools, poolSize, false, 2, 1, []string{"SeedClub"}, tag)
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
				oldPools, _, err := BuildPoolPhase(r, poolSize, false, 2)
				if err != nil {
					t.Fatalf("BuildPoolPhase error: %v", err)
				}
				newPools, _, err := BuildPoolPhaseTreeAware(r, poolSize, false, 2, 1)
				if err != nil {
					t.Fatalf("BuildPoolPhaseTreeAware error: %v", err)
				}
				sc.identityConfigs++
				oldOK := identityContractHolds(r, numPools, oldPools)
				newOK := identityContractHolds(r, numPools, newPools)
				if !oldOK {
					sc.identityOldFail++
				}
				if !newOK {
					sc.identityNewFail++
					t.Errorf("pools=%d size=%d: new path breaks the round-robin identity contract", numPools, poolSize)
				}
			}
		}
	})

	// --- Sweep 4: end-to-end region metric across courts 1/2/4 and winners
	// 1/2, judged against the BRUTE-FORCE CEILING for every config. ---
	//
	// The gate originally demanded the three Phase 0 reproducers beat round
	// 1. That demand was WRONG, proven by exhaustive brute force: at
	// winners=1 those shapes' clubs occupy more than half the qualifying
	// pools, so pigeonhole forces a round-1 club pair no matter which pools
	// are chosen -- their ceiling IS 1, for any algorithm. The correct
	// requirement, enforced below for EVERY config with data: the new path's
	// earliest club meeting must EQUAL the best any pool assignment could
	// achieve (max over all pool subsets of the min pairwise meeting round),
	// unfixable shapes included, where equalling a ceiling of 1 passes.
	t.Run("end_to_end_region", func(t *testing.T) {
		for numPools := 3; numPools <= 7; numPools++ {
			for clubSize := 2; clubSize <= numPools+2; clubSize++ {
				poolSize := 4
				if clubSize > numPools*poolSize {
					continue
				}
				r := buildClubRoster(numPools, poolSize, 1, clubSize, 0)
				for _, courts := range []int{1, 2, 4} {
					for _, winners := range []int{1, 2} {
						tag := fmt.Sprintf("e2e pools=%d club=%d courts=%d winners=%d", numPools, clubSize, courts, winners)
						oldPools, drawCourts, err := BuildPoolPhase(r, poolSize, false, courts)
						if err != nil {
							t.Errorf("%s: BuildPoolPhase error: %v", tag, err)
							continue
						}
						newPools, newDrawCourts, err := BuildPoolPhaseTreeAware(r, poolSize, false, courts, winners)
						if err != nil {
							t.Errorf("%s: BuildPoolPhaseTreeAware error: %v", tag, err)
							continue
						}
						checkRegionMetric(t, sc, oldPools, newPools, winners, drawCourts, "Club0", tag)

						newRound := earliestClubMeetingRound(newPools, winners, newDrawCourts, "Club0")
						if newRound != math.MaxInt {
							_, newSizes, tErr := poolTargetSizes(len(r), poolSize, false)
							if tErr != nil {
								t.Errorf("%s: poolTargetSizes: %v", tag, tErr)
								continue
							}
							span := clubSize
							if span > numPools {
								span = numPools
							}
							ceiling := bruteForceMeetingCeiling(newSizes, winners, newDrawCourts, span)
							sc.optimumMeetConfigs++
							if newRound < ceiling {
								sc.optimumMeetBelow++
								t.Errorf("%s: new path meets at round %d but the brute-force ceiling for this shape is %d", tag, newRound, ceiling)
							}
						}
					}
				}
			}
		}
	})

	t.Log(sc.String())

	// Overall gate: the region metric must STRICTLY improve in aggregate,
	// not merely tie. A gate that only promises "no worse" would ship a
	// rebuild that changes nothing.
	if sc.regionNewSum <= sc.regionOldSum {
		t.Errorf("GATE FAIL: region metric did not strictly improve overall: old-sum=%d new-sum=%d", sc.regionOldSum, sc.regionNewSum)
	}
	if sc.regionConfigs == 0 {
		t.Errorf("GATE FAIL: region sweep produced no data (configs with a club spanning >=2 pools) -- the gate cannot be evaluated")
	}
}

// identityContractHolds is TestPoolDistribution_UniqueDojoIdentity's own
// check, extracted so the gate scorecard can run it against both pipelines.
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
// assignment could achieve for a club spanning `span` pools: the maximum,
// over every subset of `span` pools, of the minimum pairwise meeting round
// between the chosen pools' qualifier slots. Exponential in numPools, which
// is capped at 7 in the sweep, so at most C(7,3)=35 subsets of pair-checks.
func bruteForceMeetingCeiling(targetSizes []int, poolWinners, drawCourts, span int) int {
	slots := treeAwareQualifierSlots(targetSizes, poolWinners, drawCourts)
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
