package export

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestBuildResultsWorkbook_TeamBracketSubMatchScoresLandInCorrectCells is the
// regression test for writeTeamSubMatchScores' BRACKET call site
// (overlayTeamBracketScores, builder.go), which every other test in this
// package reaches only by calling writeTeamSubMatchScores directly (see
// TestWriteTeamSubMatchScores_OutOfRangePositionSkipped and
// TestTeamNameFallback_SheetAndStandingsDivergeOnANamedRow). That call site
// hands the callee two adjacent ints (courtStartCol, headerExcelRow+3) and
// two adjacent strings (bm.SideA, bm.SideB); a transposition of either pair
// compiles clean and silently misplaces or misattributes the sub-bout marks,
// and nothing exercising the function directly would ever notice.
//
// This drives the REAL export path (BuildResultsWorkbook ->
// overlayBracketScores -> overlayTeamBracketScores) with a team bracket
// match carrying two kinds of SubResults row:
//   - a FIXED-ORDER row (no fighter names) whose mark can only be attributed
//     via the encounter's own team names, i.e. via matchSideA/matchSideB as
//     threaded from bm.SideA/bm.SideB -- pinning that argument's order;
//   - a NAMED row, which pins the documented row formula "H + 2 + Position"
//     from overlayTeamBracketScores' own doc comment.
func TestBuildResultsWorkbook_TeamBracketSubMatchScoresLandInCorrectCells(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "team"
	comp.TeamSize = 3
	comp.Format = state.CompFormatMixed
	require.NoError(t, store.SaveCompetition(comp))

	pools := makeTeamPools()
	require.NoError(t, store.SavePools(compID, pools))

	// Pool phase: Red A and Red B each win their pool 2-1 on individual
	// victories, so they meet in the single bracket final below. Mirrors the
	// known-good pool setup from TestBuildResultsWorkbook_TeamResults.
	poolResults := []state.MatchResult{
		{
			ID: "Pool A-0", SideA: "Red A", SideAID: "Red A", SideB: "Blue A", SideBID: "Blue A",
			Status: state.MatchStatusCompleted, Winner: "Red A", WinnerID: "Red A",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Red A", SideB: "Blue A", IpponsA: []string{"M", "K"}, Winner: "Red A"},
				{Position: 2, SideA: "Red A", SideB: "Blue A", IpponsB: []string{"M"}, Winner: "Blue A"},
				{Position: 3, SideA: "Red A", SideB: "Blue A", IpponsA: []string{"D"}, Winner: "Red A"},
			},
		},
		{
			ID: "Pool B-0", SideA: "Red B", SideAID: "Red B", SideB: "Blue B", SideBID: "Blue B",
			Status: state.MatchStatusCompleted, Winner: "Red B", WinnerID: "Red B",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Red B", SideB: "Blue B", IpponsA: []string{"M"}, Winner: "Red B"},
				{Position: 2, SideA: "Red B", SideB: "Blue B", IpponsA: []string{"K"}, Winner: "Red B"},
				{Position: 3, SideA: "Red B", SideB: "Blue B", IpponsB: []string{"M"}, Winner: "Blue B"},
			},
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, poolResults))

	// The team elimination final: Red A (bm.SideA) beats Red B (bm.SideB).
	// Position 1 is a FIXED-ORDER bout (no fighter names): its mark can only
	// be attributed via matchSideA/matchSideB as threaded from bm.SideA/
	// bm.SideB at the call site under test. Position 3 is a NAMED bout whose
	// ippon letters must land at the documented row H+2+3 = H+5. Position 2
	// is deliberately absent so the row between the two populated ones stays
	// empty, catching a stray write from a wrong offset.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "B1", SideA: "Red A", SideB: "Red B", Winner: "Red A",
					Status: state.MatchStatusCompleted, MatchNumber: 1,
					Decision: "fought",
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "", SideB: "", Winner: "Red A", Decision: "fusensho"},
						{Position: 3, SideA: "Kenji", SideB: "Taro", IpponsA: []string{"M", "K"}, Winner: "Kenji"},
					},
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Locate the "Round _ - Match 1" header the real skeleton rendered for
	// this final. The header's own position is what the call site's
	// headerExcelRow/courtStartCol derive from, so discovering it by
	// scanning the rendered sheet (rather than assuming a row/column)
	// keeps this test independent of the production code it is checking.
	headerRow, headerCol := -1, -1
	for r, row := range rows {
		for c, cellVal := range row {
			if parseRoundMatchLabel(cellVal) == 1 {
				headerRow, headerCol = r, c
			}
		}
		if headerRow >= 0 {
			break
		}
	}
	require.GreaterOrEqual(t, headerRow, 0, "Elimination Matches sheet must contain the 'Round _ - Match 1' header")

	headerExcelRow := headerRow + 1 // 1-based, matches overlayTeamBracketScores' H
	courtStartCol := headerCol + 1  // 1-based
	lCol := colNum(courtStartCol + 1)
	rCol := colNum(courtStartCol + 5)

	cellAt := func(col string, row int) string {
		v, cellErr := f.GetCellValue(helper.SheetEliminationMatches, fmt.Sprintf("%s%d", col, row))
		require.NoError(t, cellErr)
		return v
	}

	// Position 1 (fixed-order): row H+2+1 = H+3. Red A (bm.SideA) won by
	// fusensho, so the LEFT column carries the default-win maru + Fus. mark
	// and the RIGHT column carries neither. Swapping bm.SideA/bm.SideB at
	// the call site flips this onto the right column instead.
	pos1Row := headerExcelRow + 3
	left1 := cellAt(lCol, pos1Row)
	right1 := cellAt(rCol, pos1Row)
	assert.Contains(t, left1, "Fus.", "Red A (bm.SideA) is the encounter's actual winner and sits in the LEFT column")
	assert.Contains(t, left1, "○", "and carries the default-win maru")
	assert.Empty(t, right1, "the losing side's column must carry neither mark nor maru")

	// Position 3 (named row): row H+2+3 = H+5, ippon letters "MK" verbatim.
	// A wrong row derivation (e.g. an off-by-one at the call site) leaves
	// this cell empty and plants "MK" one row away instead.
	pos3Row := headerExcelRow + 5
	assert.Equal(t, "MK", cellAt(lCol, pos3Row), "position 3's ippon letters must land at H+2+Position")

	// The row between the two populated sub-bouts (position 2, absent from
	// SubResults) must stay untouched by either write.
	assert.Empty(t, cellAt(lCol, headerExcelRow+4), "no stray content between the two populated sub-bout rows")
}
