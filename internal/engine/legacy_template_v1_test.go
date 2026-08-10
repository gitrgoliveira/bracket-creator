package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// livePlaceholderBracket runs the LIVE draw pipeline for one (pools, qualifiers)
// combination and returns the bracket it produces, placeholders and all.
func livePlaceholderBracket(t *testing.T, eng *Engine, comp *state.Competition, poolNames []string, poolWinners int) *state.Bracket {
	t.Helper()
	pools := make([]helper.Pool, len(poolNames))
	for i, n := range poolNames {
		pools[i] = helper.Pool{PoolName: n}
	}
	finals := helper.GenerateFinals(pools, poolWinners)
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	bracket, err := eng.buildBracketFromLeaves(comp, helper.TreeToLeafArray(tree))
	require.NoError(t, err)
	return bracket
}

func testPoolNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = "Pool " + string(rune('A'+i))
	}
	return names
}

// TestLegacyPlaceholderTemplateV1_MatchesLivePipeline is the proof that the
// frozen v1 builder (legacy_template_v1.go) started out FAITHFUL: for every
// combination below it reproduces, slot for slot, exactly what the live draw
// records in PlaceholderA/B/Winner.
//
// WHEN THE LIVE PIPELINE CHANGES (bc-draw Phase 4), THIS TEST IS EXPECTED TO GO
// RED, AND THE FIX IS TO CONVERT IT TO A STATIC EXPECTATION — record today's
// values as literals and compare the frozen builder against those — NOT to
// delete it, and NOT to "update" the frozen builder to agree with the new
// pipeline. The frozen builder must keep reproducing the OLD draw, because the
// brackets it reconstructs were drawn by the old draw. A deleted test would
// leave it unverified for the one release in which it still matters.
func TestLegacyPlaceholderTemplateV1_MatchesLivePipeline(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "v1-faithful"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A", "B"}, StartTime: "09:00",
	}))
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)

	// Pool counts run past a power of two on both sides so the padding, bye and
	// winner-propagation shapes are all exercised, not just the clean cases.
	// Qualifier counts include 11-13, where GetOrdinal switches to the "th"
	// exception: the frozen copy has to spell "Pool A-11th" the same way the live
	// one does or the labels it reconstructs would not match any resolver key.
	poolWinnerCounts := []int{1, 2, 3, 4, 11, 12, 13}
	for numPools := 2; numPools <= 17; numPools++ {
		for _, poolWinners := range poolWinnerCounts {
			name := fmt.Sprintf("P%02d-W%02d", numPools, poolWinners)
			t.Run(name, func(t *testing.T) {
				poolNames := testPoolNames(numPools)
				live := livePlaceholderBracket(t, eng, comp, poolNames, poolWinners)
				frozen := legacyPlaceholderTemplateV1(poolNames, poolWinners)

				require.Len(t, frozen, len(live.Rounds), "round count")
				for ri := range live.Rounds {
					require.Lenf(t, frozen[ri], len(live.Rounds[ri]), "round %d match count", ri)
					for mi := range live.Rounds[ri] {
						m := live.Rounds[ri][mi]
						f := frozen[ri][mi]
						assert.Equalf(t, m.PlaceholderA, f.SideA, "r%d m%d SideA", ri, mi)
						assert.Equalf(t, m.PlaceholderB, f.SideB, "r%d m%d SideB", ri, mi)
						assert.Equalf(t, m.PlaceholderWinner, f.Winner, "r%d m%d Winner", ri, mi)
						// The live draw's placeholders ARE its sides at draw time;
						// pinning that here keeps the frozen builder anchored to
						// the bracket itself, not merely to the new fields.
						assert.Equalf(t, m.SideA, f.SideA, "r%d m%d live SideA", ri, mi)
						assert.Equalf(t, m.SideB, f.SideB, "r%d m%d live SideB", ri, mi)
						assert.Equalf(t, m.Winner, f.Winner, "r%d m%d live Winner", ri, mi)
					}
				}
			})
		}
	}
}

// TestLegacyPlaceholderTemplateV1_Degenerate covers the inputs that produce no
// draw at all, so the backfill reports "nothing learned" instead of panicking or
// writing an empty template over a real bracket.
func TestLegacyPlaceholderTemplateV1_Degenerate(t *testing.T) {
	assert.Nil(t, legacyPlaceholderTemplateV1(nil, 2), "no pools, no template")
	assert.Nil(t, legacyPlaceholderTemplateV1([]string{"Pool A"}, 0), "no qualifiers, no template")
	assert.Nil(t, legacyPlaceholderTemplateV1([]string{"Pool A"}, 1), "a single finalist has no match to place")

	assert.False(t, backfillDrawPlaceholdersV1(nil, []string{"Pool A", "Pool B"}, 1), "nil bracket")
	assert.False(t, backfillDrawPlaceholdersV1(&state.Bracket{}, nil, 1), "empty template writes nothing")
}

// TestLegacyPlaceholderTemplateV1_BronzeIsNotTemplated pins the frozen builder's
// one deliberate omission: the bronze never held a pool placeholder under v1,
// because it is created after the bracket is built and fed from semifinal losers.
func TestLegacyPlaceholderTemplateV1_BronzeIsNotTemplated(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "v1-bronze"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual", Naginata: true,
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00",
	}))
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)

	live := livePlaceholderBracket(t, eng, comp, testPoolNames(4), 1)
	require.NotNil(t, live.ThirdPlaceMatch, "naginata bracket with a semifinal has a bronze")
	assert.Empty(t, live.ThirdPlaceMatch.PlaceholderA)
	assert.Empty(t, live.ThirdPlaceMatch.PlaceholderB)
	assert.Empty(t, live.ThirdPlaceMatch.PlaceholderWinner)
}

// stripDrawPlaceholders rewrites a saved bracket into the shape it had before the
// placeholder fields existed: same sides, no record of where they came from. This
// is what every bracket drawn before this change looks like on disk.
func stripDrawPlaceholders(t *testing.T, store *state.Store, dir, compID string) {
	t.Helper()
	require.NoError(t, store.UpdateBracket(compID, func(b *state.Bracket) error {
		for ri := range b.Rounds {
			for mi := range b.Rounds[ri] {
				m := &b.Rounds[ri][mi]
				m.PlaceholderA, m.PlaceholderB, m.PlaceholderWinner = "", "", ""
			}
		}
		if b.ThirdPlaceMatch != nil {
			b.ThirdPlaceMatch.PlaceholderA = ""
			b.ThirdPlaceMatch.PlaceholderB = ""
			b.ThirdPlaceMatch.PlaceholderWinner = ""
		}
		return nil
	}))
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "bracket.json"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "placeholder",
		"the fixture must be a genuine pre-Phase-4 bracket.json, with no placeholder keys at all")
}

func drawPlaceholders(b *state.Bracket) [][]string {
	var out [][]string
	for _, round := range b.Rounds {
		var r []string
		for _, m := range round {
			r = append(r, m.PlaceholderA+"|"+m.PlaceholderB+"|"+m.PlaceholderWinner)
		}
		out = append(out, r)
	}
	return out
}

// TestResolveQualifiedPools_LegacyBracketBackfill is the migration test: a
// bracket.json drawn BEFORE the placeholder fields existed must still resolve
// correctly, via the frozen v1 builder, and must come out of the first resolution
// with the fields backfilled and persisted.
//
// It uses the 3-pools x 2-qualifiers draw, which pads 6 finishers into 8 slots
// and therefore exercises the frozen copy's bye handling and winner propagation,
// not just its round-0 pairing. The expected slot mapping is the same one
// TestResolveQualifiedPools_ThreePoolsTwoWinners_SlotMapping pins for a
// new-format bracket: the migration must be invisible.
func TestResolveQualifiedPools_LegacyBracketBackfill(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "legacy-backfill"

	pools, participants, results := unbalancedPools(3)
	saveMixedScaffold(t, store, compID, pools, 2)
	require.NoError(t, store.SaveParticipants(compID, participants))

	drawn, err := store.LoadBracket(compID)
	require.NoError(t, err)
	want := drawPlaceholders(drawn)
	require.NotEmpty(t, want)

	stripDrawPlaceholders(t, store, dir, compID)
	require.NoError(t, store.SavePoolMatches(compID, results))

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved, "a legacy bracket must resolve exactly like a new one")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"A1|",   // Pool A winner byes
		"C1|B2", //
		"B1|",   // Pool B winner byes
		"A2|C2", //
	}, round0Slots(b), "legacy resolution must produce the same slot mapping as a new-format bracket")

	assert.Equal(t, want, drawPlaceholders(b),
		"the frozen v1 builder must reconstruct exactly the labels the draw originally wrote")

	raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "bracket.json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"placeholderA"`, "the backfill must be persisted, not recomputed forever")
}

// TestResolveQualifiedPools_LegacyBackfillPersistsWithNothingResolved covers the
// case where the migration is the ONLY change: a legacy bracket whose pools are
// all still running. Nothing resolves, but the reconstruction must still be saved
// — otherwise it would be recomputed on every pool-match completion, and (worse)
// a placement change landing mid-pool-phase would find no record to fall back on.
func TestResolveQualifiedPools_LegacyBackfillPersistsWithNothingResolved(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "legacy-backfill-only"

	pools, participants, _ := unbalancedPools(2)
	saveMixedScaffold(t, store, compID, pools, 1)
	require.NoError(t, store.SaveParticipants(compID, participants))
	// Both pools scheduled, none complete.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: state.MatchStatusScheduled},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Status: state.MatchStatusScheduled},
	}))

	drawn, err := store.LoadBracket(compID)
	require.NoError(t, err)
	want := drawPlaceholders(drawn)
	require.True(t, drawn.Preview, "scaffold saves a preview bracket")
	stripDrawPlaceholders(t, store, dir, compID)

	resolvedNow, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	assert.Equal(t, 0, resolvedNow, "no pool is finished, so nothing may be seeded")
	assert.False(t, allResolved)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, want, drawPlaceholders(b), "the backfill must be persisted on its own")
	assert.True(t, b.Preview, "a backfill-only call must not flip the bracket live")

	// Second call: the fields are present now, so the frozen builder is never
	// consulted again and the call is a clean no-op.
	resolvedNow, _, err = eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	assert.Equal(t, 0, resolvedNow)
}

// TestResolveQualifiedPools_LegacyBracketGeometryMismatch covers a hand-edited /
// truncated legacy bracket: the frozen template is larger than the file. The
// backfill fills what it can and the resolver must not panic on the rest.
func TestResolveQualifiedPools_LegacyBracketGeometryMismatch(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "legacy-truncated"

	pools, participants, results := unbalancedPools(4)
	saveMixedScaffold(t, store, compID, pools, 1)
	require.NoError(t, store.SaveParticipants(compID, participants))
	require.NoError(t, store.SavePoolMatches(compID, results))

	// Drop the final round and one round-0 match, then strip the placeholders:
	// a bracket that no longer matches the draw it came from.
	require.NoError(t, store.UpdateBracket(compID, func(b *state.Bracket) error {
		b.Rounds = [][]state.BracketMatch{b.Rounds[0][:1]}
		return nil
	}))
	stripDrawPlaceholders(t, store, dir, compID)

	_, _, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, b.Rounds, 1)
	require.Len(t, b.Rounds[0], 1)
	assert.Equal(t, "A1", b.Rounds[0][0].SideA, "the surviving slot still resolves from its own reconstructed label")
	assert.Equal(t, "B1", b.Rounds[0][0].SideB)
}

// TestDrawPlaceholdersAreNotWrittenForPlayoffs pins the write gate: a standalone
// playoffs bracket's leaves are real competitors, nothing resolves into it, and
// recording player names under a "placeholder" field would mislead and bloat
// every bracket.json for no reader.
func TestDrawPlaceholdersAreNotWrittenForPlayoffs(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "playoffs-no-placeholders"
	createTestCompetition(t, store, compID, state.CompFormatPlayoffs, 4)
	require.NoError(t, store.SaveParticipants(compID, makePlayers(8)))
	require.NoError(t, eng.GenerateDraw(compID))

	raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "bracket.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "placeholder")

	var parsed state.Bracket
	require.NoError(t, json.Unmarshal(raw, &parsed))
	require.NotEmpty(t, parsed.Rounds)
	for _, round := range parsed.Rounds {
		for _, m := range round {
			assert.Empty(t, m.PlaceholderA)
			assert.Empty(t, m.PlaceholderB)
		}
	}
}
