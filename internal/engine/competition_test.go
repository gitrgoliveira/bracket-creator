package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
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

// --- ReplaceParticipantInDraw tests ---

// setupDrawReadyMixed creates a draw-ready mixed-format competition with the
// given participants and returns the engine, store, and compID.
func setupDrawReadyMixed(t *testing.T, names []string) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	compID := "replace-test"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)

	players := make([]domain.Player, len(names))
	for i, n := range names {
		players[i] = domain.Player{Name: n, Dojo: fmt.Sprintf("Dojo%d", i)}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.GenerateDraw(compID))
	return eng, store, compID
}

// setupDrawReadyPlayoffs creates a draw-ready playoffs competition.
func setupDrawReadyPlayoffs(t *testing.T, names []string) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	compID := "replace-playoffs"
	createTestCompetition(t, store, compID, state.CompFormatPlayoffs, 0)

	players := make([]domain.Player, len(names))
	for i, n := range names {
		players[i] = domain.Player{Name: n, Dojo: fmt.Sprintf("Dojo%d", i)}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.GenerateDraw(compID))
	return eng, store, compID
}

// findPlayerInPools returns true when name appears in any pool.
func findPlayerInPools(pools []helper.Pool, name string) bool {
	for _, p := range pools {
		for _, pl := range p.Players {
			if pl.Name == name {
				return true
			}
		}
	}
	return false
}

// findNameInBracket returns true when name appears in any bracket side.
func findNameInBracket(bracket *state.Bracket, name string) bool {
	for _, round := range bracket.Rounds {
		for _, m := range round {
			if m.SideA == name || m.SideB == name {
				return true
			}
		}
	}
	return false
}

func TestReplaceParticipantInDraw_PoolsHappyPath(t *testing.T) {
	eng, store, compID := setupDrawReadyMixed(t, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	// Find Alice's pool entry before the swap.
	poolsBefore, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.True(t, findPlayerInPools(poolsBefore, "Alice"), "Alice must be in pools before swap")

	warnings, err := eng.ReplaceParticipantInDraw(compID, "Alice", "DojoA", "", "Alicia", "DojoA", "")
	require.NoError(t, err)

	// Dojo unchanged, no conflict expected.
	assert.Empty(t, warnings)

	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.False(t, findPlayerInPools(poolsAfter, "Alice"), "Alice must be removed from pools after swap")
	assert.True(t, findPlayerInPools(poolsAfter, "Alicia"), "Alicia must appear in pools after swap")

	// pool-matches.csv should be updated too.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		assert.NotEqual(t, "Alice", m.SideA, "old name must not appear in pool matches")
		assert.NotEqual(t, "Alice", m.SideB, "old name must not appear in pool matches")
	}
}

func TestReplaceParticipantInDraw_PlayoffsBracket(t *testing.T) {
	eng, store, compID := setupDrawReadyPlayoffs(t, []string{"Alice", "Bob", "Charlie", "Dave"})

	bracketBefore, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.True(t, findNameInBracket(bracketBefore, "Alice"), "Alice must be in bracket before swap")

	warnings, err := eng.ReplaceParticipantInDraw(compID, "Alice", "DojoA", "", "Alicia", "DojoA", "")
	require.NoError(t, err)
	assert.Empty(t, warnings)

	bracketAfter, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.False(t, findNameInBracket(bracketAfter, "Alice"), "old name must not appear in bracket after swap")
	assert.True(t, findNameInBracket(bracketAfter, "Alicia"), "new name must appear in bracket after swap")
}

func TestReplaceParticipantInDraw_DojoConflict(t *testing.T) {
	// Create 6 players where pool-size=3 so we get 2 pools of 3.
	// Alice and Bob both come from DojoX; the generator will try to keep them
	// apart. We then replace Charlie (different dojo) with a DojoX player,
	// which may create a conflict in one pool.
	eng, store, _ := setupTestEngine(t)
	compID := "replace-dojo-conflict"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoX"},
		{Name: "Bob", Dojo: "DojoX"},
		{Name: "Charlie", Dojo: "DojoY"},
		{Name: "Dave", Dojo: "DojoZ"},
		{Name: "Eve", Dojo: "DojoZ"},
		{Name: "Frank", Dojo: "DojoW"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.GenerateDraw(compID))

	// Find which pool Charlie is in; replace Charlie with a DojoX player.
	poolsBefore, err := store.LoadPools(compID)
	require.NoError(t, err)

	charliesPool := ""
	for _, p := range poolsBefore {
		for _, pl := range p.Players {
			if pl.Name == "Charlie" {
				charliesPool = p.PoolName
			}
		}
	}
	require.NotEmpty(t, charliesPool)

	// Check if Alice or Bob is in the same pool as Charlie.
	// If so, swapping Charlie to DojoX will create a conflict.
	aliceOrBobInSamePool := false
	for _, p := range poolsBefore {
		if p.PoolName != charliesPool {
			continue
		}
		for _, pl := range p.Players {
			if pl.Name == "Alice" || pl.Name == "Bob" {
				aliceOrBobInSamePool = true
			}
		}
	}

	// Swap Charlie (DojoY) → Grace (DojoX).
	warnings, err := eng.ReplaceParticipantInDraw(compID, "Charlie", "DojoY", "", "Grace", "DojoX", "")
	require.NoError(t, err)

	if aliceOrBobInSamePool {
		// We expect a dojo conflict warning.
		assert.NotEmpty(t, warnings, "dojo conflict warning expected when DojoX already appears in pool")
		for _, w := range warnings {
			assert.Contains(t, w, "dojo conflict")
		}
	}

	// Regardless of conflict, the swap must succeed.
	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.False(t, findPlayerInPools(poolsAfter, "Charlie"), "old name must be gone")
	assert.True(t, findPlayerInPools(poolsAfter, "Grace"), "new name must be present")
}

func TestReplaceParticipantInDraw_SeedsUntouched(t *testing.T) {
	eng, store, compID := setupDrawReadyMixed(t, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	// Seed Alice at rank 1. In the real flow, state.UpdateParticipant
	// renames seeds.csv before ReplaceParticipantInDraw runs, so the
	// engine function must NOT touch seeds — verify it leaves them as-is.
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1},
	}))

	warnings, err := eng.ReplaceParticipantInDraw(compID, "Alice", "DojoA", "", "Alicia", "DojoA", "")
	require.NoError(t, err)
	assert.Empty(t, warnings, "no seed warnings — seed rename is handled by UpdateParticipant")

	// seeds.csv must be unchanged (still "Alice") because the engine
	// function does not touch seeds.
	seeds, err := store.LoadSeeds(compID)
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Alice", seeds[0].Name, "engine must not rename seeds — that's UpdateParticipant's job")
}

func TestReplaceParticipantInDraw_WrongState(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "replace-wrong-state"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))

	// Competition is now in pools state, not draw-ready.
	_, err := eng.ReplaceParticipantInDraw(compID, "Alice", "DojoA", "", "Alicia", "DojoA", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in draw-ready state")
}

func TestReplaceParticipantInDraw_ParticipantNotInDraw(t *testing.T) {
	eng, _, compID := setupDrawReadyMixed(t, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	_, err := eng.ReplaceParticipantInDraw(compID, "Nonexistent", "DojoX", "", "Someone", "DojoX", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in draw artifacts")
}

func TestReplaceParticipantInDraw_NoopWhenUnchanged(t *testing.T) {
	eng, store, compID := setupDrawReadyMixed(t, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	poolsBefore, err := store.LoadPools(compID)
	require.NoError(t, err)

	// Same name, same dojo, same displayName → no-op.
	warnings, err := eng.ReplaceParticipantInDraw(compID, "Alice", "DojoA", "", "Alice", "DojoA", "")
	require.NoError(t, err)
	assert.Empty(t, warnings)

	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.Equal(t, poolsBefore, poolsAfter, "pools must be unchanged on no-op")
}
