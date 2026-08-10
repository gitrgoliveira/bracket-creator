package engine

import (
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---------------------------------------------------------------

// saveMixedScaffold writes a mixed competition with the given pools and a
// pool-origin PREVIEW bracket (placeholders) shaped like generatePoolPreviewBracket
// would produce. poolWinners controls how many finishers each pool promotes.
func saveMixedScaffold(t *testing.T, store *state.Store, compID string, pools []helper.Pool, poolWinners int) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        compID,
		Kind:        "individual",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		PoolWinners: poolWinners,
	}))
	require.NoError(t, store.SavePools(compID, pools))

	// Build the preview bracket the same way the engine does so placeholder
	// labels match exactly (GenerateFinals → CreateBalancedTree →
	// ApplyPoolAdjustments → TreeToLeafArray → buildBracketFromLeaves).
	finals := helper.GenerateFinals(pools, poolWinners)
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	leaves := helper.TreeToLeafArray(tree)
	eng := New(store)
	comp, _ := store.LoadCompetition(compID)
	bracket, err := eng.buildBracketFromLeaves(comp, leaves)
	require.NoError(t, err)
	bracket.Preview = true
	require.NoError(t, store.SaveBracket(compID, bracket))
}

func bracketSides(b *state.Bracket) []string {
	var out []string
	for _, round := range b.Rounds {
		for _, m := range round {
			out = append(out, m.SideA, m.SideB)
		}
	}
	return out
}

// --- ResolveQualifiedPools: incremental seeding ----------------------------

// TestResolveQualifiedPools_Incremental is the core test for gate-free, per-pool
// knockout seeding: a pool's finishers drop into their bracket slots the moment
// that pool finishes, while other pools are still in progress.
func TestResolveQualifiedPools_Incremental(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "incremental"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}}},
	}
	saveMixedScaffold(t, store, compID, pools, 2)
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"}, {Name: "B2"},
	}))

	// Pool A round-robin done (A1 > A2); Pool B still scheduled.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Status: state.MatchStatusScheduled},
	}))

	resolvedNow, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	assert.Greater(t, resolvedNow, 0, "Pool A finishers must be seeded immediately")
	assert.False(t, allResolved, "Pool B is still running, so not all pools resolved")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	sides := bracketSides(b)
	assert.Contains(t, sides, "A1", "Pool A 1st must be seeded")
	assert.Contains(t, sides, "A2", "Pool A 2nd must be seeded")
	assert.Contains(t, sides, "Pool B-1st", "Pool B placeholders must remain until Pool B finishes")
	assert.NotContains(t, sides, "B1", "Pool B not finished, must not be seeded yet")

	// Competition must remain in pools (Pool B still running).
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status)

	// Now finish Pool B.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Winner: "B1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))
	_, allResolved, err = eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	assert.True(t, allResolved, "with both pools finished, every placeholder must be resolved")

	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.False(t, bracketHasPoolPlaceholders(b), "no pool placeholders may remain")
	assert.False(t, b.Preview, "Preview flag must be cleared once the bracket is seeded")
}

// TestResolveQualifiedPools_ReSeedAfterRescore verifies the re-seedable contract:
// if an operator re-scores a completed pool match AFTER that pool was seeded into
// the knockout, changing the finisher order, the new finisher overwrites the
// stale name in the same bracket slot (not silently dropped). This is the mp-turx
// incremental-seeding desync the /security-review sub-agent caught.
func TestResolveQualifiedPools_ReSeedAfterRescore(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "reseed"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}}},
	}
	saveMixedScaffold(t, store, compID, pools, 2)
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"}, {Name: "B2"},
	}))

	// Record the placeholder slot positions BEFORE any resolution so we can assert
	// the SAME slot is re-seeded after a re-score.
	tpl, err := store.LoadBracket(compID)
	require.NoError(t, err)
	tplSides := bracketSides(tpl)
	idxA1st, idxA2nd := -1, -1
	for i, s := range tplSides {
		if s == "Pool A-1st" {
			idxA1st = i
		}
		if s == "Pool A-2nd" {
			idxA2nd = i
		}
	}
	require.GreaterOrEqual(t, idxA1st, 0, "template must contain Pool A-1st")
	require.GreaterOrEqual(t, idxA2nd, 0, "template must contain Pool A-2nd")

	// First scoring: A1 beats A2 (A1 is 1st), B1 beats B2. Seed.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Winner: "B1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))
	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	sides := bracketSides(b)
	require.Equal(t, "A1", sides[idxA1st], "Pool A-1st slot holds A1 after first scoring")
	require.Equal(t, "A2", sides[idxA2nd], "Pool A-2nd slot holds A2 after first scoring")

	// RE-SCORE Pool A so A2 now wins (A2 becomes 1st, A1 becomes 2nd) while the
	// comp is still in the pool phase. This is a routine operator correction.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A2", IpponsB: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Winner: "B1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))
	resolvedNow, _, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	assert.Greater(t, resolvedNow, 0, "re-score must re-seed the changed slots")

	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	sides = bracketSides(b)
	assert.Equal(t, "A2", sides[idxA1st], "Pool A-1st slot must now hold A2 after the re-score (re-seeded, not stale)")
	assert.Equal(t, "A1", sides[idxA2nd], "Pool A-2nd slot must now hold A1 after the re-score")
}

// TestResolveQualifiedPools_LonePoolNoMatches verifies that a pool with exactly
// one participant (round-robin generates ZERO matches for it) is treated as
// complete, so its lone finisher is seeded and the comp does not get stuck in
// `pools`. Scenario: 3 participants, PoolSize=2, max mode → Pool A (2 players,
// 1 match) + Pool B (1 player, 0 matches), poolWinners=1.
func TestResolveQualifiedPools_LonePoolNoMatches(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "lone-pool"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}}}, // lone qualifier, no matches
	}
	saveMixedScaffold(t, store, compID, pools, 1)
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"},
	}))
	// Only Pool A has a match; Pool B has none (size 1).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	assert.True(t, allResolved, "a 1-participant pool (zero matches) must count as complete so the comp isn't stuck")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.False(t, bracketHasPoolPlaceholders(b), "both A1 (Pool A 1st) and B1 (lone Pool B) must be seeded")
	sides := bracketSides(b)
	assert.Contains(t, sides, "A1")
	assert.Contains(t, sides, "B1")
}

// TestResolveQualifiedPools_DegeneratePoolClampsBye verifies that a completed
// pool with fewer finishers than PoolWinners does not error, instead the
// unfillable placeholder is resolved as a bye ("") so the bracket advances.
// This scenario is unreachable via supported flows (generatePools rejects it)
// but can arise from hand-edited tournament-data or legacy imports.
func TestResolveQualifiedPools_DegeneratePoolClampsBye(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "degenerate"

	// Pool A has 2 players (normal), Pool B has 1 player (degenerate when
	// poolWinners=2: can only supply a 1st-place finisher, not a 2nd).
	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}}},
	}
	saveMixedScaffold(t, store, compID, pools, 2)
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err, "degenerate pool must not return an error, clamp to bye instead")
	assert.True(t, allResolved, "both pools should be fully resolved (B's 2nd-place slot is a bye)")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.False(t, bracketHasPoolPlaceholders(b), "no pool placeholder should remain")

	sides := bracketSides(b)
	assert.Contains(t, sides, "A1")
	assert.Contains(t, sides, "A2")
	assert.Contains(t, sides, "B1")

	// The match with one empty side (the degenerate bye) must be
	// auto-completed with Winner set to the non-empty side.
	foundBye := false
	for _, round := range b.Rounds {
		for _, m := range round {
			if (m.SideA == "" && m.SideB != "") || (m.SideA != "" && m.SideB == "") {
				foundBye = true
				assert.Equal(t, state.MatchStatusCompleted, m.Status, "bye match must be auto-completed")
				nonEmpty := m.SideA
				if nonEmpty == "" {
					nonEmpty = m.SideB
				}
				assert.Equal(t, nonEmpty, m.Winner, "bye match Winner must be the non-empty side")
			}
		}
	}
	require.True(t, foundBye, "expected at least one bye match (one empty side) from the degenerate pool")
}

// TestResolveQualifiedPools_NonMixedNoOp verifies the resolver is a no-op for
// competitions that have no pool placeholders (standalone playoffs / league).
func TestResolveQualifiedPools_NonMixedNoOp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	for _, format := range []string{state.CompFormatPlayoffs, state.CompFormatLeague} {
		compID := "noop-" + format
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: compID, Format: format, Status: state.CompStatusPlayoffs, Courts: []string{"A"},
		}))
		n, all, err := eng.ResolveQualifiedPools(compID)
		require.NoError(t, err)
		assert.Equal(t, 0, n)
		assert.False(t, all)
	}
}

// TestResolveQualifiedPools_CrossSeedOrder is the regression test for the
// seeding bug: with poolWinners=2 the cross-seed order differs from rank order,
// so the two pool WINNERS must land on opposite ends of the draw (never in the
// same first-round match).
func TestResolveQualifiedPools_CrossSeedOrder(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "crossseed"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}, {Name: "A3"}, {Name: "A4"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}, {Name: "B3"}, {Name: "B4"}}},
	}
	saveMixedScaffold(t, store, compID, pools, 2)
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "A3"}, {Name: "A4"},
		{Name: "B1"}, {Name: "B2"}, {Name: "B3"}, {Name: "B4"},
	}))

	win := func(id, a, b, w string) state.MatchResult {
		return state.MatchResult{ID: id, SideA: a, SideB: b, Winner: w, IpponsA: []string{"M"}, Status: state.MatchStatusCompleted}
	}
	// Distinct win counts → A1>A2>A3>A4 and B1>B2>B3>B4 (no ties → no tiebreakers).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		win("Pool A-0", "A1", "A2", "A1"), win("Pool A-1", "A1", "A3", "A1"), win("Pool A-2", "A1", "A4", "A1"),
		win("Pool A-3", "A2", "A3", "A2"), win("Pool A-4", "A2", "A4", "A2"), win("Pool A-5", "A3", "A4", "A3"),
		win("Pool B-0", "B1", "B2", "B1"), win("Pool B-1", "B1", "B3", "B1"), win("Pool B-2", "B1", "B4", "B1"),
		win("Pool B-3", "B2", "B3", "B2"), win("Pool B-4", "B2", "B4", "B2"), win("Pool B-5", "B3", "B4", "B3"),
	}))

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	r0 := b.Rounds[0]
	for _, m := range r0 {
		pair := map[string]bool{m.SideA: true, m.SideB: true}
		assert.False(t, pair["A1"] && pair["B1"],
			"pool winners A1 and B1 must NOT meet in the first round (cross-seed must keep them apart)")
	}
}

// TestResolveQualifiedPools_ByeWinnerField verifies that when a finisher draws a
// bye (odd finalist count), the placeholder is resolved in the bye match's SideA
// AND its pre-filled Winner field. Uses 3 pools × 1 winner = 3 finalists in a
// 4-slot bracket → one bye.
func TestResolveQualifiedPools_ByeWinnerField(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bye"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}}},
		{PoolName: "Pool C", Players: []helper.Player{{Name: "C1"}, {Name: "C2"}}},
	}
	saveMixedScaffold(t, store, compID, pools, 1)
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"}, {Name: "B2"}, {Name: "C1"}, {Name: "C2"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Winner: "B1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool C-0", SideA: "C1", SideB: "C2", Winner: "C1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	// No placeholder may survive in ANY field, including the bye match's Winner.
	for _, round := range b.Rounds {
		for _, m := range round {
			assert.Falsef(t, helper.IsPoolFinalistPlaceholder(m.SideA), "SideA placeholder leaked: %q", m.SideA)
			assert.Falsef(t, helper.IsPoolFinalistPlaceholder(m.SideB), "SideB placeholder leaked: %q", m.SideB)
			assert.Falsef(t, helper.IsPoolFinalistPlaceholder(m.Winner), "Winner placeholder leaked: %q", m.Winner)
		}
	}
}

// --- per-match playability gate --------------------------------------------

// TestBracketMatchPlayable covers the structural predicate used to gate scoring.
func TestBracketMatchPlayable(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Alice", "Bob", true},
		{"Pool A-1st", "Bob", false},
		{"Alice", "Pool B-2nd", false},
		{"Winner of r2-m0", "Alice", false},
		{"Alice", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got := bracketMatchPlayable(&state.BracketMatch{SideA: c.a, SideB: c.b})
		assert.Equalf(t, c.want, got, "playable(%q,%q)", c.a, c.b)
	}
}

// TestScoreKnockout_PerMatchGate verifies that the scoring path rejects a
// knockout match with an unresolved side and accepts one with both sides
// resolved, replacing the old bracket-wide Preview gate.
func TestScoreKnockout_PerMatchGate(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "permatch"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual", Format: state.CompFormatMixed,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
			{ID: "m-r1-1", SideA: "Pool B-1st", SideB: "Carol", Status: state.MatchStatusScheduled},
		}},
	}))

	// Unresolved side → rejected.
	err := eng.RecordMatchResult(compID, "m-r1-1", &state.MatchResult{
		SideA: "Pool B-1st", SideB: "Carol", Winner: "Carol", Status: state.MatchStatusCompleted,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready to score")

	// Both sides resolved → accepted.
	err = eng.RecordMatchResult(compID, "m-r1-0", &state.MatchResult{
		SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", b.Rounds[0][0].Winner)
}

// TestKnockoutOnly_ScoreableFromDraw verifies that a standalone (knockout-only)
// playoffs competition is scoreable from draw time, its round-1 leaves are real
// players, so the per-match gate lets them through with no pool resolution.
func TestKnockoutOnly_ScoreableFromDraw(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "ko-only"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual", Format: state.CompFormatPlayoffs,
		Status: state.CompStatusPlayoffs, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
				{ID: "m-r1-1", SideA: "Carol", SideB: "Dan", Status: state.MatchStatusScheduled},
			},
			{
				{ID: "m-r2-0", SideA: "Winner of r2-m0", SideB: "Winner of r2-m1", Status: state.MatchStatusScheduled},
			},
		},
	}))

	// Round-1 match is playable immediately.
	require.NoError(t, eng.RecordMatchResult(compID, "m-r1-0", &state.MatchResult{
		SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted,
	}))
	// The final is NOT playable yet, its sides are "Winner of …" feeders.
	err := eng.RecordMatchResult(compID, "m-r2-0", &state.MatchResult{
		SideA: "Winner of r2-m0", SideB: "Winner of r2-m1", Winner: "Winner of r2-m0", Status: state.MatchStatusCompleted,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready to score")
}

// --- MaybeAutoCompletePools mixed/league branches --------------------------

// TestMaybeAutoCompletePools_MixedStaysInPoolsWhileScheduled: a mixed comp with
// an unfinished pool match must not flip to playoffs.
func TestMaybeAutoCompletePools_MixedStaysInPoolsWhileScheduled(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mixed-running"
	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}}},
	}
	saveMixedScaffold(t, store, compID, pools, 1)
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"}, {Name: "B2"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Status: state.MatchStatusScheduled},
	}))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompletePoolsResolved, outcome, "Pool A seeded → bracket changed, but comp stays in pools")
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status)
}

// TestMaybeAutoCompletePools_MixedFlipsWhenAllPoolsDone: once the last pool is
// seeded, the comp moves pools → playoffs.
func TestMaybeAutoCompletePools_MixedFlipsWhenAllPoolsDone(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mixed-flip"
	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}}},
	}
	saveMixedScaffold(t, store, compID, pools, 1)
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"}, {Name: "B2"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Winner: "B1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteKnockoutStarted, outcome)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPlayoffs, comp.Status)
}

// TestMaybeAutoCompletePools_LeagueCompletes: league still auto-completes.
func TestMaybeAutoCompletePools_LeagueCompletes(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-done"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "A1", SideB: "A3", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool A-2", SideA: "A2", SideB: "A3", Winner: "A2", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status)
}

// --- draw-time invariant ---------------------------------------------------

// TestGeneratePools_MixedRequiresTwoPools verifies the draw-time invariant: a
// mixed competition refuses to generate when participants + PoolSize would
// produce fewer than 2 pools.
func TestGeneratePools_MixedRequiresTwoPools(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mixed-too-few-pools"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Tiny Mixed", Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusSetup,
		Courts: []string{"A"}, StartTime: "09:00",
		PoolSize: 10, PoolSizeMode: "max", PoolWinners: 2,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"}, {Name: "Dan"}, {Name: "Eve"},
	}))
	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 2 pools")
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusSetup, comp.Status)
}

// TestGeneratePools_MixedRejectsUnderfilledPool verifies the draw-time invariant
// that every pool can supply PoolWinners finishers. With PoolSize=2 in "max"
// mode an odd participant count leaves a 1-participant last pool; with the
// default PoolWinners=2 that pool can't produce a 2nd finisher, so the draw must
// be rejected up front rather than failing mid-tournament in ResolveQualifiedPools.
func TestGeneratePools_MixedRejectsUnderfilledPool(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mixed-underfilled-pool"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Uneven Mixed", Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusSetup,
		Courts: []string{"A"}, StartTime: "09:00",
		PoolSize: 2, PoolSizeMode: "max", PoolWinners: 2,
	}))
	// 5 participants @ PoolSize=2 (max) → at least one pool of size 1.
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"}, {Name: "Dan"}, {Name: "Eve"},
	}))
	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "advance to the knockout")
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "draw must not advance status on validation failure")
}

// --- unbalanced (non-power-of-two) pool counts, real names ------------------

// unbalancedPools builds n two-player pools named "Pool A".."Pool <n>" with
// competitors <letter>1 and <letter>2, plus the completed round-robin result
// that makes <letter>1 the pool winner and <letter>2 the runner-up. Two-player
// pools keep the standings unambiguous (one match, distinct win counts) so the
// assertions below are about the DRAW, not about tie-breaking.
func unbalancedPools(n int) ([]helper.Pool, []domain.Player, []state.MatchResult) {
	pools := make([]helper.Pool, 0, n)
	participants := make([]domain.Player, 0, 2*n)
	results := make([]state.MatchResult, 0, n)
	for i := 0; i < n; i++ {
		letter := string(rune('A' + i))
		first, second := letter+"1", letter+"2"
		name := "Pool " + letter
		pools = append(pools, helper.Pool{
			PoolName: name,
			Players:  []helper.Player{{Name: first}, {Name: second}},
		})
		participants = append(participants, domain.Player{Name: first}, domain.Player{Name: second})
		results = append(results, state.MatchResult{
			ID: name + "-0", SideA: first, SideB: second, Winner: first,
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted,
		})
	}
	return pools, participants, results
}

// round0Slots returns the first knockout round as "SideA|SideB" strings in slot
// order, so a whole round's placement is asserted in one comparison.
func round0Slots(b *state.Bracket) []string {
	slots := []string{}
	if b == nil || len(b.Rounds) == 0 {
		return slots
	}
	for _, m := range b.Rounds[0] {
		slots = append(slots, m.SideA+"|"+m.SideB)
	}
	return slots
}

// TestResolveQualifiedPools_ThreePoolsTwoWinners_SlotMapping is the real-name
// unbalanced counterpart to the balanced placeholder tests above (bc-draw
// Phase 1): 3 pools x 2 qualifiers = 6 finishers in an 8-slot bracket, so the
// draw is NOT a clean power of two and the two structural byes have to land
// somewhere. It pins exactly which qualifier reaches which slot.
//
// The same shape appears in internal/helper/testdata/draw_shapes.json under
// case "P03-W2-C1"; this test proves the live engine path (real standings
// through ResolveQualifiedPools) produces it with real competitor names, not
// just the placeholder pipeline.
//
// CURRENT BEHAVIOUR, TWO DEFECTS PINNED:
//   - Pool C's WINNER (C1) plays a round-1 match while the winners of pools A
//     and B bye. bc-draw R6 wants byes distributed by seed and pool size, not
//     by whichever leaf the balanced-tree split happened to leave odd.
//   - A2 vs C2 is a runner-up-versus-runner-up round-1 match, so the
//     "every round-1 match is a 1st against a 2nd" property does not hold at
//     3 pools (it holds only at power-of-two pool counts; see
//     helper.TestBracketCrossPoolMatching).
func TestResolveQualifiedPools_ThreePoolsTwoWinners_SlotMapping(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "unbalanced-3x2"

	pools, participants, results := unbalancedPools(3)
	saveMixedScaffold(t, store, compID, pools, 2)
	require.NoError(t, store.SaveParticipants(compID, participants))
	require.NoError(t, store.SavePoolMatches(compID, results))

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved, "every pool finished, so no placeholder may survive")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.False(t, bracketHasPoolPlaceholders(b), "no pool placeholder may remain")
	require.Len(t, b.Rounds, 3, "8 slots produce 3 knockout rounds")

	// Slot-for-slot placement of the first round. "" is a bye.
	assert.Equal(t, []string{
		"A1|",   // Pool A winner byes
		"C1|B2", // Pool C WINNER must play, against Pool B's runner-up
		"B1|",   // Pool B winner byes
		"A2|C2", // runner-up vs runner-up
	}, round0Slots(b), "3-pool x 2-qualifier slot mapping changed")

	// The two byes are auto-completed with the bye holder as winner.
	assert.Equal(t, "A1", b.Rounds[0][0].Winner)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[0][0].Status)
	assert.Equal(t, "B1", b.Rounds[0][2].Winner)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[0][2].Status)

	// The two real round-1 matches are still waiting to be played.
	assert.Equal(t, state.MatchStatusScheduled, b.Rounds[0][1].Status)
	assert.Equal(t, state.MatchStatusScheduled, b.Rounds[0][3].Status)

	// Same-pool qualifiers are kept apart until the final: A1/A2, B1/B2 and
	// C1/C2 each sit in opposite halves of the 8-slot draw. This property DOES
	// hold today and the rewrite (bc-draw R5) must preserve it.
	slots := round0Slots(b)
	topHalf := slots[0] + "|" + slots[1]
	bottomHalf := slots[2] + "|" + slots[3]
	for _, letter := range []string{"A", "B", "C"} {
		assert.Truef(t, strings.Contains(topHalf, letter+"1") != strings.Contains(bottomHalf, letter+"1"),
			"pool %s's winner must appear in exactly one half", letter)
		assert.Truef(t, strings.Contains(topHalf, letter+"2") != strings.Contains(bottomHalf, letter+"2"),
			"pool %s's runner-up must appear in exactly one half", letter)
		assert.Truef(t, strings.Contains(topHalf, letter+"1") != strings.Contains(topHalf, letter+"2"),
			"pool %s's two qualifiers must be in opposite halves", letter)
	}
}

// TestResolveQualifiedPools_FivePoolsTwoWinners_ByePools extends the unbalanced
// coverage to 5 pools x 2 qualifiers (10 finishers in a 16-slot bracket) with
// real names. It exists because the 3-pool case could be read as "the earliest
// pools bye"; here they do not.
//
// CURRENT BEHAVIOUR, DEFECTS PINNED:
//   - the byes go to pools C and D, chosen by nothing but where the balanced
//     split left an odd leaf (bc-draw R6 replaces this with seed/pool-size
//     precedence);
//   - the draw contains two round-1 matches with BOTH sides empty, an artefact
//     of padding 10 finishers into 16 slots. They are auto-completed with no
//     winner, and the round-2 match above each is effectively another bye.
func TestResolveQualifiedPools_FivePoolsTwoWinners_ByePools(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "unbalanced-5x2"

	pools, participants, results := unbalancedPools(5)
	saveMixedScaffold(t, store, compID, pools, 2)
	require.NoError(t, store.SaveParticipants(compID, participants))
	require.NoError(t, store.SavePoolMatches(compID, results))

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.False(t, bracketHasPoolPlaceholders(b))

	assert.Equal(t, []string{
		"A1|B2",
		"|", // both sides bye: padding artefact, no match is ever played here
		"C1|",
		"E1|D2",
		"B1|A2",
		"|", // both sides bye
		"D1|",
		"C2|E2",
	}, round0Slots(b), "5-pool x 2-qualifier slot mapping changed")

	// Only pools C and D get a bye - not the first two pools, and nothing about
	// pool size or seeding was consulted.
	var byeHolders []string
	for _, m := range b.Rounds[0] {
		switch {
		case m.SideA != "" && m.SideB == "":
			byeHolders = append(byeHolders, m.SideA)
		case m.SideA == "" && m.SideB != "":
			byeHolders = append(byeHolders, m.SideB)
		}
	}
	assert.Equal(t, []string{"C1", "D1"}, byeHolders,
		"byes land on the pool C and pool D winners purely by tree position")

	// The empty round-1 matches are completed with no winner at all.
	for _, idx := range []int{1, 5} {
		m := b.Rounds[0][idx]
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "empty slot %d must be auto-completed", idx)
		assert.Empty(t, m.Winner, "empty slot %d has no winner to record", idx)
	}
}
