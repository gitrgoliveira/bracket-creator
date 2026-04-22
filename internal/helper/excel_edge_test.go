package helper

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestPrintPoolMatchesEdgeCourts(t *testing.T) {
	playerA1 := &Player{Name: "Alice", sheetName: "Pool Draw", cell: "A1"}
	playerA2 := &Player{Name: "Bob", sheetName: "Pool Draw", cell: "A2"}
	poolA := Pool{
		PoolName:  "Pool A",
		sheetName: "Pool Draw",
		cell:      "B1",
		Players:   []Player{*playerA1, *playerA2},
		Matches:   []Match{{SideA: playerA1, SideB: playerA2}},
	}

	t.Run("numCourts = 0", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		pools := []Pool{poolA}
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 0)
		if len(matchWinners) == 0 {
			t.Errorf("expected match winners even with 0 courts, got %d", len(matchWinners))
		}
	})

	t.Run("numCourts > len(pools)", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		pools := []Pool{poolA}
		numCourts := 5
		matchWinners := PrintPoolMatches(f, pools, 0, 1, numCourts)
		if len(matchWinners) != 1 {
			t.Errorf("expected 1 match winner, got %d", len(matchWinners))
		}
		// Verify court 5 header exists but is empty of pools
		colName, _ := excelize.ColumnNumberToName(1 + 4*8)
		val, _ := f.GetCellValue("Pool Matches", colName+"1")
		if val != "Shiaijo E" {
			t.Errorf("expected Shiaijo E header, got '%s'", val)
		}
	})
}

func TestPrintPoolMatchesEdgeTournament(t *testing.T) {
	t.Run("1-player pool", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		playerA1 := &Player{Name: "Alice", sheetName: "Pool Draw", cell: "A1"}
		poolA := Pool{
			PoolName:  "Pool A",
			sheetName: "Pool Draw",
			cell:      "B1",
			Players:   []Player{*playerA1},
			Matches:   []Match{}, // No matches possible
		}
		pools := []Pool{poolA}
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 1)
		if len(matchWinners) != 1 {
			t.Errorf("expected 1 match winner, got %d", len(matchWinners))
		}
		// Results should still be printed at row 7
		// Header(2) + Results(1+3) = 6.
		// Actually, Header is 2 rows (4,5). poolRow=6. Result 1 is poolRow++=7.
		val, _ := f.GetCellValue("Pool Matches", "F7")
		if val != "1. " {
			v6, _ := f.GetCellValue("Pool Matches", "F6")
			v8, _ := f.GetCellValue("Pool Matches", "F8")
			t.Errorf("expected result 1. at F7 for single player pool, got '%s' (F6='%s', F8='%s')", val, v6, v8)
		}
	})

	t.Run("empty tournament", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		var pools []Pool
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 1)
		if len(matchWinners) != 0 {
			t.Errorf("expected 0 match winners, got %d", len(matchWinners))
		}
	})
}

func TestPrintPoolMatchesEdgeTeamMatches(t *testing.T) {
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

	t.Run("teamMatches = 1", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		PrintPoolMatches(f, pools, 1, 1, 1)
		// Result 1 should be at row 12
		val, _ := f.GetCellValue("Pool Matches", "F12")
		if val != "1. " {
			v11, _ := f.GetCellValue("Pool Matches", "F11")
			v13, _ := f.GetCellValue("Pool Matches", "F13")
			t.Errorf("expected result 1. at F12 for teamMatches=1, got '%s' (F11='%s', F13='%s')", val, v11, v13)
		}
	})

	t.Run("teamMatches = 10", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		PrintPoolMatches(f, pools, 10, 1, 1)
		// Header(1) + Match(1+2+10+2+1+3=19) = 20. Result 1 at 21.
		val, _ := f.GetCellValue("Pool Matches", "F21")
		if val != "1. " {
			v20, _ := f.GetCellValue("Pool Matches", "F20")
			v22, _ := f.GetCellValue("Pool Matches", "F22")
			v25, _ := f.GetCellValue("Pool Matches", "F25")
			t.Errorf("expected result 1. at F21 for teamMatches=10, got '%s' (F20='%s', F22='%s', F25='%s')", val, v20, v22, v25)
		}
	})
}
