package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestPrintPoolMatchesEdgeCourts(t *testing.T) {
	playerA1 := &Player{Name: "Alice"}
	playerA2 := &Player{Name: "Bob"}
	poolA := Pool{
		PoolName: "Pool A",
		Players:  []Player{*playerA1, *playerA2},
		Matches:  []Match{{SideA: playerA1, SideB: playerA2}},
	}

	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetPoolDraw, cell: "B1"},
	}
	pCoords := map[string]playerCellCoord{
		playerCoordKey(*playerA1): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
		playerCoordKey(*playerA2): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A2"}},
	}

	t.Run("numCourts = 0", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		pools := []Pool{poolA}
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 0, false, poolCoords, pCoords, false)
		if len(matchWinners) == 0 {
			t.Errorf("expected match winners even with 0 courts, got %d", len(matchWinners))
		}
	})

	// A court with no home pool would own an empty band, so PrintPoolMatches
	// bands on EffectiveDrawCourts(len(pools), numCourts), not on the requested
	// allocation: one pool carries exactly one shiaijo however many were asked
	// for. This used to print all five headers, so the sheet sent operators to
	// four shiaijo nothing was ever scheduled on.
	t.Run("numCourts > len(pools)", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		pools := []Pool{poolA}
		numCourts := 5
		require.Equal(t, 1, EffectiveDrawCourts(len(pools), numCourts),
			"one pool must clamp to one shiaijo for this case to test the clamp")

		matchWinners := PrintPoolMatches(f, pools, 0, 1, numCourts, false, poolCoords, pCoords, false)
		if len(matchWinners) != 1 {
			t.Errorf("expected 1 match winner, got %d", len(matchWinners))
		}

		// The single pool's own band is printed...
		firstCol, _ := excelize.ColumnNumberToName(1)
		val, _ := f.GetCellValue(SheetPoolMatches, firstCol+"1")
		assert.Equal(t, "Shiaijo A", val, "the pool's own band must be printed")

		// ...and nothing beyond it, including the requested fifth court.
		for c := 1; c < numCourts; c++ {
			colName, _ := excelize.ColumnNumberToName(1 + c*CourtsColumnsPerCourt)
			val, _ := f.GetCellValue(SheetPoolMatches, colName+"1")
			assert.Emptyf(t, val, "band %d must not be printed: only one pool exists to schedule", c+1)
		}
	})
}

func TestPrintPoolMatchesEdgeTournament(t *testing.T) {
	t.Run("1-player pool", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		playerA1 := &Player{Name: "Alice"}
		poolA := Pool{
			PoolName: "Pool A",
			Players:  []Player{*playerA1},
			Matches:  []Match{},
		}
		poolCoords := map[string]cellCoord{
			"Pool A": {sheetName: SheetPoolDraw, cell: "B1"},
		}
		pCoords := map[string]playerCellCoord{
			playerCoordKey(*playerA1): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
		}
		pools := []Pool{poolA}
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 1, false, poolCoords, pCoords, false)
		if len(matchWinners) != 1 {
			t.Errorf("expected 1 match winner, got %d", len(matchWinners))
		}
		val, _ := f.GetCellValue(SheetPoolMatches, "F10")
		if val != "1." {
			t.Errorf("expected result 1. at F10 for single player pool, got '%s'", val)
		}
	})

	t.Run("empty tournament", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		var pools []Pool
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 1, false, nil, nil, false)
		if len(matchWinners) != 0 {
			t.Errorf("expected 0 match winners, got %d", len(matchWinners))
		}
	})
}

func TestPrintPoolMatchesEdgeTeamMatches(t *testing.T) {
	t.Run("teamMatches = 1", func(t *testing.T) {
		playerA1 := &Player{Name: "Alice"}
		playerA2 := &Player{Name: "Bob"}
		pools := []Pool{{
			PoolName: "Pool A",
			Players:  []Player{*playerA1, *playerA2},
			Matches:  []Match{{SideA: playerA1, SideB: playerA2}},
		}}
		poolCoords := map[string]cellCoord{
			"Pool A": {sheetName: SheetPoolDraw, cell: "B1"},
		}
		pCoords := map[string]playerCellCoord{
			playerCoordKey(*playerA1): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
			playerCoordKey(*playerA2): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A2"}},
		}

		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		PrintPoolMatches(f, pools, 1, 1, 1, false, poolCoords, pCoords, false)
		val, _ := f.GetCellValue(SheetPoolMatches, "F18")
		assert.Equalf(t, "1.", val, "expected result 1. at F18 for teamMatches=1, got '%s'", val)
	})

	t.Run("teamMatches = 10", func(t *testing.T) {
		playerA1 := &Player{Name: "Alice"}
		playerA2 := &Player{Name: "Bob"}
		pools := []Pool{{
			PoolName: "Pool A",
			Players:  []Player{*playerA1, *playerA2},
			Matches:  []Match{{SideA: playerA1, SideB: playerA2}},
		}}
		poolCoords := map[string]cellCoord{
			"Pool A": {sheetName: SheetPoolDraw, cell: "B1"},
		}
		pCoords := map[string]playerCellCoord{
			playerCoordKey(*playerA1): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
			playerCoordKey(*playerA2): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A2"}},
		}

		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		PrintPoolMatches(f, pools, 10, 1, 1, false, poolCoords, pCoords, false)
		val, _ := f.GetCellValue(SheetPoolMatches, "F27")
		assert.Equalf(t, "1.", val, "expected result 1. at F27 for teamMatches=10, got '%s'", val)
	})
}

func TestPrintPoolMatchesMirroring(t *testing.T) {
	playerA1 := &Player{Name: "Alice"}
	playerA2 := &Player{Name: "Bob"}
	poolA := Pool{
		PoolName: "Pool A",
		Players:  []Player{*playerA1, *playerA2},
		Matches:  []Match{{SideA: playerA1, SideB: playerA2}},
	}
	pools := []Pool{poolA}
	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetPoolDraw, cell: "B1"},
	}
	pCoords := map[string]playerCellCoord{
		playerCoordKey(*playerA1): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
		playerCoordKey(*playerA2): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A2"}},
	}

	t.Run("mirror = true (default behavior)", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		PrintPoolMatches(f, pools, 0, 1, 1, true, poolCoords, pCoords, false)
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

		PrintPoolMatches(f, pools, 0, 1, 1, false, poolCoords, pCoords, false)
		// Header row should be Red vs White
		val, _ := f.GetCellValue(SheetPoolMatches, "A3")
		assert.Equal(t, "Red", val, "expected Red on left (mirror=false)")
		val, _ = f.GetCellValue(SheetPoolMatches, "G3")
		assert.Equal(t, "White", val, "expected White on right (mirror=false)")
	})
}

// testDrawFor builds the draw a hand-written rounds slice describes: the final
// is the root, cut into numCourts regions exactly as a playoffs draw is. The
// elimination sheet bands by the draw's regions, so these tests need the draw
// the rounds came from rather than a bare court count.
func testDrawFor(rounds [][]*Node, numCourts int) *KnockoutDraw {
	if len(rounds) == 0 || len(rounds[len(rounds)-1]) == 0 {
		return nil
	}
	return NewPlayoffDraw(rounds[len(rounds)-1][0], numCourts)
}

func TestPrintTeamEliminationMatchesMirroring(t *testing.T) {
	// LeafNode: true mirrors real construction (CreateBalancedTree always sets it
	// for true leaves); printSingleEliminationMatch now branches on this field
	// rather than parsing LeafVal as a cell reference (mp-uagg).
	nodeA := &Node{LeafNode: true, LeafVal: "Pool A", matchNum: 1}
	nodeB := &Node{LeafNode: true, LeafVal: "Pool B", matchNum: 1}
	eliminationMatchRounds := [][]*Node{
		{{Left: nodeA, Right: nodeB, matchNum: 1}},
	}
	poolMatchWinners := map[string]MatchWinner{
		"Pool A": {cellCoord: cellCoord{sheetName: "Pool Results", cell: "A1"}},
		"Pool B": {cellCoord: cellCoord{sheetName: "Pool Results", cell: "B1"}},
	}

	t.Run("mirror = true (default behavior)", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetEliminationMatches)
		f.NewSheet("Pool Results")

		PrintTeamEliminationMatches(f, poolMatchWinners, eliminationMatchRounds, 3, testDrawFor(eliminationMatchRounds, 2), true, false)
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

		PrintTeamEliminationMatches(f, poolMatchWinners, eliminationMatchRounds, 3, testDrawFor(eliminationMatchRounds, 2), false, false)
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

		// A REAL draw, because the band a bout is printed in is the region that
		// owns it, not its position in the round.
		//
		// 4 pools sending one qualifier each over 4 shiaijo is the case that
		// tells the two rules apart. Each region holds a single qualifier, so
		// round 1's two bouts pair regions 0+1 and 2+3 and belong to shiaijo A
		// and C. Dividing the match count by the court count instead answers A
		// and B, which is what this sheet used to print while the app called
		// the second bout to shiaijo C.
		pools := make([]Pool, 4)
		for i := range pools {
			pools[i] = Pool{
				PoolName: fmt.Sprintf("Pool %s", CourtLabel(i)),
				Players:  []Player{{Name: fmt.Sprintf("p%da", i)}, {Name: fmt.Sprintf("p%db", i)}},
			}
		}
		draw := BuildKnockoutDraw(pools, 1, 4)
		require.NotNil(t, draw)
		require.Equal(t, 4, draw.NumCourts())
		eliminationMatchRoundsMulti := BuildEliminationMatchRounds(draw.Root)
		require.Len(t, eliminationMatchRoundsMulti[0], 2, "4 qualifiers make two first-round bouts")

		PrintTeamEliminationMatches(f, poolMatchWinners, eliminationMatchRoundsMulti, 0, draw, false, false)

		// The band each bout is printed in must be the region that owns it.
		courtOfNode := draw.NodeCourts()
		wantCourts := []int{}
		for _, m := range eliminationMatchRoundsMulti[0] {
			wantCourts = append(wantCourts, courtOfNode[m])
		}
		require.Equal(t, []int{0, 2}, wantCourts,
			"the draw itself must put these bouts on shiaijo A and C, or this case cannot "+
				"tell the region rule apart from dividing matches by courts")

		// Match numbers are assigned by RenderKnockoutPages, not here, so each
		// bout is identified by the number its own node carries.
		for i, c := range wantCourts {
			m := eliminationMatchRoundsMulti[0][i]
			col := mustColumnName(1 + c*CourtsColumnsPerCourt)
			val, _ := f.GetCellValue(SheetEliminationMatches, fmt.Sprintf("%s2", col))
			assert.Equalf(t, fmt.Sprintf("Round 1 - Match %d", m.matchNum), val,
				"the bout pairing %s belongs to shiaijo %s, so it must print in that band (column %s)",
				CourtLabel(c), CourtLabel(c), col)
		}

		// Shiaijo B's band must be EMPTY in round 1: nothing is scheduled there.
		// This is the assertion the old rule failed, by printing the shiaijo-C
		// bout under B and sending its competitors to the wrong court.
		val, _ := f.GetCellValue(SheetEliminationMatches, "I2")
		assert.Emptyf(t, val, "shiaijo B has no first-round bout, so its band must not carry one")

		// Verify Shiaijo headers
		hdrA, _ := f.GetCellValue(SheetEliminationMatches, "A1")
		assert.Equal(t, "Shiaijo A", hdrA)
		hdrB, _ := f.GetCellValue(SheetEliminationMatches, "I1")
		assert.Equal(t, "Shiaijo B", hdrB)
		hdrC, _ := f.GetCellValue(SheetEliminationMatches, "Q1")
		assert.Equal(t, "Shiaijo C", hdrC)
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
			&Player{Name: "P1"},
			&Player{Name: "P2"},
			&Player{Name: "P3"},
		),
		makePool("Pool B",
			&Player{Name: "P4"},
			&Player{Name: "P5"},
			&Player{Name: "P6"},
		),
		makePool("Pool C",
			&Player{Name: "P7"},
			&Player{Name: "P8"},
			&Player{Name: "P9"},
		),
		makePool("Pool D",
			&Player{Name: "P10"},
			&Player{Name: "P11"},
			&Player{Name: "P12"},
		),
	}

	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetPoolDraw, cell: "A1"},
		"Pool B": {sheetName: SheetPoolDraw, cell: "B1"},
		"Pool C": {sheetName: SheetPoolDraw, cell: "C1"},
		"Pool D": {sheetName: SheetPoolDraw, cell: "D1"},
	}
	pCoords := map[string]playerCellCoord{
		"P1":  {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
		"P2":  {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A2"}},
		"P3":  {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A3"}},
		"P4":  {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "B1"}},
		"P5":  {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "B2"}},
		"P6":  {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "B3"}},
		"P7":  {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "C1"}},
		"P8":  {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "C2"}},
		"P9":  {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "C3"}},
		"P10": {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "D1"}},
		"P11": {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "D2"}},
		"P12": {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "D3"}},
	}

	f := excelize.NewFile()
	defer f.Close()
	f.NewSheet(SheetPoolMatches)
	f.NewSheet(SheetEliminationMatches)

	poolWinners := 2
	matchWinners := PrintPoolMatches(f, pools, 0, poolWinners, 1, false, poolCoords, pCoords, false)

	tree := BuildKnockoutDraw(pools, poolWinners, 1).Root
	depth := CalculateDepth(tree)
	rounds := make([][]*Node, depth-1)
	for i := depth; i > 1; i-- {
		rounds[depth-i] = TraverseRounds(tree, 1, i-1)
	}

	PrintTeamEliminationMatches(f, matchWinners, rounds, 0, testDrawFor(rounds, 1), false, false)

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
// sheet references ("!) caused by a key format mismatch between PrintPoolMatches
// and the tree's LeafVal strings.
func TestPoolWinnerFormulaReferences(t *testing.T) {
	playerA1 := &Player{Name: "Alice"}
	playerA2 := &Player{Name: "Bob"}
	playerA3 := &Player{Name: "Carol"}
	playerB1 := &Player{Name: "Dave"}
	playerB2 := &Player{Name: "Eve"}
	playerB3 := &Player{Name: "Frank"}

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

	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetPoolDraw, cell: "A1"},
		"Pool B": {sheetName: SheetPoolDraw, cell: "B1"},
	}
	pCoords := map[string]playerCellCoord{
		playerCoordKey(*playerA1): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
		playerCoordKey(*playerA2): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A2"}},
		playerCoordKey(*playerA3): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A3"}},
		playerCoordKey(*playerB1): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "B1"}},
		playerCoordKey(*playerB2): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "B2"}},
		playerCoordKey(*playerB3): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "B3"}},
	}

	f := excelize.NewFile()
	defer f.Close()
	f.NewSheet(SheetPoolMatches)
	f.NewSheet(SheetEliminationMatches)

	poolWinners := 2
	matchWinners := PrintPoolMatches(f, pools, 0, poolWinners, 1, false, poolCoords, pCoords, false)

	// Build elimination tree using the same LeafVal format as in production.
	tree := BuildKnockoutDraw(pools, poolWinners, 1).Root
	depth := CalculateDepth(tree)
	eliminationMatchRounds := make([][]*Node, depth-1)
	for i := depth; i > 1; i-- {
		eliminationMatchRounds[depth-i] = TraverseRounds(tree, 1, i-1)
	}

	PrintTeamEliminationMatches(f, matchWinners, eliminationMatchRounds, 0, testDrawFor(eliminationMatchRounds, 1), false, false)

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

// TestPrintTeamEliminationMatches_CellRefLikeLeafNames is a regression test for
// mp-uagg: printSingleEliminationMatch used to decide leaf- vs match-feeder
// nodes by asking whether Node.LeafVal parsed as an Excel cell reference
// (excelize.SplitCellName). A no-pools playoffs bracket (the blank-template CLI
// export, cmd/create-playoffs.go) renders raw participant names as leaves via
// ConvertPlayersToWinners, so a competitor named like a cell coordinate
// ("P1" = column P row 1, "M3", "A4") was misclassified as a match-feeder,
// producing a broken CONCATENATE(...,”!) formula. Fixed by checking the
// structural Node.LeafNode flag instead of parsing LeafVal.
func TestPrintTeamEliminationMatches_CellRefLikeLeafNames(t *testing.T) {
	names := []string{"P1", "M3", "A4", "Z9"}
	players := make([]Player, len(names))
	pCoords := make(map[string]playerCellCoord, len(names))
	for i, name := range names {
		players[i] = Player{Name: name}
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		pCoords[playerCoordKey(players[i])] = playerCellCoord{cellCoord: cellCoord{sheetName: SheetData, cell: cell}}
	}

	f := excelize.NewFile()
	defer f.Close()
	f.NewSheet(SheetData)
	f.NewSheet(SheetEliminationMatches)

	tree := CreateBalancedTree(names)
	depth := CalculateDepth(tree)
	eliminationMatchRounds := make([][]*Node, depth-1)
	for i := depth; i > 1; i-- {
		eliminationMatchRounds[depth-i] = TraverseRounds(tree, 1, i-1)
	}

	matchWinners := ConvertPlayersToWinners(players, false, pCoords)
	PrintTeamEliminationMatches(f, matchWinners, eliminationMatchRounds, 0, testDrawFor(eliminationMatchRounds, 1), false, false)

	rows, err := f.GetRows(SheetEliminationMatches)
	require.NoError(t, err)
	foundFormula := false
	for rowIdx, row := range rows {
		for colIdx := range row {
			cellName, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+1)
			formula, ferr := f.GetCellFormula(SheetEliminationMatches, cellName)
			if ferr != nil || formula == "" {
				continue
			}
			foundFormula = true
			assert.NotContains(t, formula, "''!",
				"cell-ref-like entrant name produced a broken empty-sheet reference in %s: %s", cellName, formula)
		}
	}
	assert.True(t, foundFormula, "expected at least one formula cell in the elimination sheet")
}
