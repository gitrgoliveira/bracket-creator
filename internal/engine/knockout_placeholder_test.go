package engine

import (
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDrawRecordsPlaceholders pins the write half of the contract: a pool-fed
// knockout records, on every match, the label its slot held at draw time.
func TestDrawRecordsPlaceholders(t *testing.T) {
	_, store, _ := setupTestEngine(t)
	compID := "draw-records-placeholders"

	pools, _, _ := unbalancedPools(4)
	saveMixedScaffold(t, store, compID, pools, 2)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotEmpty(t, b.Rounds)

	poolOrigin := 0
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := b.Rounds[ri][mi]
			assert.Equalf(t, m.SideA, m.PlaceholderA, "r%d m%d: placeholder must equal the draw-time side", ri, mi)
			assert.Equalf(t, m.SideB, m.PlaceholderB, "r%d m%d", ri, mi)
			assert.Equalf(t, m.Winner, m.PlaceholderWinner, "r%d m%d", ri, mi)
			if helper.IsPoolFinalistPlaceholder(m.PlaceholderA) {
				poolOrigin++
			}
			if helper.IsPoolFinalistPlaceholder(m.PlaceholderB) {
				poolOrigin++
			}
		}
	}
	assert.Equal(t, 8, poolOrigin, "4 pools x 2 qualifiers = 8 pool-origin slots recorded")
}

// finisherFor maps a pool-origin placeholder to the competitor unbalancedPools
// makes finish there: "Pool A-1st" → "A1", "Pool B-2nd" → "B2".
func finisherFor(placeholder string) (string, bool) {
	if !helper.IsPoolFinalistPlaceholder(placeholder) {
		return "", false
	}
	idx := strings.LastIndex(placeholder, "-")
	pool := strings.TrimPrefix(placeholder[:idx], "Pool ")
	rank := strings.TrimSuffix(strings.TrimSuffix(placeholder[idx+1:], "st"), "nd")
	return pool + rank, true
}

// round0Placeholders renders the first round's recorded draw labels, so a
// bracket's placement can be compared in one assertion.
func round0Placeholders(b *state.Bracket) []string {
	out := []string{}
	if b == nil || len(b.Rounds) == 0 {
		return out
	}
	for _, m := range b.Rounds[0] {
		out = append(out, m.PlaceholderA+"|"+m.PlaceholderB)
	}
	return out
}

// TestResolveQualifiedPools_SurvivesPlacementChange is THE hazard regression, and
// the reason the placeholder fields exist.
//
// The resolver used to reconstruct the placeholder template by rerunning the live
// draw algorithm on every call, and match it against the running bracket BY
// POSITION. That is correct only while the algorithm never changes. bc-draw
// Phase 4 changes it, and an operator who upgrades between a competition's draw
// and the end of its pool phase — an ordinary thing to do at a two-day event —
// would have had qualifiers written into the WRONG slots of a live knockout, with
// nothing to detect it: the structural guards only ever caught differing
// round/match COUNTS, which a placement change does not alter.
//
// The test stands in for that upgrade by drawing a bracket whose placement
// DIFFERS from what the current algorithm produces (two whole round-0 pairings
// swapped, sides and recorded labels together, which is what any placement change
// looks like from the resolver's side: same shape, different pool-to-slot map).
// It then resolves every pool and asserts each slot receives the finisher ITS OWN
// recorded label names.
//
// Against the old recompute-based resolver this fails: the recomputed template
// still says slot 0 belongs to the unswapped pairing, so it writes those
// qualifiers into the swapped slots. Verified by reverting the resolver.
func TestResolveQualifiedPools_SurvivesPlacementChange(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "placement-change"

	pools, participants, results := unbalancedPools(4)
	saveMixedScaffold(t, store, compID, pools, 2)
	require.NoError(t, store.SaveParticipants(compID, participants))
	require.NoError(t, store.SavePoolMatches(compID, results))

	asDrawnByCurrentAlgorithm, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, asDrawnByCurrentAlgorithm.Rounds[0], 4, "8 qualifiers → 4 round-1 matches")

	// Re-draw with a DIFFERENT placement: swap the first and last round-1
	// pairings, sides and recorded labels together. Nothing else moves, so the
	// bracket keeps exactly the round/match counts the old guards checked.
	require.NoError(t, store.UpdateBracket(compID, func(b *state.Bracket) error {
		first, last := &b.Rounds[0][0], &b.Rounds[0][3]
		first.SideA, last.SideA = last.SideA, first.SideA
		first.SideB, last.SideB = last.SideB, first.SideB
		first.PlaceholderA, last.PlaceholderA = last.PlaceholderA, first.PlaceholderA
		first.PlaceholderB, last.PlaceholderB = last.PlaceholderB, first.PlaceholderB
		return nil
	}))

	redrawn, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotEqual(t, round0Placeholders(asDrawnByCurrentAlgorithm), round0Placeholders(redrawn),
		"the fixture must genuinely differ from what a recompute produces, otherwise this test proves nothing")

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)

	// Every slot holds the finisher its OWN draw-time label names. This is the
	// property the recompute could not provide.
	checked := 0
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := b.Rounds[ri][mi]
			if want, ok := finisherFor(m.PlaceholderA); ok {
				assert.Equalf(t, want, m.SideA, "r%d m%d SideA: slot drawn as %q", ri, mi, m.PlaceholderA)
				checked++
			}
			if want, ok := finisherFor(m.PlaceholderB); ok {
				assert.Equalf(t, want, m.SideB, "r%d m%d SideB: slot drawn as %q", ri, mi, m.PlaceholderB)
				checked++
			}
		}
	}
	assert.Equal(t, 8, checked, "every one of the 8 qualifier slots must be checked")

	// Pinned end state: the swapped pairings hold the swapped qualifiers, NOT the
	// ones the current algorithm would compute for those positions.
	assert.Equal(t, []string{
		"D1|B2", // drawn last by the current algorithm, first by this one
		"B1|D2",
		"C1|A2",
		"A1|C2", // and vice versa
	}, round0Slots(b), "qualifiers must land where the DRAW put them, not where a recompute would")
}
