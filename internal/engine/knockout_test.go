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

// TestStartKnockout_UUIDAndNumberPreserved verifies that when GetPoolRanking
// returns a player with a UUID and Number, those values are preserved in the
// resolved roster (identity preservation flag = true).
func TestStartKnockout_UUIDAndNumberPreserved(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "knockout-identity"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Identity Preservation",
		Kind:        "individual",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		PoolWinners: 1,
	}))

	// Participants carry real UUIDs and numbers.
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

	// Single pool needs only 1 winner; PoolWinners=1 → 1 slot.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Pool A-1st", Status: state.MatchStatusCompleted, Winner: "Pool A-1st"},
		}},
	}))

	// resolvePoolWinnersFromSource with preserveIdentity=true should carry through UUID/Number.
	resolved, err := eng.resolvePoolWinnersFromSource(compID, true)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "Alice", resolved[0].Name)
	// UUID and Number must be preserved.
	assert.Equal(t, "aaaaaaaa-0000-0000-0000-000000000001", resolved[0].ID)
	assert.Equal(t, "1", resolved[0].Number)
}

// TestLegacySeparatePlayoffsStillWorks verifies that the legacy
// StartCompetition playoffs path (with SourceCompID) still functions for
// existing separate playoffs competitions after the refactor.
func TestLegacySeparatePlayoffsStillWorks(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "src-legacy"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     srcID,
		Name:   "Legacy Source",
		Format: state.CompFormatMixed,
		Status: state.CompStatusComplete,
	}))
	require.NoError(t, store.SaveParticipants(srcID, []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
	}))
	require.NoError(t, store.SavePools(srcID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	require.NoError(t, store.SavePoolMatches(srcID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	// Create a legacy separate playoffs competition.
	playoffID := "legacy-playoffs"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           playoffID,
		Name:         "Legacy - Playoffs",
		Format:       state.CompFormatPlayoffs,
		SourceCompID: srcID,
		Courts:       []string{"A"},
		StartTime:    "09:00",
	}))

	// StartCompetition must still work for the legacy path.
	require.NoError(t, eng.StartCompetition(playoffID))

	comp, err := store.LoadCompetition(playoffID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPlayoffs, comp.Status)

	bracket, err := store.LoadBracket(playoffID)
	require.NoError(t, err)
	assert.NotEmpty(t, bracket.Rounds)
}
