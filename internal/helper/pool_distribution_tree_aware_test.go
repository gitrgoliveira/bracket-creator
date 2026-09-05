package helper

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReorderPositionsIsAValidPermutation sanity-checks reorderPositions'
// output shape.
//
// reorderPositions used to hand-derive ReorderPoolsForCourts' own
// i%numCourts grouping arithmetic a second time, and this test compared
// that hand copy's output against the real ReorderPoolsForCourts run over
// marker pools -- a genuine pin, back when the two were independent
// implementations. reorderPositions itself now BUILDS those same marker
// pools and calls the real ReorderPoolsForCourts internally (see its own
// doc comment), so a test that re-does the identical marker-pool dance and
// compares the two outputs would be comparing reorderPositions against
// itself: it can never fail, however reorderPositions is written, and pins
// nothing (this repo has already learned that lesson once on this branch).
// What is still worth asserting here is that reorderPositions' contract
// holds regardless of implementation: for every numPools/numCourts, `post`
// must be a bijection over [0, numPools) -- every pre-reorder index maps to
// exactly one in-range post-reorder position, and no two collide.
func TestReorderPositionsIsAValidPermutation(t *testing.T) {
	for numPools := 1; numPools <= 10; numPools++ {
		for numCourts := 1; numCourts <= 8; numCourts++ {
			t.Run(fmt.Sprintf("pools=%d courts=%d", numPools, numCourts), func(t *testing.T) {
				post := reorderPositions(numPools, numCourts)
				require.Len(t, post, numPools)
				seen := make([]bool, numPools)
				for preIdx, postIdx := range post {
					require.GreaterOrEqualf(t, postIdx, 0, "pool %d", preIdx)
					require.Lessf(t, postIdx, numPools, "pool %d", preIdx)
					require.Falsef(t, seen[postIdx], "post-reorder position %d claimed by more than one pre-reorder index", postIdx)
					seen[postIdx] = true
				}
			})
		}
	}
}

// seedPoolByName maps every seeded player's Name to the PoolName it ended up
// in, for a finished []Pool. Used to compare seed placement between the old
// and new pipelines regardless of unseeded ordering differences.
func seedPoolByName(pools []Pool) map[string]string {
	out := map[string]string{}
	for _, p := range pools {
		for _, pl := range p.Players {
			if pl.Seed > 0 {
				out[pl.Name] = p.PoolName
			}
		}
	}
	return out
}

// TestSeedPlacementEquality_OldVsTreeAware is Phase 2's mandated pin: every
// seed must land in the SAME named pool as the PRE-SWAP pipeline put it,
// because placeSeedIndices is an extraction of PoolSeeding's own seed
// arithmetic -- any drift means the extraction broke something, not that the
// new path made a different but valid choice.
//
// The old side is referencePoolSeedingPipeline (the gate test's faithful
// reconstruction of PoolSeeding -> CreatePools -> ReorderPoolsForCourts).
// It was BuildPoolPhase when this pin was written, which was right up until
// the Phase 4 swap made BuildPoolPhase DELEGATE to the tree-aware path --
// from that moment the pin compared the new path against itself and held for
// any seed arithmetic whatsoever. A self-comparison that can never fail is
// not a pin.
func TestSeedPlacementEquality_OldVsTreeAware(t *testing.T) {
	total := 0
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			// bc-drwx item 4: the nSeeds <= numPools bound used to exclude
			// every wrapped-seed config (nSeeds > numPools) entirely, so
			// placeSeedIndices' wrapped-seed pool-avoidance passes had no
			// old-vs-new coverage at all. Both sides call the identical
			// shared placeSeedIndices, so this remains byte-identical by
			// construction; the point of widening it is to catch a FUTURE
			// divergence between the two entry points in that range, not
			// to re-verify the fix itself (see
			// TestPlaceSeedIndices_WrappedSeedAvoidsDojoMatePool for that).
			for nSeeds := 2; nSeeds <= 4; nSeeds++ {
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

					for _, numCourts := range []int{1, 2, 4} {
						for _, poolWinners := range []int{1, 2} {
							oldPools, _, err := referencePoolSeedingPipeline(r, poolSize, false, numCourts)
							require.NoError(t, err)
							newPools, _, err := BuildPoolPhaseTreeAware(r, poolSize, false, numCourts, poolWinners)
							require.NoError(t, err)
							total++

							oldSeedPool := seedPoolByName(oldPools)
							newSeedPool := seedPoolByName(newPools)
							require.Equal(t, len(oldSeedPool), len(newSeedPool),
								"pools=%d size=%d seeds=%d extra=%d courts=%d winners=%d: seed count differs",
								numPools, poolSize, nSeeds, dojoExtra, numCourts, poolWinners)
							for name, oldPool := range oldSeedPool {
								assert.Equalf(t, oldPool, newSeedPool[name],
									"pools=%d size=%d seeds=%d extra=%d courts=%d winners=%d: seed %s landed in %s (old) vs %s (new)",
									numPools, poolSize, nSeeds, dojoExtra, numCourts, poolWinners, name, oldPool, newSeedPool[name])
							}
						}
					}
				}
			}
		}
	}
	require.GreaterOrEqual(t, total, 200, "sweep shrank below the measured 210-config space (x courts x winners)")
}

// TestSeedPlacementEquality_MultiDojo broadens the pin with the Phase-0
// multi-dojo roster generator (several dojos at once, isMax variants, seeds
// beyond maxSeedRanks=4).
func TestSeedPlacementEquality_MultiDojo(t *testing.T) {
	total := 0
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			for _, isMax := range []bool{false, true} {
				for nDojos := 2; nDojos <= 4; nDojos++ {
					for dojoGroupSize := 2; dojoGroupSize <= numPools+2; dojoGroupSize++ {
						// bc-drwx item 4: was `nSeeds < numPools`, which never
						// reached a wrapped seed (nSeeds > numPools) at all.
						// Both sides call the identical shared
						// placeSeedIndices, so widening this stays
						// byte-identical by construction; see
						// TestSeedPlacementEquality_OldVsTreeAware's own
						// comment for why the equality itself is not what
						// verifies the fix.
						for nSeeds := 1; nSeeds <= 4; nSeeds++ {
							// nSeeds starts at 1: a seedless config has no
							// placement to compare, and the seedless sweep
							// burned most of this test's former 23-second
							// runtime comparing empty maps.
							if nDojos*dojoGroupSize > numPools*poolSize {
								continue
							}
							r := buildMultiDojoRoster(numPools, poolSize, nDojos, dojoGroupSize, nSeeds)
							for _, courts := range []int{1, 2} {
								oldPools, _, err := referencePoolSeedingPipeline(r, poolSize, isMax, courts)
								require.NoError(t, err)
								newPools, _, err := BuildPoolPhaseTreeAware(r, poolSize, isMax, courts, 1)
								require.NoError(t, err)
								total++

								oldSeedPool := seedPoolByName(oldPools)
								newSeedPool := seedPoolByName(newPools)
								for name, oldPool := range oldSeedPool {
									assert.Equalf(t, oldPool, newSeedPool[name],
										"pools=%d size=%d isMax=%v dojos=%dx%d seeds=%d courts=%d: seed %s mismatch",
										numPools, poolSize, isMax, nDojos, dojoGroupSize, nSeeds, courts, name)
								}
							}
						}
					}
				}
			}
		}
	}
	require.GreaterOrEqual(t, total, 2000, "sweep shrank; the pin is meaningless if it no longer covers the measured space")
}

// TestBuildPoolPhaseTreeAware_SizesMatchOldPath pins that the new path's
// pool SIZES equal the old path's on every config -- a hard requirement
// independent of who ends up in which pool.
func TestBuildPoolPhaseTreeAware_SizesMatchOldPath(t *testing.T) {
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			for _, isMax := range []bool{false, true} {
				for _, courts := range []int{1, 2, 4} {
					n := numPools * poolSize
					r := make([]Player, n)
					for i := range r {
						r[i] = Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo%03d", i+1)}
					}
					oldPools, oldCourts, err := BuildPoolPhase(r, poolSize, isMax, courts)
					require.NoError(t, err)
					newPools, newCourts, err := BuildPoolPhaseTreeAware(r, poolSize, isMax, courts, 1)
					require.NoError(t, err)

					require.Equal(t, oldCourts, newCourts, "pools=%d size=%d isMax=%v courts=%d: drawCourts differs", numPools, poolSize, isMax, courts)
					require.Len(t, newPools, len(oldPools))
					oldSizes := make([]int, len(oldPools))
					newSizes := make([]int, len(newPools))
					for i, p := range oldPools {
						oldSizes[i] = len(p.Players)
					}
					for i, p := range newPools {
						newSizes[i] = len(p.Players)
					}
					assert.Equal(t, oldSizes, newSizes, "pools=%d size=%d isMax=%v courts=%d: pool sizes differ", numPools, poolSize, isMax, courts)

					total := 0
					for _, p := range newPools {
						total += len(p.Players)
					}
					assert.Equal(t, n, total, "player lost or duplicated in the new path")
				}
			}
		}
	}
}

// TestBuildPoolPhaseTreeAware_NoTwoSeedsShareAPool is a basic sanity check
// independent of the scorecard: the new path must never put two seeds in one
// pool, exactly like the old path.
func TestBuildPoolPhaseTreeAware_NoTwoSeedsShareAPool(t *testing.T) {
	for numPools := 3; numPools <= 7; numPools++ {
		for nSeeds := 2; nSeeds <= 4 && nSeeds <= numPools; nSeeds++ {
			r := buildMultiDojoRoster(numPools, 4, 1, nSeeds, nSeeds)
			pools, _, err := BuildPoolPhaseTreeAware(r, 4, false, 2, 1)
			require.NoError(t, err)
			for _, p := range pools {
				seeds := 0
				for _, pl := range p.Players {
					if pl.Seed > 0 {
						seeds++
					}
				}
				assert.LessOrEqual(t, seeds, 1, "two seeds share %s (pools=%d seeds=%d)", p.PoolName, numPools, nSeeds)
			}
		}
	}
}

// TestBuildPoolPhaseTreeAwareWithMode_RefusesBlankDojo pins FIX 1
// (bc-dojo-least-conflicted-pool): a roster containing a blank-dojo player
// must be refused outright by the shared pre-flight
// (buildPoolPhaseTreeAwareCore), reached by all three tree-aware entry
// points, rather than silently corrupting the descent's capacity accounting
// (recordDojoOccupancy is guarded on `p.Dojo != ""`, so a blank-dojo player
// consumes a real pool seat without the tree ever seeing it) or
// improveDojoMeetings' spread/meeting objective (which would otherwise count
// Dojo=="" as a phantom dojo). The error must name the offending player and
// no pools may be returned.
func TestBuildPoolPhaseTreeAwareWithMode_RefusesBlankDojo(t *testing.T) {
	players := []Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "NoDojo", Dojo: ""},
		{Name: "Carol", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoA"},
		{Name: "Erin", Dojo: "DojoB"},
		{Name: "Frank", Dojo: "DojoC"},
		{Name: "Grace", Dojo: "DojoA"},
	}

	pools, drawCourts, err := BuildPoolPhaseTreeAwareWithMode(players, 4, false, 1, 2, "")
	require.Error(t, err, "a blank-dojo roster must be refused, not silently drawn")
	assert.ErrorIs(t, err, ErrBlankDojoInDraw)
	assert.Contains(t, err.Error(), "NoDojo", "the error must name the offending player so the operator knows which row to repair")
	assert.Nil(t, pools)
	assert.Zero(t, drawCourts)

	// Every tree-aware entry point funnels through the same pre-flight.
	pools2, _, err2 := BuildPoolPhaseTreeAware(players, 4, false, 1, 2)
	require.Error(t, err2)
	assert.ErrorIs(t, err2, ErrBlankDojoInDraw)
	assert.Nil(t, pools2)

	pools3, _, err3 := BuildPoolPhaseFillBracketTreeAware(players, 4, 1)
	require.Error(t, err3)
	assert.ErrorIs(t, err3, ErrBlankDojoInDraw)
	assert.Nil(t, pools3)
}

// TestBuildPoolPhaseTreeAwareWithMode_RefusesBlankDojo_MultipleNames checks
// that every offending player is named, not just the first found, so an
// operator with several bad rows can fix them all from one error rather
// than re-running the draw once per row.
func TestBuildPoolPhaseTreeAwareWithMode_RefusesBlankDojo_MultipleNames(t *testing.T) {
	players := []Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "NoDojoOne", Dojo: ""},
		{Name: "NoDojoTwo", Dojo: ""},
		{Name: "Carol", Dojo: "DojoC"},
	}
	_, _, err := BuildPoolPhaseTreeAwareWithMode(players, 2, false, 1, 2, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NoDojoOne")
	assert.Contains(t, err.Error(), "NoDojoTwo")
}

// TestBuildPoolPhaseTreeAwareWithMode_RefusesWhitespaceOnlyDojo pins
// ValidateNoBlankDojo's TrimSpace alignment with state.ErrBlankDojo's own
// write-floor check (saveParticipantsNoLock): a Dojo of "   " is exactly as
// blank as "" and must be refused at the draw too, not just at the
// participant-write floor, so a future in-memory producer that skips that
// floor cannot slip whitespace past this guard.
func TestBuildPoolPhaseTreeAwareWithMode_RefusesWhitespaceOnlyDojo(t *testing.T) {
	players := []Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "WhitespaceDojo", Dojo: "   "},
		{Name: "Carol", Dojo: "DojoC"},
	}
	_, _, err := BuildPoolPhaseTreeAwareWithMode(players, 1, false, 1, 2, "")
	require.Error(t, err, "a whitespace-only dojo must be refused, not silently drawn")
	assert.ErrorIs(t, err, ErrBlankDojoInDraw)
	assert.Contains(t, err.Error(), "WhitespaceDojo", "the error must name the offending player")
}

// referenceEarliestDojoMeeting is the pre-P3 nested-scan algorithm, kept
// here ONLY as an independent oracle for TestEarliestDojoMeeting_MatchesReference:
// it rediscovers pool membership inside the pair loop itself (O(P^2*poolSize))
// instead of collecting the dojo's occupied pools once up front, which is
// exactly the shape earliestDojoMeeting had before bc-dojo-least-conflicted-pool's
// P3 speedup. Any behavioural drift between this and the real function is a
// bug in the speedup, not a matter of style.
func referenceEarliestDojoMeeting(pools []Pool, pairRound [][]int, dojo string, keys dojoKeyCache) int {
	earliest := math.MaxInt
	for i := range pools {
		if countDojoInPool(pools[i], dojo, keys) == 0 || i >= len(pairRound) {
			continue
		}
		for j := i + 1; j < len(pools); j++ {
			if countDojoInPool(pools[j], dojo, keys) == 0 || j >= len(pairRound) {
				continue
			}
			if r := pairRound[i][j]; r < earliest {
				earliest = r
			}
		}
	}
	return earliest
}

// TestEarliestDojoMeeting_MatchesReference pins the P3 rewrite of
// earliestDojoMeeting (collect the dojo's occupied pools once, then scan
// only those via direct matrix lookups) against referenceEarliestDojoMeeting,
// the original nested-scan algorithm, across pool/dojo shapes including a
// dojo absent from every pool, present in exactly one, present in many
// (interleaved with other dojos), an empty dojo string (present and absent),
// a single pool holding the whole dojo, zero pools, and a pairRound matrix
// shorter than len(pools) (the "no region information" fallback the real
// caller hits when a mode's skeleton builder returns nil).
func TestEarliestDojoMeeting_MatchesReference(t *testing.T) {
	makePool := func(dojos ...string) Pool {
		var players []Player
		for i, d := range dojos {
			players = append(players, Player{Name: fmt.Sprintf("P%d", i), Dojo: d})
		}
		return Pool{Players: players}
	}

	// A deterministic, asymmetric-looking pairRound matrix so that a wrong
	// index pairing (e.g. transposed i/j, or an off-by-one shift) produces a
	// different value rather than accidentally matching by symmetry.
	buildPairRound := func(n int) [][]int {
		m := make([][]int, n)
		for i := range m {
			m[i] = make([]int, n)
			for j := range m[i] {
				if i == j {
					m[i][j] = math.MaxInt
					continue
				}
				m[i][j] = i*7 + j*3 + 1
			}
		}
		return m
	}

	cases := []struct {
		name           string
		pools          []Pool
		dojo           string
		shortPairRound bool // when true, pairRound is forced to length 0
	}{
		{
			name:  "dojo absent from every pool",
			pools: []Pool{makePool("A"), makePool("B"), makePool("C")},
			dojo:  "Z",
		},
		{
			name:  "dojo present in exactly one pool",
			pools: []Pool{makePool("A"), makePool("B", "A"), makePool("C")},
			dojo:  "A",
		},
		{
			name: "dojo present in many pools, interleaved with others",
			pools: []Pool{
				makePool("X", "Y"),
				makePool("Y"),
				makePool("X", "Z"),
				makePool("Z", "X"),
				makePool("Y", "Y"),
				makePool("X"),
				makePool("W", "X", "Z"),
				makePool("Y", "Z"),
			},
			dojo: "X",
		},
		{
			name:  "empty dojo string present in some pools",
			pools: []Pool{makePool("", "A"), makePool("B"), makePool("", "")},
			dojo:  "",
		},
		{
			name:  "empty dojo string absent",
			pools: []Pool{makePool("A"), makePool("B")},
			dojo:  "",
		},
		{
			name:  "single pool holding the whole dojo",
			pools: []Pool{makePool("A", "A", "A")},
			dojo:  "A",
		},
		{
			name:  "no pools at all",
			pools: nil,
			dojo:  "A",
		},
		{
			name:           "pairRound shorter than pools (feature-disabled fallback)",
			pools:          []Pool{makePool("A"), makePool("A"), makePool("A")},
			dojo:           "A",
			shortPairRound: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pairRound [][]int
			if tc.shortPairRound {
				pairRound = [][]int{}
			} else {
				pairRound = buildPairRound(len(tc.pools))
			}
			keys := make(dojoKeyCache)
			want := referenceEarliestDojoMeeting(tc.pools, pairRound, tc.dojo, keys)
			// count ignores its id argument and always resolves back to
			// tc.dojo directly: earliestDojoMeeting only ever calls it with
			// the ONE id this test case resolves below, so there is nothing
			// to translate an id back to a dojo string FOR.
			count := dojoCounter(func(poolIdx int, id int) int {
				return countDojoInPool(tc.pools[poolIdx], tc.dojo, keys)
			})
			ids := newDojoIDCache(keys, 0)
			got := earliestDojoMeeting(tc.pools, pairRound, ids.of(tc.dojo), count)
			assert.Equal(t, want, got)
		})
	}
}

// buildPreRepairPoolsForTest replicates buildPoolPhaseTreeAwareCore's steps
// 1-3 (seed placement, then the dojo-tree descent over the unseeded) for
// STANDARD mode, stopping just before step 4 (improveDojoMeetings) so a
// test can hand the identical starting pools to two different repair
// implementations and compare their output directly. baseTargetSizes must
// already sum to len(players) (realTargetSizes is then a no-op, matching
// the only shapes this test needs). Mirrors buildPoolPhaseTreeAwareCore's
// body exactly; if that function's pre-repair steps ever change, this must
// change with it.
func buildPreRepairPoolsForTest(t *testing.T, players []Player, numPools int, baseTargetSizes []int, numCourts, poolWinners int) ([]Pool, []int, [][]int) {
	t.Helper()
	require.NoError(t, ValidateNoBlankDojo(players))

	drawCourts := EffectiveDrawCourts(numPools, numCourts)
	pools := make([]Pool, numPools)
	for i := range pools {
		pools[i].PoolName = poolPositionName(i)
	}
	targetSizes := realTargetSizes(baseTargetSizes, len(players))
	seeded, unseeded := partitionSeeded(players)

	seedIdx := placeSeedIndices(seeded, numPools, clampCourts(drawCourts), len(players))
	seedPoolIdx := make(map[int]int, len(seeded))
	for si, idx := range seedIdx {
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
	mode := qualifierMode{ExtraQualifiers: QualifierModeStandard, SeedPoolIndex: seedPoolIdx}

	qualifierSlots := treeAwareQualifierSlots(targetSizes, poolWinners, drawCourts, mode)
	ids, keys := newDojoIDCacheFor(players)
	require.NoError(t, assignUnseededByDojoTree(pools, targetSizes, unseeded, qualifierSlots, keys, ids))
	return pools, targetSizes, qualifierSlots
}

// clonePools returns a deep-enough copy of pools (PoolName plus an
// independent Players slice) so two repair implementations can each mutate
// their own copy from the identical starting point.
func clonePools(pools []Pool) []Pool {
	out := make([]Pool, len(pools))
	for i, p := range pools {
		out[i] = Pool{PoolName: p.PoolName}
		out[i].Players = append([]Player(nil), p.Players...)
	}
	return out
}

// referenceImproveDojoMeetings is a faithful copy of improveDojoMeetings as
// it existed BEFORE the P2 speedup (bc-dojo-least-conflicted-pool): every
// "before" meeting round is recomputed from scratch via earliestDojoMeeting
// on every (i, ai, j, bi) candidate, instead of hoisting the a-side value to
// the ai level and caching the b-side value per dojo for the whole pass.
// Kept ONLY as TestImproveDojoMeetings_MatchesUncachedReference's oracle --
// the acceptance rule, tie-break order, objective and scan-restart-after-
// accept semantics are copied unchanged, so any drift from the real
// (cached) function can only be attributed to the caching itself.
func referenceImproveDojoMeetings(pools []Pool, targetSizes []int, qualifierSlots [][]int, roster []Player, keys dojoKeyCache) {
	winnerSlots := make([][]int, len(qualifierSlots))
	for i, s := range qualifierSlots {
		if len(s) > 0 {
			winnerSlots[i] = s[:1]
		}
	}
	pairRound := poolPairRounds(winnerSlots)
	allQualPairRound := poolPairRounds(qualifierSlots)
	// ids/idOfID exist only to translate the (bc-pnum) int-id interface
	// earliestDojoMeeting/dojoCounter now take back into the raw dojo
	// string this reference's own count closure needs -- the reference
	// itself stays string-keyed throughout, on purpose (see below).
	ids := newDojoIDCache(keys, len(roster))
	idOfID := make(map[int]string, len(roster))
	for _, p := range roster {
		idOfID[ids.of(p.Dojo)] = p.Dojo
	}
	// count wraps countDojoInPool directly (unlike the real function's own
	// incrementally-maintained `counts` map): this reference exists to pin
	// the P2 caching optimisation alone, so its own dojo-count source stays
	// the pre-P2 (and pre-bc-drwx-item-1) O(poolSize) scan on purpose.
	count := dojoCounter(func(poolIdx int, id int) int {
		return countDojoInPool(pools[poolIdx], idOfID[id], keys)
	})
	// Normalized via dojoKey (bc-drwx item 3), matching the real function,
	// so this reference's only remaining difference from it is the caching
	// this test exists to isolate (see this function's own doc comment).
	footprint := make(map[string]int, len(roster))
	for _, p := range roster {
		footprint[keys.of(p.Dojo)]++
	}
	numPools := len(pools)
	optimum := func(dojo string) int {
		return (footprint[keys.of(dojo)] + numPools - 1) / numPools
	}
	excessOf := func(dojo string, count int) int {
		if over := count - optimum(dojo); over > 0 {
			return over
		}
		return 0
	}
	totalExcess := func() int {
		total := 0
		counts := map[string]int{}
		for i := range pools {
			for k := range counts {
				delete(counts, k)
			}
			for _, pl := range pools[i].Players {
				counts[keys.of(pl.Dojo)]++
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
				if seen[keys.of(pl.Dojo)] {
					continue
				}
				seen[keys.of(pl.Dojo)] = true
				plID := ids.of(pl.Dojo)
				if m := earliestDojoMeeting(pools, pairRound, plID, count); m != math.MaxInt {
					if m <= 1 {
						roundOnes++
					}
					negSum -= m
				}
				if m := earliestDojoMeeting(pools, allQualPairRound, plID, count); m != math.MaxInt {
					allQualNegSum -= m
				}
			}
		}
		return excess, roundOnes, negSum, allQualNegSum
	}
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

	for pass := 0; pass < len(roster)*numPools+1; pass++ {
		curExc, curR1, curNS, curAQ := objective()
		improved := false
		for i := 0; i < numPools && !improved; i++ {
			for ai := 0; ai < len(pools[i].Players) && !improved; ai++ {
				a := pools[i].Players[ai]
				if a.Seed > 0 {
					continue
				}
				aID := ids.of(a.Dojo)
				hasMeetingSignal := earliestDojoMeeting(pools, pairRound, aID, count) != math.MaxInt
				hasExcessSignal := excessOf(a.Dojo, countDojoInPool(pools[i], a.Dojo, keys)) > 0
				if !hasMeetingSignal && !hasExcessSignal {
					continue
				}
				for j := 0; j < numPools && !improved; j++ {
					if j == i {
						continue
					}
					for bi := 0; bi < len(pools[j].Players) && !improved; bi++ {
						b := pools[j].Players[bi]
						if b.Seed > 0 || keys.of(b.Dojo) == keys.of(a.Dojo) {
							continue
						}
						bID := ids.of(b.Dojo)
						cAi := countDojoInPool(pools[i], a.Dojo, keys)
						cAj := countDojoInPool(pools[j], a.Dojo, keys)
						cBj := countDojoInPool(pools[j], b.Dojo, keys)
						cBi := countDojoInPool(pools[i], b.Dojo, keys)
						beforeExc := excessOf(a.Dojo, cAi) + excessOf(a.Dojo, cAj) + excessOf(b.Dojo, cBi) + excessOf(b.Dojo, cBj)
						afterExc := excessOf(a.Dojo, cAi-1) + excessOf(a.Dojo, cAj+1) + excessOf(b.Dojo, cBi+1) + excessOf(b.Dojo, cBj-1)
						deltaExc := afterExc - beforeExc
						if deltaExc > 0 {
							continue
						}
						beforeA := earliestDojoMeeting(pools, pairRound, aID, count)
						beforeB := earliestDojoMeeting(pools, pairRound, bID, count)
						beforeAQA := earliestDojoMeeting(pools, allQualPairRound, aID, count)
						beforeAQB := earliestDojoMeeting(pools, allQualPairRound, bID, count)
						pools[i].Players[ai], pools[j].Players[bi] = b, a
						afterA := earliestDojoMeeting(pools, pairRound, aID, count)
						afterB := earliestDojoMeeting(pools, pairRound, bID, count)
						afterAQA := earliestDojoMeeting(pools, allQualPairRound, aID, count)
						afterAQB := earliestDojoMeeting(pools, allQualPairRound, bID, count)
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
			break
		}
	}

	for i := range pools {
		for k := range pools[i].Players {
			pools[i].Players[k].PoolPosition = int64(k + 1)
		}
	}
}

// TestImproveDojoMeetings_MatchesUncachedReference pins the P2 speedup
// (hoist the a-side "before" meeting rounds to the ai level, cache the
// b-side ones per dojo for the whole scan pass) against
// referenceImproveDojoMeetings, the pre-P2 always-recompute version, from
// identical starting pools, across a namesake-free roster, clustered-dojo
// rosters (poolWinners 1 and 2), a multi-dojo interleaved roster, and a
// roster where one dojo heavily oversubscribes the per-pool optimum (the
// shape that exercises tier (a) excess).
func TestImproveDojoMeetings_MatchesUncachedReference(t *testing.T) {
	dojoGrouped := func(nDojos, groupSize int) []Player {
		var out []Player
		for c := 0; c < nDojos; c++ {
			for i := 0; i < groupSize; i++ {
				out = append(out, Player{Name: fmt.Sprintf("C%d_%02d", c, i), Dojo: fmt.Sprintf("Dojo%d", c)})
			}
		}
		return out
	}
	interleaved := func(n int, dojos []string) []Player {
		out := make([]Player, n)
		for i := range out {
			out[i] = Player{Name: fmt.Sprintf("I%02d", i), Dojo: dojos[i%len(dojos)]}
		}
		return out
	}
	namesakeFree := func(n int) []Player {
		out := make([]Player, n)
		for i := range out {
			out[i] = Player{Name: fmt.Sprintf("U%02d", i), Dojo: fmt.Sprintf("Dojo%02d", i)}
		}
		return out
	}

	cases := []struct {
		name                             string
		players                          []Player
		poolSize, poolWinners, numCourts int
	}{
		{"namesake-free", namesakeFree(12), 3, 1, 2},
		{"clustered dojo, poolWinners=1", dojoGrouped(4, 4), 4, 1, 2},
		{"clustered dojo, poolWinners=2", dojoGrouped(6, 4), 4, 2, 2},
		{"multi-dojo interleaved", interleaved(24, []string{"Alpha", "Beta", "Gamma"}), 4, 1, 3},
		{"heavy single-dojo oversubscription", dojoGrouped(2, 8), 4, 1, 2},
		// Known (documented in TestProduction_DeepOversubscription_SpreadTierClosesLastSeat)
		// to require the repair loop's tier (a) to fire at least once: 24
		// entrants, exactly half from one dojo, zero slack anywhere, pool
		// size 4, 2 courts, poolWinners=2 -- this is what actually
		// exercises the multi-pass cache invalidation P2 touches, unlike
		// the shapes above, which the dojo-tree descent alone already
		// solves without any accepted exchange.
		{"deep-oversubscription (forces repair passes)", drawGoldenDojoRoster(24, 12, drawGoldenDojoName), drawGoldenPoolSize, drawGoldenDojoPoolWinners, 2},
		// bc-pnum review (G1), belt and braces: the case above only ever
		// fires tier (a) ONCE (confirmed by instrumenting improveDojoMeetings
		// during review: exactly one accepted swap), so it can pin the P2
		// cache but cannot exercise a SECOND accepted exchange re-reading
		// poolDojoIDs/counts after the first one already moved them. That
		// gap mattered for the OLD hand-mirrored swap/revert (two
		// independently typed-out call sites): a half-update there could
		// stay latent through a REJECTED candidate (whose own revert
		// happens to re-assert the correct values regardless) and only
		// surface once an ACCEPTED swap's corruption was read back by a
		// later pass. Doubling the same shape (48 entrants, still exactly
		// half from one dojo, zero slack) reaches two accepted swaps
		// (confirmed the same way) and is kept as this belt-and-braces
		// case even though the exchange closure below turned out to make
		// the corruption MUCH louder than that: a half-update inside the
		// shared `exchange` closure corrupts poolDojoIDs on the very FIRST
		// candidate it is ever called for, accepted or rejected (the
		// closure's revert is a second call to itself, so it reads back
		// the already-wrong value rather than independently re-asserting
		// the right one) -- verified by mutation (dropping exchange's
		// poolDojoIDs[j][bi] write): this case fails, ALONGSIDE the
		// single-swap case above and several others, since the corruption
		// is no longer confined to the accepted-swap-then-reused-later
		// path the old architecture had.
		{"deep-oversubscription, two accepted swaps", drawGoldenDojoRoster(48, 24, drawGoldenDojoName), drawGoldenPoolSize, drawGoldenDojoPoolWinners, 2},
	}

	runCase := func(t *testing.T, players []Player, poolSize, poolWinners, numCourts int) {
		t.Helper()
		numPools, baseTargetSizes, err := poolTargetSizes(len(players), poolSize, false)
		require.NoError(t, err)

		pools, targetSizes, qualifierSlots := buildPreRepairPoolsForTest(t, players, numPools, baseTargetSizes, numCourts, poolWinners)

		poolsCached := clonePools(pools)
		poolsRef := clonePools(pools)

		cachedIDs, _ := newDojoIDCacheFor(players)
		improveDojoMeetings(poolsCached, qualifierSlots, cachedIDs)
		referenceImproveDojoMeetings(poolsRef, targetSizes, qualifierSlots, players, make(dojoKeyCache))

		require.Len(t, poolsCached, len(poolsRef))
		for i := range poolsCached {
			require.Equalf(t, len(poolsRef[i].Players), len(poolsCached[i].Players), "pool %d size mismatch", i)
			for k := range poolsCached[i].Players {
				assert.Equalf(t, poolsRef[i].Players[k].Name, poolsCached[i].Players[k].Name,
					"pool %d slot %d: cached=%q reference=%q", i, k, poolsCached[i].Players[k].Name, poolsRef[i].Players[k].Name)
				assert.Equalf(t, poolsRef[i].Players[k].PoolPosition, poolsCached[i].Players[k].PoolPosition,
					"pool %d slot %d position mismatch", i, k)
			}
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCase(t, tc.players, tc.poolSize, tc.poolWinners, tc.numCourts)
		})
	}

	// The named cases above are illustrative but mostly no-ops for the
	// repair loop: the dojo-tree descent already solves them without a
	// single accepted exchange, so they never touch the pass-scoped cache
	// this test exists to pin. A broad multi-dojo sweep (the same
	// buildMultiDojoRoster shape TestPoolDistribution_Invariants uses,
	// documented elsewhere as the space where ~12/1596 configs need the
	// repair loop to run MULTIPLE accepted exchanges) is what actually
	// forces the cache to survive across several passes.
	t.Run("broad multi-dojo sweep", func(t *testing.T) {
		total := 0
		for numPools := 3; numPools <= 6; numPools++ {
			for poolSize := 3; poolSize <= 5; poolSize++ {
				for nDojos := 2; nDojos <= 4; nDojos++ {
					for dojoGroupSize := 2; dojoGroupSize <= 2*numPools; dojoGroupSize++ {
						if nDojos*dojoGroupSize > numPools*poolSize {
							continue
						}
						for _, poolWinners := range []int{1, 2} {
							for _, numCourts := range []int{1, 2} {
								r := buildMultiDojoRoster(numPools, poolSize, nDojos, dojoGroupSize, 0)
								total++
								t.Run(fmt.Sprintf("pools=%d size=%d dojos=%dx%d winners=%d courts=%d", numPools, poolSize, nDojos, dojoGroupSize, poolWinners, numCourts), func(t *testing.T) {
									runCase(t, r, poolSize, poolWinners, numCourts)
								})
							}
						}
					}
				}
			}
		}
		require.GreaterOrEqual(t, total, 300, "sweep shrank; the pin is meaningless if it no longer covers the measured space")
	})
}
