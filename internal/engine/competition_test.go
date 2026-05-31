package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCourtsEqual exercises every branch of the nil/empty/equal/unequal
// logic. Both nil and empty slice must compare as equal ("no courts"
// from the config's point of view).
func TestCourtsEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []string{}, []string{}, true},
		{"nil vs empty", nil, []string{}, true},
		{"empty vs nil", []string{}, nil, true},
		{"equal single", []string{"A"}, []string{"A"}, true},
		{"equal multiple", []string{"A", "B", "C"}, []string{"A", "B", "C"}, true},
		{"different lengths", []string{"A", "B"}, []string{"A"}, false},
		{"different values", []string{"A", "B"}, []string{"A", "C"}, false},
		{"one nil one single", nil, []string{"A"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, courtsEqual(tc.a, tc.b))
		})
	}
}

// TestMaybeAutoCompletePools_AllComplete verifies that calling
// MaybeAutoCompletePools on a pools competition whose every match is
// completed transitions Status to "completed" and returns AutoCompleteTransitioned.
func TestMaybeAutoCompletePools_AllComplete(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-all"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Auto Complete Test",
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
	assert.Equal(t, AutoCompleteTransitioned, outcome, "all matches completed should trigger status transition")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status)
}

// TestMaybeAutoCompletePools_OnePending verifies that a single
// scheduled/running match prevents the auto-complete transition.
func TestMaybeAutoCompletePools_OnePending(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-pending"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Pending Test",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	matches := []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "P1-1", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "a pending match must prevent auto-complete")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "status should remain Pools")
}

// TestMaybeAutoCompletePools_NonPoolsFormat verifies no-op for
// competitions that are not in the Pools format.
func TestMaybeAutoCompletePools_NonPoolsFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-playoffs"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Playoffs Test",
		Format: state.CompFormatPlayoffs,
		Status: state.CompStatusPlayoffs,
		Courts: []string{"A"},
	}))
	// No pool matches on disk — all pool match loads return empty.

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	// No matches at all → outer fast-path sees "all complete" (vacuously true),
	// but the inner transform guard rejects CompFormatPlayoffs → no transition.
	assert.Equal(t, AutoCompleteNoChange, outcome, "non-pools format must not trigger auto-complete")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPlayoffs, comp.Status)
}

// TestMaybeAutoCompletePools_AlreadyComplete verifies idempotency:
// calling the function on an already-completed competition must return
// AutoCompleteNoChange, not error.
func TestMaybeAutoCompletePools_AlreadyComplete(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-idempotent"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Already Done",
		Format: state.CompFormatMixed,
		Status: state.CompStatusComplete,
		Courts: []string{"A"},
	}))

	matches := []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "already-completed competition must return AutoCompleteNoChange")
}

// TestMaybeAutoCompletePools_LeagueFormat verifies that a league-format
// competition with all matches completed transitions to CompStatusComplete,
// the same as a pools-format competition.
func TestMaybeAutoCompletePools_LeagueFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-league"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "League Auto Complete",
		Format: state.CompFormatLeague,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	matches := []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "P1-1", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Charlie"},
		{ID: "P1-2", SideA: "Bob", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Bob"},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "league format with all matches done must transition to complete")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status)
}

// TestStartCompetition_SwissFormat pins the Swiss start flow:
// status must move to "pools", SwissCurrentRound must be 1,
// Round 1 matches must be written to pool-matches.csv, and no
// bracket.json must be created.
func TestStartCompetition_SwissFormat(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "swiss-start"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Swiss Start Test",
		Kind:        "individual",
		Format:      state.CompFormatSwiss,
		SwissRounds: 3,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		Status:      state.CompStatusSetup,
	}))
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	require.NoError(t, eng.StartCompetition(compID))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "Swiss start must set status to pools, not playoffs")
	assert.Equal(t, 1, comp.SwissCurrentRound, "Swiss start must set SwissCurrentRound to 1")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, matches, "Round 1 matches must be written to pool-matches.csv on start")
	for _, m := range matches {
		assert.True(t, strings.HasPrefix(m.ID, "Swiss-R1-"),
			"Round 1 match IDs must carry Swiss-R1- prefix, got %s", m.ID)
	}

	_, statErr := os.Stat(filepath.Join(dir, "competitions", compID, "bracket.json"))
	assert.True(t, os.IsNotExist(statErr), "bracket.json must not be created for Swiss start")
}

// TestStartCompetition_SwissRoundAlreadyGenerated verifies that StartCompetition
// rejects the call when SwissCurrentRound != 0. This guards against
// AdvanceSwissRound having partially run before start (matches written,
// round bumped) and StartCompetition silently overwriting them.
func TestStartCompetition_SwissRoundAlreadyGenerated(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "swiss-start-guard"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                compID,
		Name:              "Swiss Guard Test",
		Kind:              "individual",
		Format:            state.CompFormatSwiss,
		SwissRounds:       3,
		Courts:            []string{"A"},
		StartTime:         "09:00",
		Status:            state.CompStatusSetup,
		SwissCurrentRound: 1, // simulates AdvanceSwissRound having already run
	}))
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave",
	})

	err := eng.StartCompetition(compID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already generated")
}

// TestStartCompetition_SwissMatchesOnDiskRoundZero_Scored verifies Guard 2:
// pool-matches.csv has scored entries (non-scheduled status) and
// SwissCurrentRound==0 — StartCompetition must reject to avoid data loss.
// This covers AdvanceSwissRound having partially run (wrote matches, scored
// a match, but the round-bump UpdateCompetitionChanged failed).
func TestStartCompetition_SwissMatchesOnDiskRoundZero_Scored(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "swiss-start-guard-scored"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Swiss CSV Guard Test (scored)",
		Kind:        "individual",
		Format:      state.CompFormatSwiss,
		SwissRounds: 3,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		Status:      state.CompStatusSetup,
		// SwissCurrentRound deliberately left at 0 — simulates the bump failing
	}))
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave",
	})
	// Pre-write matches where one has been scored (completed). Guard 2 must
	// reject here because overwriting would silently discard that result.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Swiss-R1-0", Status: state.MatchStatusCompleted},
		{ID: "Swiss-R1-1", Status: state.MatchStatusScheduled},
	}))

	err := eng.StartCompetition(compID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scored Swiss matches")
}

// TestStartCompetition_SwissMatchesOnDiskRoundZero_AllScheduled verifies that
// Guard 2 allows retry when all pool-matches.csv entries are still scheduled
// (no scoring has occurred). This covers StartCompetition itself having
// partially run — it writes round-1 matches then fails inside
// UpdateCompetitionChanged. The operator must be able to retry without first
// manually cleaning up the CSV.
func TestStartCompetition_SwissMatchesOnDiskRoundZero_AllScheduled(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "swiss-start-retry-scheduled"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Swiss Retry Test (all scheduled)",
		Kind:        "individual",
		Format:      state.CompFormatSwiss,
		SwissRounds: 3,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		Status:      state.CompStatusSetup,
	}))
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave",
	})
	// Pre-write purely scheduled matches — simulates a prior StartCompetition
	// that wrote round-1 matches then failed at UpdateCompetitionChanged.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Swiss-R1-0", Status: state.MatchStatusScheduled},
		{ID: "Swiss-R1-1", Status: state.MatchStatusScheduled},
	}))

	// Retry must succeed (regenerates/overwrites unscored matches).
	require.NoError(t, eng.StartCompetition(compID))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status)
	assert.Equal(t, 1, comp.SwissCurrentRound)
}

// TestGenerateDraw_PoolsFormat verifies that GenerateDraw transitions a pools
// competition from Setup to DrawReady and writes pools/pool-matches artifacts.
func TestGenerateDraw_PoolsFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "generate-draw-pools"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})

	require.NoError(t, eng.GenerateDraw(compID))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusDrawReady, comp.Status, "GenerateDraw must set status to draw-ready")

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, pools, "pools must be written on GenerateDraw")

	poolMatches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, poolMatches, "pool-matches must be written on GenerateDraw")
}

// TestGenerateDraw_PlayoffsFormat verifies GenerateDraw on a playoffs
// competition writes bracket.json and sets draw-ready.
func TestGenerateDraw_PlayoffsFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "generate-draw-playoffs"

	createTestCompetition(t, store, compID, state.CompFormatPlayoffs, 0)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})

	require.NoError(t, eng.GenerateDraw(compID))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusDrawReady, comp.Status)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.NotNil(t, bracket, "bracket must be written on GenerateDraw for playoffs")
}

// TestGenerateDraw_MixedFormat_WritesPreviewBracket verifies that GenerateDraw
// on a mixed (Pools + Knockout) competition also writes a PREVIEW bracket whose
// leaves are pool-origin placeholders (mp-9dz). The operator sees the knockout
// structure that the pools feed, mirroring the Excel Tree sheet, before the
// separate Playoffs competition is created.
func TestGenerateDraw_MixedFormat_WritesPreviewBracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "generate-draw-mixed-preview"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})

	require.NoError(t, eng.GenerateDraw(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket, "mixed GenerateDraw must write a preview bracket")
	assert.True(t, bracket.Preview, "mixed bracket must be flagged as a preview")
	require.NotEmpty(t, bracket.Rounds, "preview bracket must have rounds")

	// Every first-round non-bye leaf must be a pool-origin placeholder
	// ("Pool A 1st" / "Pool A-1st"), never a real participant name.
	sawPoolLabel := false
	for _, side := range []string{bracket.Rounds[0][0].SideA, bracket.Rounds[0][0].SideB} {
		if side != "" {
			assert.Contains(t, side, "Pool", "preview leaf must reference a pool, got %q", side)
			sawPoolLabel = true
		}
	}
	assert.True(t, sawPoolLabel, "expected at least one pool-origin leaf in round 1")

	// Real participant names must NOT leak into the preview leaves.
	for _, r := range bracket.Rounds {
		for _, m := range r {
			assert.NotContains(t, m.SideA, "Alice", "preview leaf must not contain a resolved player name")
			assert.NotContains(t, m.SideB, "Alice", "preview leaf must not contain a resolved player name")
		}
	}
}

// TestGenerateDraw_LeagueFormat_NoPreviewBracket ensures league (no knockout
// stage) does NOT get a preview bracket.
func TestGenerateDraw_LeagueFormat_NoPreviewBracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "generate-draw-league-nobracket"

	createTestCompetition(t, store, compID, state.CompFormatLeague, 0)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})

	require.NoError(t, eng.GenerateDraw(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	// LoadBracket returns nil or an empty bracket when no file exists.
	if bracket != nil {
		assert.Empty(t, bracket.Rounds, "league must not generate a preview bracket")
	}
}

// TestGenerateDraw_RejectsDrawReady ensures GenerateDraw returns an error
// when the competition is already in draw-ready state.
func TestGenerateDraw_RejectsDrawReady(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "generate-draw-already-ready"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})

	require.NoError(t, eng.GenerateDraw(compID))

	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	var ve *ValidationError
	assert.ErrorAs(t, err, &ve, "second GenerateDraw must return a ValidationError")
}

// TestDiscardDraw verifies that DiscardDraw resets status to Setup and
// removes draw artifacts.
func TestDiscardDraw_ResetsToSetup(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "discard-draw"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})

	require.NoError(t, eng.GenerateDraw(compID))
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusDrawReady, comp.Status)

	require.NoError(t, eng.DiscardDraw(compID))

	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "DiscardDraw must reset status to setup")
	assert.Equal(t, 0, comp.SwissCurrentRound, "DiscardDraw must reset SwissCurrentRound to 0")

	// Draw artifacts should be deleted.
	compDir := filepath.Join(dir, "competitions", compID)
	for _, f := range []string{"pools.csv", "pool-matches.csv", "bracket.json"} {
		_, ferr := os.Stat(filepath.Join(compDir, f))
		assert.True(t, os.IsNotExist(ferr), "%s must be deleted after DiscardDraw", f)
	}
}

// TestDiscardDraw_RejectsNonDrawReady ensures DiscardDraw errors when not in
// draw-ready state.
func TestDiscardDraw_RejectsNonDrawReady(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "discard-draw-guard"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})

	err := eng.DiscardDraw(compID)
	require.Error(t, err)
	var ve *ValidationError
	assert.ErrorAs(t, err, &ve, "DiscardDraw on setup competition must return ValidationError")
}

// TestStartCompetition_FromDrawReady verifies that StartCompetition on a
// draw-ready competition transitions to running without regenerating the draw.
func TestStartCompetition_FromDrawReady(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "start-from-draw-ready"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})

	require.NoError(t, eng.GenerateDraw(compID))
	comp, _ := store.LoadCompetition(compID)
	require.Equal(t, state.CompStatusDrawReady, comp.Status)

	require.NoError(t, eng.StartCompetition(compID))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "StartCompetition from DrawReady must set status to pools")
}

// TestStartCompetition_BackwardCompatSetup verifies that StartCompetition
// still works directly from Setup (one-click path, no explicit GenerateDraw).
func TestStartCompetition_BackwardCompatSetup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "start-backward-compat"

	createTestCompetition(t, store, compID, state.CompFormatPlayoffs, 0)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})

	require.NoError(t, eng.StartCompetition(compID))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPlayoffs, comp.Status, "StartCompetition from Setup must set status to playoffs for playoffs format")
}

// TestGenerateDraw_ThenDiscardThenRegenerateAndStart exercises the full
// preview workflow: generate, discard, regenerate, then start.
func TestGenerateDraw_ThenDiscardThenRegenerateAndStart(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "full-preview-flow"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})

	require.NoError(t, eng.GenerateDraw(compID))
	require.NoError(t, eng.DiscardDraw(compID))
	require.NoError(t, eng.GenerateDraw(compID))
	require.NoError(t, eng.StartCompetition(compID))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status)
}
