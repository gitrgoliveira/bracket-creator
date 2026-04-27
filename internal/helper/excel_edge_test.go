package helper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestPrintPoolMatchesEdgeCourts(t *testing.T) {
	playerA1 := &Player{Name: "Alice", sheetName: SheetPoolDraw, cell: "A1"}
	playerA2 := &Player{Name: "Bob", sheetName: SheetPoolDraw, cell: "A2"}
	poolA := Pool{
		PoolName:  "Pool A",
		sheetName: SheetPoolDraw,
		cell:      "B1",
		Players:   []Player{*playerA1, *playerA2},
		Matches:   []Match{{SideA: playerA1, SideB: playerA2}},
	}

	t.Run("numCourts = 0", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		pools := []Pool{poolA}
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 0, false)
		if len(matchWinners) == 0 {
			t.Errorf("expected match winners even with 0 courts, got %d", len(matchWinners))
		}
	})

	t.Run("numCourts > len(pools)", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		pools := []Pool{poolA}
		numCourts := 5
		matchWinners := PrintPoolMatches(f, pools, 0, 1, numCourts, false)
		if len(matchWinners) != 1 {
			t.Errorf("expected 1 match winner, got %d", len(matchWinners))
		}
		// Verify court 5 header exists but is empty of pools
		colName, _ := excelize.ColumnNumberToName(1 + 4*8)
		val, _ := f.GetCellValue(SheetPoolMatches, colName+"1")
		if val != "Shiaijo E" {
			t.Errorf("expected Shiaijo E header, got '%s'", val)
		}
	})
}

func TestPrintPoolMatchesEdgeTournament(t *testing.T) {
	t.Run("1-player pool", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		playerA1 := &Player{Name: "Alice", sheetName: SheetPoolDraw, cell: "A1"}
		poolA := Pool{
			PoolName:  "Pool A",
			sheetName: SheetPoolDraw,
			cell:      "B1",
			Players:   []Player{*playerA1},
			Matches:   []Match{}, // No matches possible
		}
		pools := []Pool{poolA}
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 1, false)
		if len(matchWinners) != 1 {
			t.Errorf("expected 1 match winner, got %d", len(matchWinners))
		}
		// Results should still be printed at row 5
		// Header(2) + Results(1+3) = 6.
		// Actually, Header is 2 rows (2,3). poolRow=4. Result 1 is poolRow++=5.
		val, _ := f.GetCellValue(SheetPoolMatches, "F5")
		if val != "1. " {
			v4, _ := f.GetCellValue(SheetPoolMatches, "F4")
			v6, _ := f.GetCellValue(SheetPoolMatches, "F6")
			t.Errorf("expected result 1. at F5 for single player pool, got '%s' (F4='%s', v6='%s')", val, v4, v6)
		}
	})

	t.Run("empty tournament", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		var pools []Pool
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 1, false)
		if len(matchWinners) != 0 {
			t.Errorf("expected 0 match winners, got %d", len(matchWinners))
		}
	})
}

func TestPrintPoolMatchesEdgeTeamMatches(t *testing.T) {
	playerA1 := &Player{Name: "Alice", sheetName: SheetPoolDraw, cell: "A1"}
	playerA2 := &Player{Name: "Bob", sheetName: SheetPoolDraw, cell: "A2"}
	poolA := Pool{
		PoolName:  "Pool A",
		sheetName: SheetPoolDraw,
		cell:      "B1",
		Players:   []Player{*playerA1, *playerA2},
		Matches:   []Match{{SideA: playerA1, SideB: playerA2}},
	}
	pools := []Pool{poolA}

	t.Run("teamMatches = 1", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		PrintPoolMatches(f, pools, 1, 1, 1, false)
		// Result 1 should be at row 9
		val, _ := f.GetCellValue(SheetPoolMatches, "F9")
		if val != "1. " {
			v8, _ := f.GetCellValue(SheetPoolMatches, "F8")
			v10, _ := f.GetCellValue(SheetPoolMatches, "F10")
			t.Errorf("expected result 1. at F9 for teamMatches=1, got '%s' (F8='%s', F10='%s')", val, v8, v10)
		}
	})

	t.Run("teamMatches = 10", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		PrintPoolMatches(f, pools, 10, 1, 1, false)
		// Header(1) + Match(1+2+10+2+1+2=18) = 19. Result 1 at 18.
		val, _ := f.GetCellValue(SheetPoolMatches, "F18")
		if val != "1. " {
			v17, _ := f.GetCellValue(SheetPoolMatches, "F17")
			v19, _ := f.GetCellValue(SheetPoolMatches, "F19")
			v22, _ := f.GetCellValue(SheetPoolMatches, "F22")
			t.Errorf("expected result 1. at F18 for teamMatches=10, got '%s' (F17='%s', v19='%s', v22='%s')", val, v17, v19, v22)
		}
	})
}

func TestPrintPoolMatchesMirroring(t *testing.T) {
	playerA1 := &Player{Name: "Alice", sheetName: SheetPoolDraw, cell: "A1"}
	playerA2 := &Player{Name: "Bob", sheetName: SheetPoolDraw, cell: "A2"}
	poolA := Pool{
		PoolName:  "Pool A",
		sheetName: SheetPoolDraw,
		cell:      "B1",
		Players:   []Player{*playerA1, *playerA2},
		Matches:   []Match{{SideA: playerA1, SideB: playerA2}},
	}
	pools := []Pool{poolA}

	t.Run("mirror = true (default behavior)", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		PrintPoolMatches(f, pools, 0, 1, 1, true)
		// Header row should be White vs Red
		val, _ := f.GetCellValue(SheetPoolMatches, "A3")
		assert.Equal(t, "White", val, "expected White on left (mirror=true)")
		val, _ = f.GetCellValue(SheetPoolMatches, "G3")
		assert.Equal(t, "Red", val, "expected Red on right (mirror=true)")
	})

	t.Run("mirror = false", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		PrintPoolMatches(f, pools, 0, 1, 1, false)
		// Header row should be Red vs White
		val, _ := f.GetCellValue(SheetPoolMatches, "A3")
		assert.Equal(t, "Red", val, "expected Red on left (mirror=false)")
		val, _ = f.GetCellValue(SheetPoolMatches, "G3")
		assert.Equal(t, "White", val, "expected White on right (mirror=false)")
	})
}

func TestPrintTeamEliminationMatchesMirroring(t *testing.T) {
	nodeA := &Node{LeafVal: "Pool A", matchNum: 1}
	nodeB := &Node{LeafVal: "Pool B", matchNum: 1}
	eliminationMatchRounds := [][]*Node{
		{{Left: nodeA, Right: nodeB, matchNum: 1}},
	}
	poolMatchWinners := map[string]MatchWinner{
		"Pool A": {sheetName: "Pool Results", cell: "A1"},
		"Pool B": {sheetName: "Pool Results", cell: "B1"},
	}

	t.Run("mirror = true (default behavior)", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetEliminationMatches)
		f.NewSheet("Pool Results")

		PrintTeamEliminationMatches(f, poolMatchWinners, eliminationMatchRounds, 3, 2, true)
		// Match header row (Red/White labels) should be swapped: White vs Red
		// Round header was removed, first match header at row 3
		val, _ := f.GetCellValue(SheetEliminationMatches, "A3")
		assert.Equal(t, "White", val, "expected White on left (mirror=true)")
		val, _ = f.GetCellValue(SheetEliminationMatches, "G3")
		assert.Equal(t, "Red", val, "expected Red on right (mirror=true)")
	})

	t.Run("mirror = false", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetEliminationMatches)
		f.NewSheet("Pool Results")

		PrintTeamEliminationMatches(f, poolMatchWinners, eliminationMatchRounds, 3, 2, false)
		// Match header row should be Red vs White
		val, _ := f.GetCellValue(SheetEliminationMatches, "A3")
		assert.Equal(t, "Red", val, "expected Red on left (mirror=false)")
		val, _ = f.GetCellValue(SheetEliminationMatches, "G3")
		assert.Equal(t, "White", val, "expected White on right (mirror=false)")
	})

	t.Run("multiple courts", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetEliminationMatches)
		f.NewSheet("Pool Results")

		// 3 matches in round 1, spread across 2 courts
		eliminationMatchRoundsMulti := [][]*Node{
			{
				{Left: nodeA, Right: nodeB, matchNum: 1},
				{Left: nodeA, Right: nodeB, matchNum: 2},
				{Left: nodeA, Right: nodeB, matchNum: 3},
			},
		}

		PrintTeamEliminationMatches(f, poolMatchWinners, eliminationMatchRoundsMulti, 0, 2, false)

		// Match 1 (Shiaijo A) should be at column 1
		val, _ := f.GetCellValue(SheetEliminationMatches, "A2") // Match 1 title row
		assert.Equal(t, "Round 1 - Match 1", val)

		// Match 2 (Shiaijo B) should be at column 9
		val, _ = f.GetCellValue(SheetEliminationMatches, "I2") // Match 2 title row (Column 9 = I)
		assert.Equal(t, "Round 1 - Match 2", val)

		// Match 3 (Shiaijo B again) should be below Match 2
		// Verify Match 3 starts at row 10
		val, _ = f.GetCellValue(SheetEliminationMatches, "I10")
		assert.Equal(t, "Round 1 - Match 3", val)

		// Verify Shiaijo headers
		val, _ = f.GetCellValue(SheetEliminationMatches, "A1")
		assert.Equal(t, "Shiaijo A", val)
		val, _ = f.GetCellValue(SheetEliminationMatches, "I1")
		assert.Equal(t, "Shiaijo B", val)
	})
}

// TestEliminationMatchSameSheetFormulas verifies that when later-round elimination
// matches reference earlier-round results on the same sheet, the formula uses a
// plain cell reference (e.g. G6) rather than a qualified one ('Elimination Matches'!G6).
// The qualified form causes Excel to flag the formula as invalid and repair/remove it.
func TestEliminationMatchSameSheetFormulas(t *testing.T) {
	// 4 pools × 2 finalists = 8 finalists → 3 rounds; rounds 2+ reference same sheet.
	makePool := func(name string, players ...*Player) Pool {
		ps := make([]Player, len(players))
		for i, p := range players {
			ps[i] = *p
		}
		matches := []Match{}
		for i := 0; i < len(players); i++ {
			for j := i + 1; j < len(players); j++ {
				matches = append(matches, Match{SideA: players[i], SideB: players[j]})
			}
		}
		return Pool{PoolName: name, Players: ps, Matches: matches}
	}
	pools := []Pool{
		makePool("Pool A",
			&Player{Name: "P1", sheetName: SheetPoolDraw, cell: "A1"},
			&Player{Name: "P2", sheetName: SheetPoolDraw, cell: "A2"},
			&Player{Name: "P3", sheetName: SheetPoolDraw, cell: "A3"},
		),
		makePool("Pool B",
			&Player{Name: "P4", sheetName: SheetPoolDraw, cell: "B1"},
			&Player{Name: "P5", sheetName: SheetPoolDraw, cell: "B2"},
			&Player{Name: "P6", sheetName: SheetPoolDraw, cell: "B3"},
		),
		makePool("Pool C",
			&Player{Name: "P7", sheetName: SheetPoolDraw, cell: "C1"},
			&Player{Name: "P8", sheetName: SheetPoolDraw, cell: "C2"},
			&Player{Name: "P9", sheetName: SheetPoolDraw, cell: "C3"},
		),
		makePool("Pool D",
			&Player{Name: "P10", sheetName: SheetPoolDraw, cell: "D1"},
			&Player{Name: "P11", sheetName: SheetPoolDraw, cell: "D2"},
			&Player{Name: "P12", sheetName: SheetPoolDraw, cell: "D3"},
		),
	}

	f := excelize.NewFile()
	defer f.Close()
	f.NewSheet(SheetPoolMatches)
	f.NewSheet(SheetEliminationMatches)

	poolWinners := 2
	matchWinners := PrintPoolMatches(f, pools, 0, poolWinners, 1, false)

	finalists := GenerateFinals(pools, poolWinners)
	tree := CreateBalancedTree(finalists)
	depth := CalculateDepth(tree)
	rounds := make([][]*Node, depth-1)
	for i := depth; i > 1; i-- {
		rounds[depth-i] = TraverseRounds(tree, 1, i-1)
	}

	PrintTeamEliminationMatches(f, matchWinners, rounds, 0, 1, false)

	// Collect all formula cells in the Elimination Matches sheet.
	rows, err := f.GetRows(SheetEliminationMatches)
	require.NoError(t, err)
	for rowIdx, row := range rows {
		for colIdx := range row {
			cellName, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+1)
			formula, err := f.GetCellFormula(SheetEliminationMatches, cellName)
			if err != nil || formula == "" {
				continue
			}
			assert.NotContains(t, formula, "'Elimination Matches'!",
				"same-sheet self-reference in cell %s: %s", cellName, formula)
		}
	}
}

// TestPoolWinnerFormulaReferences verifies that elimination match cells contain
// valid CONCATENATE formulas referencing actual pool result cells, not empty
// sheet references (”!) caused by a key format mismatch between PrintPoolMatches
// and the tree's LeafVal strings.
func TestPoolWinnerFormulaReferences(t *testing.T) {
	playerA1 := &Player{Name: "Alice", sheetName: SheetPoolDraw, cell: "A1"}
	playerA2 := &Player{Name: "Bob", sheetName: SheetPoolDraw, cell: "A2"}
	playerA3 := &Player{Name: "Carol", sheetName: SheetPoolDraw, cell: "A3"}
	playerB1 := &Player{Name: "Dave", sheetName: SheetPoolDraw, cell: "B1"}
	playerB2 := &Player{Name: "Eve", sheetName: SheetPoolDraw, cell: "B2"}
	playerB3 := &Player{Name: "Frank", sheetName: SheetPoolDraw, cell: "B3"}

	pools := []Pool{
		{
			PoolName: "Pool A",
			Players:  []Player{*playerA1, *playerA2, *playerA3},
			Matches: []Match{
				{SideA: playerA1, SideB: playerA2},
				{SideA: playerA1, SideB: playerA3},
				{SideA: playerA2, SideB: playerA3},
			},
		},
		{
			PoolName: "Pool B",
			Players:  []Player{*playerB1, *playerB2, *playerB3},
			Matches: []Match{
				{SideA: playerB1, SideB: playerB2},
				{SideA: playerB1, SideB: playerB3},
				{SideA: playerB2, SideB: playerB3},
			},
		},
	}

	f := excelize.NewFile()
	defer f.Close()
	f.NewSheet(SheetPoolMatches)
	f.NewSheet(SheetEliminationMatches)

	poolWinners := 2
	matchWinners := PrintPoolMatches(f, pools, 0, poolWinners, 1, false)

	// Build elimination tree using the same LeafVal format as in production.
	finalists := GenerateFinals(pools, poolWinners)
	tree := CreateBalancedTree(finalists)
	depth := CalculateDepth(tree)
	eliminationMatchRounds := make([][]*Node, depth-1)
	for i := depth; i > 1; i-- {
		eliminationMatchRounds[depth-i] = TraverseRounds(tree, 1, i-1)
	}

	PrintTeamEliminationMatches(f, matchWinners, eliminationMatchRounds, 0, 1, false)

	// The first round has 2 matches; each match's player row is at startRow+2=4.
	// Left player is in column A (col 1), right player in column G (col 7).
	// Every CONCATENATE formula must reference a real cell, not an empty sheet
	// reference (''!) which indicates the pool winner key lookup failed.
	playerCells := []string{"A4", "G4", "A12", "G12"}
	for _, cell := range playerCells {
		formula, err := f.GetCellFormula(SheetEliminationMatches, cell)
		assert.NoError(t, err)
		assert.True(t, strings.Contains(formula, "CONCATENATE"),
			"expected CONCATENATE formula in %s, got: %q", cell, formula)
		assert.NotContains(t, formula, "''!",
			"formula in Elimination Matches %s has empty sheet reference: %s", cell, formula)
	}
}
