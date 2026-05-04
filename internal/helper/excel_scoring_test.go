package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// Row layout reference for court 1 (startCol=1, startRow=2):
//
// Individual (teamMatches=0), 2-player pool (1 match):
//   Row 4: score input  B4=lV(Victories)  C4=lP(Points)  D4=vs  E4=rP  F4=rV
//   Row 6: results header
//   Row 7: Alice (SideA, left)  B7=W  C7=L  D7=T  E7=PW  F7=PL
//   Row 8: Bob   (SideB, right) B8=W  C8=L  D8=T  E8=PW  F8=PL
//
// Individual, 3-player round-robin (3 matches):
//   Row 4: Match 0 Alice vs Bob
//   Row 5: Match 1 Bob vs Carol
//   Row 6: Match 2 Alice vs Carol
//   Row 8: results header
//   Row 9: Alice, Row 10: Bob, Row 11: Carol
//
// Team (teamMatches=1), 2-player pool (1 match):
//   Row 3: match header (Red/White)
//   Row 4: summary row   B4=IV_left  C4=PW_left  D4=vs  E4=PW_right  F4=IV_right
//   Row 5: sub-match 1   B5=lV  C5=lP  D5=vs  E5=rP  F5=rV
//   Row 8: Table 1 header (W/L/T at B8/C8/D8)
//   Row 9: Alice W/L/T  B9, C9, D9
//   Row 10: Bob  W/L/T  B10, C10, D10
//   Row 12: Table 2 header (IV/IL/IT/PW/PL at B12–F12)
//   Row 13: Alice IV/IL/IT/PW/PL  B13, C13, D13, E13, F13
//   Row 14: Bob   IV/IL/IT/PW/PL  B14, C14, D14, E14, F14
//
// NOTE: excelize's CalcCellValue does not vectorize functions like LEN/SUBSTITUTE
// over multi-row ranges inside SUMPRODUCT (e.g., SUMPRODUCT(LEN(B5:B6)) returns
// only LEN(B5), not LEN(B5)+LEN(B6)). Team tests therefore use teamMatches=1 so
// that all SUMPRODUCT ranges are single-cell (B5:B5 etc.) and evaluate correctly.

// scoringSetup2Players creates a 2-player, 1-match pool and calls PrintPoolMatches.
// SideA/SideB point to &pool.Players[0/1] — the same backing array used for the
// player→match-record map lookup — so all formula cells are populated correctly.
func scoringSetup2Players(t *testing.T, teamMatches int) *excelize.File {
	t.Helper()
	pool := Pool{
		PoolName:  "Pool A",
		sheetName: SheetPoolDraw,
		cell:      "B1",
		Players: []Player{
			{Name: "Alice", sheetName: SheetPoolDraw, cell: "A1"},
			{Name: "Bob", sheetName: SheetPoolDraw, cell: "A2"},
		},
	}
	pool.Matches = []Match{{SideA: &pool.Players[0], SideB: &pool.Players[1]}}

	f := excelize.NewFile()
	t.Cleanup(func() { f.Close() })
	f.NewSheet(SheetPoolMatches)
	f.NewSheet(SheetPoolDraw)
	PrintPoolMatches(f, []Pool{pool}, teamMatches, 1, 1, false)
	return f
}

// scoringSetup3PlayerRoundRobin creates a 3-player round-robin pool.
// Match order: Alice vs Bob (row 4), Bob vs Carol (row 5), Alice vs Carol (row 6).
func scoringSetup3PlayerRoundRobin(t *testing.T) *excelize.File {
	t.Helper()
	pool := Pool{
		PoolName:  "Pool A",
		sheetName: SheetPoolDraw,
		cell:      "B1",
		Players: []Player{
			{Name: "Alice", sheetName: SheetPoolDraw, cell: "A1"},
			{Name: "Bob", sheetName: SheetPoolDraw, cell: "A2"},
			{Name: "Carol", sheetName: SheetPoolDraw, cell: "A3"},
		},
	}
	pool.Matches = []Match{
		{SideA: &pool.Players[0], SideB: &pool.Players[1]},
		{SideA: &pool.Players[1], SideB: &pool.Players[2]},
		{SideA: &pool.Players[0], SideB: &pool.Players[2]},
	}

	f := excelize.NewFile()
	t.Cleanup(func() { f.Close() })
	f.NewSheet(SheetPoolMatches)
	f.NewSheet(SheetPoolDraw)
	PrintPoolMatches(f, []Pool{pool}, 0, 1, 1, false)
	return f
}

// calcScore evaluates a formula cell on SheetPoolMatches and returns its string value.
func calcScore(t *testing.T, f *excelize.File, cell string) string {
	t.Helper()
	v, err := f.CalcCellValue(SheetPoolMatches, cell)
	require.NoError(t, err, "CalcCellValue(%s)", cell)
	return v
}

// setScore places a value in a score-input cell on SheetPoolMatches.
func setScore(f *excelize.File, cell, value string) {
	f.SetCellValue(SheetPoolMatches, cell, value)
}

// TestIndividualPoolScoringFormulas verifies that the W/L/T/PW/PL formula cells
// in the pool results table compute correct values for typical scoring inputs.
// Score letters (M, K, D, T, H) each count as one point; "0", "-", and spaces
// are stripped and do not count. Ties require an explicit "X" in the vs column.
func TestIndividualPoolScoringFormulas(t *testing.T) {
	type expect struct{ w, l, t, pw, pl string }
	type tc struct {
		name  string
		setup func(*excelize.File)
		alice expect // SideA, left, row 7
		bob   expect // SideB, right, row 8
	}

	cases := []tc{
		{
			name:  "left wins",
			setup: func(f *excelize.File) { setScore(f, "B4", "M") },
			alice: expect{"1", "0", "0", "1", "0"},
			bob:   expect{"0", "1", "0", "0", "1"},
		},
		{
			name:  "right wins",
			setup: func(f *excelize.File) { setScore(f, "F4", "M") },
			alice: expect{"0", "1", "0", "0", "1"},
			bob:   expect{"1", "0", "0", "1", "0"},
		},
		{
			name:  "tie by uppercase X",
			setup: func(f *excelize.File) { setScore(f, "D4", "X") },
			alice: expect{"0", "0", "1", "0", "0"},
			bob:   expect{"0", "0", "1", "0", "0"},
		},
		{
			name:  "tie by lowercase x",
			setup: func(f *excelize.File) { setScore(f, "D4", "x") },
			alice: expect{"0", "0", "1", "0", "0"},
			bob:   expect{"0", "0", "1", "0", "0"},
		},
		{
			name:  "unplayed returns all zeros",
			setup: func(*excelize.File) {},
			alice: expect{"0", "0", "0", "0", "0"},
			bob:   expect{"0", "0", "0", "0", "0"},
		},
		{
			name: "multiple points left wins",
			setup: func(f *excelize.File) {
				setScore(f, "B4", "MM") // 2 points for left
				setScore(f, "F4", "K")  // 1 point for right
			},
			alice: expect{"1", "0", "0", "2", "1"},
			bob:   expect{"0", "1", "0", "1", "2"},
		},
		{
			// Entering "0" in a score cell marks a 0-0 tie without awarding points.
			name: "zero entry not counted as point",
			setup: func(f *excelize.File) {
				setScore(f, "B4", "0")
				setScore(f, "D4", "X")
			},
			alice: expect{"0", "0", "1", "0", "0"},
			bob:   expect{"0", "0", "1", "0", "0"},
		},
		{
			// Entering "-" is also used for 0-0 ties without points.
			name: "dash entry not counted as point",
			setup: func(f *excelize.File) {
				setScore(f, "B4", "-")
				setScore(f, "D4", "X")
			},
			alice: expect{"0", "0", "1", "0", "0"},
			bob:   expect{"0", "0", "1", "0", "0"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := scoringSetup2Players(t, 0)
			c.setup(f)

			assert.Equal(t, c.alice.w, calcScore(t, f, "B7"), "Alice W")
			assert.Equal(t, c.alice.l, calcScore(t, f, "C7"), "Alice L")
			assert.Equal(t, c.alice.t, calcScore(t, f, "D7"), "Alice T")
			assert.Equal(t, c.alice.pw, calcScore(t, f, "E7"), "Alice PW")
			assert.Equal(t, c.alice.pl, calcScore(t, f, "F7"), "Alice PL")

			assert.Equal(t, c.bob.w, calcScore(t, f, "B8"), "Bob W")
			assert.Equal(t, c.bob.l, calcScore(t, f, "C8"), "Bob L")
			assert.Equal(t, c.bob.t, calcScore(t, f, "D8"), "Bob T")
			assert.Equal(t, c.bob.pw, calcScore(t, f, "E8"), "Bob PW")
			assert.Equal(t, c.bob.pl, calcScore(t, f, "F8"), "Bob PL")
		})
	}
}

// TestIndividualPoolScoringFormulas_MultiMatch verifies that the W/L/T/PW/PL
// formulas accumulate correctly across multiple matches per player.
// A 3-player round-robin generates two match records per player, so the
// joinFormulas sum must produce correct totals.
func TestIndividualPoolScoringFormulas_MultiMatch(t *testing.T) {
	// Match 0 (Alice left vs Bob right):   row 4
	// Match 1 (Bob left vs Carol right):   row 5
	// Match 2 (Alice left vs Carol right): row 6
	// Results header: row 8
	// Alice row 9, Bob row 10, Carol row 11

	f := scoringSetup3PlayerRoundRobin(t)

	// Alice wins both her matches; Bob beats Carol.
	setScore(f, "B4", "M") // Alice beats Bob (Alice on left, B=lV)
	setScore(f, "B5", "M") // Bob beats Carol (Bob on left)
	setScore(f, "B6", "M") // Alice beats Carol (Alice on left)

	t.Run("Alice wins all", func(t *testing.T) {
		assert.Equal(t, "2", calcScore(t, f, "B9"), "Alice W")
		assert.Equal(t, "0", calcScore(t, f, "C9"), "Alice L")
		assert.Equal(t, "0", calcScore(t, f, "D9"), "Alice T")
		assert.Equal(t, "2", calcScore(t, f, "E9"), "Alice PW")
		assert.Equal(t, "0", calcScore(t, f, "F9"), "Alice PL")
	})

	t.Run("Bob wins one loses one", func(t *testing.T) {
		assert.Equal(t, "1", calcScore(t, f, "B10"), "Bob W")
		assert.Equal(t, "1", calcScore(t, f, "C10"), "Bob L")
		assert.Equal(t, "0", calcScore(t, f, "D10"), "Bob T")
		assert.Equal(t, "1", calcScore(t, f, "E10"), "Bob PW")
		assert.Equal(t, "1", calcScore(t, f, "F10"), "Bob PL")
	})

	t.Run("Carol loses all", func(t *testing.T) {
		assert.Equal(t, "0", calcScore(t, f, "B11"), "Carol W")
		assert.Equal(t, "2", calcScore(t, f, "C11"), "Carol L")
		assert.Equal(t, "0", calcScore(t, f, "D11"), "Carol T")
		assert.Equal(t, "0", calcScore(t, f, "E11"), "Carol PW")
		assert.Equal(t, "2", calcScore(t, f, "F11"), "Carol PL")
	})
}

// TestTeamSummaryRowFormulas verifies the SUMPRODUCT formulas on the team-match
// summary row (row 4 for teamMatches=1):
//
//	B4 = IV_left  (individual victories for left side)
//	C4 = PW_left  (total points scored by left side)
//	E4 = PW_right
//	F4 = IV_right
//
// The sub-match input row is row 5: B5=lV, C5=lP, D5=vs, E5=rP, F5=rV.
//
// Uses teamMatches=1 so SUMPRODUCT ranges are single-cell (B5:B5 etc.), which
// excelize's CalcCellValue evaluates correctly.
func TestTeamSummaryRowFormulas(t *testing.T) {
	type expect struct{ ivLeft, ivRight, pwLeft, pwRight string }
	type tc struct {
		name  string
		setup func(*excelize.File)
		exp   expect
	}

	cases := []tc{
		{
			name:  "left wins sub-match",
			setup: func(f *excelize.File) { setScore(f, "B5", "M") },
			exp:   expect{"1", "0", "1", "0"},
		},
		{
			name:  "right wins sub-match",
			setup: func(f *excelize.File) { setScore(f, "F5", "M") },
			exp:   expect{"0", "1", "0", "1"},
		},
		{
			// "X" in the sub-match vs column excludes that sub from IV counts.
			name:  "tied sub-match does not count as IV win for either side",
			setup: func(f *excelize.File) { setScore(f, "D5", "X") },
			exp:   expect{"0", "0", "0", "0"},
		},
		{
			name:  "multiple score letters count in PW",
			setup: func(f *excelize.File) { setScore(f, "B5", "MK") },
			exp:   expect{"1", "0", "2", "0"},
		},
		{
			// "0" in a score cell should not add to PW; D5="X" marks the sub as tied.
			name: "zero not counted as point in PW",
			setup: func(f *excelize.File) {
				setScore(f, "B5", "0")
				setScore(f, "D5", "X")
			},
			exp: expect{"0", "0", "0", "0"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := scoringSetup2Players(t, 1)
			c.setup(f)

			assert.Equal(t, c.exp.ivLeft, calcScore(t, f, "B4"), "IV_left (B4)")
			assert.Equal(t, c.exp.ivRight, calcScore(t, f, "F4"), "IV_right (F4)")
			assert.Equal(t, c.exp.pwLeft, calcScore(t, f, "C4"), "PW_left (C4)")
			assert.Equal(t, c.exp.pwRight, calcScore(t, f, "E4"), "PW_right (E4)")
		})
	}
}

// TestTeamWLTTableFormulas verifies the W/L/T cells in Table 1 of the team pool
// results section (rows 9–10 for teamMatches=1):
//
//	Alice (left): B9=W, C9=L, D9=T
//	Bob  (right): B10=W, C10=L, D10=T
//
// Team match outcome: higher IV wins; equal IV → higher PW wins; still equal →
// organizer must enter "X" in D4 (summary vs column) to record a tie.
// Ties at the individual sub-match level (D5="X") only exclude that sub from IV;
// they do NOT automatically mark the overall team match as a draw.
func TestTeamWLTTableFormulas(t *testing.T) {
	type expect struct{ w, l, t string }
	type tc struct {
		name  string
		setup func(*excelize.File)
		alice expect
		bob   expect
	}

	cases := []tc{
		{
			name:  "left wins by IV",
			setup: func(f *excelize.File) { setScore(f, "B5", "M") },
			alice: expect{"1", "0", "0"},
			bob:   expect{"0", "1", "0"},
		},
		{
			name:  "right wins by IV",
			setup: func(f *excelize.File) { setScore(f, "F5", "M") },
			alice: expect{"0", "1", "0"},
			bob:   expect{"1", "0", "0"},
		},
		{
			// Organizer enters "X" in the SUMMARY row's vs column (D4) to record a team tie.
			name:  "team tie by explicit X in summary row D4",
			setup: func(f *excelize.File) { setScore(f, "D4", "X") },
			alice: expect{"0", "0", "1"},
			bob:   expect{"0", "0", "1"},
		},
		{
			// Equal IV (both 0) and equal PW without D4="X" → neither W nor T is recorded.
			// The organizer must explicitly mark D4="X" to record the tie in the table.
			name: "equal IV and PW without X shows zeros",
			setup: func(f *excelize.File) {
				setScore(f, "D5", "X") // sub tied → IV=0 for both sides
				setScore(f, "B5", "M")
				setScore(f, "F5", "M") // equal points
			},
			alice: expect{"0", "0", "0"},
			bob:   expect{"0", "0", "0"},
		},
		{
			// Sub-match is tied (D5="X") so IV=0:0; left scored more total points
			// (PW_left=2 > PW_right=1) so left wins the team match by tiebreak.
			name: "left wins by PW tiebreak when IV equal",
			setup: func(f *excelize.File) {
				setScore(f, "D5", "X") // sub tied → IV=0:0
				setScore(f, "B5", "MK")
				setScore(f, "F5", "M") // PW_left=2, PW_right=1
			},
			alice: expect{"1", "0", "0"},
			bob:   expect{"0", "1", "0"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := scoringSetup2Players(t, 1)
			c.setup(f)

			assert.Equal(t, c.alice.w, calcScore(t, f, "B9"), "Alice W")
			assert.Equal(t, c.alice.l, calcScore(t, f, "C9"), "Alice L")
			assert.Equal(t, c.alice.t, calcScore(t, f, "D9"), "Alice T")

			assert.Equal(t, c.bob.w, calcScore(t, f, "B10"), "Bob W")
			assert.Equal(t, c.bob.l, calcScore(t, f, "C10"), "Bob L")
			assert.Equal(t, c.bob.t, calcScore(t, f, "D10"), "Bob T")
		})
	}
}

// TestTeamIVILITPWPLTableFormulas verifies the IV/IL/IT/PW/PL cells in Table 2
// of the team pool results section (rows 13–14 for teamMatches=1):
//
//	Alice (left): B13=IV, C13=IL, D13=IT, E13=PW, F13=PL
//	Bob  (right): B14=IV, C14=IL, D14=IT, E14=PW, F14=PL
//
// IV/IL are derived from the summary-row IV formulas (B4/F4); PW/PL reference
// the summary-row PW columns (C4/E4). IT counts sub-match rows with "X"/"x" in D.
//
// Note: IT only registers when the match is considered "played" (at least one
// score letter entered, or D4="X"). A tied sub-match (D5="X") alone does not
// set played=true; a score entry in the same match is also needed.
func TestTeamIVILITPWPLTableFormulas(t *testing.T) {
	type expect struct{ iv, il, it, pw, pl string }
	type tc struct {
		name  string
		setup func(*excelize.File)
		alice expect
		bob   expect
	}

	cases := []tc{
		{
			name:  "left wins sub-match",
			setup: func(f *excelize.File) { setScore(f, "B5", "M") },
			// Alice: IV=1, IL=0, IT=0, PW=1 (M), PL=0
			// Bob:   IV=0, IL=1, IT=0, PW=0, PL=1
			alice: expect{"1", "0", "0", "1", "0"},
			bob:   expect{"0", "1", "0", "0", "1"},
		},
		{
			// Sub-match is tied (D5="X") and left scored one point (B5="M").
			// IT=1 for both; IV=0 because the tied sub is excluded from IV counts.
			// PW uses total score letters regardless of D5; PL is the opponent's PW.
			name: "tied sub-match counts in IT; score letters still count in PW",
			setup: func(f *excelize.File) {
				setScore(f, "D5", "X") // sub tied
				setScore(f, "B5", "M") // left scored — needed to set played=true
			},
			alice: expect{"0", "0", "1", "1", "0"},
			bob:   expect{"0", "0", "1", "0", "1"},
		},
		{
			name:  "unplayed match returns all zeros",
			setup: func(*excelize.File) {},
			alice: expect{"0", "0", "0", "0", "0"},
			bob:   expect{"0", "0", "0", "0", "0"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := scoringSetup2Players(t, 1)
			c.setup(f)

			assert.Equal(t, c.alice.iv, calcScore(t, f, "B13"), "Alice IV")
			assert.Equal(t, c.alice.il, calcScore(t, f, "C13"), "Alice IL")
			assert.Equal(t, c.alice.it, calcScore(t, f, "D13"), "Alice IT")
			assert.Equal(t, c.alice.pw, calcScore(t, f, "E13"), "Alice PW")
			assert.Equal(t, c.alice.pl, calcScore(t, f, "F13"), "Alice PL")

			assert.Equal(t, c.bob.iv, calcScore(t, f, "B14"), "Bob IV")
			assert.Equal(t, c.bob.il, calcScore(t, f, "C14"), "Bob IL")
			assert.Equal(t, c.bob.it, calcScore(t, f, "D14"), "Bob IT")
			assert.Equal(t, c.bob.pw, calcScore(t, f, "E14"), "Bob PW")
			assert.Equal(t, c.bob.pl, calcScore(t, f, "F14"), "Bob PL")
		})
	}
}
