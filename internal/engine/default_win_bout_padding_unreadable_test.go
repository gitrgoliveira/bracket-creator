package engine

// bc-tmfn/bc-cse follow-up: the write-time default-win padding gate in
// RecordMatchResultWithIneligibilityTx (scoring_tx.go) must never destroy an
// unreadable stored SubResults cell (state.MatchResult.SubResultsRaw / the
// DATA LOSS this file's first test guards), and must still fire for the
// ordinary case where the client's payload omits SideA/SideB and relies on
// applyPoolWrite's reconcileSides to backfill them from the stored row (the
// gate is read BEFORE that backfill runs, so it must consult the STORED
// row's sides, not the payload's).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mangleCellForTest corrupts a byte sequence in a file on disk, mirroring
// state's own (unexported, package-scoped) mangleCell test helper.
func mangleCellForTest(t *testing.T, path, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	mangled := strings.Replace(string(raw), from, to, 1)
	require.NotEqual(t, string(raw), mangled, "the mangle pattern must actually match")
	require.NoError(t, os.WriteFile(path, []byte(mangled), 0600)) // #nosec G306
}

// TestWriteTimeDefaultWinPaddingNeverOverwritesAnUnreadableCell guards the
// write-time half of the bc-tmfn data-loss finding: a kiken recorded (via
// the real engine.RecordDecision entry point) against a team match whose
// stored SubResults cell already failed to parse must not pad over it. The
// legacy-load half of the same bug is pinned separately in
// internal/state/default_win_bout_padding_unreadable_test.go.
func TestWriteTimeDefaultWinPaddingNeverOverwritesAnUnreadableCell(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "dwb-unreadable-write"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Unreadable Cell Write Path",
		Format: state.CompFormatLeague, Status: state.CompStatusPools,
		Courts: []string{"A"}, Kind: "team", TeamSize: 3,
	}))
	match := state.MatchResult{
		ID: "Pool A-0", SideA: "Kenshikan", SideB: "Sanshukan", Court: "A",
		Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "Tanaka", SideB: "Suzuki", IpponsA: []string{"M"}, Winner: "Tanaka"},
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{match}))

	path := filepath.Join(dir, "competitions", compID, "pool-matches.csv")
	mangleCellForTest(t, path, `""position"":1`, `""position"":1x`)

	// Confirm the cell reads back unreadable BEFORE the write under test,
	// so the assertions below are about what the write does, not the load.
	preLoad, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.True(t, preLoad[0].SubResultsUnreadable)

	// Kenshikan (aka = SideA) withdraws. RecordDecision fills SideA/SideB
	// from the stored row itself, so this exercises the SubResultsUnreadable
	// guard in isolation from the sides-backfill-ordering fix below.
	_, _, err = eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "aka", "no-show", nil, false)
	require.NoError(t, err)

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Empty(t, loaded[0].SubResults,
		"an unreadable cell must not be padded by the write-time gate")
	assert.True(t, loaded[0].SubResultsUnreadable, "and must still say so")

	raw, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Tanaka",
		"the malformed cell's bytes must survive the kiken write")
	assert.Contains(t, string(raw), `""position"":1x`,
		"and must survive VERBATIM, not partially rewritten by padding")
}

// TestWriteTimeDefaultWinPaddingReadsPriorSidesBeforeBackfill guards the
// bc-cse ordering finding: the padding gate runs BEFORE
// applyPoolWrite/applyBracketResultIn's reconcileSides backfills an omitted
// SideA/SideB from the stored row, so a client payload that (as is common)
// states only the decision and leaves SideA/SideB blank must still get its
// default-win bout padding -- the gate has to read the STORED row's sides,
// not the payload's.
func TestWriteTimeDefaultWinPaddingReadsPriorSidesBeforeBackfill(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "dwb-sides-backfill"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Sides Backfill Ordering",
		Format: state.CompFormatLeague, Status: state.CompStatusPools,
		Courts: []string{"A"}, Kind: "team", TeamSize: 3,
	}))
	match := state.MatchResult{
		ID: "Pool A-0", SideA: "Kenshikan", SideB: "Sanshukan", Court: "A",
		Status: state.MatchStatusRunning,
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{match}))

	// Prime EnsureLegacyUpgraded for this competition BEFORE the write under
	// test, so the assertions below observe the WRITE-TIME gate in
	// isolation: EnsureLegacyUpgraded runs at most once per competition per
	// Store instance, and its last step (upgradeTeamDefaultWinBoutPaddingLocked)
	// would otherwise silently repair an unpadded row on the post-write
	// LoadPoolMatches call, masking a write-time regression behind the
	// legacy pass's own (separately tested) padding.
	_, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	// SideA/SideB deliberately omitted: this is the shape a score-endpoint
	// payload that only states the decision takes.
	result := &state.MatchResult{
		ID:         "Pool A-0",
		Decision:   "kiken-voluntary",
		Status:     state.MatchStatusCompleted,
		Winner:     "Sanshukan",
		WinnerSide: "B",
	}
	_, err = eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", result)
	require.NoError(t, err)

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Kenshikan", loaded[0].SideA, "sides backfilled from the stored row")
	assert.Equal(t, "Sanshukan", loaded[0].SideB)
	assert.Len(t, loaded[0].SubResults, 3,
		"padding must still apply even though the payload omitted SideA/SideB")
}

// TestWriteTimeDefaultWinPaddingPadsAnExplicitHikiwakeCorrectionOverAWithdrawal
// guards the advisor-caught regression in the padDecision computation:
// KeepsWithdrawalRuling accepts an incoming Decision of "" OR "hikiwake" (its
// own definition), not only "". A correction that explicitly re-states the
// encounter as "hikiwake" over a stored withdrawal must still be recognised
// as one that keeps the ruling and get its default-win padding -- gating the
// KeepsWithdrawalRuling check on padDecision == "" would miss exactly this
// shape, since padDecision is "hikiwake" (not a default-win decision) before
// the check ever runs.
func TestWriteTimeDefaultWinPaddingPadsAnExplicitHikiwakeCorrectionOverAWithdrawal(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "dwb-hikiwake-correction"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Hikiwake Correction Over Withdrawal",
		Format: state.CompFormatLeague, Status: state.CompStatusPools,
		Courts: []string{"A"}, Kind: "team", TeamSize: 3,
	}))
	match := state.MatchResult{
		ID: "Pool A-0", SideA: "Kenshikan", SideB: "Sanshukan", Court: "A",
		Status: state.MatchStatusRunning,
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{match}))
	_, err := store.LoadPoolMatches(compID) // prime EnsureLegacyUpgraded, as above
	require.NoError(t, err)

	// Kenshikan (aka = SideA) withdraws before any bout is fought; padded to
	// 3 placeholder positions credited to Sanshukan.
	_, _, err = eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "aka", "no-show", nil, false)
	require.NoError(t, err)

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Len(t, loaded[0].SubResults, 3, "padded by the withdrawal itself")

	// A bout-level CORRECTION explicitly re-states the encounter as a draw
	// (KeepsWithdrawalRuling's other accepted shape besides "") and sends
	// only ONE of the three positions, as a genuine bout-level edit would.
	// preserveWithdrawalRuling reinstates the stored kiken-voluntary AFTER
	// the padding gate runs, so the gate must recognise this write as one
	// that keeps the ruling and still pad the two rows it omits.
	correction := &state.MatchResult{
		ID:       "Pool A-0",
		Decision: "hikiwake",
		Status:   state.MatchStatusCompleted,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "Tanaka", SideB: "Suzuki"},
		},
		CorrectionReason: "typo in bout 1 names",
	}
	_, err = eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", correction)
	require.NoError(t, err)

	after, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.Equal(t, "kiken-voluntary", after[0].Decision,
		"the withdrawal ruling is reinstated over the correction's explicit hikiwake")
	assert.Len(t, after[0].SubResults, 3,
		"the correction sent only 1 of 3 positions; padding must still fill the rest")
}
