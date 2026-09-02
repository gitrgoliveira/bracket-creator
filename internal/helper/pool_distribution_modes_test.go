package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-dojo Phase 4: seam tests for the mode-aware qualifier-slot dispatch
// (treeAwareQualifierSlots / qualifierMode, pool_distribution_tree_aware.go)
// against the REAL production draw builders -- BuildKnockoutDrawPerPool for
// larger-pools, BuildKnockoutDrawFillBracket for fill-bracket -- mirroring
// the standard-mode gate test's own end-to-end sweep
// (pool_distribution_gate_test.go's "end_to_end_region" subtest), which
// only ever exercised the standard/uniform skeleton.
//
// Both tests hold pool COUNT and per-pool target SIZES fixed between a
// mode-AWARE placement and a mode-BLIND one (the identical roster and
// target sizes, but scored with qualifierModeStandard instead of the real
// mode) and feed BOTH into the SAME real per-mode draw builder. Pool count
// and sizes being fixed also fixes which pool INDICES are oversized/seeded,
// so larger-pools' overrides map and fill-bracket's draftPoolIdx are
// IDENTICAL between the two runs -- the two pool sets differ only in WHICH
// PLAYER occupies which pool, never in the knockout tree's own shape. That
// isolates exactly what mode-awareness is supposed to buy: better dojo
// placement against the tree production ACTUALLY cuts for that mode, not a
// different tree.

// buildLargerPoolsOverrides mirrors state.Competition.QualifiersForPool's
// larger-pools rule (poolWinners+1 for a pool whose participant count
// exceeds minSize) directly off REAL, already-formed pools -- the same test
// double cmd/create-pools.go's cliExtraQualifierOverrides and
// internal/engine/playoff_skeleton.go's extraQualifierOverrides use their
// own state.Competition for, reimplemented here since a helper-package test
// cannot import internal/state (state imports helper).
func buildLargerPoolsOverrides(pools []Pool, minSize, poolWinners int) map[int]int {
	var overrides map[int]int
	for i, p := range pools {
		if len(p.Players) > minSize {
			if overrides == nil {
				overrides = make(map[int]int, len(pools))
			}
			overrides[i] = poolWinners + 1
		}
	}
	return overrides
}

// TestQualifierModeSeam_LargerPools sweeps a handful of oversubscribed-dojo
// shapes at poolWinners=1 (state.ValidateExtraQualifiers' own gate for this
// mode) and asserts:
//  1. BuildPoolPhaseTreeAwareWithMode(..., "larger-pools") produces pools
//     BuildKnockoutDrawPerPool (production's real larger-pools builder) can
//     build a draw from.
//  2. Scoring against the REAL larger-pools tree (mode-aware) never places
//     a dojo's own qualifiers to meet EARLIER than mode-blind (standard
//     scoring) placement would, for the identical pool count/sizes fed to
//     the identical real draw -- and at least one swept config shows a
//     STRICT improvement, proving the seam changes the outcome rather than
//     being a no-op wired to nothing.
func TestQualifierModeSeam_LargerPools(t *testing.T) {
	const poolWinners = 1
	strictImprovement := 0
	configs := 0

	for numPools := 3; numPools <= 6; numPools++ {
		for minSize := 3; minSize <= 4; minSize++ {
			for _, courts := range []int{1, 2} {
				// One player past an exact multiple: PoolCount's own floor
				// division keeps numPools pools and forces exactly ONE of
				// them to minSize+1 via realTargetSizes' remainder spread --
				// the oversized pool larger-pools' extra qualifier exists
				// for.
				n := numPools*minSize + 1
				r := buildMultiDojoRoster(numPools, minSize, 1, 2, 0)
				// buildMultiDojoRoster sizes its roster at numPools*poolSize
				// exactly; pad by one extra unique-dojo filler to reach n.
				r = append(r, Player{Name: "Filler", Dojo: "FillerDojo"})
				require.Len(t, r, n)

				tag := fmt.Sprintf("larger-pools pools=%d minSize=%d courts=%d", numPools, minSize, courts)

				awarePools, drawCourts, err := BuildPoolPhaseTreeAwareWithMode(r, minSize, false, courts, poolWinners, qualifierModeLargerPools)
				require.NoError(t, err, tag)

				numPoolsGot, baseSizes, err := poolTargetSizes(n, minSize, false)
				require.NoError(t, err, tag)
				blindPools, _, err := buildPoolPhaseTreeAwareCore(r, numPoolsGot, baseSizes, courts, poolWinners, qualifierMode{ExtraQualifiers: qualifierModeStandard})
				require.NoError(t, err, tag)

				// Pool count/sizes must be identical between the two runs,
				// which is what makes overrides (and therefore the real
				// draw's SHAPE) identical too.
				require.Len(t, blindPools, len(awarePools), tag)
				for i := range awarePools {
					require.Equal(t, len(blindPools[i].Players), len(awarePools[i].Players), "%s: pool %d size differs between mode-aware and mode-blind runs", tag, i)
				}

				overrides := buildLargerPoolsOverrides(awarePools, minSize, poolWinners)
				awareDraw := BuildKnockoutDrawPerPool(awarePools, poolWinners, overrides, drawCourts)
				require.NotNil(t, awareDraw, "%s: BuildKnockoutDrawPerPool refused the mode-aware pools", tag)
				blindDraw := BuildKnockoutDrawPerPool(blindPools, poolWinners, overrides, drawCourts)
				require.NotNil(t, blindDraw, "%s: BuildKnockoutDrawPerPool refused the mode-blind pools", tag)

				awareRound := earliestMeetingRoundInDraw(awareDraw, awarePools, "Dojo0")
				blindRound := earliestMeetingRoundInDraw(blindDraw, blindPools, "Dojo0")
				if awareRound == mathMaxIntSentinel {
					continue // Dojo0 did not span >=2 pools in this shape: no data
				}
				configs++
				assert.GreaterOrEqualf(t, awareRound, blindRound,
					"%s: mode-aware larger-pools placement (round %d) is WORSE than mode-blind (round %d)", tag, awareRound, blindRound)
				if awareRound > blindRound {
					strictImprovement++
				}
			}
		}
	}

	require.Greater(t, configs, 0, "sweep produced no data (no config had Dojo0 spanning >=2 pools)")
	assert.Greater(t, strictImprovement, 0, "mode-aware larger-pools scoring never strictly improved over mode-blind scoring across the sweep; the seam may be wired to nothing")
}

// TestQualifierModeSeam_FillBracket mirrors TestQualifierModeSeam_LargerPools
// for fill-bracket mode: BuildPoolPhaseFillBracketTreeAware's pools must
// feed SelectFillBracketDraftIndices + BuildKnockoutDrawFillBracket
// (production's real fill-bracket pipeline) successfully, and mode-aware
// scoring must never place a dojo to meet earlier than mode-blind scoring
// does under the IDENTICAL real fill-bracket draw (same pool count/sizes,
// hence the same draftPoolIdx -- draft selection reads only sizes and which
// pools are seeded, both invariant between the two runs).
func TestQualifierModeSeam_FillBracket(t *testing.T) {
	const minSize = 3
	strictImprovement := 0
	configs := 0

	for _, n := range []int{20, 23, 29, 38} {
		for _, courts := range []int{1, 2, 4} {
			tag := fmt.Sprintf("fill-bracket n=%d courts=%d", n, courts)

			// A single UNRELATED seed (rank 1) gives fill-bracket's own
			// seeded-pools-first draft selection something to draft from
			// (mirrors TestBuildPoolPhaseFillBracket_45's own shape) --
			// Dojo0 itself is entirely UNSEEDED, deliberately: a seed's
			// pool is fixed by placeSeedIndices regardless of qualifier
			// mode (mode only ever influences the UNSEEDED one-pass
			// placement), so a dojo made of seeds would land identically
			// under mode-aware and mode-blind scoring and could never show
			// the improvement this test exists to catch.
			r := make([]Player, 0, n)
			r = append(r, Player{Name: "Seed1", Dojo: "SeedDojo", Seed: 1})
			r = append(r,
				Player{Name: "C0_1", Dojo: "Dojo0"},
				Player{Name: "C0_2", Dojo: "Dojo0"},
			)
			for i := len(r) + 1; i <= n; i++ {
				r = append(r, Player{Name: fmt.Sprintf("O%d", i), Dojo: fmt.Sprintf("D%02d", i)})
			}

			awarePools, drawCourts, err := BuildPoolPhaseFillBracketTreeAware(r, minSize, courts)
			if err != nil {
				// Not every (n, courts) combination has a valid
				// FillBracketPoolCount formation; skip rather than fail the
				// seam test on a shape this package's own formation rules
				// already refuse.
				continue
			}

			seeded, _ := partitionSeeded(r)
			seedRanks := make([]int, len(seeded))
			for i, p := range seeded {
				seedRanks[i] = p.Seed
			}
			numPoolsGot, _, ferr := FillBracketPoolCount(n, minSize, seedRanks)
			require.NoError(t, ferr, tag)
			base := make([]int, numPoolsGot)
			for i := range base {
				base[i] = minSize
			}
			blindPools, _, err := buildPoolPhaseTreeAwareCore(r, numPoolsGot, base, courts, 1, qualifierMode{ExtraQualifiers: qualifierModeStandard})
			require.NoError(t, err, tag)

			require.Len(t, blindPools, len(awarePools), tag)
			for i := range awarePools {
				require.Equal(t, len(blindPools[i].Players), len(awarePools[i].Players), "%s: pool %d size differs between mode-aware and mode-blind runs", tag, i)
			}

			draftIdx, derr := SelectFillBracketDraftIndices(awarePools, minSize, drawCourts)
			require.NoError(t, derr, tag)
			awareDraw := BuildKnockoutDrawFillBracket(awarePools, draftIdx, drawCourts)
			require.NotNil(t, awareDraw, "%s: BuildKnockoutDrawFillBracket refused the mode-aware pools", tag)
			blindDraw := BuildKnockoutDrawFillBracket(blindPools, draftIdx, drawCourts)
			require.NotNil(t, blindDraw, "%s: BuildKnockoutDrawFillBracket refused the mode-blind pools", tag)

			awareRound := earliestMeetingRoundInDraw(awareDraw, awarePools, "Dojo0")
			blindRound := earliestMeetingRoundInDraw(blindDraw, blindPools, "Dojo0")
			if awareRound == mathMaxIntSentinel {
				continue
			}
			configs++
			assert.GreaterOrEqualf(t, awareRound, blindRound,
				"%s: mode-aware fill-bracket placement (round %d) is WORSE than mode-blind (round %d)", tag, awareRound, blindRound)
			if awareRound > blindRound {
				strictImprovement++
			}
		}
	}

	require.Greater(t, configs, 0, "sweep produced no data (no config had Dojo0 spanning >=2 pools)")
	assert.Greater(t, strictImprovement, 0, "mode-aware fill-bracket scoring never strictly improved over mode-blind scoring across the sweep; the seam may be wired to nothing")
}
