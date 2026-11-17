package export

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestBuildResultsWorkbook_TeamKnockoutNames is the regression test for the team-
// knockout summary-name off-by-one in overlayKnockoutBracketNames. A no-pools team
// knockout repeats the entrant-name formulas on the summary row at
// header+4+teamSize; the overlay must overwrite THAT row (clearing the broken ”!
// formula) and leave the "Victories / Points" label at header+5+teamSize intact.
// Before the fix it targeted header+5+teamSize, leaving a broken formula behind and
// clobbering the label. This path (Knockout + TeamSize>0, no pools) had no coverage.
func TestBuildResultsWorkbook_TeamKnockoutNames(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "team"
	comp.TeamSize = 3
	comp.Format = state.CompFormatKnockout
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Team A", Dojo: "D"}, {Name: "Team B", Dojo: "D"},
		{Name: "Team C", Dojo: "D"}, {Name: "Team D", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, br)
	// Score the first round's encounters with 3 sub-bouts each.
	for mi := range br.Rounds[0] {
		m := &br.Rounds[0][mi]
		if m.SideA == "" || m.SideB == "" {
			continue
		}
		m.Winner = m.SideA
		m.Status = state.MatchStatusCompleted
		m.SubResults = []state.SubMatchResult{
			{Position: 1, SideA: m.SideA, SideB: m.SideB, IpponsA: []string{"M", "K"}, Winner: m.SideA},
			{Position: 2, SideA: m.SideA, SideB: m.SideB, IpponsB: []string{"M"}, Winner: m.SideB},
			{Position: 3, SideA: m.SideA, SideB: m.SideB, IpponsA: []string{"D"}, Winner: m.SideA},
		}
	}
	require.NoError(t, store.SaveBracket(compID, br))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// The off-by-one left a broken ''! entrant-name formula on the summary row.
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	// The "Victories / Points" label sits one row below the summary-name row; the
	// off-by-one clobbered it with a team name.
	assert.True(t, sheetContainsCell(rows, "Victories / Points"),
		"the team summary 'Victories / Points' label must survive the name overlay")
	assert.True(t, sheetContainsCell(rows, "Team A"),
		"a team entrant name must be written literally into the bracket")
}

// TestBuildResultsWorkbook_TeamResults exercises the team overlays end-to-end:
// team pool sub-match scores, the two-pass team standings (W/L/T + IV/IL/IT/PW/PL),
// and a daihyosen-decided team elimination encounter. Assertions target specific
// cells so an off-by-one in the row math (not just a collapsed formula) fails.
func TestBuildResultsWorkbook_TeamResults(t *testing.T) {
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

	// Red A beats Blue A on individual victories (2-1); Red B beats Blue B (2-1).
	results := []state.MatchResult{
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
	require.NoError(t, store.SavePoolMatches(compID, results))

	// Team elimination: Red A (Aka) beats Red B (Shiro) 2-1 on individual
	// victories, 3-1 on points. The daihyosen placeholder/decision is kept
	// only to pin the "(DH)" middle mark below; it does not need a real tie
	// (BuildResultsWorkbook renders whatever is stored, it does not
	// re-validate the tie rule that would normally trigger one), and an
	// unequal IV/PW split is what makes the left/right assertions below
	// actually distinguish Shiro's column from Aka's.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "B1", SideA: "Red A", SideB: "Red B", Winner: "Red A",
					Status: state.MatchStatusCompleted, MatchNumber: 1,
					Decision: string(domain.DecisionDaihyosen),
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "Red A", SideB: "Red B", IpponsA: []string{"M", "K"}, Winner: "Red A"},
						{Position: 2, SideA: "Red A", SideB: "Red B", IpponsA: []string{"M"}, Winner: "Red A"},
						{Position: 3, SideA: "Red A", SideB: "Red B", IpponsB: []string{"M"}, Winner: "Red B"},
						{Position: -1, Decision: string(domain.DecisionDaihyosen)},
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

	// ---- Pool Matches: team standings + sub-match scores ----
	poolRows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// Locate the first "Team Results" header and assert the winning team's literal
	// W (Table 1) and IV (Table 2). Player order is [Red A, Blue A]; Red A won.
	tr := -1
	trCol := -1
	for r, row := range poolRows {
		for cIdx, cell := range row {
			if cell == "Team Results" {
				tr, trCol = r, cIdx
				break
			}
		}
		if tr >= 0 {
			break
		}
	}
	require.GreaterOrEqual(t, tr, 0, "Pool Matches sheet must contain a 'Team Results' header")
	// Table 1: Red A row = tr+1 (0-based); W col = trCol+1.
	require.Greater(t, len(poolRows), tr+1)
	require.Greater(t, len(poolRows[tr+1]), trCol+1)
	assert.Equal(t, "1", poolRows[tr+1][trCol+1], "Red A team-wins (W) must be literal 1, not a collapsed formula")
	// Table 2 header = tr + nPlayers(2) + 3; Red A IV row (0-based) = tr + 5; IV col = trCol+1.
	require.Greater(t, len(poolRows), tr+5)
	require.Greater(t, len(poolRows[tr+5]), trCol+1)
	assert.Equal(t, "2", poolRows[tr+5][trCol+1], "Red A individual-victories (IV) must be literal 2")

	// Sub-match ippon letters overlaid.
	assert.True(t, sheetContainsCell(poolRows, "M"), "team pool sub-match ippon 'M' must be present")
	assert.True(t, sheetContainsCell(poolRows, "D"), "team pool sub-match ippon 'D' must be present")

	// ---- Elimination: team encounter summary + DH suffix + winner ----
	elimRows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// The "Victories / Points" tally row holds literal IV/PW, Shiro (Red B) on
	// the LEFT and Aka (Red A) on the RIGHT (helper.WhiteLeft). The fixture's
	// unequal split (Aka IV=2/PW=3, Shiro IV=1/PW=1) means a swapped pair
	// would fail these, not just pass either way.
	vp := -1
	vpCol := -1
	for r, row := range elimRows {
		for cIdx, cell := range row {
			if cell == "Victories / Points" {
				vp, vpCol = r, cIdx
				break
			}
		}
		if vp >= 0 {
			break
		}
	}
	require.GreaterOrEqual(t, vp, 0, "elimination sheet must contain a 'Victories / Points' summary row")
	require.Greater(t, len(elimRows[vp]), vpCol+5)
	assert.Equal(t, "1", elimRows[vp][vpCol+1], "Red B (Shiro) bracket IV must be literal 1 on the LEFT summary cell")
	assert.Equal(t, "2", elimRows[vp][vpCol+5], "Red A (Aka) bracket IV must be literal 2 on the RIGHT summary cell")
	assert.Equal(t, "1", elimRows[vp][vpCol+2], "Red B (Shiro) bracket PW must be literal 1 on the LEFT summary cell")
	assert.Equal(t, "3", elimRows[vp][vpCol+4], "Red A (Aka) bracket PW must be literal 3 on the RIGHT summary cell")

	assert.True(t, sheetContainsCell(elimRows, "(DH)"), "the '(DH)' middle mark must appear on the team encounter")
	assert.True(t, sheetContainsCell(elimRows, "Red A"), "winner 'Red A' must be written as a literal")
}

// TestWriteTeamSubMatchScores_OutOfRangePositionSkipped verifies the upper-bound
// guard: a corrupted sub.Position beyond teamSize must be skipped rather than
// writing ippon letters into the row of the NEXT encounter's block.
func TestWriteTeamSubMatchScores_OutOfRangePositionSkipped(t *testing.T) {
	t.Parallel()
	f := excelize.NewFile()
	defer f.Close()
	sheet := helper.SheetPoolMatches
	f.NewSheet(sheet)

	const teamSize = 3
	const courtStartCol = 1 // column A band
	const subStartRow = 5
	subs := []state.SubMatchResult{
		{Position: 1, IpponsA: []string{"M"}},
		{Position: 3, IpponsA: []string{"K"}},
		{Position: 9, IpponsA: []string{"D"}}, // corrupted: > teamSize
	}
	writeTeamSubMatchScores(f, sheet, courtStartCol, subStartRow, subs, teamSize, "", "", domain.MatchSideNone)

	// Position 1 -> row 5, Position 3 -> row 7 (both written). IpponsA is
	// Aka's, so it lands in the RIGHT cell (column F).
	v1, _ := f.GetCellValue(sheet, "F5")
	v3, _ := f.GetCellValue(sheet, "F7")
	assert.Equal(t, "M", v1)
	assert.Equal(t, "K", v3)
	// Position 9 would land at row subStartRow+8 = 13; it must NOT be written.
	v9, _ := f.GetCellValue(sheet, "F13")
	assert.Empty(t, v9, "an out-of-range sub.Position must be skipped, not written into a neighbouring block")
}

func TestBuildResultsWorkbook_KachinukiDetailSheet(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	// Format is Knockout, not Mixed (mp-yuy8 task 1): a Mixed competition
	// with an EMPTY pools list but a bracket that already carries real
	// round content is not a shape any real write path produces (Mixed
	// always populates pools.csv before it ever builds a bracket) -- it is
	// exactly the bracket/settings mismatch ErrBracketDrawMismatch refuses.
	// Knockout with no pools is the shape this fixture actually needs:
	// KnockoutLeavesFromBracket reads the leaf order straight off the same
	// hand-crafted bracket below, so the draw derives cleanly and the
	// Kachinuki Detail sheet assertion further down still exercises the
	// same bracket-bout-log data path, independent of pool/elimination
	// rendering.
	comp.Format = state.CompFormatKnockout
	comp.TeamMatchType = state.TeamMatchTypeKachinuki
	comp.TeamSize = 3
	require.NoError(t, store.SaveCompetition(comp))

	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "R1M0", SideA: "Ryu", SideB: "Tora",
					Winner: "Tora", Status: state.MatchStatusCompleted,
					Decision: "kachinuki-exhaustion", MatchNumber: 1,
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "Ryu Ichiro", SideB: "Tora Taro", Winner: "Ryu Ichiro", Decision: "fought"},
						{Position: 2, SideA: "Ryu Ichiro", SideB: "Tora Jiro", Winner: "Tora Jiro", Decision: "fought"},
					},
				},
			},
		},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	sheets := f.GetSheetList()
	require.Contains(t, sheets, helper.SheetKachinukiDetail,
		"results workbook must contain the Kachinuki Detail sheet for a kachinuki comp")

	rows, err := f.GetRows(helper.SheetKachinukiDetail)
	require.NoError(t, err)
	var flat string
	for _, row := range rows {
		for _, cell := range row {
			flat += cell + "|"
		}
	}
	assert.Contains(t, flat, "Ryu Ichiro", "bout player names must be in the sheet")
	assert.Contains(t, flat, "Tora Jiro", "bout player names must be in the sheet")
}

func TestBuildResultsWorkbook_FixedFormatNoKachinukiSheet(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.TeamMatchType = state.TeamMatchTypeFixed
	comp.TeamSize = 3
	require.NoError(t, store.SaveCompetition(comp))

	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	assert.NotContains(t, f.GetSheetList(), helper.SheetKachinukiDetail,
		"fixed-format comps must not emit a Kachinuki Detail sheet")
}

// TestBuildResultsWorkbook_RepresentativeBoutsSheetIffARepresentativeBoutExists
// pins the results workbook's Representative Bouts sheet (the main sheets'
// team block lists numbered bouts only, so a representative bout and its
// representatives reached no sheet): present, editable, naming both
// representatives, DH as the bout number and (DH) in the centre, exactly when a
// match of the fixed-order team competition holds a position -1 row, and never
// beside a Kachinuki Detail sheet.
func TestBuildResultsWorkbook_RepresentativeBoutsSheetIffARepresentativeBoutExists(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T, withRepresentativeBout bool) *excelize.File {
		t.Helper()
		dir, store, eng, compID := testSetup(t)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })

		saveFixedTeamBracket(t, store, compID, withRepresentativeBout)

		data, err := BuildResultsWorkbook(store, eng, compID)
		require.NoError(t, err)
		f, err := excelize.OpenReader(bytes.NewReader(data))
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		return f
	}

	t.Run("with a representative bout", func(t *testing.T) {
		t.Parallel()
		f := build(t, true)

		require.Contains(t, f.GetSheetList(), helper.SheetRepresentativeBouts)
		assert.NotContains(t, f.GetSheetList(), helper.SheetKachinukiDetail,
			"a fixed-order competition never gets a Kachinuki Detail sheet")
		protection, err := f.GetSheetProtection(helper.SheetRepresentativeBouts)
		require.NoError(t, err)
		assert.Equal(t, excelize.SheetProtectionOptions{}, protection, "the sheet holds no formula to protect")

		rows, err := f.GetRows(helper.SheetRepresentativeBouts)
		require.NoError(t, err)
		var cells []string
		for _, row := range rows {
			cells = append(cells, row...)
		}
		flat := strings.Join(cells, "|")
		assert.Contains(t, flat, "(Representative bout)", "the section is titled")
		assert.Contains(t, flat, "Hiro", "Aka's representative")
		assert.Contains(t, flat, "Sayaka", "Shiro's representative")
		assert.Contains(t, flat, helper.RepresentativeBoutRowLabel+" T1.1 Hiro", "DH is the bout's number, then the representative")
		assert.Contains(t, cells, "(DH)", "the centre carries the representative-bout mark")
		assert.Contains(t, cells, "Ht", "the hantei mark rides beside the winner")
		assert.NotContains(t, flat, "-1", "the stored position is never printed")
	})

	t.Run("without one", func(t *testing.T) {
		t.Parallel()
		f := build(t, false)

		assert.NotContains(t, f.GetSheetList(), helper.SheetRepresentativeBouts,
			"no representative bout, no sheet: the main sheets' workbook is unchanged")
		assert.NotContains(t, f.GetSheetList(), helper.SheetKachinukiDetail)
	})
}
