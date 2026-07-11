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
// SideA/SideB point to &pool.Players[0/1], the same backing array used for the
// player→match-record map lookup, so all formula cells are populated correctly.
// Pass engi=true to exercise the engi flag-scoring column layout (same cell geometry
// as engi=false; only standings columns differ: W/L/Flags/Rank instead of W/L/T/PW/PL/Rank).
//
// Individual (teamMatches=0), engi=false, 2-player pool (1 match):
//
//	Row 4: score input  B4=lV(Victories)  C4=lP(Points)  D4=vs  E4=rP  F4=rV
//	Row 6: results header
//	Row 7: Alice (SideA, left)  B7=W  C7=L  D7=T  E7=PW  F7=PL
//	Row 8: Bob   (SideB, right) B8=W  C8=L  D8=T  E8=PW  F8=PL
//
// Individual (teamMatches=0), engi=true:
//
//	Row 4: score input  B4=lFlags  D4=vs  F4=rFlags  (C, E blank)
//	Row 6: results header (W / L / Flags / Rank)
//	Row 7: Alice  B7=W  C7=L  D7=Flags  G7=Rank
//	Row 8: Bob    B8=W  C8=L  D8=Flags  G8=Rank
func scoringSetup2Players(t *testing.T, teamMatches int, engi bool) *excelize.File {
	t.Helper()
	pool := Pool{
		PoolName: "Pool A",
		Players: []Player{
			{Name: "Alice"},
			{Name: "Bob"},
		},
	}
	pool.Matches = []Match{{SideA: &pool.Players[0], SideB: &pool.Players[1]}}

	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetPoolDraw, cell: "B1"},
	}
	pCoords := map[string]playerCellCoord{
		playerCoordKey(pool.Players[0]): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
		playerCoordKey(pool.Players[1]): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A2"}},
	}

	f := excelize.NewFile()
	t.Cleanup(func() { f.Close() })
	f.NewSheet(SheetPoolMatches)
	f.NewSheet(SheetPoolDraw)
	PrintPoolMatches(f, []Pool{pool}, teamMatches, 1, 1, false, poolCoords, pCoords, engi)
	return f
}

// scoringSetup3PlayerRoundRobin creates a 3-player round-robin pool.
// Pass engi=true to exercise the engi flag-scoring column layout.
// Match order: Alice vs Bob (row 4), Bob vs Carol (row 5), Alice vs Carol (row 6).
//
// Non-engi standings (engi=false):
//
//	Row 8: results header (W / L / T / PW / PL / Rank)
//	Row 9: Alice, Row 10: Bob, Row 11: Carol
//
// Engi standings (engi=true): B=W, C=L, D=Flags, G=Rank, U=hidden Score.
// PW/PL columns (E, F) are intentionally left blank in engi mode.
func scoringSetup3PlayerRoundRobin(t *testing.T, engi bool) *excelize.File {
	t.Helper()
	pool := Pool{
		PoolName: "Pool A",
		Players: []Player{
			{Name: "Alice"},
			{Name: "Bob"},
			{Name: "Carol"},
		},
	}
	pool.Matches = []Match{
		{SideA: &pool.Players[0], SideB: &pool.Players[1]},
		{SideA: &pool.Players[1], SideB: &pool.Players[2]},
		{SideA: &pool.Players[0], SideB: &pool.Players[2]},
	}

	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetPoolDraw, cell: "B1"},
	}
	pCoords := map[string]playerCellCoord{
		playerCoordKey(pool.Players[0]): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
		playerCoordKey(pool.Players[1]): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A2"}},
		playerCoordKey(pool.Players[2]): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A3"}},
	}

	f := excelize.NewFile()
	t.Cleanup(func() { f.Close() })
	f.NewSheet(SheetPoolMatches)
	f.NewSheet(SheetPoolDraw)
	PrintPoolMatches(f, []Pool{pool}, 0, 1, 1, false, poolCoords, pCoords, engi)
	return f
}

// calcScore evaluates a formula cell on SheetPoolMatches and returns its string value.
func calcScore(t *testing.T, f *excelize.File, cell string) string {
	t.Helper()
	v, err := f.CalcCellValue(SheetPoolMatches, cell)
	require.NoErrorf(t, err, "CalcCellValue(%s)", cell)
	return v
}

// setScore places a value in a score-input cell on SheetPoolMatches.
func setScore(f *excelize.File, cell, value string) {
	f.SetCellValue(SheetPoolMatches, cell, value)
}

// TestIndividualPoolScoringFormulas verifies that the W/L/T/PW/PL formula cells
// in the pool results table compute correct values for typical scoring inputs.
// Score letters (M, K, D, T, H) each count as one point; "0", "-", and spaces
// are stripped and do not count. Ties are detected when "X"/"x" is in the vs
// column OR when both sides finish with equal score character counts (auto-tie).
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
		{
			// Equal non-zero scores on both sides → auto-detected tie, no X needed.
			name: "equal non-zero scores auto-detected as tie",
			setup: func(f *excelize.File) {
				setScore(f, "B4", "M")
				setScore(f, "F4", "M")
			},
			alice: expect{"0", "0", "1", "1", "1"},
			bob:   expect{"0", "0", "1", "1", "1"},
		},
		{
			// Both sides enter "0" (explicit 0-0) → auto-detected tie, no X needed.
			name: "both zero scores (0-0 played) auto-detected as tie",
			setup: func(f *excelize.File) {
				setScore(f, "B4", "0")
				setScore(f, "F4", "0")
			},
			alice: expect{"0", "0", "1", "0", "0"},
			bob:   expect{"0", "0", "1", "0", "0"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := scoringSetup2Players(t, 0, false)
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

// TestEngiPoolScoringFormulas verifies that the W/L/Flags formula cells in
// the pool results table compute correct values when engi=true.
// Engi scores are integer flag counts (not ippon letters), so the formulas
// must use ISNUMBER-based numeric comparison, not LEN/SUBSTITUTE character
// counting. Ties are impossible in engi (flag totals are always odd).
//
// Cell layout (same as non-engi, 2-player pool):
//
//	Row 4: score input  B4=left-flags  F4=right-flags
//	Row 6: results header (W / L / Flags / Rank)
//	Row 7: Alice (SideA, left)  B7=W  C7=L  D7=Flags
//	Row 8: Bob   (SideB, right) B8=W  C8=L  D8=Flags
func TestEngiPoolScoringFormulas(t *testing.T) {
	type expect struct{ w, l, flags string }
	type tc struct {
		name  string
		setup func(*excelize.File)
		alice expect
		bob   expect
	}

	cases := []tc{
		{
			name: "left wins 3-2",
			setup: func(f *excelize.File) {
				f.SetCellValue(SheetPoolMatches, "B4", 3)
				f.SetCellValue(SheetPoolMatches, "F4", 2)
			},
			alice: expect{"1", "0", "3"},
			bob:   expect{"0", "1", "2"},
		},
		{
			name: "left wins 5-0",
			setup: func(f *excelize.File) {
				f.SetCellValue(SheetPoolMatches, "B4", 5)
				f.SetCellValue(SheetPoolMatches, "F4", 0)
			},
			alice: expect{"1", "0", "5"},
			bob:   expect{"0", "1", "0"},
		},
		{
			// Robustness: a played bout where one side holds a non-numeric
			// string must treat that side as 0 flags. N() coercion guarantees
			// N("x")=0, so Alice's 3 still beats Bob's stray text.
			name: "non-numeric opponent cell treated as zero flags",
			setup: func(f *excelize.File) {
				f.SetCellValue(SheetPoolMatches, "B4", 3)
				f.SetCellValue(SheetPoolMatches, "F4", "x")
			},
			alice: expect{"1", "0", "3"},
			bob:   expect{"0", "1", "0"},
		},
		{
			name:  "unplayed returns all zeros",
			setup: func(*excelize.File) {},
			alice: expect{"0", "0", "0"},
			bob:   expect{"0", "0", "0"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := scoringSetup2Players(t, 0, true)
			c.setup(f)

			assert.Equal(t, c.alice.w, calcScore(t, f, "B7"), "Alice W")
			assert.Equal(t, c.alice.l, calcScore(t, f, "C7"), "Alice L")
			assert.Equal(t, c.alice.flags, calcScore(t, f, "D7"), "Alice Flags")

			assert.Equal(t, c.bob.w, calcScore(t, f, "B8"), "Bob W")
			assert.Equal(t, c.bob.l, calcScore(t, f, "C8"), "Bob L")
			assert.Equal(t, c.bob.flags, calcScore(t, f, "D8"), "Bob Flags")
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

	f := scoringSetup3PlayerRoundRobin(t, false)

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
			f := scoringSetup2Players(t, 1, false)
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
// the match is automatically a draw (T=1). "X" in D4 also forces a draw.
// Ties at the individual sub-match level (D5="X" or equal sub-match scores)
// only exclude that sub from IV counts; the team-level draw is determined
// independently from the summary-row IV and PW totals.
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
			// Equal IV and equal PW → auto-detected team draw, no X needed.
			name: "equal IV and PW auto-detected as team tie",
			setup: func(f *excelize.File) {
				setScore(f, "D5", "X") // sub tied → IV=0 for both sides
				setScore(f, "B5", "M")
				setScore(f, "F5", "M") // equal points
			},
			alice: expect{"0", "0", "1"},
			bob:   expect{"0", "0", "1"},
		},
		{
			// Equal sub-match scores (no X) → auto-detected as team tie at both sub and team level.
			name: "equal sub-match scores auto-detected as team tie",
			setup: func(f *excelize.File) {
				setScore(f, "B5", "M")
				setScore(f, "F5", "M")
			},
			alice: expect{"0", "0", "1"},
			bob:   expect{"0", "0", "1"},
		},
		{
			// All sub-matches marked X with no score entries → team match is played and T=1.
			// This is the common case where all individual fights are 0-0 draws.
			name: "all sub-matches X with no scores gives team tie",
			setup: func(f *excelize.File) {
				setScore(f, "D5", "X")
			},
			alice: expect{"0", "0", "1"},
			bob:   expect{"0", "0", "1"},
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
			f := scoringSetup2Players(t, 1, false)
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
// the summary-row PW columns (C4/E4). IT counts sub-match rows where "X"/"x"
// is in D, or where the sub-match has been played and both sides have equal scores.
//
// Note: IT only registers when the team match is considered "played" (at least one
// score cell in the sub-match rows is filled, or D4="X"). A tied sub-match (D5="X")
// alone does not set played=true; a score entry in the same match is also needed.
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
				setScore(f, "D5", "X") // sub tied, D5="X" alone sets played=true
				setScore(f, "B5", "M")
			},
			alice: expect{"0", "0", "1", "1", "0"},
			bob:   expect{"0", "0", "1", "0", "1"},
		},
		{
			// D5="X" alone (no score entries) marks the sub-match as played.
			// IT=1 for both; PW=PL=0 since no score letters were entered.
			name: "sub-match X alone sets played and counts as IT",
			setup: func(f *excelize.File) {
				setScore(f, "D5", "X")
			},
			alice: expect{"0", "0", "1", "0", "0"},
			bob:   expect{"0", "0", "1", "0", "0"},
		},
		{
			name:  "unplayed match returns all zeros",
			setup: func(*excelize.File) {},
			alice: expect{"0", "0", "0", "0", "0"},
			bob:   expect{"0", "0", "0", "0", "0"},
		},
		{
			// Equal scores in sub-match auto-detected as IT without X.
			// IV=0 for both sides since equal scores don't count as a win.
			name: "equal sub-match scores auto-detected as IT",
			setup: func(f *excelize.File) {
				setScore(f, "B5", "M")
				setScore(f, "F5", "M")
			},
			alice: expect{"0", "0", "1", "1", "1"},
			bob:   expect{"0", "0", "1", "1", "1"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := scoringSetup2Players(t, 1, false)
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

// TestEngiPoolScoringFormulas_MultiMatch verifies that the W/L/Flags formula
// cells accumulate correctly across multiple matches per player in a 3-player
// engi round-robin pool. Each player appears in two matches; the formulas must
// correctly sum wins and flag totals from both records.
//
// Match setup (Alice beats Bob 3-2; Carol beats Bob 4-1; Alice beats Carol 5-0):
//
//	Row 4: Alice (left, B4=3) vs Bob  (right, F4=2)
//	Row 5: Bob   (left, B5=1) vs Carol(right, F5=4)
//	Row 6: Alice (left, B6=5) vs Carol(right, F6=0)
//
// Standings: Row 8 header; Row 9=Alice, Row 10=Bob, Row 11=Carol.
// Columns: B=W, C=L, D=Flags.
func TestEngiPoolScoringFormulas_MultiMatch(t *testing.T) {
	f := scoringSetup3PlayerRoundRobin(t, true)

	// Alice beats Bob 3-2.
	f.SetCellValue(SheetPoolMatches, "B4", 3) // Alice (left) flags
	f.SetCellValue(SheetPoolMatches, "F4", 2) // Bob   (right) flags
	// Carol beats Bob 4-1: Match 1 row is Bob (left) vs Carol (right).
	f.SetCellValue(SheetPoolMatches, "B5", 1) // Bob   (left) flags
	f.SetCellValue(SheetPoolMatches, "F5", 4) // Carol (right) flags
	// Alice beats Carol 5-0: Match 2 row is Alice (left) vs Carol (right).
	f.SetCellValue(SheetPoolMatches, "B6", 5) // Alice (left) flags
	f.SetCellValue(SheetPoolMatches, "F6", 0) // Carol (right) flags

	t.Run("Alice wins both accumulates W=2 Flags=8", func(t *testing.T) {
		assert.Equal(t, "2", calcScore(t, f, "B9"), "Alice W")
		assert.Equal(t, "0", calcScore(t, f, "C9"), "Alice L")
		assert.Equal(t, "8", calcScore(t, f, "D9"), "Alice Flags (3+5)")
	})

	t.Run("Bob loses both accumulates W=0 Flags=3", func(t *testing.T) {
		assert.Equal(t, "0", calcScore(t, f, "B10"), "Bob W")
		assert.Equal(t, "2", calcScore(t, f, "C10"), "Bob L")
		assert.Equal(t, "3", calcScore(t, f, "D10"), "Bob Flags (2+1)")
	})

	t.Run("Carol one win one loss Flags=4", func(t *testing.T) {
		assert.Equal(t, "1", calcScore(t, f, "B11"), "Carol W")
		assert.Equal(t, "1", calcScore(t, f, "C11"), "Carol L")
		assert.Equal(t, "4", calcScore(t, f, "D11"), "Carol Flags (4+0)")
	})
}

// TestEngiScoreAndRankFormulas verifies that the hidden Score cell (column U)
// and the visible Rank cell (column G) compute correct values in a 3-player
// engi round-robin pool.
//
// Score formula: (W*1000000)+(Flags). Rank formula: RANK+COUNTIF breaking
// ties by row order (first occurrence gets the lower rank number).
//
// Using the same match setup as TestEngiPoolScoringFormulas_MultiMatch:
// Alice W=2 Flags=8, Carol W=1 Flags=4, Bob W=0 Flags=3.
// Expected scores: Alice=2000008, Carol=1000004, Bob=3.
// Expected ranks:  Alice=1, Carol=2, Bob=3.
func TestEngiScoreAndRankFormulas(t *testing.T) {
	f := scoringSetup3PlayerRoundRobin(t, true)

	f.SetCellValue(SheetPoolMatches, "B4", 3)
	f.SetCellValue(SheetPoolMatches, "F4", 2)
	f.SetCellValue(SheetPoolMatches, "B5", 1)
	f.SetCellValue(SheetPoolMatches, "F5", 4)
	f.SetCellValue(SheetPoolMatches, "B6", 5)
	f.SetCellValue(SheetPoolMatches, "F6", 0)

	t.Run("hidden Score cells", func(t *testing.T) {
		assert.Equal(t, "2000008", calcScore(t, f, "U9"), "Alice Score (2*1000000+8)")
		assert.Equal(t, "1000004", calcScore(t, f, "U11"), "Carol Score (1*1000000+4)")
		assert.Equal(t, "3", calcScore(t, f, "U10"), "Bob Score (0*1000000+3)")
	})

	t.Run("Rank cells reflect win-then-flag ordering", func(t *testing.T) {
		assert.Equal(t, "1", calcScore(t, f, "G9"), "Alice Rank")
		assert.Equal(t, "2", calcScore(t, f, "G11"), "Carol Rank")
		assert.Equal(t, "3", calcScore(t, f, "G10"), "Bob Rank")
	})
}

// TestEngiPoolScoringFormulas_EqualFlagsDefensive exercises the equal-flags
// edge case (B4=3, F4=3) to verify the formula degrades sanely.
//
// Equal flag totals are unreachable in a real engi bout: the referee awards
// an odd number of flags to exactly one pair per match, so a draw is
// structurally impossible. The formulas must still produce defined output:
// W=L=0 for both sides, Flags=3 for each, Score=3 for each.
//
// Rank tie-breaking: RANK+COUNTIF assigns sequential ranks by row position
// when scores are equal. Alice (row 7, first in range) gets rank 1; Bob
// (row 8) sees one prior occurrence of score=3 in the COUNTIF range, so
// his rank resolves to 1+1=2.
func TestEngiPoolScoringFormulas_EqualFlagsDefensive(t *testing.T) {
	f := scoringSetup2Players(t, 0, true)

	// Equal flags on both sides — unreachable in real engi data.
	f.SetCellValue(SheetPoolMatches, "B4", 3)
	f.SetCellValue(SheetPoolMatches, "F4", 3)

	t.Run("both W=0 L=0", func(t *testing.T) {
		assert.Equal(t, "0", calcScore(t, f, "B7"), "Alice W")
		assert.Equal(t, "0", calcScore(t, f, "C7"), "Alice L")
		assert.Equal(t, "0", calcScore(t, f, "B8"), "Bob W")
		assert.Equal(t, "0", calcScore(t, f, "C8"), "Bob L")
	})

	t.Run("Flags accumulate correctly", func(t *testing.T) {
		assert.Equal(t, "3", calcScore(t, f, "D7"), "Alice Flags")
		assert.Equal(t, "3", calcScore(t, f, "D8"), "Bob Flags")
	})

	t.Run("Score cells are equal", func(t *testing.T) {
		aliceScore := calcScore(t, f, "U7")
		bobScore := calcScore(t, f, "U8")
		assert.Equal(t, "3", aliceScore, "Alice Score (0+3)")
		assert.Equal(t, aliceScore, bobScore, "tied scores are equal")
	})

	// RANK+COUNTIF breaks ties by position: first player in the range gets rank 1,
	// the second sees one prior occurrence in the COUNTIF window and gets rank 2.
	// This state is unreachable in real engi data (flag totals are always odd,
	// so equal flags between two players is impossible), but the formula still
	// produces a defined, stable result.
	t.Run("Rank breaks tie by row position", func(t *testing.T) {
		assert.Equal(t, "1", calcScore(t, f, "G7"), "Alice Rank (first in range)")
		assert.Equal(t, "2", calcScore(t, f, "G8"), "Bob Rank (second in range)")
	})
}

// TestEngiPoolScoringFormulas_BothCellsText verifies that when both input
// cells hold non-numeric text, the OR(ISNUMBER(...)) gate evaluates to FALSE
// and the entire bout is treated as unplayed: W=L=0, Flags=0 for both sides.
//
// This exercises the boundary between "played" and "unplayed" in the engi
// formula. The flags column uses N(), which returns 0 for text, so it never
// contributes to a player's accumulated flag total.
func TestEngiPoolScoringFormulas_BothCellsText(t *testing.T) {
	f := scoringSetup2Players(t, 0, true)

	// Both cells are non-numeric text: OR(ISNUMBER(...)) = FALSE → unplayed.
	f.SetCellValue(SheetPoolMatches, "B4", "x")
	f.SetCellValue(SheetPoolMatches, "F4", "y")

	t.Run("Alice all zeros", func(t *testing.T) {
		assert.Equal(t, "0", calcScore(t, f, "B7"), "Alice W")
		assert.Equal(t, "0", calcScore(t, f, "C7"), "Alice L")
		assert.Equal(t, "0", calcScore(t, f, "D7"), "Alice Flags")
	})

	t.Run("Bob all zeros", func(t *testing.T) {
		assert.Equal(t, "0", calcScore(t, f, "B8"), "Bob W")
		assert.Equal(t, "0", calcScore(t, f, "C8"), "Bob L")
		assert.Equal(t, "0", calcScore(t, f, "D8"), "Bob Flags")
	})
}

// TestEngiPoolScoringFormulas_NumericTextInput pins the behavior when one
// score cell is stored as a text string "3" (via SetCellStr) and the other
// as a numeric 2 (via SetCellValue).
//
// EXCELIZE EVALUATOR NOTE: In real Excel/LibreOffice/Sheets, N(textCell)=0
// even for numeric-looking strings like "3". The intended production behavior
// is therefore: text side = 0 flags, opponent's numeric 2 wins.
//
// excelize's CalcCellValue evaluator differs: it converts numeric-looking
// text strings to numbers, yielding N("3")=3. As a result, the text side
// appears to WIN in unit tests even though it would lose in a real
// spreadsheet application. These assertions document the excelize evaluator
// behavior. Operators must enter actual numeric values — not text — to
// guarantee correct results in the Excel file itself.
func TestEngiPoolScoringFormulas_NumericTextInput(t *testing.T) {
	f := scoringSetup2Players(t, 0, true)

	// B4 stored as text string "3"; F4 stored as numeric 2.
	// In real Excel: N("3")=0, so Alice would lose 0-2.
	// In excelize's evaluator: N("3")=3, so Alice appears to win 3-2.
	require.NoError(t, f.SetCellStr(SheetPoolMatches, "B4", "3"))
	f.SetCellValue(SheetPoolMatches, "F4", 2)

	// ISNUMBER(F4)=TRUE → played=TRUE (OR gate). Both sides are evaluated.
	// excelize evaluator: N("3")=3, N(2)=2 → Alice "wins" 3-2.
	t.Run("Alice W=1 Flags=3 (excelize evaluator quirk: N(text)=number)", func(t *testing.T) {
		assert.Equal(t, "1", calcScore(t, f, "B7"), "Alice W")
		assert.Equal(t, "0", calcScore(t, f, "C7"), "Alice L")
		assert.Equal(t, "3", calcScore(t, f, "D7"), "Alice Flags (N(\"3\")=3 in excelize)")
	})

	t.Run("Bob W=0 Flags=2", func(t *testing.T) {
		assert.Equal(t, "0", calcScore(t, f, "B8"), "Bob W")
		assert.Equal(t, "1", calcScore(t, f, "C8"), "Bob L")
		assert.Equal(t, "2", calcScore(t, f, "D8"), "Bob Flags")
	})
}

// TestEngiPoolStandings_NoPWPLCells is a regression guard for the
// `if !ctx.engi` gate in printIndividualResultsTableSection that prevents
// PW/PL formula cells from being written in engi mode.
//
// Engi standings only use W, L, Flags, and Rank; PW/PL have no meaning
// because there are no individual "points" in kata competition. Leaving those
// cells blank avoids misleading operators who open the spreadsheet.
func TestEngiPoolStandings_NoPWPLCells(t *testing.T) {
	t.Run("engi PW/PL cells have no formula and no value", func(t *testing.T) {
		f := scoringSetup2Players(t, 0, true)

		pwPlCells := []string{"E7", "F7", "E8", "F8"}
		for _, cell := range pwPlCells {
			formula, err := f.GetCellFormula(SheetPoolMatches, cell)
			require.NoErrorf(t, err, "GetCellFormula(%s)", cell)
			assert.Equal(t, "", formula, "engi cell %s must have no formula", cell)

			value, err := f.CalcCellValue(SheetPoolMatches, cell)
			require.NoErrorf(t, err, "CalcCellValue(%s)", cell)
			assert.Equal(t, "", value, "engi cell %s must have empty value", cell)
		}
	})

	t.Run("non-engi PW cell E7 has a formula", func(t *testing.T) {
		f := scoringSetup2Players(t, 0, false)

		formula, err := f.GetCellFormula(SheetPoolMatches, "E7")
		require.NoError(t, err, "GetCellFormula(E7)")
		assert.NotEqual(t, "", formula, "non-engi E7 must contain a PW formula")
	})
}
