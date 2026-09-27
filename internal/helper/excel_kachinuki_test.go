package helper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

// makeKachinukiTestMatch builds a 6-bout kachinuki team-match fixture used by
// the detail-sheet tests. Side A wins 4 bouts and Side B wins 1 with one
// hikiwake; in practice this is enough to drive every renderer branch (winner
// rows, draw row, summary tallies) without needing multiple fixtures. The
// match's own winner/decision are no longer carried here (operator decision
// 2026-09-27, no Winner/Decision column): the match's Pool or Elimination
// Matches row is where that result lives.
func makeKachinukiTestMatch() KachinukiMatchDetail {
	return KachinukiMatchDetail{
		Label:        "Pool A - Match 1",
		SideATeam:    "Team Alpha",
		SideBTeam:    "Team Bravo",
		EliminationA: 1, // 1 player from Team Alpha was retired (hikiwake bout)
		EliminationB: 5, // 5 players from Team Bravo retired (losses + hikiwake)
		Bouts: []KachinukiBout{
			{Position: 1, SideAName: "Alice", SideAPos: "Senpo", ScoreA: "MM", SideBName: "Bob", SideBPos: "Senpo", ScoreB: ""},
			{Position: 2, SideAName: "Alice", SideAPos: "Senpo", ScoreA: "M", SideBName: "Carol", SideBPos: "Jiho", ScoreB: ""},
			{Position: 3, SideAName: "Alice", SideAPos: "Senpo", ScoreA: "", SideBName: "Dan", SideBPos: "Chuken", ScoreB: "", Middle: "X"},
			{Position: 4, SideAName: "Eve", SideAPos: "Jiho", ScoreA: "MK", SideBName: "Frank", SideBPos: "Fukusho", ScoreB: "K"},
			{Position: 5, SideAName: "Eve", SideAPos: "Jiho", ScoreA: "", SideBName: "Grace", SideBPos: "Taisho", ScoreB: "M"},
			{Position: 6, SideAName: "Hank", SideAPos: "Chuken", ScoreA: "MMK", SideBName: "Grace", SideBPos: "Taisho", ScoreB: ""},
		},
	}
}

// TestKachinukiDetail_ShiroLeftAkaRight pins bc-kdsc's P1 header: Shiro
// (SideB) on the LEFT in a white header cell carrying the Shiro team's own
// NAME, Aka (SideA) on the RIGHT in a red header cell carrying the Aka
// team's name, no side words ("Side A"/"Side B"/"Shiro"/"Aka"/"White"/"Red")
// anywhere, and no Winner or Decision column at all (the sheet is six
// columns, A..F). Team names deliberately contain no side word, so the
// no-words assertion cannot pass by accident (it would pass vacuously if the
// team names themselves happened to say "Aka" or "White").
func TestKachinukiDetail_ShiroLeftAkaRight(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	match := KachinukiMatchDetail{
		Label:     "Pool A - Match 1",
		SideATeam: "Kodokan",  // Aka
		SideBTeam: "Mumeishi", // Shiro
		Bouts: []KachinukiBout{
			{Position: 1, SideAName: "Akagi", ScoreA: "M", SideBName: "Shirai", ScoreB: "KK"},
		},
		EliminationA: 1,
		EliminationB: 3,
	}
	require.NoError(t, WriteKachinukiDetailSheet(f, []KachinukiMatchDetail{match}))

	// Subtitle row: Shiro's team first (matching the scoreboard, "White vs
	// Red" reading left to right).
	subtitle, err := f.GetCellValue(SheetKachinukiDetail, "A2")
	require.NoError(t, err)
	assert.Equal(t, "Mumeishi vs Kodokan", subtitle)

	// Header row (row 3): B carries Mumeishi (Shiro, white header), E
	// carries Kodokan (Aka, red header). B:C and E:F are merged.
	const headerRow = "3"
	headerB, err := f.GetCellValue(SheetKachinukiDetail, "B"+headerRow)
	require.NoError(t, err)
	assert.Equal(t, "Mumeishi", headerB)
	headerE, err := f.GetCellValue(SheetKachinukiDetail, "E"+headerRow)
	require.NoError(t, err)
	assert.Equal(t, "Kodokan", headerE)

	merges, err := f.GetMergeCells(SheetKachinukiDetail)
	require.NoError(t, err)
	var mergedBC, mergedEF bool
	for _, m := range merges {
		switch {
		case m.GetStartAxis() == "B"+headerRow && m.GetEndAxis() == "C"+headerRow:
			mergedBC = true
		case m.GetStartAxis() == "E"+headerRow && m.GetEndAxis() == "F"+headerRow:
			mergedEF = true
		}
	}
	assert.True(t, mergedBC, "B%s:C%s must be merged for the Shiro team name", headerRow, headerRow)
	assert.True(t, mergedEF, "E%s:F%s must be merged for the Aka team name", headerRow, headerRow)

	whiteStyle := getWhiteHeaderStyle(f)
	redStyle := getRedHeaderStyle(f)
	bStyle, err := f.GetCellStyle(SheetKachinukiDetail, "B"+headerRow)
	require.NoError(t, err)
	assert.Equal(t, whiteStyle, bStyle, "Shiro's header cell must use the white header style")
	eStyle, err := f.GetCellStyle(SheetKachinukiDetail, "E"+headerRow)
	require.NoError(t, err)
	assert.Equal(t, redStyle, eStyle, "Aka's header cell must use the red header style")

	// No side word in the HEADER row specifically (scoped there, not swept
	// across bout rows: a fighter's real name, e.g. "Akagi", may legitimately
	// contain the substring "Aka" without the renderer having printed the
	// side word).
	headerForbidden := []string{"Side A", "Side B", "Shiro", "Aka", "White", "Red"}
	headerCells := []string{"A" + headerRow, "B" + headerRow, "C" + headerRow, "D" + headerRow, "E" + headerRow, "F" + headerRow}
	for _, cell := range headerCells {
		v, err := f.GetCellValue(SheetKachinukiDetail, cell)
		require.NoError(t, err)
		for _, word := range headerForbidden {
			assert.NotContainsf(t, v, word, "header cell %s (%q) must not contain %q", cell, v, word)
		}
	}

	// No cell anywhere in the section says "Winner" or "Decision", and
	// columns G/H are empty on every row: the sheet is six columns, A..F.
	rows, err := f.GetRows(SheetKachinukiDetail)
	require.NoError(t, err)
	for r, row := range rows {
		for c, cell := range row {
			for _, word := range []string{"Winner", "Decision"} {
				assert.NotContainsf(t, cell, word,
					"row %d col %d (%q) must not contain %q", r+1, c+1, cell, word)
			}
		}
		// Columns G (index 6) and H (index 7) must be empty on every row: the
		// sheet has no seventh or eighth column left to hold anything.
		assert.Lessf(t, len(row), 7, "row %d must not extend into column G", r+1)
	}

	// Bout row (row 4): Shiro (Shirai/KK) on the left, Aka (Akagi/M) on the
	// right.
	const boutRow = "4"
	boutB, err := f.GetCellValue(SheetKachinukiDetail, "B"+boutRow)
	require.NoError(t, err)
	assert.Contains(t, boutB, "Shirai")
	boutC, err := f.GetCellValue(SheetKachinukiDetail, "C"+boutRow)
	require.NoError(t, err)
	assert.Equal(t, "KK", boutC)
	boutE, err := f.GetCellValue(SheetKachinukiDetail, "E"+boutRow)
	require.NoError(t, err)
	assert.Equal(t, "M", boutE)
	boutF, err := f.GetCellValue(SheetKachinukiDetail, "F"+boutRow)
	require.NoError(t, err)
	assert.Contains(t, boutF, "Akagi")

	// Summary row (row 5): Shiro's (3) elimination count on the left (B),
	// Aka's (1) on the right (F).
	const summaryRow = "5"
	summaryB, err := f.GetCellValue(SheetKachinukiDetail, "B"+summaryRow)
	require.NoError(t, err)
	assert.Contains(t, summaryB, "3")
	summaryF, err := f.GetCellValue(SheetKachinukiDetail, "F"+summaryRow)
	require.NoError(t, err)
	assert.Contains(t, summaryF, "1")
}

// TestKachinukiDetailSheetExists is T195: when a competition has
// teamMatchType=kachinuki and at least one kachinuki match with bouts,
// the workbook contains a sheet named SheetKachinukiDetail.
func TestKachinukiDetailSheetExists(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	names := f.GetSheetList()
	found := false
	for _, n := range names {
		if n == SheetKachinukiDetail {
			found = true
			break
		}
	}
	assert.Truef(t, found, "expected sheet %q in workbook, got %v", SheetKachinukiDetail, names)
}

// TestKachinukiDetailSheetSkippedWhenEmpty confirms the renderer is a no-op
// when there are zero kachinuki matches, the detail sheet must not be
// created (T201 acceptance).
func TestKachinukiDetailSheetSkippedWhenEmpty(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	require.NoError(t, WriteKachinukiDetailSheet(f, nil))

	for _, n := range f.GetSheetList() {
		assert.NotEqual(t, SheetKachinukiDetail, n, "Kachinuki Detail sheet must not be created when no kachinuki matches exist")
	}
}

// TestKachinukiDetailBoutRows is T196: a 6-bout kachinuki match renders 6
// bout rows plus a header row and a summary row, with columns Bout #,
// <Shiro team> (merged B:C, white header), vs, <Aka team> (merged E:F, red
// header). There is no Winner or Decision column (operator decision
// 2026-09-27, bc-kdsc): the sheet is six columns, A..F.
func TestKachinukiDetailBoutRows(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	// Section layout (deterministic, defined by the renderer):
	//   row 1: match title (merged across columns A..F)
	//   row 2: subtitle "<Shiro team> vs <Aka team>"
	//   row 3: column headers
	//   rows 4..9: 6 bout rows (one per bout)
	//   row 10: summary row
	// Column letters A..F map to: Bout #, Shiro (name+score), vs, Aka (score+name).

	titleRow := 1
	subtitleRow := 2
	headerRow := 3
	firstBoutRow := 4
	summaryRow := firstBoutRow + 6

	// Header values: B:C merged holds the Shiro team's name (SideB, "Team
	// Bravo"), E:F merged holds the Aka team's name (SideA, "Team Alpha").
	boutHeader, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(headerRow))
	require.NoError(t, err)
	assert.Equal(t, "Bout #", boutHeader)
	leftHeader, err := f.GetCellValue(SheetKachinukiDetail, "B"+intToString(headerRow))
	require.NoError(t, err)
	assert.Equal(t, "Team Bravo", leftHeader, "Shiro (SideB) heads the left columns")
	vsHeader, err := f.GetCellValue(SheetKachinukiDetail, "D"+intToString(headerRow))
	require.NoError(t, err)
	assert.Equal(t, "vs", vsHeader)
	rightHeader, err := f.GetCellValue(SheetKachinukiDetail, "E"+intToString(headerRow))
	require.NoError(t, err)
	assert.Equal(t, "Team Alpha", rightHeader, "Aka (SideA) heads the right columns")

	// Title row should mention the match label and identify it as Kachinuki.
	title, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(titleRow))
	require.NoError(t, err)
	assert.Contains(t, title, "Pool A - Match 1")
	assert.Contains(t, strings.ToLower(title), "kachinuki")

	// Subtitle row should reference both team names.
	subtitle, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(subtitleRow))
	require.NoError(t, err)
	assert.Contains(t, subtitle, "Team Alpha")
	assert.Contains(t, subtitle, "Team Bravo")

	// Spot-check each bout row. Shiro (SideB) is LEFT (columns B/C), Aka
	// (SideA) is RIGHT (columns E/F); bout 3 is the fixture's hikiwake, so
	// its centre column reads "X" rather than the default "vs".
	wantBouts := []struct {
		boutNum   int
		sideAName string
		sideAPos  string
		scoreA    string
		scoreB    string
		sideBName string
		sideBPos  string
		middle    string
	}{
		{1, "Alice", "Senpo", "MM", "", "Bob", "Senpo", "vs"},
		{2, "Alice", "Senpo", "M", "", "Carol", "Jiho", "vs"},
		{3, "Alice", "Senpo", "", "", "Dan", "Chuken", "X"},
		{4, "Eve", "Jiho", "MK", "K", "Frank", "Fukusho", "vs"},
		{5, "Eve", "Jiho", "", "M", "Grace", "Taisho", "vs"},
		{6, "Hank", "Chuken", "MMK", "", "Grace", "Taisho", "vs"},
	}

	for i, b := range wantBouts {
		row := firstBoutRow + i

		// Bout number (column A)
		boutVal, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(row))
		require.NoError(t, err)
		assert.Equalf(t, intToString(b.boutNum), boutVal, "row %d: bout number", row)

		// Side B (Shiro) name + position, LEFT (column B).
		leftCell, err := f.GetCellValue(SheetKachinukiDetail, "B"+intToString(row))
		require.NoError(t, err)
		assert.Containsf(t, leftCell, b.sideBName, "row %d: Shiro name", row)
		assert.Containsf(t, leftCell, b.sideBPos, "row %d: Shiro position", row)

		// Side B (Shiro) score, LEFT (column C).
		leftScore, err := f.GetCellValue(SheetKachinukiDetail, "C"+intToString(row))
		require.NoError(t, err)
		assert.Equalf(t, b.scoreB, leftScore, "row %d: Shiro score", row)

		// Centre column (D): the bout's one middle mark, or "vs".
		middle, err := f.GetCellValue(SheetKachinukiDetail, "D"+intToString(row))
		require.NoError(t, err)
		assert.Equalf(t, b.middle, middle, "row %d: centre mark", row)

		// Side A (Aka) score, RIGHT (column E).
		rightScore, err := f.GetCellValue(SheetKachinukiDetail, "E"+intToString(row))
		require.NoError(t, err)
		assert.Equalf(t, b.scoreA, rightScore, "row %d: Aka score", row)

		// Side A (Aka) name + position, RIGHT (column F).
		rightCell, err := f.GetCellValue(SheetKachinukiDetail, "F"+intToString(row))
		require.NoError(t, err)
		assert.Containsf(t, rightCell, b.sideAName, "row %d: Aka name", row)
		assert.Containsf(t, rightCell, b.sideAPos, "row %d: Aka position", row)
	}

	// Sanity check: the row immediately after the last bout is the summary,
	// not another bout row, the bout column should NOT contain a bout number
	// (it should hold a "Summary" or "Total" label instead).
	postBouts, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(summaryRow))
	require.NoError(t, err)
	assert.NotEqual(t, "7", postBouts, "expected exactly 6 bout rows, found a 7th")
}

// TestKachinukiDetailBoutRows_WithSquadLabel verifies the squad member
// label (bc-pnum: "make a team member's label available to the public
// surfaces") is written beside the fighter's name on the detail sheet, and
// that a blank label (the SideB fighter here has none) falls back to just
// the name -- unaffected, matching every other row in this file's fixture.
func TestKachinukiDetailBoutRows_WithSquadLabel(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	match := KachinukiMatchDetail{
		Label:     "Pool A - Match 1",
		SideATeam: "Team Alpha",
		SideBTeam: "Team Bravo",
		Bouts: []KachinukiBout{
			{Position: 1, SideAName: "Alice", SideALabel: "T10.1", SideAPos: "Senpo", ScoreA: "MM", SideBName: "Bob", SideBPos: "Senpo", ScoreB: ""},
		},
	}
	require.NoError(t, WriteKachinukiDetailSheet(f, []KachinukiMatchDetail{match}))

	firstBoutRow := 4
	// Side A (Aka, labelled T10.1) is RIGHT, column F.
	sideACell, err := f.GetCellValue(SheetKachinukiDetail, "F"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Equal(t, "T10.1 Alice (Senpo)", sideACell, "label leads the name, ahead of the position suffix")

	// Side B (Shiro, no label recorded) is LEFT, column B.
	sideBCell, err := f.GetCellValue(SheetKachinukiDetail, "B"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Equal(t, "Bob (Senpo)", sideBCell, "no label recorded for Side B: falls back to name + position, unchanged")
}

// TestKachinukiDetailBoutRow_FusenshoMarkBesideScore pins the per-bout
// default-win mark (bc-kdsc change 8b): the winner's result mark (e.g. the
// exhaustion walkover's Fus.) rides beside its OWN score cell, composed with
// the same score+mark join the main sheets use, never in the centre column,
// which the closed-set middle-mark rule reserves for vs/X/(E)/(DH).
func TestKachinukiDetailBoutRow_FusenshoMarkBesideScore(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	match := KachinukiMatchDetail{
		Label:     "Pool A - Match 1",
		SideATeam: "Team Alpha",
		SideBTeam: "Team Bravo",
		Bouts: []KachinukiBout{
			// Aka (SideA) wins by default: MarkA rides beside ScoreA.
			{Position: 1, SideAName: "Ivy", SideBName: "Jack", ScoreA: "○○", MarkA: "Fus."},
		},
	}
	require.NoError(t, WriteKachinukiDetailSheet(f, []KachinukiMatchDetail{match}))

	firstBoutRow := 4
	// Aka (SideA) is RIGHT (column E): score and mark compose one cell.
	rightScore, err := f.GetCellValue(SheetKachinukiDetail, "E"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Equal(t, "○○ Fus.", rightScore, "the winner's maru and Fus. mark ride together in its own score cell")

	// The loser's cell (Shiro, column C) carries neither score nor mark.
	leftScore, err := f.GetCellValue(SheetKachinukiDetail, "C"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Empty(t, leftScore, "fusensho marks only the winner; the no-show's own cell stays blank")

	// The centre stays untouched: no bout.Middle set, so it falls back to
	// the template's own "vs" -- a default win is not a middle-mark decision.
	middle, err := f.GetCellValue(SheetKachinukiDetail, "D"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Equal(t, "vs", middle, "a default-win mark never reaches the centre column")
}

// TestKachinukiDetailSummaryRow is T197: the summary row shows total
// eliminations per team, Shiro (SideB) left and Aka (SideA) right through
// WhiteLeft like every other row on this sheet. There is no Winner or
// Decision cell (operator decision 2026-09-27, bc-kdsc): the match's own
// Pool or Elimination Matches row carries the outcome.
func TestKachinukiDetailSummaryRow(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	// 6 bouts + 3 leading rows (title, subtitle, header) → summary at row 10.
	summaryRow := 10

	// A label like "Total" / "Summary", exact text not pinned, just that
	// some label is present.
	label, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(summaryRow))
	require.NoError(t, err)
	assert.NotEmpty(t, label, "summary row should have a label in column A")
	lowerLabel := strings.ToLower(label)
	assert.True(t,
		strings.Contains(lowerLabel, "summary") || strings.Contains(lowerLabel, "total"),
		"summary row label should mention Summary or Total, got %q", label)

	// Eliminations per team: Team A (Aka) retired 1 (hikiwake), Team B
	// (Shiro) retired 5. Shiro's count is LEFT (column B), Aka's is RIGHT
	// (column F). We don't pin the exact wording, just that the integers
	// appear under the correct side.
	shiroElim, err := f.GetCellValue(SheetKachinukiDetail, "B"+intToString(summaryRow))
	require.NoError(t, err)
	assert.Contains(t, shiroElim, "5", "Shiro (Team B) elimination count should appear in the LEFT column B")

	akaElim, err := f.GetCellValue(SheetKachinukiDetail, "F"+intToString(summaryRow))
	require.NoError(t, err)
	assert.Contains(t, akaElim, "1", "Aka (Team A) elimination count should appear in the RIGHT column F")

	// No Winner or Decision cell: the sheet is six columns, A..F, and the
	// match's own Pool or Elimination Matches row carries the outcome.
	for _, col := range []string{"G", "H"} {
		v, err := f.GetCellValue(SheetKachinukiDetail, col+intToString(summaryRow))
		require.NoError(t, err)
		assert.Empty(t, v, "column %s must be empty: no Winner or Decision column", col)
	}
}

// TestKachinukiDetailBlankSection pins the hand-entry section (operator
// decision 2026-09-27, bc-kdsc): a match with no bout recorded prints
// BlankBoutRows empty numbered rows, "vs" in the centre as an unplayed row
// reads, and a summary row whose counts are left for the hand to fill in; a
// match with bouts lists exactly those, whatever BlankBoutRows says.
func TestKachinukiDetailBlankSection(t *testing.T) {
	cases := []struct {
		name        string
		match       KachinukiMatchDetail
		wantRows    int
		wantSummary [2]string // left (Shiro), right (Aka) summary cells
	}{
		{
			name: "no bouts: empty numbered rows",
			match: KachinukiMatchDetail{
				Label: "Round 2 - Match 3", SideATeam: "M 1", SideBTeam: "Kodokan", BlankBoutRows: 5,
			},
			wantRows: 5,
		},
		{
			name: "bouts recorded: exactly those",
			match: KachinukiMatchDetail{
				Label: "Pool Match 1", SideATeam: "Kodokan", SideBTeam: "Mumeishi", BlankBoutRows: 5,
				Bouts: []KachinukiBout{
					{Position: 1, SideAName: "Akagi", ScoreA: "M", SideBName: "Shirai"},
					{Position: 2, SideAName: "Akagi", SideBName: "Shimizu", ScoreB: "KK"},
				},
				EliminationA: 1, EliminationB: 1,
			},
			wantRows:    2,
			wantSummary: [2]string{"1 eliminated", "1 eliminated"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer func() { _ = f.Close() }()
			require.NoError(t, WriteKachinukiDetailSheet(f, []KachinukiMatchDetail{tc.match}))

			cell := func(col string, row int) string {
				v, err := f.GetCellValue(SheetKachinukiDetail, col+intToString(row))
				require.NoError(t, err)
				return v
			}
			// Rows 1-3 are title, subtitle and header; bouts start on row 4.
			assert.Contains(t, cell("A", 2), tc.match.SideATeam, "the subtitle names both sides")
			for i := 0; i < tc.wantRows; i++ {
				row := 4 + i
				assert.Equal(t, intToString(i+1), cell("A", row), "row %d carries bout number %d", row, i+1)
				assert.Equal(t, "vs", cell("D", row), "row %d centre", row)
				if len(tc.match.Bouts) == 0 {
					for _, col := range []string{"B", "C", "E", "F"} {
						assert.Empty(t, cell(col, row), "an empty row leaves %s%d for the hand", col, row)
					}
				}
			}
			summaryRow := 4 + tc.wantRows
			assert.Equal(t, "Summary", cell("A", summaryRow), "the summary follows the last row")
			assert.Equal(t, tc.wantSummary[0], cell("B", summaryRow))
			assert.Equal(t, tc.wantSummary[1], cell("F", summaryRow))
		})
	}
}

// TestKachinukiDetailSheetSkippedWithNothingToList confirms a match with
// neither bouts nor empty rows to print adds no section, and no sheet when
// it is the only one.
func TestKachinukiDetailSheetSkippedWithNothingToList(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	require.NoError(t, WriteKachinukiDetailSheet(f, []KachinukiMatchDetail{{Label: "Pool Match 1", SideATeam: "A", SideBTeam: "B"}}))
	assert.NotContains(t, f.GetSheetList(), SheetKachinukiDetail)
}

// TestKachinukiDetailSectionsStayOnOnePage pins the keep-together page break:
// a section that would cross KachinukiDetailRowsPerPage starts a new page
// rather than being split. Five empty sections of nine rows are 13 rows each
// plus a separator, so the fourth (rows 43-55) would cross the 50-row budget
// and the page ends after row 42; the fifth still fits beside it.
func TestKachinukiDetailSectionsStayOnOnePage(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := make([]KachinukiMatchDetail, 5)
	for i := range matches {
		matches[i] = KachinukiMatchDetail{Label: "Pool Match " + intToString(i+1), SideATeam: "Kodokan", SideBTeam: "Mumeishi", BlankBoutRows: 9}
	}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)

	breaks, err := bctest.RowBreaks(buf.Bytes(), SheetKachinukiDetail)
	require.NoError(t, err)
	assert.Equal(t, []int{42}, breaks)
	title, err := f.GetCellValue(SheetKachinukiDetail, "A43")
	require.NoError(t, err)
	assert.Equal(t, "Pool Match 4 (Kachinuki)", title, "the new page starts on the fourth section's title")
}

// TestKachinukiDetailMultipleMatches verifies the renderer writes one
// section per match with blank-row separation. Two matches → two title
// rows, two summary rows, no overlap.
func TestKachinukiDetailMultipleMatches(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	m1 := makeKachinukiTestMatch()
	m2 := makeKachinukiTestMatch()
	m2.Label = "Pool A - Match 2"
	m2.SideATeam = "Team Charlie"
	m2.SideBTeam = "Team Delta"
	m2.EliminationA = 5
	m2.EliminationB = 2

	require.NoError(t, WriteKachinukiDetailSheet(f, []KachinukiMatchDetail{m1, m2}))

	// First match: title at row 1.
	t1, _ := f.GetCellValue(SheetKachinukiDetail, "A1")
	assert.Contains(t, t1, "Pool A - Match 1")

	// Second match: title appears somewhere on the sheet, distinct from m1.
	// Walk down looking for the second title row. The renderer leaves at
	// least one blank row between sections (>= row 12 in the test fixture:
	// 10 rows of m1 + 1 separator + start of m2).
	foundSecond := false
	for row := 11; row <= 25; row++ {
		v, _ := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(row))
		if strings.Contains(v, "Pool A - Match 2") {
			foundSecond = true
			break
		}
	}
	assert.True(t, foundSecond, "expected second match section after first")
}

// TestKachinukiMainSheetStillSummary is T198: when the workbook is built
// with kachinuki team matches the existing Pool Matches and Elimination
// Matches sheets are still present and structured as 8-column-per-court
// adding the detail sheet does not regress the main-sheet layout
// invariant (NFR-023).
//
// We exercise this by calling the existing CLI helper code path (which
// is kachinuki-agnostic) and confirming the sheet names are present and
// usable. The detail sheet is then added by WriteKachinukiDetailSheet
// and must not disturb Pool Matches / Elimination Matches.
func TestKachinukiMainSheetStillSummary(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// Build the standard set of sheets the way NewFileFromScratch does it
	// every Phase 11 invocation must coexist with these.
	for _, name := range []string{SheetTimeEstimator, SheetPoolDraw, SheetPoolMatches, SheetEliminationMatches, SheetNamesToPrint, SheetTree} {
		_, err := f.NewSheet(name)
		require.NoError(t, err)
	}

	// Add the kachinuki detail sheet (the new Phase 11 sheet).
	require.NoError(t, WriteKachinukiDetailSheet(f, []KachinukiMatchDetail{makeKachinukiTestMatch()}))

	// Required sheets after Phase 11 wiring.
	required := []string{
		SheetPoolMatches,
		SheetEliminationMatches,
		SheetKachinukiDetail,
	}
	present := map[string]bool{}
	for _, n := range f.GetSheetList() {
		present[n] = true
	}
	for _, r := range required {
		assert.Truef(t, present[r], "expected sheet %q to be present", r)
	}

	// Pool Matches and Elimination Matches must remain on the 8-column-per-court
	// layout, i.e. nothing in the detail-sheet wiring path should rewrite the
	// CourtsColumnsPerCourt invariant.
	assert.Equal(t, 8, CourtsColumnsPerCourt, "CourtsColumnsPerCourt invariant must remain 8")
}

// TestKachinukiDetailPageLayout pins the page setup fold-in: the sheet did
// not call any of the shared layout helpers before (LibreOffice printed it
// across two pages), so it must now print one page wide, portrait A4, like
// every other sheet in the workbook (SetSheetLayoutPortraitA4).
func TestKachinukiDetailPageLayout(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	layout, err := f.GetPageLayout(SheetKachinukiDetail)
	require.NoError(t, err)
	require.NotNil(t, layout.Orientation)
	assert.Equal(t, "portrait", *layout.Orientation)
	require.NotNil(t, layout.FitToWidth)
	assert.Equal(t, 1, *layout.FitToWidth, "page must scale to exactly one page wide")
}

// TestKachinukiDetailFighterColumnsWideEnoughForALabelledName pins the
// fighter columns (B/F) wide enough that a labelled name like
// "T12.4 Yui Nakamura (Fukusho)" (28 characters) does not clip at this
// sheet's 12pt centred font; a LibreOffice render at 24 clipped the
// 26-character "T3.4 Yui Nakamura (Chuken)". The print is fitted to one page
// wide (TestKachinukiDetailPageLayout), so the width costs the print nothing.
func TestKachinukiDetailFighterColumnsWideEnoughForALabelledName(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	left, err := f.GetColWidth(SheetKachinukiDetail, kachinukiColLeft)
	require.NoError(t, err)
	right, err := f.GetColWidth(SheetKachinukiDetail, kachinukiColRight)
	require.NoError(t, err)
	assert.Equal(t, 36.0, left, "the fighter column must be wide enough for a labelled name plus margin")
	assert.Equal(t, 36.0, right, "the fighter column must be wide enough for a labelled name plus margin")
}

// intToString is a small helper that converts an int to a string without
// pulling in strconv just for these tests. Mirrors fmt.Sprint("%d", n) on
// non-negative ints, which is all we need for row indexing.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
