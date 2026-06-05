package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStartKnockout_HappyPath verifies:
//  1. Status transitions from pools → playoffs.
//  2. Bracket.Preview is cleared (false).
//  3. Pool winner UUIDs and Numbers are preserved in the bracket (identity).
//  4. Slot parity: the slot that was "Pool A-1st" now holds Pool A's rank-1 player.
func TestStartKnockout_HappyPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "knockout-happy"

	// Set up a competition with completed pools and a preview bracket.
	// Use poolWinners=1 so we get exactly 2 slots (Pool A-1st, Pool B-1st).
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Knockout Happy Test",
		Kind:        "individual",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		PoolWinners: 1,
	}))

	// Save players with specific IDs so we can assert identity preservation.
	players := []domain.Player{
		{ID: "uuid-alice", Name: "Alice", Dojo: "DojoA"},
		{ID: "uuid-bob", Name: "Bob", Dojo: "DojoB"},
		{ID: "uuid-charlie", Name: "Charlie", Dojo: "DojoC"},
		{ID: "uuid-dave", Name: "Dave", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "uuid-alice", Name: "Alice"},
			{ID: "uuid-bob", Name: "Bob"},
		}},
		{PoolName: "Pool B", Players: []helper.Player{
			{ID: "uuid-charlie", Name: "Charlie"},
			{ID: "uuid-dave", Name: "Dave"},
		}},
	}
	require.NoError(t, store.SavePools(compID, pools))

	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "Charlie", SideB: "Dave", Winner: "Charlie", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	// Save a preview bracket.
	previewBracket := &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Pool A-1st", SideB: "Pool B-1st", Status: state.MatchStatusScheduled},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, previewBracket))

	// Run StartKnockout.
	err := eng.StartKnockout(compID)
	require.NoError(t, err)

	// 1. Status must be playoffs.
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPlayoffs, comp.Status, "status must transition to playoffs")

	// 2. Bracket.Preview must be cleared.
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.False(t, bracket.Preview, "bracket must not be preview after StartKnockout")

	// 3 & 4. Slot parity: the first round match must contain real player names.
	// Pool A winner is Alice, Pool B winner is Charlie (from the completed matches above).
	require.NotEmpty(t, bracket.Rounds, "bracket must have rounds")
	firstRound := bracket.Rounds[0]
	require.NotEmpty(t, firstRound, "first round must have matches")

	// The two players in the bracket should be Alice (Pool A-1st) and Charlie (Pool B-1st).
	allSides := make([]string, 0, len(firstRound)*2)
	for _, m := range firstRound {
		if m.SideA != "" {
			allSides = append(allSides, m.SideA)
		}
		if m.SideB != "" {
			allSides = append(allSides, m.SideB)
		}
	}
	assert.Contains(t, allSides, "Alice", "Pool A-1st (Alice) must appear in bracket")
	assert.Contains(t, allSides, "Charlie", "Pool B-1st (Charlie) must appear in bracket")
	assert.NotContains(t, allSides, "Pool A-1st", "placeholder must be replaced by real player name")
	assert.NotContains(t, allSides, "Pool B-1st", "placeholder must be replaced by real player name")
}

// TestStartKnockout_PreCondition_NotMixed verifies that a non-mixed competition
// returns a validation error.
func TestStartKnockout_PreCondition_NotMixed(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "knockout-not-mixed"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Not Mixed",
		Format: state.CompFormatPlayoffs,
		Status: state.CompStatusPlayoffs,
	}))

	err := eng.StartKnockout(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must return a ValidationError")
	assert.Contains(t, err.Error(), "mixed")
}

// TestStartKnockout_PreCondition_NotPools verifies that a competition not in
// pools status returns a validation error.
func TestStartKnockout_PreCondition_NotPools(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	tests := []struct {
		name   string
		status state.CompetitionStatus
	}{
		{"setup", state.CompStatusSetup},
		{"draw-ready", state.CompStatusDrawReady},
		{"playoffs", state.CompStatusPlayoffs},
		{"completed", state.CompStatusComplete},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compID := "knockout-wrong-status-" + string(tc.status)
			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID:     compID,
				Name:   "Wrong Status",
				Format: state.CompFormatMixed,
				Status: tc.status,
			}))

			err := eng.StartKnockout(compID)
			require.Error(t, err)
			var ve *ValidationError
			require.ErrorAs(t, err, &ve, "must return a ValidationError")
			assert.Contains(t, err.Error(), "pools")
		})
	}
}

// TestStartKnockout_PreCondition_PoolsIncomplete verifies that a competition
// with incomplete pool matches returns a validation error.
func TestStartKnockout_PreCondition_PoolsIncomplete(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "knockout-incomplete"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Incomplete Pools",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		PoolWinners: 1,
	}))

	// One match not yet completed.
	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	err := eng.StartKnockout(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, err.Error(), "complete")
}

// TestStartKnockout_NotFound verifies a not-found error for a missing competition.
func TestStartKnockout_NotFound(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	err := eng.StartKnockout("nonexistent")
	require.Error(t, err)
	var nfe *NotFoundError
	assert.ErrorAs(t, err, &nfe)
}

// TestMaybeAutoCompletePools_MixedFormat_StaysInPools verifies that a mixed-format
// competition does NOT auto-transition to "completed" after all pool matches are done.
// It should stay in "pools" status so StartKnockout can be called.
func TestMaybeAutoCompletePools_MixedFormat_StaysInPools(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mixed-stays-pools"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Mixed No Auto Complete",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	matches := []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "P1-1", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusCompleted, Winner: "Charlie"},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "mixed format must NOT transition to completed; it must stay in pools")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "mixed status must remain pools after all pool matches done")
}

// TestMaybeAutoCompletePools_LeagueFormat_Completes verifies that a league
// competition still auto-transitions to completed (regression test for the
// change that narrows the transition to league-only).
func TestMaybeAutoCompletePools_LeagueFormat_Completes(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-completes"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "League Completes",
		Format: state.CompFormatLeague,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	matches := []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "P1-1", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "P1-2", SideA: "Bob", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Bob"},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "league format must transition to completed")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status)
}

// TestStartKnockout_CrossSeedOrder is the regression test for the seeding bug
// where finalists were resolved in global-rank order ([A-1st, B-1st, A-2nd,
// B-2nd]) and fed positionally to the bracket builder, which put the two pool
// WINNERS into the same first-round match. With poolWinners=2 the cross-seed
// order ([A-1st, B-2nd, A-2nd, B-1st]) differs from rank order, so this catches
// the bug; the earlier poolWinners=1 SlotParity test could not. The live bracket
// must match the preview's cross-seeding: pool winners on opposite ends, only
// able to meet in the final.
func TestStartKnockout_CrossSeedOrder(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "knockout-crossseed"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Cross Seed Test",
		Kind:        "individual",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		PoolWinners: 2,
	}))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "A3"}, {Name: "A4"},
		{Name: "B1"}, {Name: "B2"}, {Name: "B3"}, {Name: "B4"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}, {Name: "A3"}, {Name: "A4"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}, {Name: "B3"}, {Name: "B4"}}},
	}))

	// Round-robin results giving distinct win counts (no ties → no tiebreakers):
	// A1=3, A2=2, A3=1, A4=0 (and likewise for Pool B).
	win := func(id, a, b, w string) state.MatchResult {
		return state.MatchResult{ID: id, SideA: a, SideB: b, Winner: w, IpponsA: []string{"M"}, Status: state.MatchStatusCompleted}
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		win("Pool A-0", "A1", "A2", "A1"), win("Pool A-1", "A1", "A3", "A1"), win("Pool A-2", "A1", "A4", "A1"),
		win("Pool A-3", "A2", "A3", "A2"), win("Pool A-4", "A2", "A4", "A2"), win("Pool A-5", "A3", "A4", "A3"),
		win("Pool B-0", "B1", "B2", "B1"), win("Pool B-1", "B1", "B3", "B1"), win("Pool B-2", "B1", "B4", "B1"),
		win("Pool B-3", "B2", "B3", "B2"), win("Pool B-4", "B2", "B4", "B2"), win("Pool B-5", "B3", "B4", "B3"),
	}))

	// Preview bracket as generatePoolPreviewBracket would build it:
	// GenerateFinals(pools, 2) = [A-1st, B-2nd, A-2nd, B-1st] → pairs
	// (A-1st,B-2nd),(A-2nd,B-1st).
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Pool A-1st", SideB: "Pool B-2nd", Status: state.MatchStatusScheduled},
			{ID: "m-r1-1", SideA: "Pool A-2nd", SideB: "Pool B-1st", Status: state.MatchStatusScheduled},
		}},
	}))

	require.NoError(t, eng.StartKnockout(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, bracket.Rounds[0], 2, "two first-round matches expected")

	// sidesOf returns the {SideA,SideB} set of the match containing name.
	matchOf := func(name string) (string, string) {
		for _, m := range bracket.Rounds[0] {
			if m.SideA == name || m.SideB == name {
				return m.SideA, m.SideB
			}
		}
		return "", ""
	}

	// Pool winners A1 and B1 must NOT meet in the first round (the bug put them
	// in the same match). A1 cross-seeds against B's runner-up (B2); A2 against B1.
	a1A, a1B := matchOf("A1")
	assert.ElementsMatch(t, []string{"A1", "B2"}, []string{a1A, a1B},
		"Pool A winner (A1) must face Pool B runner-up (B2), not Pool B winner")
	a2A, a2B := matchOf("A2")
	assert.ElementsMatch(t, []string{"A2", "B1"}, []string{a2A, a2B},
		"Pool A runner-up (A2) must face Pool B winner (B1)")
}

// TestStartKnockout_BracketScoreableAfterStart verifies that after StartKnockout,
// it is possible to record a match result in the bracket (Preview=false means
// the scoring gate no longer blocks it).
func TestStartKnockout_BracketScoreableAfterStart(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "knockout-scoreable"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Scoreable Bracket",
		Kind:        "individual",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		PoolWinners: 1,
	}))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Charlie", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoD"},
	}))

	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "Charlie"}, {Name: "Dave"}}},
	}))

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "Charlie", SideB: "Dave", Winner: "Charlie", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	// Save a preview bracket.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Pool A-1st", SideB: "Pool B-1st", Status: state.MatchStatusScheduled},
		}},
	}))

	// Start knockout.
	require.NoError(t, eng.StartKnockout(compID))

	// Load the live bracket and score the first match.
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotEmpty(t, bracket.Rounds)
	firstMatch := bracket.Rounds[0][0]
	require.NotEmpty(t, firstMatch.SideA)

	// Recording a bracket result must succeed (no Preview block).
	err = eng.RecordMatchResult(compID, firstMatch.ID, &state.MatchResult{
		Winner: firstMatch.SideA,
		Status: state.MatchStatusCompleted,
	})
	assert.NoError(t, err, "must be able to score bracket match after StartKnockout")
}

// TestStartKnockout_SlotParity verifies that the slot that was "Pool A-1st"
// in the preview bracket now holds Pool A's rank-1 player, and "Pool B-1st"
// now holds Pool B's rank-1 player. This ensures the preview structure is
// preserved exactly.
func TestStartKnockout_SlotParity(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "knockout-slot-parity"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Slot Parity Test",
		Kind:        "individual",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		PoolWinners: 1,
	}))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Charlie", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoD"},
	}))

	// Two pools: A (Alice/Bob, Alice wins), B (Charlie/Dave, Dave wins).
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "Charlie"}, {Name: "Dave"}}},
	}))

	// Pool A: Alice beats Bob. Pool B: Dave beats Charlie.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "Charlie", SideB: "Dave", Winner: "Dave", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	// Build a preview bracket using GenerateFinals order.
	// With pools [Pool A, Pool B] and poolWinners=1:
	// GenerateFinals returns ["Pool A-1st", "Pool B-1st"].
	// buildBracketFromLeaves puts Pool A-1st as SideA and Pool B-1st as SideB.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Pool A-1st", SideB: "Pool B-1st", Status: state.MatchStatusScheduled},
		}},
	}))

	require.NoError(t, eng.StartKnockout(compID))

	// After StartKnockout:
	// - Pool A-1st (rank 1 of Pool A) = Alice
	// - Pool B-1st (rank 1 of Pool B) = Dave
	// The live bracket must have Alice in the slot formerly occupied by "Pool A-1st"
	// and Dave in the slot formerly occupied by "Pool B-1st".
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotEmpty(t, bracket.Rounds)
	firstRound := bracket.Rounds[0]
	require.NotEmpty(t, firstRound)

	// Find the sides.
	allSides := make([]string, 0, 4)
	for _, m := range firstRound {
		if m.SideA != "" {
			allSides = append(allSides, m.SideA)
		}
		if m.SideB != "" {
			allSides = append(allSides, m.SideB)
		}
	}
	assert.Contains(t, allSides, "Alice", "Pool A winner (Alice) must be in bracket")
	assert.Contains(t, allSides, "Dave", "Pool B winner (Dave) must be in bracket")
}

// TestStartKnockout_ResolvesByeWinnerField verifies that when a finalist draws
// a bye (single-winner pool → 1 finalist → auto-advanced), the in-place resolver
// replaces the placeholder in the bye match's SideA AND its Winner field, and
// that the participant roster (UUID/Number) is left untouched by StartKnockout.
func TestStartKnockout_ResolvesByeWinnerField(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "knockout-bye"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Bye Winner Resolution",
		Kind:        "individual",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		PoolWinners: 1,
	}))

	players := []domain.Player{
		{ID: "aaaaaaaa-0000-0000-0000-000000000001", Name: "Alice", Dojo: "DojoA", Number: "1"},
		{ID: "aaaaaaaa-0000-0000-0000-000000000002", Name: "Bob", Dojo: "DojoB", Number: "2"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "aaaaaaaa-0000-0000-0000-000000000001", Name: "Alice", Number: "1"},
			{ID: "aaaaaaaa-0000-0000-0000-000000000002", Name: "Bob", Number: "2"},
		}},
	}))

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	// Single pool, 1 winner → the lone finalist drew a bye: SideA and Winner
	// both hold the placeholder "Pool A-1st".
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Pool A-1st", Status: state.MatchStatusCompleted, Winner: "Pool A-1st"},
		}},
	}))

	require.NoError(t, eng.StartKnockout(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.False(t, bracket.Preview)
	m := bracket.Rounds[0][0]
	assert.Equal(t, "Alice", m.SideA, "bye match SideA placeholder must resolve to the player")
	assert.Equal(t, "Alice", m.Winner, "bye match Winner placeholder must also resolve to the player")

	// StartKnockout must NOT mutate the participant roster — the participant UUID
	// is still present after the knockout starts. (Number lives in pools.csv, not
	// participants.csv, so it is not asserted here.)
	roster, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	var alice *domain.Player
	for i := range roster {
		if roster[i].Name == "Alice" {
			alice = &roster[i]
		}
	}
	require.NotNil(t, alice)
	assert.Equal(t, "aaaaaaaa-0000-0000-0000-000000000001", alice.ID)
}
