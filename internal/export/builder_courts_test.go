package export

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func TestBuildResultsWorkbook_TwoCourts(t *testing.T) {
	// Multi-court competition: verifies that pools are correctly spread across
	// courts and the score overlay addresses the right column bands.
	t.Parallel()
	dir, err := os.MkdirTemp("", "export-test-2courts-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)

	compID := "two-court-comp"
	comp := &state.Competition{
		ID:     compID,
		Name:   "Two Courts",
		Courts: []string{"A", "B"},
	}
	require.NoError(t, store.SaveCompetition(comp))

	// Four pools across two courts.
	pools := []helper.Pool{
		make2PlayerPool("Pool A", "Alice", "Bob"),
		make2PlayerPool("Pool B", "Charlie", "Dave"),
		make2PlayerPool("Pool C", "Eve", "Frank"),
		make2PlayerPool("Pool D", "Grace", "Hank"),
	}
	require.NoError(t, store.SavePools(compID, pools))

	// Record a win in Pool C (second court, first pool).
	results := []state.MatchResult{
		{
			ID:       "Pool C-0",
			SideA:    "Eve",
			SideAID:  "Eve",
			SideB:    "Frank",
			SideBID:  "Frank",
			IpponsA:  []string{"M"},
			IpponsB:  []string{},
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Eve",
			WinnerID: "Eve",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// At least one row must contain "M" (Eve's ippon in Pool C, court B).
	foundM := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "M" {
				foundM = true
				break
			}
		}
		if foundM {
			break
		}
	}
	assert.True(t, foundM, "Pool C ippon 'M' must be present in two-court workbook")
}

// TestBuildResultsWorkbook_MultiCourtStandingsColumns is the regression test for
// the multi-court standings column-map bug: overlayPoolStandings built its
// header map from the WHOLE row, and buildColumnMap keeps only the first
// occurrence of each label. Pool Matches repeats the W/L/T/PW/PL/Rank headers
// once per court band, so a win in a court-B pool used to be written into court
// A's W column. Only Pool C (court B) is scored; court A's W column must stay
// clean while court B's shows the win.
func TestBuildResultsWorkbook_MultiCourtStandingsColumns(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "export-test-mcstand-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)

	compID := "mc-standings"
	comp := &state.Competition{ID: compID, Name: "MC Standings", Courts: []string{"A", "B"}}
	require.NoError(t, store.SaveCompetition(comp))

	// Pools 0,1 -> court A; pools 2,3 -> court B (contiguous assignment).
	pools := []helper.Pool{
		make2PlayerPool("Pool A", "Alice", "Bob"),
		make2PlayerPool("Pool B", "Charlie", "Dave"),
		make2PlayerPool("Pool C", "Eve", "Frank"),
		make2PlayerPool("Pool D", "Grace", "Hank"),
	}
	require.NoError(t, store.SavePools(compID, pools))

	// Score ONLY Pool C (court B): Eve wins, so Eve's standings W = 1.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool C-0", SideA: "Eve", SideAID: "Eve", SideB: "Frank", SideBID: "Frank", IpponsA: []string{"M"}, Decision: "fought", Status: state.MatchStatusCompleted, Winner: "Eve", WinnerID: "Eve"},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	courtAW := headerColInBand(rows, "W", 0, helper.CourtsColumnsPerCourt)
	courtBW := headerColInBand(rows, "W", helper.CourtsColumnsPerCourt, 2*helper.CourtsColumnsPerCourt)
	require.GreaterOrEqual(t, courtAW, 0, "court A W header must exist")
	require.GreaterOrEqual(t, courtBW, 0, "court B W header must exist")

	assert.True(t, columnContains(rows, courtBW, "1"),
		"court B's W column must carry Eve's win (=1)")
	assert.False(t, columnContains(rows, courtAW, "1"),
		"court A pools are unscored: a '1' in court A's W column means court B's win leaked into court A (the multi-court colMap bug)")
}

// TestBuildResultsWorkbook_BracketTwoCourts guards the multi-court bracket
// overlay: courts are laid out side-by-side, so two "Round N - Match N" headers
// can share a row at different column bands. The overlay must fill BOTH, not
// just the left-most court's match (Copilot review of PR #336).
func TestBuildResultsWorkbook_BracketTwoCourts(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Courts = []string{"A", "B"}
	comp.PoolWinners = 1
	comp.Format = state.CompFormatMixed
	require.NoError(t, store.SaveCompetition(comp))

	// 4 pools (2 per court) → 4 finalists → a semifinal round of 2 matches, one
	// per court, rendered side-by-side on the same rows.
	pools := []helper.Pool{
		make2PlayerPool("Pool A", "Alice", "Bob"),
		make2PlayerPool("Pool B", "Charlie", "Dave"),
		make2PlayerPool("Pool C", "Eve", "Frank"),
		make2PlayerPool("Pool D", "Grace", "Hank"),
	}
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	// Two completed semifinals with distinct scores: match 1 (left court) "MK",
	// match 2 (right court) "DT". Both must appear if every header on the row is
	// processed.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "sf1", SideA: "Alice", SideB: "Charlie", Winner: "Alice", Status: state.MatchStatusCompleted, IpponsA: []string{"M", "K"}, Decision: "fought", MatchNumber: 1},
				{ID: "sf2", SideA: "Eve", SideB: "Grace", Winner: "Eve", Status: state.MatchStatusCompleted, IpponsA: []string{"D", "T"}, Decision: "fought", MatchNumber: 2},
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
	assert.True(t, sheetContainsCell(rows, "MK"), "left-court semifinal score 'MK' must be overlaid")
	assert.True(t, sheetContainsCell(rows, "DT"), "right-court semifinal score 'DT' must be overlaid (multi-court)")
}

// TestBuildResultsWorkbook_ClampedShiaijoBands is the results-workbook half of
// the export banding bug (the stored-draw half lives in
// internal/engine/excel_draw_parity_test.go).
//
// THREE pools on a FOUR-shiaijo allocation is the clamped regime: three pools
// cannot carry four shiaijo, so helper.EffectiveDrawCourts(3, 4) steps the draw
// down to 2 and the live app runs pools A+B on shiaijo A and pool C on shiaijo
// B. Banding the sheets on the raw 4 instead spread the three pools one per
// court over A, B and C and emitted a fourth, completely EMPTY "Shiaijo D" band.
//
// The score placement is the sharper half of the assertion. The overlays
// take their column bands from helper.PoolsByCourt ->
// AssignPoolsToCourts, so they must be handed the SAME court count as
// PrintPoolMatches; a value that disagrees with the skeleton writes every score
// and standing into the wrong cells. Pool C's ippon is therefore checked in the
// court-B band by column, not merely found somewhere on the sheet.
func TestBuildResultsWorkbook_ClampedShiaijoBands(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "export-test-clamped-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)

	const allocatedCourts = 4
	pools := []helper.Pool{
		make2PlayerPool("Pool A", "Alice", "Bob"),
		make2PlayerPool("Pool B", "Charlie", "Dave"),
		make2PlayerPool("Pool C", "Eve", "Frank"),
	}
	// The regime this test exists for; without the clamp firing the assertions
	// below would pass vacuously.
	require.Equalf(t, 2, helper.EffectiveDrawCourts(len(pools), allocatedCourts),
		"%d pools on %d shiaijo must clamp to 2", len(pools), allocatedCourts)

	compID := "clamped-bands-comp"
	courts := make([]string, allocatedCourts)
	for i := range courts {
		courts[i] = helper.CourtLabel(i)
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Clamped bands",
		Courts: courts,
	}))
	require.NoError(t, store.SavePools(compID, pools))

	// One completed match per pool, each with a distinct ippon letter so the
	// score's landing column identifies the pool that produced it.
	ipponOfPool := map[string]string{"Pool A": "M", "Pool B": "K", "Pool C": "T"}
	var results []state.MatchResult
	for _, p := range pools {
		results = append(results, state.MatchResult{
			ID:       p.PoolName + "-0",
			SideA:    p.Players[0].Name,
			SideB:    p.Players[1].Name,
			SideAID:  p.Players[0].ID,
			SideBID:  p.Players[1].ID,
			IpponsA:  []string{ipponOfPool[p.PoolName]},
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   p.Players[0].Name,
		})
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	assert.Equal(t, []string{"A", "B"}, readCourtBandLetters(t, rows, helper.SheetPoolMatches),
		"%s must be banded for the shiaijo the pools actually run on", helper.SheetPoolMatches)

	// writeScoreRowCells puts SideA's (Red, right) score at the band's start
	// column + 5. Band c starts at 0-based column c*CourtsColumnsPerCourt.
	scoreCol := func(court int) int { return court*helper.CourtsColumnsPerCourt + 5 }
	assert.Truef(t, columnContains(rows, scoreCol(0), "M"),
		"Pool A's ippon must land in the shiaijo A band (column %d)", scoreCol(0)+1)
	assert.Truef(t, columnContains(rows, scoreCol(0), "K"),
		"Pool B's ippon must land in the shiaijo A band (column %d)", scoreCol(0)+1)
	assert.Truef(t, columnContains(rows, scoreCol(1), "T"),
		"Pool C's ippon must land in the shiaijo B band (column %d), the shiaijo the app runs it on", scoreCol(1)+1)
	assert.Falsef(t, columnContains(rows, scoreCol(2), "T"),
		"Pool C's ippon must NOT land in a shiaijo C band (column %d): the draw has only two shiaijo", scoreCol(2)+1)
}
