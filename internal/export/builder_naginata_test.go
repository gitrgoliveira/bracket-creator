package export

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

// TestBuildResultsWorkbook_NaginataThirdPlaceRendered verifies that a scored
// naginata bronze match appears as a "3rd Place" block on the Elimination
// Matches sheet with the winner's ippon letter in the score row.
func TestBuildResultsWorkbook_NaginataThirdPlaceRendered(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataKnockout(t, store, compID, false)
	bracket := startNaginataWith4Players(t, store, eng, compID, false)

	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2, "expected 2 semifinals for 4 players")

	// Score both SFs.
	require.NoError(t, eng.RecordMatchResult(compID, sf[0].ID, &state.MatchResult{
		Winner:  sf[0].SideA,
		IpponsA: []string{"M"},
		Status:  state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, sf[1].ID, &state.MatchResult{
		Winner:  sf[1].SideB,
		IpponsB: []string{"K"},
		Status:  state.MatchStatusCompleted,
	}))

	// Reload bracket to get the bronze sides populated by the engine.
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bronzeWinner := bracket.ThirdPlaceMatch.SideA

	// Score bronze with "D" ippon (distinctive; SFs used "M" and "K").
	require.NoError(t, eng.RecordMatchResult(compID, "m-bronze", &state.MatchResult{
		Winner:  bronzeWinner,
		IpponsA: []string{"D"},
		Status:  state.MatchStatusCompleted,
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	assert.True(t, sheetContainsCell(rows, helper.ThirdPlaceLabel),
		"Elimination Matches sheet must have a '3rd Place' header block for naginata")
	assert.True(t, sheetContainsCell(rows, "D"),
		"Elimination Matches sheet must show the bronze score 'D' (kendo ippon, only in bronze)")

	// Also assert that both semifinal losers' names appear in the bronze score row.
	assertBronzeEntrantsPopulated(t, rows)
}

// TestBuildResultsWorkbook_NaginataThirdPlaceNamesBeforeBronze verifies that
// the bronze entrant names appear in the 3rd Place score row even when the
// bronze match has not yet been played (semis scored, bronze open).
func TestBuildResultsWorkbook_NaginataThirdPlaceNamesBeforeBronze(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataKnockout(t, store, compID, false)
	bracket := startNaginataWith4Players(t, store, eng, compID, false)

	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2, "expected 2 semifinals for 4 players")

	// Score both SFs so the engine populates ThirdPlaceMatch.SideA/SideB.
	require.NoError(t, eng.RecordMatchResult(compID, sf[0].ID, &state.MatchResult{
		Winner:  sf[0].SideA,
		IpponsA: []string{"M"},
		Status:  state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, sf[1].ID, &state.MatchResult{
		Winner:  sf[1].SideB,
		IpponsB: []string{"K"},
		Status:  state.MatchStatusCompleted,
	}))

	// Verify the engine populated the bronze sides before export.
	b2, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotEmpty(t, b2.ThirdPlaceMatch.SideA, "engine must populate ThirdPlaceMatch.SideA after SFs")
	require.NotEmpty(t, b2.ThirdPlaceMatch.SideB, "engine must populate ThirdPlaceMatch.SideB after SFs")

	// Export WITHOUT scoring bronze.
	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Names must appear even though the bronze has not been played.
	assertBronzeEntrantsPopulated(t, rows)

	// Score cells (lVCol = col B / index 1, rVCol = col F / index 5) must be empty.
	thirdPlaceRow := bctest.FindCellRow(rows, helper.ThirdPlaceLabel)
	scoreRowIdx := thirdPlaceRow + 2
	scoreRow := rows[scoreRowIdx]
	var leftScore, rightScore string
	if len(scoreRow) > 1 {
		leftScore = scoreRow[1]
	}
	if len(scoreRow) > 5 {
		rightScore = scoreRow[5]
	}
	assert.Empty(t, leftScore, "bronze score cell (col B) must be empty before bronze is played")
	assert.Empty(t, rightScore, "bronze score cell (col F) must be empty before bronze is played")
}

// TestBuildResultsWorkbook_EngiNaginataThirdPlaceFlags verifies that an engi
// naginata bronze match renders on the Elimination Matches sheet with flag counts.
func TestBuildResultsWorkbook_EngiNaginataThirdPlaceFlags(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataKnockout(t, store, compID, true)
	bracket := startNaginataWith4Players(t, store, eng, compID, true)

	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2)

	// Score both SFs via the engi (flag) path.
	_, err := eng.RecordMatchResultWithIneligibility(compID, sf[0].ID, &state.MatchResult{
		FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)
	_, err = eng.RecordMatchResultWithIneligibility(compID, sf[1].ID, &state.MatchResult{
		FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)

	// Score bronze 5-0 (distinctive flag counts not used in the SFs).
	_, err = eng.RecordMatchResultWithIneligibility(compID, "m-bronze", &state.MatchResult{
		FlagsA: 5, FlagsB: 0, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	assert.True(t, sheetContainsCell(rows, helper.ThirdPlaceLabel),
		"Elimination Matches sheet must have a '3rd Place' header for engi naginata")

	// Find the "3rd Place" row and verify the score row (header+2) carries "5".
	thirdPlaceRow := bctest.FindCellRow(rows, helper.ThirdPlaceLabel)
	require.GreaterOrEqual(t, thirdPlaceRow, 0, "'3rd Place' header row must be found")
	scoreRowIdx := thirdPlaceRow + 2
	require.Less(t, scoreRowIdx, len(rows), "bronze score row (header+2) must exist")
	assert.True(t, slices.Contains(rows[scoreRowIdx], "5"),
		"bronze score row must contain '5' (FlagsA=5 winner count)")

	// Also assert entrant names appear in the bronze score row.
	assertBronzeEntrantsPopulated(t, rows)
}

// TestBuildResultsWorkbook_NonNaginataNoThirdPlace verifies that a standard
// kendo (non-naginata) knockout export does NOT emit a "3rd Place" block,
// preserving byte-identical output for non-naginata competitions.
func TestBuildResultsWorkbook_NonNaginataNoThirdPlace(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Kendo knockout (Naginata=false).
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = state.CompFormatKnockout
	comp.Naginata = false
	comp.Kind = "individual"
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 2
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Charlie", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	assert.False(t, sheetContainsCell(rows, helper.ThirdPlaceLabel),
		"non-naginata (kendo) export must NOT contain a '3rd Place' block")
}

// TestBuildResultsWorkbook_NaginataThirdPlaceEntrantFormulas verifies that the
// bronze block's entrant cells carry CONCATENATE formulas referencing the "2."
// (loser) lines of the two semifinals. This covers the workbook state BEFORE any
// matches are scored: the overlay writes no literals, so the formula skeleton must
// self-document who belongs in the bronze.
func TestBuildResultsWorkbook_NaginataThirdPlaceEntrantFormulas(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataKnockout(t, store, compID, false)
	// startNaginataWith4Players starts the competition without scoring any matches.
	// ThirdPlaceMatch exists but SideA/SideB are empty, so overlayBracketScores
	// writes no literal names and the skeleton formulas remain visible.
	startNaginataWith4Players(t, store, eng, compID, false)

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Locate the "3rd Place" header row (0-based index).
	thirdPlaceRowIdx := bctest.FindCellRow(rows, helper.ThirdPlaceLabel)
	require.GreaterOrEqual(t, thirdPlaceRowIdx, 0, "'3rd Place' header must be present")

	// Score row: 1-based Excel row = (0-based idx + 1) + 2.
	scoreExcelRow := thirdPlaceRowIdx + 3

	leftFormula, err := f.GetCellFormula(helper.SheetEliminationMatches, fmt.Sprintf("A%d", scoreExcelRow))
	require.NoError(t, err)
	rightFormula, err := f.GetCellFormula(helper.SheetEliminationMatches, fmt.Sprintf("G%d", scoreExcelRow))
	require.NoError(t, err)

	// Both cells must hold CONCATENATE formulas referencing the two semifinal
	// losers. For a 4-player bracket the semis are M 1 (upper) and M 2
	// (lower); the upper semi's loser is the bronze's SideA (Aka), so it sits
	// on the RIGHT and the lower semi's loser on the LEFT.
	assert.Contains(t, leftFormula, "CONCATENATE",
		"bronze entrant cells must carry CONCATENATE formulas (no scoring yet, no literal names)")
	assert.Contains(t, rightFormula, "CONCATENATE",
		"bronze entrant cells must carry CONCATENATE formulas (no scoring yet, no literal names)")
	assert.Contains(t, leftFormula, `"M 2 "`,
		"the left (Shiro) bronze entrant is the loser of semifinal M 2")
	assert.Contains(t, rightFormula, `"M 1 "`,
		"the right (Aka) bronze entrant is the loser of semifinal M 1")
}

// TestBuildResultsWorkbook_NaginataThirdPlacePrintAreaCoversBlock verifies that
// after BuildResultsWorkbook renders a naginata 4-player bracket (with a bronze
// block), the _xlnm.Print_Area defined name for the Elimination Matches sheet
// includes at least the row where the "3rd Place" header appears. Before the fix
// the print area was set before the bronze block, leaving the bronze block outside
// the print area and invisible when printing or PDF-exporting.
func TestBuildResultsWorkbook_NaginataThirdPlacePrintAreaCoversBlock(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataKnockout(t, store, compID, false)
	startNaginataWith4Players(t, store, eng, compID, false)

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Locate the "3rd Place" header row (1-based Excel row).
	thirdPlaceExcelRow := bctest.FindCellRow(rows, helper.ThirdPlaceLabel) + 1
	require.GreaterOrEqual(t, thirdPlaceExcelRow, 1,
		"'3rd Place' header row must be present in Elimination Matches")

	printAreaLastRow := findPrintAreaLastRow(f, helper.SheetEliminationMatches)
	require.Greater(t, printAreaLastRow, 0,
		"_xlnm.Print_Area for Elimination Matches must exist and be parseable")
	assert.GreaterOrEqual(t, printAreaLastRow, thirdPlaceExcelRow,
		"Print_Area last row (%d) must cover at least the '3rd Place' header row (%d); bronze block falls outside the print area",
		printAreaLastRow, thirdPlaceExcelRow)
}
