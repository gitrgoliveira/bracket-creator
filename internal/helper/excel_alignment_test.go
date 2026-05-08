package helper

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestPrintPoolMatchesAlignment(t *testing.T) {
	// Create two pools with different match counts
	// Pool A: 1 match
	playerA1 := &Player{Name: "Alice"}
	playerA2 := &Player{Name: "Bob"}
	poolA := Pool{
		PoolName: "Pool A",
		Players:  []Player{*playerA1, *playerA2},
		Matches:  []Match{{SideA: playerA1, SideB: playerA2}},
	}

	// Pool B: 3 matches (3 players round robin: A-B, B-C, A-C)
	playerB1 := &Player{Name: "Charlie"}
	playerB2 := &Player{Name: "Dave"}
	playerB3 := &Player{Name: "Eve"}
	poolB := Pool{
		PoolName: "Pool B",
		Players:  []Player{*playerB1, *playerB2, *playerB3},
		Matches: []Match{
			{SideA: playerB1, SideB: playerB2},
			{SideA: playerB2, SideB: playerB3},
			{SideA: playerB1, SideB: playerB3},
		},
	}

	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetPoolDraw, cell: "B1"},
		"Pool B": {sheetName: SheetPoolDraw, cell: "B2"},
	}
	pCoords := map[string]playerCellCoord{
		playerCoordKey(*playerA1): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A1"}},
		playerCoordKey(*playerA2): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A2"}},
		playerCoordKey(*playerB1): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A3"}},
		playerCoordKey(*playerB2): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A4"}},
		playerCoordKey(*playerB3): {cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: "A5"}},
	}

	t.Run("pool matches vertical alignment", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		// Assign both to different courts but they will be processed in the same "row" of pools
		pools := []Pool{poolA, poolB}
		numCourts := 2

		matchWinners := PrintPoolMatches(f, pools, 0, 1, numCourts, false, poolCoords, pCoords)
		if len(matchWinners) != 2 {
			t.Errorf("expected 2 matchWinners, got %d", len(matchWinners))
		}

		valA1, _ := f.GetCellValue(SheetPoolMatches, "F14")
		if valA1 != "1." {
			t.Errorf("Expected Pool A result 1. at F14, got '%s'", valA1)
		}

		valB1, _ := f.GetCellValue(SheetPoolMatches, "N15")
		if valB1 != "1." {
			t.Errorf("Expected Pool B result 1. at N15, got '%s'", valB1)
		}
	})
}

func TestPrintPoolMatchesTeamAlignment(t *testing.T) {
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

	t.Run("team matches vertical alignment", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet(SheetPoolMatches)
		f.NewSheet(SheetPoolDraw)

		// teamMatches = 3
		PrintPoolMatches(f, pools, 3, 1, 1, false, poolCoords, pCoords)

		// Header(1) + Match(1+2+3+2+1+2=11) = 12. Result 1 at 13.
		// Wait, startRow=2. PoolHeader=2. poolRow=3. Match1 starts at 3. Height 9.
		// Results start at 3+9-2+1=11.
		// Result 1 should be at row 20
		val, _ := f.GetCellValue(SheetPoolMatches, "F20")
		if val != "1." {
			t.Errorf("Expected result 1. at F20, got '%s'", val)
		}
	})
}
