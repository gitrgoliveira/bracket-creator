package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-qual LP-4: the "fill-bracket" qualifier mode.
//
// FillBracketPoolCount's formation arithmetic is pinned against the four
// 19WKC 2024 events plus an 11-entrant hand-worked case (bead bc-draw's
// Phase 0 comments, dated 2026-08-19). SelectFillBracketDrafts and
// BuildKnockoutDrawFillBracket are exercised at the shapes that arithmetic
// produces, and at deliberately out-of-scope shapes that must fail loudly.

// TestFillBracketPoolCount_19WKCShapes pins the formation objective against
// every verified 19WKC 2024 event plus the 11-entrant hand-worked case:
//   - 60 entrants (19WKC Men's Team) -> 16 pools (12 of 4, 4 of 3), 0 drafts.
//   - 45 (19WKC Women's Team) -> 14 pools (11 of 3, 3 of 4), 2 drafts -- the
//     naive floor(45/3)=15 (all pools of exactly 3) exists but has ZERO
//     oversized pools to draft a missing bracket slot from, which is WHY 14
//     wins.
//   - 203 (19WKC Women's Individual) -> 64 pools (53 of 3, 11 of 4), 0 drafts.
//   - 242 (19WKC Men's Individual) -> 64 pools (14 of 3, 50 of 4), 0 drafts.
//   - 11 (hand-worked) -> 3 pools (one of 3, two of 4), 1 draft.
//
// Fault injection (manually verified, reverted after): changing the
// constraint check from `need <= remainder` to `need < remainder` (an
// off-by-one that rejects the exact-fit case, need==remainder==0) turns the
// "n exactly at minSize" case red (P=1 needs need==remainder==0, which the
// mutant now rejects, exhausting the range and returning an error instead
// of P=1) -- the four 19WKC shapes and n=11 stay green under this mutant
// because their winning P always has strictly positive remainder, so the
// off-by-one never bites them; the n=1-pool edge case is what actually
// pins the boundary. Changing `p >= minP` to `p > minP` turns BOTH the
// n=11 case red (its only legal P is minP itself: 3, so the mutant finds
// nothing in range and errors instead of returning P=3) and the "n exactly
// at minSize" case red (same reason, minP==maxP==1).
func TestFillBracketPoolCount_19WKCShapes(t *testing.T) {
	tests := []struct {
		name        string
		n, minSize  int
		wantPools   int
		wantDrafts  int
		wantErr     bool
		errContains string
		errNamesN   bool // whether the error is required to mention n (not all are: an invalid minSize is rejected before n is ever consulted)
	}{
		{name: "19WKC Men's Team: 60 entrants, min 3 -> 16 pools, 0 drafts", n: 60, minSize: 3, wantPools: 16, wantDrafts: 0},
		{name: "19WKC Women's Team: 45 entrants, min 3 -> 14 pools, 2 drafts", n: 45, minSize: 3, wantPools: 14, wantDrafts: 2},
		{name: "19WKC Women's Individual: 203 entrants, min 3 -> 64 pools, 0 drafts", n: 203, minSize: 3, wantPools: 64, wantDrafts: 0},
		{name: "19WKC Men's Individual: 242 entrants, min 3 -> 64 pools, 0 drafts", n: 242, minSize: 3, wantPools: 64, wantDrafts: 0},
		{name: "hand-worked: 11 entrants, min 3 -> 3 pools, 1 draft", n: 11, minSize: 3, wantPools: 3, wantDrafts: 1},
		{name: "invalid minSize (0) is a clean error naming it", n: 60, minSize: 0, wantErr: true, errContains: "minimum pool size"},
		{name: "invalid minSize (negative) is a clean error naming it", n: 60, minSize: -1, wantErr: true, errContains: "minimum pool size"},
		{name: "n below minSize is a clean error naming both", n: 2, minSize: 3, wantErr: true, errContains: "fewer than the minimum pool size", errNamesN: true},
		{name: "n exactly at minSize forms one pool, 0 drafts (NextPow2(1)-1=0)", n: 3, minSize: 3, wantPools: 1, wantDrafts: 0},
		{name: "no valid P: 9 entrants at min 3 has only P=3 available, needs 1 draft, 0 oversized pools possible", n: 9, minSize: 3, wantErr: true, errContains: "no pool count fits", errNamesN: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pools, drafts, err := FillBracketPoolCount(tc.n, tc.minSize)
			if tc.wantErr {
				require.Error(t, err)
				if tc.errContains != "" {
					assert.Contains(t, err.Error(), tc.errContains)
				}
				if tc.errNamesN {
					// The task requires this class of error to name n and minSize.
					assert.Contains(t, err.Error(), fmt.Sprintf("%d", tc.n))
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantPools, pools, "pool count")
			assert.Equal(t, tc.wantDrafts, drafts, "draft count")
			// The arithmetic invariant itself, restated independently of the
			// function under test: winners + drafts must exactly fill a
			// power-of-two bracket, and every oversized pool used must fit
			// within the entrant count.
			assert.Equal(t, NextPow2(pools), pools+drafts, "winners + drafts must exactly fill a power-of-two bracket")
			remainder := tc.n - tc.minSize*pools
			assert.GreaterOrEqual(t, remainder, drafts, "there must be at least `drafts` oversized pools to draft from")
			assert.GreaterOrEqual(t, remainder, 0)
			assert.LessOrEqual(t, remainder, pools, "no pool can need to grow by more than one over the minimum")
		})
	}
}

// TestFillBracketPoolCount_LargestPWins proves the "LARGEST P" half of the
// objective directly: at 45 entrants / min 3, P=15 (the naive
// floor(45/3)=15, every pool at exactly the minimum) also satisfies
// 3*15<=45<=4*15, but has zero oversized pools to supply the 1 draft
// NextPow2(15)-15=1 would need -- so it must NOT be chosen. P=14 is what
// FillBracketPoolCount actually returns (pinned above); this test isolates
// WHY 15 loses, by checking the P=15 shape's own numbers fail the
// constraint that rules it out.
func TestFillBracketPoolCount_LargestPWins(t *testing.T) {
	const n, minSize = 45, 3
	p15Remainder := n - minSize*15
	p15Need := NextPow2(15) - 15
	require.Zero(t, p15Remainder, "P=15 uses every entrant with none left over")
	require.Equal(t, 1, p15Need, "P=15 (16-leaf bracket) needs exactly 1 draft")
	assert.Greater(t, p15Need, p15Remainder, "P=15 needs more drafts than it has oversized pools to supply -- this is why FillBracketPoolCount rejects it")

	pools, drafts, err := FillBracketPoolCount(n, minSize)
	require.NoError(t, err)
	assert.Equal(t, 14, pools, "the largest P that DOES satisfy both constraints must win, not the naive floor(n/minSize)")
	assert.Equal(t, 2, drafts)
}

// poolsWithOversizedAt builds n pools of size `size`, except the pools at
// oversizedIdx which get size+1 members and, if seeds is non-nil, a seeded
// player at the given rank (so SelectFillBracketDrafts' seed-then-pool-order
// has something to discriminate on). Unique dojos throughout so nothing in
// this package's dojo-conflict avoidance can perturb a hand-built list like
// this one bypasses anyway (mirrors internal/engine's uniformTestPools
// convention).
func poolsWithOversizedAt(n, size int, oversizedIdx map[int]bool, seeds map[int]int) []Pool {
	pools := make([]Pool, n)
	for i := range pools {
		char := string(rune('A' + i%26))
		pools[i].PoolName = fmt.Sprintf("Pool %s", char)
		cnt := size
		if oversizedIdx[i] {
			cnt++
		}
		for j := 0; j < cnt; j++ {
			p := Player{
				Name: fmt.Sprintf("Pool%d-Player%d", i, j),
				Dojo: fmt.Sprintf("Dojo%d-%d", i, j),
			}
			if j == 0 {
				if seed, ok := seeds[i]; ok {
					p.Seed = seed
				}
			}
			pools[i].Players = append(pools[i].Players, p)
		}
	}
	return pools
}

// selectFillBracketDraftsForCourts is a test-only convenience chaining
// FillBracketDraftCapacity -> SelectFillBracketDrafts, mirroring exactly
// what a real caller (cmd/create-pools.go, internal/engine/playoff_skeleton.go)
// does. It fails the test outright (via require) if capacity computation
// itself is not achievable -- every test using this helper is constructing
// a shape expected to be IN SCOPE at the target-arithmetic level.
func selectFillBracketDraftsForCourts(t *testing.T, pools []Pool, minSize, drafts, numCourts int) ([]int, error) {
	t.Helper()
	poolHalf, capacityByHalf, ok := FillBracketDraftCapacity(pools, drafts, numCourts)
	require.True(t, ok, "FillBracketDraftCapacity must succeed for this fixture's target arithmetic")
	return SelectFillBracketDrafts(pools, minSize, poolHalf, capacityByHalf)
}

// TestSelectFillBracketDrafts covers the CAPACITY-AWARE selection rule
// (second review rework, bc-qual LP-4): the most senior OVERSIZED pools, by
// best seed rank then pool order (rule 2) -- but a candidate is taken only
// if the OPPOSITE half from its own home still has remaining draft
// capacity (rule 3); a candidate whose destination is already full is
// SKIPPED, not a failure, and the scan continues down the order.
//
// Fault injection (manually verified, reverted after): swapping the sort
// comparator's tiebreak from `oversized[i].idx < oversized[j].idx` to `>`
// turns "unseeded: pool order breaks ties" AND "a third oversized pool
// sends nothing" red (both depend on ascending pool-index order among
// unseeded candidates; only "seeded: best seed rank wins" stays green,
// since its seeds already fully order the two drafted pools). Replacing
// `continue` with `break` in the skip branch (stop scanning at the first
// blocked candidate instead of trying the next one -- the mutation the
// third review specifically asked to be fault-injected) turns FOUR
// independent tests red: this file's own "capacity-aware: a blocked
// candidate is skipped, not failed" subtest, the sheet-compatibility
// TestSelectFillBracketDrafts_19WKCWomenTeam, the "7/7 split now RECOVERS"
// subtest of TestBuildKnockoutDrawFillBracket_OutOfScope, and the property
// sweep TestFillBracketFormationAndBuilderAgree (whose refusal count jumps
// back up toward the pre-capacity-aware baseline).
func TestSelectFillBracketDrafts(t *testing.T) {
	t.Run("seeded: best seed rank wins regardless of pool order", func(t *testing.T) {
		// Pools 0, 3 and 5 are oversized; pool 5 carries the best (lowest)
		// seed rank despite being last in pool order. 6 pools / 2 courts:
		// homeCount=[3,3], target=4, capacityByHalf=[1,1] -- capacity never
		// binds here (both selected pools land in different halves
		// naturally), so this pins the STRICT-order behaviour is unchanged
		// when capacity does not constrain it.
		pools := poolsWithOversizedAt(6, 3, map[int]bool{0: true, 3: true, 5: true}, map[int]int{5: 1, 0: 2})
		got, err := selectFillBracketDraftsForCourts(t, pools, 3, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, []int{5, 0}, got, "seeded oversized pool 5 (rank 1) must be chosen before unseeded pool 3, and before seeded pool 0 (rank 2)")
	})

	t.Run("unseeded: pool order breaks ties", func(t *testing.T) {
		pools := poolsWithOversizedAt(6, 3, map[int]bool{4: true, 1: true, 3: true}, nil)
		got, err := selectFillBracketDraftsForCourts(t, pools, 3, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, []int{1, 3}, got, "with no seeds, oversized pools are chosen in pool-index order")
	})

	t.Run("a third oversized pool sends nothing (19WKC evidence)", func(t *testing.T) {
		// 14 pools / 4 courts: homeCount=[4,4,3,3], capacityByHalf=[0,2]
		// (courts A,B already full; C,D need 1 each, both sourced from the
		// opposite half A/B). Pools 0 and 4 (both home half 0) take both
		// slots; pool 11 (home half 1, whose own destination -- half 0 --
		// has zero capacity) is correctly never even a candidate for a slot.
		pools := poolsWithOversizedAt(14, 3, map[int]bool{0: true, 4: true, 11: true}, nil)
		got, err := selectFillBracketDraftsForCourts(t, pools, 3, 2, 4)
		require.NoError(t, err)
		assert.Equal(t, []int{0, 4}, got)
		assert.NotContains(t, got, 11, "the third oversized pool (index 11) must not be drafted when only 2 are needed")
	})

	t.Run("capacity-aware: a blocked candidate is skipped, not failed, and a later one substitutes", func(t *testing.T) {
		// 6 pools / 2 courts: homeCount=[3,3], target=4, capacityByHalf=[1,1].
		// Oversized pools 0 and 1 are BOTH on court 0 (half 0); pool 5 is on
		// court 1 (half 1). In seed-then-pool order (0, 1, 5): pool 0 takes
		// half 1's only slot; pool 1's own destination (half 1) is then
		// FULL, so it is SKIPPED (not a failure) despite coming before pool
		// 5 in the order; pool 5 (home half 1, destination half 0) takes
		// half 0's only slot. This is the exact mechanism a STRICT
		// (non-capacity-aware) selection cannot express: blindly taking the
		// first two by order (0, 1) would put BOTH drafts in half 1,
		// leaving half 0 unfilled and half 1 over-supplied -- a shape
		// FillBracketPoolCount already promised was fine, refused only
		// because selection had no way to route around it (the review's
		// finding this rework closes).
		pools := poolsWithOversizedAt(6, 3, map[int]bool{0: true, 1: true, 5: true}, nil)
		got, err := selectFillBracketDraftsForCourts(t, pools, 3, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, []int{0, 5}, got, "pool 1 must be skipped for capacity, not selected merely because it precedes pool 5 in order")
	})

	t.Run("capacity total <= 0 returns nil, nil", func(t *testing.T) {
		pools := poolsWithOversizedAt(3, 3, map[int]bool{0: true}, nil)
		got, err := SelectFillBracketDrafts(pools, 3, make([]int, 3), [2]int{0, 0})
		assert.Nil(t, got)
		assert.NoError(t, err)
	})

	t.Run("error when too few oversized pools", func(t *testing.T) {
		pools := poolsWithOversizedAt(6, 3, map[int]bool{0: true}, nil)
		got, err := selectFillBracketDraftsForCourts(t, pools, 3, 2, 2)
		assert.Nil(t, got)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "1 oversized pool(s) exist")
	})

	t.Run("error when oversized pools cannot supply both halves (capacity-exhausted, not seed order)", func(t *testing.T) {
		// 14 pools / 4 courts, oversized pools 8 and 11 BOTH on courts C/D
		// (half 1); capacityByHalf=[0,2] (half 0 needs nothing, half 1
		// needs 2, both sourced from half 0). Neither candidate's
		// destination (half 0) ever has capacity, so BOTH are skipped and
		// the scan ends having placed zero of the two needed drafts.
		pools := poolsWithOversizedAt(14, 3, map[int]bool{8: true, 11: true}, nil)
		got, err := selectFillBracketDraftsForCourts(t, pools, 3, 2, 4)
		assert.Nil(t, got)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot supply both halves of the bracket")
	})

	t.Run("minSize <= 0 means nothing is oversized", func(t *testing.T) {
		pools := poolsWithOversizedAt(6, 3, map[int]bool{0: true, 1: true}, nil)
		got, err := selectFillBracketDraftsForCourts(t, pools, 0, 2, 2)
		assert.Nil(t, got)
		require.Error(t, err)
	})
}

// TestSelectFillBracketDrafts_19WKCWomenTeam pins the sheet-compatibility
// claim from the second review (bc-qual LP-4): on the 19WKC women's team
// draw (bead bc-draw Phase 0, 2026-08-19) the three oversized pools are
// blocks 1, 9 and 16, with capacityByHalf = [1, 1] (one destination slot
// per half). In seed-then-pool order (1, 9, 16): block 1 (home half 1)
// takes half 2's only slot; block 9 (home half 1, the SAME side as block 1)
// finds half 2's slot already gone and is SKIPPED; block 16 (home half 2)
// takes half 1's only remaining slot. The two pools actually drafted by the
// sheet -- 1 and 16 -- are exactly what the capacity-aware pass selects,
// and the capacity-exhaustion skip of block 9 is exactly why the sheet
// drafts nothing from it.
//
// This fixture reproduces that STRUCTURE (first and third candidates in
// order share nothing, the middle candidate shares a half with the first
// and gets skipped) using this package's own 45-entrant fixture at 2
// shiaijo (14 pools, oversized at 0, 4 and 11): pools 0 and 4 are on the
// SAME home half, pool 11 on the other -- the identical shape to blocks 1/9
// (same half) and 16 (opposite), with pool indices standing in for block
// numbers rather than reproducing WKC's own literal numbering (which this
// package does not otherwise use).
func TestSelectFillBracketDrafts_19WKCWomenTeam(t *testing.T) {
	pools := poolsWithOversizedAt(14, 3, map[int]bool{0: true, 4: true, 11: true}, nil)
	poolHalf, capacityByHalf, ok := FillBracketDraftCapacity(pools, 2, 2)
	require.True(t, ok)
	require.Equal(t, [2]int{1, 1}, capacityByHalf, "one destination slot per half, matching the sheet's capacities")
	require.Equal(t, poolHalf[0], poolHalf[4], "pools 0 and 4 (standing in for blocks 1 and 9) share a home half")
	require.NotEqual(t, poolHalf[0], poolHalf[11], "pool 11 (standing in for block 16) is on the opposite home half")

	got, err := SelectFillBracketDrafts(pools, 3, poolHalf, capacityByHalf)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 11}, got, "the first and third candidates (blocks 1 and 16) are drafted; the middle one (block 9) is skipped for capacity, matching the sheet")
}

// TestBuildKnockoutDrawFillBracket_45_ZeroByes_OppositeHalf is the required
// full draw-level test (bc-qual LP-4 task spec): 45 entrants at minimum
// pool size 3 forms 14 pools (11 of 3, 3 of 4 -- the 19WKC Women's Team
// shape) on 4 shiaijo. AssignPoolsToCourts splits 14 pools as 4/4/3/3
// (courts A,B big; C,D short by one pool each), and the 2 drafted 2nds
// (from the two oversized pools placed on courts A and B, half 0) must
// cross to the OPPOSITE half (courts C and D, half 1) and fight round 1 --
// producing a 16-leaf bracket with ZERO byes anywhere.
//
// Fault injection (manually verified, reverted after): changing
// buildFillBracketDraw's pairAcross calls to pair draftByHalf[0] with
// shortByHalf[0] (same half, the larger-pools mistake) instead of
// shortByHalf[1] turns this test red at the "opposite half" assertions
// (both drafted labels would land on courts A/B, their OWN half, and the
// short courts C/D would be left with only 3 real occupants each --
// buildBlock would then also error/behave differently since len(occ)
// wouldn't match `target`, likely nil-ing the whole draw). Changing the
// `target == 0 || NextPow2(target) != target` guard to only check
// `target == 0` (dropping the power-of-two requirement) does not affect
// THIS shape (target=4 is already a power of two) but is separately caught
// by TestBuildKnockoutDrawFillBracket_OutOfScope's hidden-bye-guard case.
func TestBuildKnockoutDrawFillBracket_45_ZeroByes_OppositeHalf(t *testing.T) {
	// Oversized pools at indices 0 (court A, half 0), 4 (court B, half 0)
	// and 11 (court D, half 1) -- AssignPoolsToCourts(14, 4) splits pool
	// indices as A=[0,3], B=[4,7], C=[8,10], D=[11,13], matching 45's
	// 11x3+3x4 composition (14 pools, 3 oversized).
	pools := poolsWithOversizedAt(14, 3, map[int]bool{0: true, 4: true, 11: true}, nil)
	require.Equal(t, 45, countPlayers(pools), "sanity: this fixture really is the 45-entrant shape")

	drafted, err := selectFillBracketDraftsForCourts(t, pools, 3, 2, 4)
	require.NoError(t, err)
	require.Equal(t, []int{0, 4}, drafted, "the two top-seed-order oversized pools (0 and 4) are drafted; pool 11 sends nothing (19WKC evidence)")

	draw := BuildKnockoutDrawFillBracket(pools, drafted, 4)
	require.NotNil(t, draw, "this shape (4/4/3/3 pool split, 2 drafts exactly filling the two short courts from the opposite half) is in scope")

	leaves := TreeToLeafArray(draw.Root)
	require.Len(t, leaves, 16, "14 pools + 2 drafts = 16-leaf bracket")
	for i, l := range leaves {
		assert.NotEmptyf(t, l, "leaf %d must not be a bye: fill-bracket guarantees zero byes", i)
	}

	// Both drafted 2nds must appear exactly once each, and both must sit at
	// an EVEN leaf index paired with a real occupant at the following ODD
	// index (or vice versa) -- i.e. inside a genuine round-1 match, never
	// isolated.
	draftALabel, draftBLabel := "Pool A-2nd", "Pool E-2nd" // pool index 0 -> "Pool A", index 4 -> "Pool E"
	for _, label := range []string{draftALabel, draftBLabel} {
		idx := indexOfLeaf(leaves, label)
		require.GreaterOrEqualf(t, idx, 0, "expected %q among the leaves", label)
		partner := idx ^ 1 // round-1 pairs are (2k, 2k+1)
		assert.NotEmptyf(t, leaves[partner], "%q's round-1 partner (leaf %d) must be a real occupant, not empty", label, partner)
	}

	// Opposite-half check: pool 0 and pool 4's OWN winners ("Pool A-1st",
	// "Pool E-1st") must sit in the FIRST half of the leaf array (indices
	// 0-7, courts A/B), while their drafted 2nds sit in the SECOND half
	// (indices 8-15, courts C/D) -- rule 3's "opposite half" in concrete
	// leaf-array terms.
	ownWinnerIdx := indexOfLeaf(leaves, "Pool A-1st")
	draftIdx := indexOfLeaf(leaves, draftALabel)
	require.GreaterOrEqual(t, ownWinnerIdx, 0)
	require.GreaterOrEqual(t, draftIdx, 0)
	assert.Less(t, ownWinnerIdx, 8, "Pool A's own winner must stay in the first half (its home court)")
	assert.GreaterOrEqual(t, draftIdx, 8, "Pool A's drafted 2nd must cross to the SECOND half -- the opposite half from its own winner")

	ownWinnerIdxE := indexOfLeaf(leaves, "Pool E-1st")
	draftIdxE := indexOfLeaf(leaves, draftBLabel)
	require.GreaterOrEqual(t, ownWinnerIdxE, 0)
	require.GreaterOrEqual(t, draftIdxE, 0)
	assert.Less(t, ownWinnerIdxE, 8, "Pool E's own winner must stay in the first half (its home court)")
	assert.GreaterOrEqual(t, draftIdxE, 8, "Pool E's drafted 2nd must cross to the SECOND half -- the opposite half from its own winner")

	// The third oversized pool (index 11, "Pool L") sends only its winner.
	assert.NotContains(t, leaves, "Pool L-2nd", "the third oversized pool must not appear as a drafted 2nd")
	assert.Contains(t, leaves, "Pool L-1st")
}

// TestBuildKnockoutDrawFillBracket_OutOfScope pins the scope discipline:
// shapes the arithmetic and court layout do not cleanly resolve into a
// zero-bye draw must return nil, never a guessed layout with a real bye
// hidden inside a bigger/lopsided region.
//
// Fault injection (manually verified, reverted after): removing the
// `len(draftByHalf[0]) != len(shortByHalf[1]) || ...` mismatch check no
// longer has a reachable test case at THIS layer (second review rework):
// capacity-aware SelectFillBracketDrafts now refuses (or reroutes) every
// shape that check used to catch, before a bad draftPoolIdx ever reaches
// this function -- the "drafts sourced from the wrong (same) half" and "7/7
// split" cases below both moved from asserting a nil draw here to asserting
// SelectFillBracketDrafts' own new error (see TestSelectFillBracketDrafts'
// "error when oversized pools cannot supply both halves" and this file's
// TestFillBracketFormationAndBuilderAgree, whose sweep dropped from 123 to
// a small residual list once selection became capacity-aware). This
// function's own check remains only as belt-and-braces against a HAND-FED
// draftPoolIdx that bypasses selection entirely, which "drafts do not
// match the court layout's gap" below still exercises directly.
// Weakening the `target == 0 || NextPow2(target) != target` guard to only
// `target == 0` (dropping the power-of-two requirement) is caught
// specifically by "target not a power of two even at zero drafts needed"
// below: with the guard weakened that shape reaches buildBlock with a
// 3-occupant court, whose own greedy layout inserts a REAL BYE (3 is odd,
// NextPow2(3)=4) -- exactly the "guessed layout with a hidden bye" this
// function's scope discipline exists to forbid.
func TestBuildKnockoutDrawFillBracket_OutOfScope(t *testing.T) {
	t.Run("target not a power of two even at zero drafts needed (hidden-bye guard)", func(t *testing.T) {
		// 6 pools over 2 shiaijo, none oversized: homeCount=[3,3], so
		// target=3 and every court's need is 0 -- the draft-count mismatch
		// check alone would NOT reject this (0 drafts requested, 0 needed).
		// Only the power-of-two guard on `target` stops it: NextPow2(3)=4,
		// so a 3-occupant court would otherwise reach buildBlock and get a
		// real, silent bye.
		pools := poolsWithOversizedAt(6, 3, nil, nil)
		draw := BuildKnockoutDrawFillBracket(pools, nil, 2)
		assert.Nil(t, draw, "a non-power-of-two court size must be refused even when no draft is needed, not silently byed")
	})

	t.Run("7/7 split now RECOVERS via capacity-aware selection (second review rework)", func(t *testing.T) {
		// 14 pools over 2 shiaijo: homeCount=[7,7], target=(14+2)/2=8 (a
		// valid power of two, >= 7) -- this shape was NEVER actually about
		// "7 is not a power of two" (an earlier, imprecise version of this
		// test's own comment): it fails STRICT selection because pools 0
		// and 4 (both < 7, so both on court 0/half 0) are the top two by
		// pool order and would both need to cross to half 1, leaving half
		// 0 unfilled. Capacity-aware selection instead skips pool 4 (its
		// destination is full after pool 0 takes it) and substitutes pool
		// 11 (home half 1), succeeding with zero byes -- this is the exact
		// disagreement-closing behaviour requirement 1 of the second
		// review asked to be re-measured.
		pools := poolsWithOversizedAt(14, 3, map[int]bool{0: true, 4: true, 11: true}, nil)
		drafted, err := selectFillBracketDraftsForCourts(t, pools, 3, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, []int{0, 11}, drafted, "pool 4 is skipped for capacity; pool 11 substitutes")
		draw := BuildKnockoutDrawFillBracket(pools, drafted, 2)
		require.NotNil(t, draw, "capacity-aware selection recovers this shape; it is no longer out of scope")
		leaves := TreeToLeafArray(draw.Root)
		require.Len(t, leaves, 16)
		for i, l := range leaves {
			assert.NotEmptyf(t, l, "leaf %d must not be a bye", i)
		}
	})

	t.Run("drafts do not match the court layout's gap (too few)", func(t *testing.T) {
		pools := poolsWithOversizedAt(14, 3, map[int]bool{0: true, 4: true, 11: true}, nil)
		draw := BuildKnockoutDrawFillBracket(pools, []int{0}, 4) // needs 2, given 1
		assert.Nil(t, draw)
	})

	t.Run("drafts do not match the court layout's gap (too many)", func(t *testing.T) {
		pools := poolsWithOversizedAt(14, 3, map[int]bool{0: true, 4: true, 11: true}, nil)
		draw := BuildKnockoutDrawFillBracket(pools, []int{0, 4, 11}, 4) // needs 2, given 3
		assert.Nil(t, draw)
	})

	t.Run("a hand-fed draftPoolIdx sourced from the wrong (same) half is still refused by the builder directly", func(t *testing.T) {
		// Bypasses SelectFillBracketDrafts (which would itself now refuse
		// this shape -- see TestSelectFillBracketDrafts' "error when
		// oversized pools cannot supply both halves") to confirm the
		// builder's own belt-and-braces capacity check still catches a
		// hand-fed, rule-3-violating draftPoolIdx directly.
		pools := poolsWithOversizedAt(14, 3, map[int]bool{8: true, 11: true}, nil)
		draw := BuildKnockoutDrawFillBracket(pools, []int{8, 11}, 4)
		assert.Nil(t, draw, "drafts sourced from the same half as the short courts they would fill are out of scope")
	})

	t.Run("empty pool list", func(t *testing.T) {
		assert.Nil(t, BuildKnockoutDrawFillBracket(nil, nil, 4))
	})

	t.Run("single shiaijo with a draft needed now builds via the synthetic half-split (rework)", func(t *testing.T) {
		// 3 pools, 1 oversized: NextPow2(3)-3=1 draft needed. The FIRST cut
		// of this function refused any single-shiaijo draft; the rework
		// (bc-qual LP-4 review) emulates two synthetic halves at one
		// shiaijo (buildFillBracketSingleCourtDraw), so this must now
		// SUCCEED with zero byes, the draft in the OTHER synthetic half
		// from its own pool's winner. Pool 0 is oversized and sits in
		// synthetic half 0 (index parity: i%2, so pools 0 and 2 are half
		// 0, pool 1 is half 1); its draft must therefore land in half 1
		// alongside pool 1.
		pools := poolsWithOversizedAt(3, 3, map[int]bool{0: true}, nil)
		drafted, err := selectFillBracketDraftsForCourts(t, pools, 3, 1, 1)
		require.NoError(t, err)
		draw := BuildKnockoutDrawFillBracket(pools, drafted, 1)
		require.NotNil(t, draw, "a single-shiaijo fill-bracket draw needing a draft must build via the half-split emulation")
		leaves := TreeToLeafArray(draw.Root)
		require.Len(t, leaves, 4)
		for i, l := range leaves {
			assert.NotEmptyf(t, l, "leaf %d must not be a bye", i)
		}
		require.Len(t, draw.Regions, 1, "one real shiaijo has exactly one region")
		require.Len(t, draw.poolCourt, 3)
		for _, c := range draw.poolCourt {
			assert.Equal(t, 0, c, "every pool is on the one real shiaijo, never the internal synthetic half")
		}
	})

	t.Run("single shiaijo with zero drafts needed works trivially", func(t *testing.T) {
		// 4 pools, none oversized: already a power of two, nothing to cross.
		pools := poolsWithOversizedAt(4, 3, nil, nil)
		draw := BuildKnockoutDrawFillBracket(pools, nil, 1)
		require.NotNil(t, draw, "a pure winner-only bracket needs no half-block emulation")
		leaves := TreeToLeafArray(draw.Root)
		assert.Len(t, leaves, 4)
		for _, l := range leaves {
			assert.NotEmpty(t, l)
		}
	})

	t.Run("docs-plan example: n=11, s=3, 1 court (P=3, D=1) builds with zero byes", func(t *testing.T) {
		// The exact worked example from the bc-qual task spec, through the
		// REAL pipeline (BuildPoolPhaseFillBracket), not a hand-built pool
		// list: FillBracketPoolCount(11, 3) gives P=3, D=1; the single real
		// shiaijo ends with T=P+D=4 total occupants (the reviewer's "T=4"),
		// realised here as two synthetic 2-occupant halves.
		players := makeUniquePlayers(11)
		pools, drawCourts, err := BuildPoolPhaseFillBracket(players, 3, 1)
		require.NoError(t, err)
		assert.Equal(t, 1, drawCourts)
		require.Len(t, pools, 3)

		drafts, err := selectFillBracketDraftsForCourts(t, pools, 3, NextPow2(len(pools))-len(pools), drawCourts)
		require.NoError(t, err)
		require.Len(t, drafts, 1)

		draw := BuildKnockoutDrawFillBracket(pools, drafts, drawCourts)
		require.NotNil(t, draw, "n=11 s=3 at 1 court must build (docs-plan worked example)")
		leaves := TreeToLeafArray(draw.Root)
		require.Len(t, leaves, 4)
		for i, l := range leaves {
			assert.NotEmptyf(t, l, "leaf %d must not be a bye", i)
		}
	})
}

// countPlayers sums every pool's roster size, used only to sanity-check a
// hand-built test fixture matches the entrant count its doc comment claims.
func countPlayers(pools []Pool) int {
	n := 0
	for _, p := range pools {
		n += len(p.Players)
	}
	return n
}

// indexOfLeaf returns the first index of label in leaves, or -1.
func indexOfLeaf(leaves []string, label string) int {
	for i, l := range leaves {
		if l == label {
			return i
		}
	}
	return -1
}

// TestCreatePoolsForCount covers the pool-cutting half of fill-bracket
// formation: explicit pool count, min-size targets, remainder spread.
//
// Fault injection (manually verified, reverted after): changing the upper
// bound check from `len(players) > (poolSize+1)*totalPools` to `>=` turns
// "exact upper bound (every pool at minSize+1) is accepted" red (a
// legitimate all-oversized shape would be rejected).
func TestCreatePoolsForCount(t *testing.T) {
	t.Run("poolSize <= 0 is a clean error", func(t *testing.T) {
		_, err := CreatePoolsForCount(nil, 0, 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pool size")
	})

	t.Run("totalPools <= 0 is a clean error", func(t *testing.T) {
		_, err := CreatePoolsForCount(nil, 3, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pool count")
	})

	t.Run("too few players for the minimum is a clean error", func(t *testing.T) {
		players := make([]Player, 5)
		_, err := CreatePoolsForCount(players, 3, 2) // needs >= 6
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only 5")
	})

	t.Run("too many players to fit within +1 per pool is a clean error", func(t *testing.T) {
		players := make([]Player, 9) // 9 > (3+1)*2=8
		_, err := CreatePoolsForCount(players, 3, 2)
		require.Error(t, err)
	})

	t.Run("exact upper bound (every pool at minSize+1) is accepted", func(t *testing.T) {
		players := makeUniquePlayers(8)
		pools, err := CreatePoolsForCount(players, 3, 2) // exactly (3+1)*2
		require.NoError(t, err)
		require.Len(t, pools, 2)
		for _, p := range pools {
			assert.Len(t, p.Players, 4)
		}
	})

	t.Run("remainder spreads outer-to-inner, exactly like CreatePools' min-mode branch", func(t *testing.T) {
		players := makeUniquePlayers(11) // totalPools=3, minSize=3: remainder 2
		pools, err := CreatePoolsForCount(players, 3, 3)
		require.NoError(t, err)
		require.Len(t, pools, 3)
		oversized := 0
		total := 0
		for _, p := range pools {
			total += len(p.Players)
			if len(p.Players) == 4 {
				oversized++
			} else {
				assert.Len(t, p.Players, 3)
			}
		}
		assert.Equal(t, 11, total)
		assert.Equal(t, 2, oversized)
	})
}

func makeUniquePlayers(n int) []Player {
	players := make([]Player, n)
	for i := range players {
		players[i] = Player{Name: fmt.Sprintf("P%d", i), Dojo: fmt.Sprintf("D%d", i)}
	}
	return players
}

// TestBuildPoolPhaseFillBracket_45 is the helper-level counterpart of the
// engine end-to-end test (internal/engine/draw_fill_bracket_test.go): the
// FULL pool phase (seeding, cutting, court reorder) for 45 entrants at
// minimum pool size 3 must produce 14 pools with 3 oversized, matching
// FillBracketPoolCount's formation, and the result must feed
// BuildKnockoutDrawFillBracket to a real zero-bye 16-leaf draw at 4 courts.
func TestBuildPoolPhaseFillBracket_45(t *testing.T) {
	players := makeUniquePlayers(45)
	pools, drawCourts, err := BuildPoolPhaseFillBracket(players, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, 4, drawCourts)
	require.Len(t, pools, 14)

	oversized := 0
	for _, p := range pools {
		if len(p.Players) == 4 {
			oversized++
		}
	}
	assert.Equal(t, 3, oversized)

	drafts, err := selectFillBracketDraftsForCourts(t, pools, 3, NextPow2(len(pools))-len(pools), drawCourts)
	require.NoError(t, err)
	require.Len(t, drafts, 2)

	draw := BuildKnockoutDrawFillBracket(pools, drafts, drawCourts)
	require.NotNil(t, draw)
	leaves := TreeToLeafArray(draw.Root)
	require.Len(t, leaves, 16)
	for _, l := range leaves {
		assert.NotEmpty(t, l)
	}
}

// TestBuildPoolPhaseFillBracket_FormationError propagates
// FillBracketPoolCount's own error rather than masking it -- a caller
// (cmd/create-pools.go, internal/engine/pools.go) relies on this to report
// the ACTUAL reason (naming the entrant count and minimum pool size), not a
// generic "cannot create pools" message from a downstream function that no
// longer knows why it was called with a doomed pool count.
func TestBuildPoolPhaseFillBracket_FormationError(t *testing.T) {
	players := makeUniquePlayers(9) // no valid P for 9 entrants at min 3
	_, _, err := BuildPoolPhaseFillBracket(players, 3, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no pool count fits")
	assert.True(t, strings.Contains(err.Error(), "9"), "error must name the entrant count")
}

// TestBuildFillBracketDraw_DefensiveGuards exercises buildFillBracketDraw's
// (unexported) own scope guards directly, with a hand-fed poolCourt
// allocation rather than one AssignPoolsToCourts would ever actually
// produce -- BuildKnockoutDrawFillBracket's exported entry point cannot
// reach most of these through any real construction (AssignPoolsToCourts
// never returns an error, never leaves a court >1 pool short of another,
// and never assigns a pool to an out-of-range court), so each guard is
// belt-and-braces against a hand-fed or future allocation function that
// might not honour those invariants -- exactly the discipline
// draw_perpool.go's buildPerPoolDraw already applies to its own inputs.
func TestBuildFillBracketDraw_DefensiveGuards(t *testing.T) {
	pools := poolsWithOversizedAt(4, 3, nil, nil)

	t.Run("numCourts < 1", func(t *testing.T) {
		assert.Nil(t, buildFillBracketDraw(pools, nil, []int{0, 0, 0, 0}, 0))
	})

	t.Run("poolCourt length mismatch", func(t *testing.T) {
		assert.Nil(t, buildFillBracketDraw(pools, nil, []int{0, 0, 0}, 2))
	})

	t.Run("an uneven split with no drafts to compensate is out of scope", func(t *testing.T) {
		// 3 pools on court 0, 1 pool on court 1, ZERO drafts supplied: no
		// target T can be both >= court 0's home count (3) and divide
		// (4+0)/2=2 evenly across both courts -- target=2 < maxHome=3, so
		// this is refused. (A >1-pool shortfall is NOT inherently out of
		// scope after the rework below -- see the next subtest, which is
		// the SAME uneven split but with enough drafts supplied to fill it
		// and succeeds.)
		unevenPools := poolsWithOversizedAt(4, 3, nil, nil)
		draw := buildFillBracketDraw(unevenPools, nil, []int{0, 0, 0, 1}, 2)
		assert.Nil(t, draw, "an uneven split with no compensating drafts must be refused, not guessed")
	})

	t.Run("a >1 pool shortfall on one court now succeeds when enough drafts fill it (rework)", func(t *testing.T) {
		// Same 3/1 uneven split as above (4 pools: 3 on court 0, 1 on
		// court 1), but this time EVERY pool is also drafted: target=4 (the
		// smallest power of two >= maxHome=3), so court 0 (3 homes) needs 1
		// draft and court 1 (1 home) needs 3 -- 4 drafts total, one per
		// pool. Each of the 3 court-0 pools' drafts (home half 0) must fill
		// court 1's need (half 1), and pool 3's own draft (home half 1)
		// fills court 0's single slot (half 0) -- 3 into 1 slot's capacity
		// and 1 into 3 slots' capacity is exactly what the capacity check
		// (not a per-court "at most one draft") now allows.
		pools := poolsWithOversizedAt(4, 3, nil, nil)
		draw := buildFillBracketDraw(pools, []int{0, 1, 2, 3}, []int{0, 0, 0, 1}, 2)
		require.NotNil(t, draw, "a >1 pool shortfall on one court must succeed once enough drafts are supplied to square every court to the same target")
		leaves := TreeToLeafArray(draw.Root)
		require.Len(t, leaves, 8, "target=4 per court * 2 courts = 8 leaves")
		for i, l := range leaves {
			assert.NotEmptyf(t, l, "leaf %d must not be a bye", i)
		}
	})

	t.Run("a court left with zero home pools yields no draw", func(t *testing.T) {
		// Every pool mapped to court 0; court 1 has none at all, so its
		// (empty) block cannot be built.
		draw := buildFillBracketDraw(pools, nil, []int{0, 0, 0, 0}, 2)
		assert.Nil(t, draw, "a court with no home pools and no draft cannot be built")
	})
}

// fillBracketTargetAlwaysAchievable independently re-derives, from public
// API only (AssignPoolsToCourts, NextPow2 -- deliberately NOT calling into
// buildFillBracketDraw's own target computation, or this would just be
// testing itself), whether a valid per-court target T exists for len(pools)
// pools plus `drafts` drafted 2nds spread over `courts` shiaijo (1 is
// treated as the synthetic 2-half split buildFillBracketSingleCourtDraw
// uses). It calls t.Fatal (via require) if not, because
// BuildKnockoutDrawFillBracket's doc comment claims this can NEVER fail for
// a power-of-two court count once FillBracketPoolCount has already accepted
// the entrant count -- see the derivation there. This is what lets
// TestFillBracketFormationAndBuilderAgree attribute every nil result from
// the builder to the OTHER, genuinely data-dependent check (rule 3's
// opposite-half capacity), rather than merely hoping that is the reason.
func fillBracketTargetAlwaysAchievable(t *testing.T, pools []Pool, drafts, courts int) {
	t.Helper()

	var homeCount []int
	divisor := courts
	if courts == 1 {
		// Mirrors buildFillBracketSingleCourtDraw's own synthetic split
		// exactly (index parity: pool i -> synthetic half i%2).
		homeCount = make([]int, 2)
		for i := range pools {
			homeCount[i%2]++
		}
		divisor = 2
	} else {
		poolCourt, err := AssignPoolsToCourts(len(pools), courts)
		require.NoError(t, err)
		homeCount = make([]int, courts)
		for _, c := range poolCourt {
			homeCount[c]++
		}
	}

	maxHome := 0
	for _, h := range homeCount {
		if h > maxHome {
			maxHome = h
		}
	}
	total := len(pools) + drafts
	require.Zerof(t, total%divisor, "target must divide evenly across %d court(s): total=%d", divisor, total)
	target := total / divisor
	require.Equalf(t, target, NextPow2(target), "target must itself be a power of two, got %d", target)
	require.GreaterOrEqualf(t, target, maxHome, "target (%d) must be >= every court's home count (max %d)", target, maxHome)
}

// TestFillBracketFormationAndBuilderAgree is the review-mandated agreement
// test (bc-qual LP-4 rework): the flaw the review caught was that
// FillBracketPoolCount (formation) and BuildKnockoutDrawFillBracket
// (placement) disagreed about which shapes are valid -- formation promised
// shapes the ORIGINAL builder refused (n=41 at 4 courts, n=45 at 2 courts,
// n=45 at 1 court all failed the old "target already a power of two,
// short by at most one" rule). This test sweeps every n in [2*minSize, 300]
// where FillBracketPoolCount(n, 3) succeeds and confirms
// BuildKnockoutDrawFillBracket ALSO succeeds, at 1, 2 and 4 shiaijo, with a
// genuine zero-bye leaf count of exactly winners+drafts -- EXCEPT for a
// case that fails ONLY the half-capacity rule (rule 3: the drafted pools'
// own halves cannot supply what the short courts in the opposite half
// need), which is data-dependent on exactly which pools end up oversized
// and where the court allocation places them. Those are counted and
// reported rather than asserted impossible; fillBracketTargetAlwaysAchievable
// aborts the test outright if a refusal is EVER attributable to anything
// else (the target/divisibility computation the doc comment claims cannot
// fail here), so a silent regression of that guarantee cannot hide behind
// this test's tolerance for half-capacity refusals.
func TestFillBracketFormationAndBuilderAgree(t *testing.T) {
	const minSize = 3
	const maxN = 300

	tested := 0
	halfCapacityRefusals := 0
	var refusalDetails []string
	realisticTested := 0 // subset with D<=4, matching every 19WKC event + docs-plan example
	realisticRefusals := 0

	for n := 2 * minSize; n <= maxN; n++ {
		p, d, err := FillBracketPoolCount(n, minSize)
		if err != nil {
			continue // formation itself declined this n: nothing to agree on
		}
		tested++

		for _, courts := range []int{1, 2, 4} {
			players := makeUniquePlayers(n)
			pools, drawCourts, ferr := BuildPoolPhaseFillBracket(players, minSize, courts)
			require.NoErrorf(t, ferr, "n=%d courts=%d: formation succeeded (P=%d D=%d) but the full pool phase failed: %v", n, courts, p, d, ferr)
			require.Lenf(t, pools, p, "n=%d courts=%d: pool count must match FillBracketPoolCount's own P", n, courts)

			// The per-court target/divisibility guarantee: confirmed BOTH
			// via the production capacity function's own ok return AND an
			// independent re-derivation (fillBracketTargetAlwaysAchievable,
			// over public API only) that cannot merely be testing itself.
			// Either failing aborts the test outright (t.Fatal via
			// require) -- this must never happen for a power-of-two court
			// count once FillBracketPoolCount accepted n (see
			// BuildKnockoutDrawFillBracket's doc comment).
			poolHalf, capacityByHalf, capOK := FillBracketDraftCapacity(pools, d, drawCourts)
			require.Truef(t, capOK, "n=%d courts=%d: FillBracketDraftCapacity must succeed (target/divisibility guarantee)", n, courts)
			fillBracketTargetAlwaysAchievable(t, pools, d, drawCourts)

			// Capacity-aware selection (second review rework): this is now
			// where the genuinely data-dependent residue surfaces -- a
			// SelectFillBracketDrafts error here means the oversized
			// pools' own positions cannot supply both halves, no matter
			// how selection orders or routes them.
			drafts, serr := SelectFillBracketDrafts(pools, minSize, poolHalf, capacityByHalf)
			if serr != nil {
				halfCapacityRefusals++
				refusalDetails = append(refusalDetails, fmt.Sprintf("n=%d courts=%d (real drawCourts=%d, P=%d D=%d): %v", n, courts, drawCourts, p, d, serr))
				if d <= 4 {
					realisticRefusals++
				}
				continue
			}

			// Selection succeeded: the builder must NEVER refuse a
			// draftPoolIdx that came from capacity-aware selection (see
			// BuildKnockoutDrawFillBracket's doc comment) -- this
			// require.NotNilf pins that guarantee across the whole sweep,
			// not just the hand-built fixtures elsewhere in this file.
			draw := BuildKnockoutDrawFillBracket(pools, drafts, drawCourts)
			require.NotNilf(t, draw, "n=%d courts=%d: SelectFillBracketDrafts succeeded but the builder still refused -- should be unreachable", n, courts)
			leaves := TreeToLeafArray(draw.Root)
			require.Lenf(t, leaves, p+d, "n=%d courts=%d: leaf count must equal winners+drafts", n, courts)
			for i, l := range leaves {
				assert.NotEmptyf(t, l, "n=%d courts=%d: leaf %d must not be a bye", n, courts, i)
			}
		}
		if d <= 4 {
			realisticTested++
		}
	}

	t.Logf("swept %d values of n in [%d,%d] where FillBracketPoolCount succeeded, x3 court counts = %d (n,courts) pairs; %d half-capacity refusals: %v",
		tested, 2*minSize, maxN, tested*3, halfCapacityRefusals, refusalDetails)
	t.Logf("restricted to D<=4 (the range every 19WKC event and the docs-plan examples fall in): %d (n,courts) pairs, %d half-capacity refusals",
		realisticTested*3, realisticRefusals)

	// Empirical finding (second review rework, capacity-aware selection):
	// making SelectFillBracketDrafts skip a blocked candidate and continue,
	// rather than committing to strict seed-then-pool-order and letting the
	// builder discover the mismatch, drops the refusal count from
	// PLACEMENT-ONLY selection's 123/867 overall (18/492 in the D<=4
	// subset) to 21/867 overall (4/492 in the D<=4 subset) -- an 83%
	// reduction overall, and exactly zero of the reduction is due to
	// abandoning the D<=4/19WKC-shaped range, which itself drops from 18 to
	// 4 (78%). ALL 21 residual pairs occur at courts=4 ONLY (zero at 1 or 2
	// courts) and ALL have an ODD draft count D with the SAME signature:
	// "need D, only D-1 could be placed" -- an unavoidable off-by-one when
	// D is odd and the two halves' oversized-pool supply happens to split
	// evenly (so one half's demand and the other's supply are each one
	// short), not a defect selection could route around by trying harder:
	// D odd forces an uneven ceil/floor split of the total across two
	// symmetric-capacity halves, and if the AVAILABLE oversized pools
	// split evenly too (which AssignPoolsToCourts' front-loaded allocation
	// and CreatePoolsForCount's remainder-spread can produce independently
	// of any selection choice), no reordering closes a supply/demand gap
	// that is genuinely one pool short on one side. The 4 realistic-range
	// (D<=4) pairs are (n=18, courts=4, P=5, D=3), (n=42, courts=4, P=13,
	// D=3), (n=90, courts=4, P=29, D=3), (n=186, courts=4, P=61, D=3) -- a
	// small, NAMED residual set (not "rare, unspecified"), each needing 3
	// drafted 2nds but only 2 placeable. These four are recorded here as
	// the set LP-5's docs/validation note should cite.
	//
	// The exact counts are PINNED (a golden-style regression guard,
	// precedent: bc-draw's "22 of 462 pool-instances moved" review
	// artifacts) rather than bounded by a threshold this file has no
	// principled way to choose: a change to either count -- in EITHER
	// direction -- means the opposite-half feasibility surface moved and
	// needs a human look, not a silently-passing test. Neither count is
	// zero, so the review's "if it is zero... assert zero" contingency
	// does not apply; this is its documented alternative ("which the test
	// should count and report").
	assert.Equalf(t, 21, halfCapacityRefusals,
		"half-capacity refusal count changed from the measured baseline (21 of 867 pairs, all at courts=4) -- review before updating this pin: %v", refusalDetails)
	assert.Equalf(t, 4, realisticRefusals,
		"D<=4 half-capacity refusal count changed from the measured baseline (4 of 492 pairs: n=18/42/90/186, all at courts=4, D=3)")
}
