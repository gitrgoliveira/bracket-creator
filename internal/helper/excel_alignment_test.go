package helper

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestPrintPoolMatchesAlignment(t *testing.T) {
	// Create two pools with different match counts
	// Pool A: 1 match
	playerA1 := &Player{Name: "Alice", sheetName: "Pool Draw", cell: "A1"}
	playerA2 := &Player{Name: "Bob", sheetName: "Pool Draw", cell: "A2"}
	poolA := Pool{
		PoolName:  "Pool A",
		sheetName: "Pool Draw",
		cell:      "B1",
		Players:   []Player{*playerA1, *playerA2},
		Matches:   []Match{{SideA: playerA1, SideB: playerA2}},
	}

	// Pool B: 3 matches (3 players round robin: A-B, B-C, A-C)
	playerB1 := &Player{Name: "Charlie", sheetName: "Pool Draw", cell: "A3"}
	playerB2 := &Player{Name: "Dave", sheetName: "Pool Draw", cell: "A4"}
	playerB3 := &Player{Name: "Eve", sheetName: "Pool Draw", cell: "A5"}
	poolB := Pool{
		PoolName:  "Pool B",
		sheetName: "Pool Draw",
		cell:      "B2",
		Players:   []Player{*playerB1, *playerB2, *playerB3},
		Matches: []Match{
			{SideA: playerB1, SideB: playerB2},
			{SideA: playerB2, SideB: playerB3},
			{SideA: playerB1, SideB: playerB3},
		},
	}

	t.Run("pool matches vertical alignment", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		// Assign both to different courts but they will be processed in the same "row" of pools
		pools := []Pool{poolA, poolB}
		numCourts := 2

		matchWinners := PrintPoolMatches(f, pools, 0, 1, numCourts, false)

		if len(matchWinners) != 2 {
			t.Errorf("expected 2 matchWinners, got %d", len(matchWinners))
		}

		// Max matches is 3.
		// Header: 2 rows (2,3).
		// Match 1: row 4.
		// Match 2: row 5.
		// Match 3: row 6.
		// Results start at poolRow++ = 7.
		// Let's check:
		// m=0: row 4. poolRow=5.
		// m=1: row 5. poolRow=6.
		// m=2: row 6. poolRow=7.
		// result=1: poolRow++=8.

		valA1, _ := f.GetCellValue("Pool Matches", "F8")
		if valA1 != "1. " {
			v7, _ := f.GetCellValue("Pool Matches", "F7")
			v9, _ := f.GetCellValue("Pool Matches", "F9")
			t.Errorf("Expected Pool A result 1. at F8, got '%s' (F7='%s', F9='%s')", valA1, v7, v9)
		}

		valB1, _ := f.GetCellValue("Pool Matches", "N8")
		if valB1 != "1. " {
			t.Errorf("Expected Pool B result 1. at N8, got '%s'", valB1)
		}
	})
}

func TestPrintPoolMatchesTeamAlignment(t *testing.T) {
	playerA1 := &Player{Name: "Alice", sheetName: "Pool Draw", cell: "A1"}
	playerA2 := &Player{Name: "Bob", sheetName: "Pool Draw", cell: "A2"}
	poolA := Pool{
		PoolName:  "Pool A",
		sheetName: "Pool Draw",
		cell:      "B1",
		Players:   []Player{*playerA1, *playerA2},
		Matches:   []Match{{SideA: playerA1, SideB: playerA2}},
	}
	pools := []Pool{poolA}

	t.Run("team matches vertical alignment", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		// teamMatches = 3
		PrintPoolMatches(f, pools, 3, 1, 1, false)

		// Header(1) + Match(1+2+3+2+1+2=11) = 12. Result 1 at 13.
		// Wait, startRow=2. PoolHeader=2. poolRow=3. Match1 starts at 3. Height 9.
		// Results start at 3+9-2+1=11.
		val1, _ := f.GetCellValue("Pool Matches", "F11")
		if val1 != "1. " {
			v10, _ := f.GetCellValue("Pool Matches", "F10")
			v12, _ := f.GetCellValue("Pool Matches", "F12")
			t.Errorf("Expected result 1. at F11, got '%s' (F10='%s', F12='%s')", val1, v10, v12)
		}
	})
}
