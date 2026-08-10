package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPoolNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = "Pool " + string(rune('A'+i))
	}
	return names
}

// livePlaceholderBracket runs the LIVE draw pipeline for one (pools,
// qualifiers) combination and returns the bracket it produces, placeholders and
// all.
func livePlaceholderBracket(t *testing.T, eng *Engine, comp *state.Competition, poolNames []string, poolWinners int) *state.Bracket {
	t.Helper()
	pools := make([]helper.Pool, len(poolNames))
	for i, n := range poolNames {
		pools[i] = helper.Pool{PoolName: n}
	}
	draw := helper.BuildKnockoutDraw(pools, poolWinners, len(comp.Courts))
	require.NotNil(t, draw)
	bracket, err := eng.buildBracketFromLeaves(comp, helper.TreeToLeafArray(draw.Root), draw.RegionSpans())
	require.NoError(t, err)
	return bracket
}

// legacyBracketOnDisk rewrites a competition's saved bracket into a genuine
// PRE-Phase-4 file: every slot carries the label the FROZEN v1 draw put there,
// and no placeholder key exists at all. Returns the v1 placeholder labels the
// backfill must reconstruct.
//
// The fixture cannot be "a bracket from the current draw with its placeholders
// stripped" any more. Since bc-draw Phase 4 the live draw and the v1 draw
// disagree about which slot a pool finisher owns, and the whole point of the
// frozen builder is to read files the LIVE draw did not produce.
func legacyBracketOnDisk(t *testing.T, store *state.Store, dir, compID string, poolNames []string, poolWinners int) [][]string {
	t.Helper()
	tpl := legacyPlaceholderTemplateV1(poolNames, poolWinners)
	require.NotEmpty(t, tpl)

	require.NoError(t, store.UpdateBracket(compID, func(b *state.Bracket) error {
		for ri := range b.Rounds {
			for mi := range b.Rounds[ri] {
				m := &b.Rounds[ri][mi]
				m.PlaceholderA, m.PlaceholderB, m.PlaceholderWinner = "", "", ""
				if ri < len(tpl) && mi < len(tpl[ri]) {
					m.SideA, m.SideB, m.Winner = tpl[ri][mi].SideA, tpl[ri][mi].SideB, tpl[ri][mi].Winner
				}
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

	want := make([][]string, 0, len(tpl))
	for _, round := range tpl {
		r := make([]string, 0, len(round))
		for _, m := range round {
			r = append(r, m.SideA+"|"+m.SideB+"|"+m.Winner)
		}
		want = append(want, r)
	}
	return want
}

func legacyTemplateGoldenPath() string {
	return filepath.Join("testdata", "legacy_template_v1.json")
}

// legacyTemplateSweep is the combination set the frozen builder is pinned over.
// Pool counts run past a power of two on both sides so the padding, bye and
// winner-propagation shapes are all exercised, not just the clean cases.
// Qualifier counts include 11-13, where GetOrdinal switches to the "th"
// exception: the frozen copy has to spell "Pool A-11th" the same way the live
// one did or the labels it reconstructs would not match any resolver key.
func legacyTemplateSweep() map[string]string {
	out := map[string]string{}
	for numPools := 2; numPools <= 17; numPools++ {
		for _, poolWinners := range []int{1, 2, 3, 4, 11, 12, 13} {
			key := fmt.Sprintf("P%02d-W%02d", numPools, poolWinners)
			out[key] = v1TemplateDigest(legacyPlaceholderTemplateV1(testPoolNames(numPools), poolWinners))
		}
	}
	return out
}

// v1TemplateDigest renders a frozen template as one canonical string and hashes
// it. A digest rather than the full labels because the sweep expands to about a
// megabyte of JSON for a file that exists for exactly one release; any change to
// any slot in any case still moves its digest, which is the whole job. The
// rendered form is kept legible so a failure can be reproduced by printing it.
func v1TemplateDigest(tpl [][]v1PlaceholderMatch) string {
	var b strings.Builder
	for ri, round := range tpl {
		fmt.Fprintf(&b, "r%d:", ri)
		for _, m := range round {
			fmt.Fprintf(&b, "[%s|%s|%s]", m.SideA, m.SideB, m.Winner)
		}
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// TestLegacyPlaceholderTemplateV1_IsFrozen is the regression gate on the frozen
// v1 draw builder (legacy_template_v1.go): for every combination in the sweep it
// must keep producing exactly the slot labels whose digest is recorded in
// testdata/legacy_template_v1.json.
//
// It used to compare the frozen builder against the LIVE pipeline instead, and
// its own doc said what to do when the two diverged: convert it to a static
// expectation, do NOT delete it and do NOT "update" the frozen builder to agree
// with the new pipeline. bc-draw Phase 4 is that divergence. The frozen builder
// must keep reproducing the OLD draw, because the brackets it reconstructs were
// drawn by the old draw; the golden below IS the old draw, generated from the
// frozen builder at the commit where the live-pipeline comparison was still
// green (see that test in git history).
//
// Regenerate ONLY if the frozen builder is deliberately corrected:
//
//	UPDATE_GOLDEN=1 go test ./internal/engine/ -run TestLegacyPlaceholderTemplateV1_IsFrozen
func TestLegacyPlaceholderTemplateV1_IsFrozen(t *testing.T) {
	got := legacyTemplateSweep()
	encoded, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)
	encoded = append(encoded, '\n')

	path := legacyTemplateGoldenPath()
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, encoded, 0o600))
		t.Logf("regenerated %s with %d cases", path, len(got))
		return
	}

	raw, err := os.ReadFile(path) // #nosec G304 -- fixed testdata path
	require.NoError(t, err, "golden missing; regenerate with UPDATE_GOLDEN=1 go test ./internal/engine/ -run TestLegacyPlaceholderTemplateV1_IsFrozen")
	assert.Equal(t, string(raw), string(encoded),
		"the frozen v1 draw MUST NOT change: it is the only way a bracket drawn before bc-draw Phase 4 can be read back")
}

// TestLegacyPlaceholderTemplateV1_DiffersFromTheLiveDraw states the thing that
// makes the frozen copy load-bearing rather than redundant: the live draw no
// longer produces it. If these two ever agree again, either the draw was
// reverted or the frozen copy was quietly "refactored" onto the live one, and
// every pre-Phase-4 bracket would then resolve into the wrong slots.
func TestLegacyPlaceholderTemplateV1_DiffersFromTheLiveDraw(t *testing.T) {
	poolNames := testPoolNames(3)
	pools := make([]helper.Pool, len(poolNames))
	for i, n := range poolNames {
		pools[i] = helper.Pool{PoolName: n}
	}
	live := helper.TreeToLeafArray(helper.BuildKnockoutDraw(pools, 2, 2).Root)
	frozen := legacyPlaceholderTemplateV1(poolNames, 2)
	require.NotEmpty(t, frozen)

	firstRound := make([]string, 0, len(frozen[0])*2)
	for _, m := range frozen[0] {
		firstRound = append(firstRound, m.SideA, m.SideB)
	}
	assert.NotEqual(t, live, firstRound,
		"the frozen v1 draw must stay DIFFERENT from the live one; it exists to read brackets the live one did not draw")
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

	poolNames := make([]string, len(pools))
	for i, p := range pools {
		poolNames[i] = p.PoolName
	}
	want := legacyBracketOnDisk(t, store, dir, compID, poolNames, 2)
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
	}, round0Slots(b), "a legacy bracket must resolve into the slots the V1 draw gave it, not the slots today's draw would")

	assert.Equal(t, want, drawPlaceholders(b),
		"the frozen v1 builder must reconstruct exactly the labels the v1 draw originally wrote")

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
	require.True(t, drawn.Preview, "scaffold saves a preview bracket")
	want := legacyBracketOnDisk(t, store, dir, compID, []string{"Pool A", "Pool B"}, 1)

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
	legacyBracketOnDisk(t, store, dir, compID, []string{"Pool A", "Pool B", "Pool C", "Pool D"}, 1)

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
