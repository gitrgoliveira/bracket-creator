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
// rows, the draw row) without needing multiple fixtures. The match's own
// winner/decision, and any tally of eliminations, are no longer carried here
// (operator decisions 2026-09-27 and 2026-10-02: no Winner/Decision column,
// no summary row): the match's Pool or Elimination Matches row is where that
// result lives, and the section simply ends at its last bout row.
func makeKachinukiTestMatch() KachinukiMatchDetail {
	return KachinukiMatchDetail{
		Label:     "Pool A - Match 1",
		SideATeam: "Team Alpha",
		SideBTeam: "Team Bravo",
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

// TestKachinukiDetail_ShiroLeftAkaRight pins the section layout (operator
// decision 2026-10-02): a section is drawn exactly like a team match block
// on the Elimination Matches sheet, seven columns A..G. Row 2 carries the
// generic White/Red header (matchHeaderLabels), never a team name; row 3
// carries the team names themselves, Shiro (SideB) on the LEFT in column A,
// Aka (SideA) on the RIGHT in column G, each in the plain grey text style,
// not the white/red header style (that belongs to row 2 alone). There is no
// Winner or Decision column, and the sheet uses exactly seven columns:
// nothing beyond G carries a value.
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
	}
	require.NoError(t, WriteKachinukiDetailSheet(f, []KachinukiMatchDetail{match}))

	// Header row (row 2): the generic White/Red labels, never a team name,
	// placed exactly as matchHeaderWithStyles places them on an Elimination
	// Matches block.
	const headerRow = "2"
	leftLabel, rightLabel := matchHeaderLabels()
	headerA, err := f.GetCellValue(SheetKachinukiDetail, "A"+headerRow)
	require.NoError(t, err)
	assert.Equal(t, leftLabel, headerA, "Shiro's header cell carries the generic left label")
	headerG, err := f.GetCellValue(SheetKachinukiDetail, "G"+headerRow)
	require.NoError(t, err)
	assert.Equal(t, rightLabel, headerG, "Aka's header cell carries the generic right label")

	whiteStyle := getWhiteHeaderStyle(f)
	redStyle := getRedHeaderStyle(f)
	aStyle, err := f.GetCellStyle(SheetKachinukiDetail, "A"+headerRow)
	require.NoError(t, err)
	assert.Equal(t, whiteStyle, aStyle, "Shiro's header cell must use the white header style")
	gStyle, err := f.GetCellStyle(SheetKachinukiDetail, "G"+headerRow)
	require.NoError(t, err)
	assert.Equal(t, redStyle, gStyle, "Aka's header cell must use the red header style")

	// Team row (row 3): the team names themselves, Shiro (SideB) LEFT,
	// Aka (SideA) RIGHT -- this is the ONE place either team's name is
	// printed in the section (see the "printed once" check below).
	const teamRow = "3"
	teamA, err := f.GetCellValue(SheetKachinukiDetail, "A"+teamRow)
	require.NoError(t, err)
	assert.Equal(t, "Mumeishi", teamA, "Shiro's team name is on the LEFT")
	teamG, err := f.GetCellValue(SheetKachinukiDetail, "G"+teamRow)
	require.NoError(t, err)
	assert.Equal(t, "Kodokan", teamG, "Aka's team name is on the RIGHT")

	// No cell anywhere in the section says "Winner" or "Decision", and
	// nothing is written beyond column G: the sheet is seven columns, A..G.
	rows, err := f.GetRows(SheetKachinukiDetail)
	require.NoError(t, err)
	for r, row := range rows {
		for c, cell := range row {
			for _, word := range []string{"Winner", "Decision"} {
				assert.NotContainsf(t, cell, word,
					"row %d col %d (%q) must not contain %q", r+1, c+1, cell, word)
			}
		}
		// Column H (index 7) and beyond must be empty on every row: the
		// sheet has no eighth column left to hold anything.
		assert.LessOrEqualf(t, len(row), 7, "row %d must not extend past column G", r+1)
	}

	// Bout row (row 4): Shiro (Shirai/KK) on the left, Aka (Akagi/M) on the
	// right, each cell carrying the bout number ahead of the fighter.
	const boutRow = "4"
	boutA, err := f.GetCellValue(SheetKachinukiDetail, "A"+boutRow)
	require.NoError(t, err)
	assert.Contains(t, boutA, "Shirai")
	boutB, err := f.GetCellValue(SheetKachinukiDetail, "B"+boutRow)
	require.NoError(t, err)
	assert.Equal(t, "KK", boutB)
	boutF, err := f.GetCellValue(SheetKachinukiDetail, "F"+boutRow)
	require.NoError(t, err)
	assert.Equal(t, "M", boutF)
	boutG, err := f.GetCellValue(SheetKachinukiDetail, "G"+boutRow)
	require.NoError(t, err)
	assert.Contains(t, boutG, "Akagi")

	// Each team's name is printed exactly ONCE in the section (on the team
	// row alone): removing the old duplicated "<Shiro> vs <Aka>" subtitle
	// line is part of this change (operator decision 2026-10-02).
	countOccurrences := func(needle string) int {
		n := 0
		for _, row := range rows {
			for _, cell := range row {
				if cell == needle {
					n++
				}
			}
		}
		return n
	}
	assert.Equal(t, 1, countOccurrences("Mumeishi"), "Shiro's team name must appear exactly once")
	assert.Equal(t, 1, countOccurrences("Kodokan"), "Aka's team name must appear exactly once")
}

// TestKachinukiDetailSheetExists is T195: given at least one match section to
// list, whether it carries recorded bouts (as here) or only the empty numbered
// rows a match with none gets, the workbook contains a sheet named
// SheetKachinukiDetail.
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
// bout rows after its title, header and team rows, each name cell carrying
// the bout number ahead of the fighter it names. There is no Winner,
// Decision, or summary row (operator decisions 2026-09-27 and 2026-10-02):
// the section is seven columns, A..G, and ends at its last bout row.
func TestKachinukiDetailBoutRows(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	// Section layout (deterministic, defined by the renderer):
	//   row 1: match title (merged across columns A..G)
	//   row 2: White | vs | Red header (generic, no team name)
	//   row 3: team row, Shiro (SideB) left, Aka (SideA) right
	//   rows 4..9: 6 bout rows (one per bout)
	// Column letters A..G map to: Shiro (name+score), unused, centre mark,
	// unused, Aka (score+name), matching a team match block.

	titleRow := 1
	headerRow := 2
	teamRow := 3
	firstBoutRow := 4
	afterLastBout := firstBoutRow + 6

	// Header row: the generic labels, never either team's name.
	leftLabel, rightLabel := matchHeaderLabels()
	gotLeftLabel, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(headerRow))
	require.NoError(t, err)
	assert.Equal(t, leftLabel, gotLeftLabel)
	vsHeader, err := f.GetCellValue(SheetKachinukiDetail, "D"+intToString(headerRow))
	require.NoError(t, err)
	assert.Equal(t, "vs", vsHeader)
	gotRightLabel, err := f.GetCellValue(SheetKachinukiDetail, "G"+intToString(headerRow))
	require.NoError(t, err)
	assert.Equal(t, rightLabel, gotRightLabel)

	// Team row: Shiro (SideB, "Team Bravo") LEFT, Aka (SideA, "Team Alpha") RIGHT.
	leftTeam, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(teamRow))
	require.NoError(t, err)
	assert.Equal(t, "Team Bravo", leftTeam, "Shiro (SideB) heads the left column")
	rightTeam, err := f.GetCellValue(SheetKachinukiDetail, "G"+intToString(teamRow))
	require.NoError(t, err)
	assert.Equal(t, "Team Alpha", rightTeam, "Aka (SideA) heads the right column")

	// Title row should mention the match label and identify it as Kachinuki.
	title, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(titleRow))
	require.NoError(t, err)
	assert.Contains(t, title, "Pool A - Match 1")
	assert.Contains(t, strings.ToLower(title), "kachinuki")

	// Spot-check each bout row. Shiro (SideB) is LEFT (columns A/B), Aka
	// (SideA) is RIGHT (columns F/G); bout 3 is the fixture's hikiwake, so
	// its centre column reads "X". Every other bout carries no middle mark
	// at all: an empty centre, never a default "vs" (that belongs to the
	// section's header row alone).
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
		{1, "Alice", "Senpo", "MM", "", "Bob", "Senpo", ""},
		{2, "Alice", "Senpo", "M", "", "Carol", "Jiho", ""},
		{3, "Alice", "Senpo", "", "", "Dan", "Chuken", "X"},
		{4, "Eve", "Jiho", "MK", "K", "Frank", "Fukusho", ""},
		{5, "Eve", "Jiho", "", "M", "Grace", "Taisho", ""},
		{6, "Hank", "Chuken", "MMK", "", "Grace", "Taisho", ""},
	}

	for i, b := range wantBouts {
		row := firstBoutRow + i

		// Side B (Shiro) name + position, led by the bout number, LEFT
		// (column A).
		wantLeft := intToString(b.boutNum) + " " + b.sideBName + " (" + b.sideBPos + ")"
		leftCell, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(row))
		require.NoError(t, err)
		assert.Equalf(t, wantLeft, leftCell, "row %d: Shiro name", row)

		// Side B (Shiro) score, LEFT (column B).
		leftScore, err := f.GetCellValue(SheetKachinukiDetail, "B"+intToString(row))
		require.NoError(t, err)
		assert.Equalf(t, b.scoreB, leftScore, "row %d: Shiro score", row)

		// Centre column (D): the bout's one middle mark, or empty.
		middle, err := f.GetCellValue(SheetKachinukiDetail, "D"+intToString(row))
		require.NoError(t, err)
		assert.Equalf(t, b.middle, middle, "row %d: centre mark", row)

		// Side A (Aka) score, RIGHT (column F).
		rightScore, err := f.GetCellValue(SheetKachinukiDetail, "F"+intToString(row))
		require.NoError(t, err)
		assert.Equalf(t, b.scoreA, rightScore, "row %d: Aka score", row)

		// Side A (Aka) name + position, led by the bout number, RIGHT
		// (column G).
		wantRight := intToString(b.boutNum) + " " + b.sideAName + " (" + b.sideAPos + ")"
		rightCell, err := f.GetCellValue(SheetKachinukiDetail, "G"+intToString(row))
		require.NoError(t, err)
		assert.Equalf(t, wantRight, rightCell, "row %d: Aka name", row)
	}

	// Sanity check: exactly 6 bout rows, nothing in a would-be 7th.
	// TestKachinukiDetailSectionEndsAtLastBoutRow pins the full-row sweep
	// for this, now that there is no summary row to tell the two apart.
	postBouts, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(afterLastBout))
	require.NoError(t, err)
	assert.Empty(t, postBouts, "expected exactly 6 bout rows, found content in a 7th")
}

// TestKachinukiDetailBoutRows_WithSquadLabel verifies the squad member
// label (bc-pnum: "make a team member's label available to the public
// surfaces") is written beside the fighter's name on the detail sheet,
// after the bout number every name cell now carries ahead of the fighter
// (operator decision 2026-10-02), and that a blank label (the SideB fighter
// here has none) falls back to just the name -- unaffected, matching every
// other row in this file's fixture.
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
	// Side A (Aka, labelled T10.1) is RIGHT, column G: the bout number
	// leads, then the label leads the name, ahead of the position suffix.
	sideACell, err := f.GetCellValue(SheetKachinukiDetail, "G"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Equal(t, "1 T10.1 Alice (Senpo)", sideACell, "the bout number leads, then the label leads the name")

	// Side B (Shiro, no label recorded) is LEFT, column A: no label
	// recorded falls back to the bout number, name and position.
	sideBCell, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Equal(t, "1 Bob (Senpo)", sideBCell, "no label recorded for Side B: falls back to number + name + position")
}

// TestKachinukiDetailBoutRow_FusenshoMarkBesideScore pins the per-bout
// default-win mark (bc-kdsc change 8b): the winner's result mark (e.g. the
// exhaustion walkover's Fus.) rides beside its OWN score cell, composed with
// the same score+mark join the main sheets use, never in the centre column,
// which the closed-set middle-mark rule reserves for vs/X/(E)/(DH) and which
// a default win never sets on this sheet.
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
	// Aka (SideA) is RIGHT (column F): score and mark compose one cell.
	rightScore, err := f.GetCellValue(SheetKachinukiDetail, "F"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Equal(t, "○○ Fus.", rightScore, "the winner's maru and Fus. mark ride together in its own score cell")

	// The loser's cell (Shiro, column B) carries neither score nor mark.
	leftScore, err := f.GetCellValue(SheetKachinukiDetail, "B"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Empty(t, leftScore, "fusensho marks only the winner; the no-show's own cell stays blank")

	// The centre stays empty: no bout.Middle set, and a default win is not a
	// middle-mark decision, so there is no fallback "vs" at bout-row level
	// (that belongs to the section's White | vs | Red header row alone).
	middle, err := f.GetCellValue(SheetKachinukiDetail, "D"+intToString(firstBoutRow))
	require.NoError(t, err)
	assert.Empty(t, middle, "a default-win mark never reaches the centre column, and the centre has no default fallback")
}

// TestKachinukiDetailSectionEndsAtLastBoutRow pins the section's end
// (operator decision 2026-10-02: the "Eliminated" summary row is dropped
// entirely, along with KachinukiMatchDetail.EliminationA/EliminationB): a
// section carries no tally and no label after its bouts, it simply ends.
// With a single section on the sheet the row right after the last bout is
// left completely empty on every column; TestKachinukiDetailMultipleMatches
// pins the other half of this, that a second section's title follows after
// exactly one blank separator row.
func TestKachinukiDetailSectionEndsAtLastBoutRow(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	// 6 bouts + 3 leading rows (title, header, team) -> the last bout is
	// row 9, so row 10 is left empty: there is only one section here, and
	// nothing ever follows a section's last bout row but either a blank
	// separator or, with none to separate from, nothing at all.
	afterLastBout := 10

	for _, col := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		v, err := f.GetCellValue(SheetKachinukiDetail, col+intToString(afterLastBout))
		require.NoError(t, err)
		assert.Emptyf(t, v, "column %s of row %d must be empty: the section ends at its last bout row, no summary follows", col, afterLastBout)
	}
}

// TestKachinukiDetailBlankSection pins the hand-entry section (operator
// decision 2026-09-27, bc-kdsc): a match with no bout recorded prints
// BlankBoutRows empty numbered rows, each showing just its row number with
// an empty centre (no default "vs" at bout-row level, see
// TestKachinukiDetailBoutRow_FusenshoMarkBesideScore); a match with bouts
// lists exactly those, whatever BlankBoutRows says. There is no summary row
// (operator decision 2026-10-02): the section simply ends at its last row.
func TestKachinukiDetailBlankSection(t *testing.T) {
	cases := []struct {
		name     string
		match    KachinukiMatchDetail
		wantRows int
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
			},
			wantRows: 2,
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
			// Rows 1-3 are title, header and team; bouts start on row 4.
			assert.Equal(t, tc.match.SideBTeam, cell("A", 3), "the team row names Shiro on the left")
			assert.Equal(t, tc.match.SideATeam, cell("G", 3), "the team row names Aka on the right")
			for i := 0; i < tc.wantRows; i++ {
				row := 4 + i
				assert.Equal(t, "", cell("D", row), "row %d centre stays empty absent a mark", row)
				if len(tc.match.Bouts) == 0 {
					// An empty row shows just its number, left for the hand.
					assert.Equal(t, intToString(i+1), cell("A", row), "row %d carries just the bout number", row)
					assert.Equal(t, intToString(i+1), cell("G", row), "row %d carries just the bout number", row)
					for _, col := range []string{"B", "C", "E", "F"} {
						assert.Empty(t, cell(col, row), "an empty row leaves %s%d for the hand", col, row)
					}
				}
			}
			// The section ends at its last row: each case's match is the
			// sheet's only section, so nothing follows it.
			afterLastRow := 4 + tc.wantRows
			for _, col := range []string{"A", "B", "C", "D", "E", "F", "G"} {
				assert.Empty(t, cell(col, afterLastRow), "no summary row follows the last bout")
			}
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
// rather than being split. Five sections of nine blank bout rows are 12
// content rows each (title, header, team, 9 bouts) plus a 1-row separator
// between sections. After three sections (36 content rows + 3 separators =
// 39 rows consumed) the fourth section's own 12 content rows would push the
// running total to 51, past the 50-row budget, so the break lands ahead of
// it and the fifth still fits on the new page.
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
	assert.Equal(t, []int{39}, breaks)
	title, err := f.GetCellValue(SheetKachinukiDetail, "A40")
	require.NoError(t, err)
	assert.Equal(t, "Pool Match 4 (Kachinuki)", title, "the new page starts on the fourth section's title")
}

// TestKachinukiDetailMultipleMatches verifies the renderer writes one
// section per match with exactly one blank separator row between them, and
// that a section's own title, header, team and bout rows never bleed into
// the next. Two matches of 6 bouts each: the first section's last bout is
// row 9, row 10 is the blank separator, and the second section's title
// starts at row 11.
func TestKachinukiDetailMultipleMatches(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	m1 := makeKachinukiTestMatch()
	m2 := makeKachinukiTestMatch()
	m2.Label = "Pool A - Match 2"
	m2.SideATeam = "Team Charlie"
	m2.SideBTeam = "Team Delta"

	require.NoError(t, WriteKachinukiDetailSheet(f, []KachinukiMatchDetail{m1, m2}))

	// First match: title at row 1.
	t1, _ := f.GetCellValue(SheetKachinukiDetail, "A1")
	assert.Contains(t, t1, "Pool A - Match 1")

	// Row 10 is the one blank separator row between the two sections.
	for _, col := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		v, _ := f.GetCellValue(SheetKachinukiDetail, col+"10")
		assert.Empty(t, v, "row 10 is the blank separator between sections")
	}

	// Second match: title at row 11, right after the separator.
	t2, err := f.GetCellValue(SheetKachinukiDetail, "A11")
	require.NoError(t, err)
	assert.Contains(t, t2, "Pool A - Match 2")
}

// TestKachinukiMainSheetStillSummary is T198: when the workbook is built
// with kachinuki team matches the existing Pool Matches and Elimination
// Matches sheets are still present and structured as 8-column-per-court
// adding the detail sheet does not regress the main-sheet layout
// invariant (NFR-023).
//
// It creates the standard sheets as NewFileFromScratch does, adds the
// detail sheet with WriteKachinukiDetailSheet, and checks that doing so
// leaves Pool Matches / Elimination Matches present and usable.
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
// fighter columns (A/G) wide enough that a labelled, numbered name like
// "17 T12.4 Yui Nakamura (Fukusho)" (31 characters -- the bout number now
// leads every name cell, operator decision 2026-10-02) does not clip at
// this sheet's 12pt centred font; a LibreOffice render at 24 clipped the
// shorter, unnumbered "T3.4 Yui Nakamura (Chuken)". The print is fitted to
// one page wide (TestKachinukiDetailPageLayout), so the width costs the
// print nothing.
func TestKachinukiDetailFighterColumnsWideEnoughForALabelledName(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	left, err := f.GetColWidth(SheetKachinukiDetail, "A")
	require.NoError(t, err)
	right, err := f.GetColWidth(SheetKachinukiDetail, "G")
	require.NoError(t, err)
	assert.Equal(t, float64(kachinukiFighterColWidth), left, "the fighter column must be wide enough for a numbered, labelled name plus margin")
	assert.Equal(t, float64(kachinukiFighterColWidth), right, "the fighter column must be wide enough for a numbered, labelled name plus margin")
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

// TestBlankKachinukiSections pins the blank template's Kachinuki Detail
// sections: every pool match in order, then every knockout match round by
// round, titled and sided as its Elimination Matches block (a match's winner
// as "M n"), then the 3rd-place match between the semifinals' losers, each
// with the rows asked for. A bye is no match, so it gets no section.
func TestBlankKachinukiSections(t *testing.T) {
	ryu, tora, kame := Player{Name: "Ryu"}, Player{Name: "Tora"}, Player{Name: "Kame"}
	pools := []Pool{
		{PoolName: "Pool A", Matches: []Match{{SideA: &ryu, SideB: &tora}, {SideA: &ryu, SideB: &kame}}},
		{PoolName: "Pool B", Matches: []Match{{SideA: &tora, SideB: &kame}}},
	}
	rounds := func(leaves ...string) [][]*Node {
		r := BuildEliminationMatchRounds(CreateBalancedTree(leaves))
		AssignMatchNumbers(r)
		return r
	}
	blank := func(label, sideA, sideB string) KachinukiMatchDetail {
		return KachinukiMatchDetail{Label: label, SideATeam: sideA, SideBTeam: sideB, BlankBoutRows: 5}
	}

	t.Run("pools, then the rounds, then the 3rd place", func(t *testing.T) {
		got := BlankKachinukiSections(pools, rounds("Pool A-1st", "Pool B-2nd", "Pool B-1st", "Pool A-2nd"), true, 5)
		assert.Equal(t, []KachinukiMatchDetail{
			blank("Pool Match 1", "Ryu", "Tora"),
			blank("Pool Match 2", "Ryu", "Kame"),
			blank("Pool Match 3", "Tora", "Kame"),
			blank("Round 1 - Match 1", "Pool A-1st", "Pool B-2nd"),
			blank("Round 1 - Match 2", "Pool B-1st", "Pool A-2nd"),
			blank("Round 2 - Match 3", "M 1", "M 2"),
			blank(ThirdPlaceLabel, "M 1", "M 2"),
		}, got)
	})
	t.Run("a bye gets no section", func(t *testing.T) {
		got := BlankKachinukiSections(nil, rounds("Ryu", "Tora", "Kame"), false, 5)
		assert.Equal(t, []KachinukiMatchDetail{
			blank("Round 1 - Match 1", "Tora", "Kame"),
			blank("Round 2 - Match 2", "Ryu", "M 1"),
		}, got)
	})
}
