package engine

import (
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
	assert.NotContains(t, sides, "B1", "Pool B not finished — must not be seeded yet")

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
// the knockout — changing the finisher order — the new finisher overwrites the
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
			assert.False(t, poolFinalistPlaceholderRE.MatchString(m.SideA), "SideA placeholder leaked: %q", m.SideA)
			assert.False(t, poolFinalistPlaceholderRE.MatchString(m.SideB), "SideB placeholder leaked: %q", m.SideB)
			assert.False(t, poolFinalistPlaceholderRE.MatchString(m.Winner), "Winner placeholder leaked: %q", m.Winner)
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
// resolved — replacing the old bracket-wide Preview gate.
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
// playoffs competition is scoreable from draw time — its round-1 leaves are real
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
	// The final is NOT playable yet — its sides are "Winner of …" feeders.
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
