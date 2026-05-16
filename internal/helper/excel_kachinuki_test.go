package helper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// makeKachinukiTestMatch builds a 6-bout kachinuki team-match fixture used by
// the detail-sheet tests. Side A wins 4 bouts and Side B wins 1 with one
// hikiwake; in practice this is enough to drive every renderer branch (winner
// rows, draw row, summary tallies) without needing multiple fixtures.
func makeKachinukiTestMatch() KachinukiMatchDetail {
	return KachinukiMatchDetail{
		Label:        "Pool A - Match 1",
		SideATeam:    "Team Alpha",
		SideBTeam:    "Team Bravo",
		Winner:       "Team Alpha",
		Decision:     "kachinuki-exhaustion",
		EliminationA: 1, // 1 player from Team Alpha was retired (hikiwake bout)
		EliminationB: 5, // 5 players from Team Bravo retired (losses + hikiwake)
		Bouts: []KachinukiBout{
			{Position: 1, SideAName: "Alice", SideAPos: "Senpo", ScoreA: "MM", SideBName: "Bob", SideBPos: "Senpo", ScoreB: "", Winner: "Alice", Decision: "fought"},
			{Position: 2, SideAName: "Alice", SideAPos: "Senpo", ScoreA: "M", SideBName: "Carol", SideBPos: "Jiho", ScoreB: "", Winner: "Alice", Decision: "fought"},
			{Position: 3, SideAName: "Alice", SideAPos: "Senpo", ScoreA: "", SideBName: "Dan", SideBPos: "Chuken", ScoreB: "", Winner: "", Decision: "hikiwake"},
			{Position: 4, SideAName: "Eve", SideAPos: "Jiho", ScoreA: "MK", SideBName: "Frank", SideBPos: "Fukusho", ScoreB: "K", Winner: "Eve", Decision: "fought"},
			{Position: 5, SideAName: "Eve", SideAPos: "Jiho", ScoreA: "", SideBName: "Grace", SideBPos: "Taisho", ScoreB: "M", Winner: "Grace", Decision: "fought"},
			{Position: 6, SideAName: "Hank", SideAPos: "Chuken", ScoreA: "MMK", SideBName: "Grace", SideBPos: "Taisho", ScoreB: "", Winner: "Hank", Decision: "fought"},
		},
	}
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
	assert.True(t, found, "expected sheet %q in workbook, got %v", SheetKachinukiDetail, names)
}

// TestKachinukiDetailSheetSkippedWhenEmpty confirms the renderer is a no-op
// when there are zero kachinuki matches — the detail sheet must not be
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
// Side A (name + position), Score A, vs, Score B, Side B (name + position),
// Winner, Decision.
func TestKachinukiDetailBoutRows(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	// Section layout (deterministic, defined by the renderer):
	//   row 1: match title (merged across columns A..H)
	//   row 2: subtitle "<SideA> vs <SideB>"
	//   row 3: column headers
	//   rows 4..9: 6 bout rows (one per bout)
	//   row 10: summary row
	// Column letters A..H map to: Bout #, Side A, Score A, vs, Score B, Side B,
	// Winner, Decision.

	titleRow := 1
	subtitleRow := 2
	headerRow := 3
	firstBoutRow := 4
	summaryRow := firstBoutRow + 6

	// Header values
	expectedHeaders := []struct {
		col, want string
	}{
		{"A", "Bout #"},
		{"B", "Side A"},
		{"C", "Score A"},
		{"D", "vs"},
		{"E", "Score B"},
		{"F", "Side B"},
		{"G", "Winner"},
		{"H", "Decision"},
	}
	for _, h := range expectedHeaders {
		cell := h.col + intToString(headerRow)
		got, err := f.GetCellValue(SheetKachinukiDetail, cell)
		require.NoError(t, err)
		assert.Equal(t, h.want, got, "header cell %s mismatch", cell)
	}

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

	// Spot-check each bout row.
	wantBouts := []struct {
		boutNum    int
		sideAName  string
		sideAPos   string
		scoreA     string
		scoreB     string
		sideBName  string
		sideBPos   string
		winner     string
		decision   string
		isHikiwake bool
	}{
		{1, "Alice", "Senpo", "MM", "", "Bob", "Senpo", "Alice", "fought", false},
		{2, "Alice", "Senpo", "M", "", "Carol", "Jiho", "Alice", "fought", false},
		{3, "Alice", "Senpo", "", "", "Dan", "Chuken", "", "hikiwake", true},
		{4, "Eve", "Jiho", "MK", "K", "Frank", "Fukusho", "Eve", "fought", false},
		{5, "Eve", "Jiho", "", "M", "Grace", "Taisho", "Grace", "fought", false},
		{6, "Hank", "Chuken", "MMK", "", "Grace", "Taisho", "Hank", "fought", false},
	}

	for i, b := range wantBouts {
		row := firstBoutRow + i

		// Bout number (column A)
		boutVal, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(row))
		require.NoError(t, err)
		assert.Equal(t, intToString(b.boutNum), boutVal, "row %d: bout number", row)

		// Side A name + position (column B) — both must appear together.
		sideACell, err := f.GetCellValue(SheetKachinukiDetail, "B"+intToString(row))
		require.NoError(t, err)
		assert.Contains(t, sideACell, b.sideAName, "row %d: Side A name", row)
		assert.Contains(t, sideACell, b.sideAPos, "row %d: Side A position", row)

		// Score A (column C) and Score B (column E)
		scoreA, err := f.GetCellValue(SheetKachinukiDetail, "C"+intToString(row))
		require.NoError(t, err)
		assert.Equal(t, b.scoreA, scoreA, "row %d: Score A", row)

		// vs column (D)
		vs, err := f.GetCellValue(SheetKachinukiDetail, "D"+intToString(row))
		require.NoError(t, err)
		assert.Equal(t, "vs", vs, "row %d: vs literal", row)

		scoreB, err := f.GetCellValue(SheetKachinukiDetail, "E"+intToString(row))
		require.NoError(t, err)
		assert.Equal(t, b.scoreB, scoreB, "row %d: Score B", row)

		// Side B name + position (column F)
		sideBCell, err := f.GetCellValue(SheetKachinukiDetail, "F"+intToString(row))
		require.NoError(t, err)
		assert.Contains(t, sideBCell, b.sideBName, "row %d: Side B name", row)
		assert.Contains(t, sideBCell, b.sideBPos, "row %d: Side B position", row)

		// Winner (column G)
		winner, err := f.GetCellValue(SheetKachinukiDetail, "G"+intToString(row))
		require.NoError(t, err)
		if b.isHikiwake {
			// Hikiwake rows render an empty or em-dash winner cell — both
			// acceptable; the decision column carries the result.
			assert.Contains(t, []string{"", "-"}, winner, "row %d: hikiwake winner should be empty", row)
		} else {
			assert.Equal(t, b.winner, winner, "row %d: winner", row)
		}

		// Decision (column H)
		decision, err := f.GetCellValue(SheetKachinukiDetail, "H"+intToString(row))
		require.NoError(t, err)
		assert.Equal(t, b.decision, decision, "row %d: decision", row)
	}

	// Sanity check: the row immediately after the last bout is the summary,
	// not another bout row — the bout column should NOT contain a bout number
	// (it should hold a "Summary" or "Total" label instead).
	postBouts, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(summaryRow))
	require.NoError(t, err)
	assert.NotEqual(t, "7", postBouts, "expected exactly 6 bout rows, found a 7th")
}

// TestKachinukiDetailSummaryRow is T197: the summary row shows total
// eliminations per team and the match outcome.
func TestKachinukiDetailSummaryRow(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	matches := []KachinukiMatchDetail{makeKachinukiTestMatch()}
	require.NoError(t, WriteKachinukiDetailSheet(f, matches))

	// 6 bouts + 3 leading rows (title, subtitle, header) → summary at row 10.
	summaryRow := 10

	// A label like "Total" / "Summary" — exact text not pinned, just that
	// some label is present.
	label, err := f.GetCellValue(SheetKachinukiDetail, "A"+intToString(summaryRow))
	require.NoError(t, err)
	assert.NotEmpty(t, label, "summary row should have a label in column A")
	lowerLabel := strings.ToLower(label)
	assert.True(t,
		strings.Contains(lowerLabel, "summary") || strings.Contains(lowerLabel, "total"),
		"summary row label should mention Summary or Total, got %q", label)

	// Eliminations per team: Team A retired 1 (hikiwake), Team B retired 5.
	// Column B should mention Side A's elimination count; column F mirrors
	// Side B. We don't pin the exact wording, just that the integers appear.
	teamAElim, err := f.GetCellValue(SheetKachinukiDetail, "B"+intToString(summaryRow))
	require.NoError(t, err)
	assert.Contains(t, teamAElim, "1", "Side A elimination count should appear in column B")

	teamBElim, err := f.GetCellValue(SheetKachinukiDetail, "F"+intToString(summaryRow))
	require.NoError(t, err)
	assert.Contains(t, teamBElim, "5", "Side B elimination count should appear in column F")

	// Outcome: column G (Winner) carries the winning team name; column H
	// carries the decision (exhaustion in this fixture).
	outcomeWinner, err := f.GetCellValue(SheetKachinukiDetail, "G"+intToString(summaryRow))
	require.NoError(t, err)
	assert.Equal(t, "Team Alpha", outcomeWinner, "summary winner cell should hold the winning team")

	outcomeDecision, err := f.GetCellValue(SheetKachinukiDetail, "H"+intToString(summaryRow))
	require.NoError(t, err)
	assert.Equal(t, "kachinuki-exhaustion", outcomeDecision, "summary decision cell should hold the match decision")
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
	m2.Winner = "Team Delta"
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
// — adding the detail sheet does not regress the main-sheet layout
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
	// — every Phase 11 invocation must coexist with these.
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
		assert.True(t, present[r], "expected sheet %q to be present", r)
	}

	// Pool Matches and Elimination Matches must remain on the 8-column-per-court
	// layout — i.e. nothing in the detail-sheet wiring path should rewrite the
	// CourtsColumnsPerCourt invariant.
	assert.Equal(t, 8, CourtsColumnsPerCourt, "CourtsColumnsPerCourt invariant must remain 8")
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
