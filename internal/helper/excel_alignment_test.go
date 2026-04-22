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

		matchWinners := PrintPoolMatches(f, pools, 0, 1, numCourts)

		if len(matchWinners) != 2 {
			t.Errorf("expected 2 matchWinners, got %d", len(matchWinners))
		}

		// Max matches is 3.
		// Header: 2 rows (4,5).
		// Match 1: row 6.
		// Match 2: row 7.
		// Match 3: row 8.
		// Results start at poolRow++ = 9. Wait, loop for matches ends at 9. poolRow++ makes it 10.
		// Let's check:
		// m=0: row 6. poolRow=7.
		// m=1: row 7. poolRow=8.
		// m=2: row 8. poolRow=9.
		// result=1: poolRow++=10.

		valA1, _ := f.GetCellValue("Pool Matches", "F10")
		if valA1 != "1. " {
			v9, _ := f.GetCellValue("Pool Matches", "F9")
			v11, _ := f.GetCellValue("Pool Matches", "F11")
			t.Errorf("Expected Pool A result 1. at F10, got '%s' (F9='%s', F11='%s')", valA1, v9, v11)
		}

		valB1, _ := f.GetCellValue("Pool Matches", "N10")
		if valB1 != "1. " {
			t.Errorf("Expected Pool B result 1. at N10, got '%s'", valB1)
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
		PrintPoolMatches(f, pools, 3, 1, 1)

		// Header(1) + Match(1+2+3+2+1+3=12) = 13. Result 1 at 14.
		val1, _ := f.GetCellValue("Pool Matches", "F14")
		if val1 != "1. " {
			v13, _ := f.GetCellValue("Pool Matches", "F13")
			v15, _ := f.GetCellValue("Pool Matches", "F15")
			t.Errorf("Expected result 1. at F14, got '%s' (F13='%s', F15='%s')", val1, v13, v15)
		}
	})
}
