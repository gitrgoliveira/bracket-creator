package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 0, false)
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
		matchWinners := PrintPoolMatches(f, pools, 0, 1, numCourts, false)
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
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 1, false)
		if len(matchWinners) != 1 {
			t.Errorf("expected 1 match winner, got %d", len(matchWinners))
		}
		// Results should still be printed at row 5
		// Header(2) + Results(1+3) = 6.
		// Actually, Header is 2 rows (2,3). poolRow=4. Result 1 is poolRow++=5.
		val, _ := f.GetCellValue("Pool Matches", "F5")
		if val != "1. " {
			v4, _ := f.GetCellValue("Pool Matches", "F4")
			v6, _ := f.GetCellValue("Pool Matches", "F6")
			t.Errorf("expected result 1. at F5 for single player pool, got '%s' (F4='%s', v6='%s')", val, v4, v6)
		}
	})

	t.Run("empty tournament", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		var pools []Pool
		matchWinners := PrintPoolMatches(f, pools, 0, 1, 1, false)
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

		PrintPoolMatches(f, pools, 1, 1, 1, false)
		// Result 1 should be at row 9
		val, _ := f.GetCellValue("Pool Matches", "F9")
		if val != "1. " {
			v8, _ := f.GetCellValue("Pool Matches", "F8")
			v10, _ := f.GetCellValue("Pool Matches", "F10")
			t.Errorf("expected result 1. at F9 for teamMatches=1, got '%s' (F8='%s', F10='%s')", val, v8, v10)
		}
	})

	t.Run("teamMatches = 10", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		PrintPoolMatches(f, pools, 10, 1, 1, false)
		// Header(1) + Match(1+2+10+2+1+2=18) = 19. Result 1 at 18.
		val, _ := f.GetCellValue("Pool Matches", "F18")
		if val != "1. " {
			v17, _ := f.GetCellValue("Pool Matches", "F17")
			v19, _ := f.GetCellValue("Pool Matches", "F19")
			v22, _ := f.GetCellValue("Pool Matches", "F22")
			t.Errorf("expected result 1. at F18 for teamMatches=10, got '%s' (F17='%s', v19='%s', v22='%s')", val, v17, v19, v22)
		}
	})
}

func TestPrintPoolMatchesMirroring(t *testing.T) {
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

	t.Run("mirror = true (default behavior)", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		PrintPoolMatches(f, pools, 0, 1, 1, true)
		// Header row should be White vs Red
		val, _ := f.GetCellValue("Pool Matches", "A3")
		assert.Equal(t, "White", val, "expected White on left (mirror=true)")
		val, _ = f.GetCellValue("Pool Matches", "G3")
		assert.Equal(t, "Red", val, "expected Red on right (mirror=true)")
	})

	t.Run("mirror = false", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Pool Matches")
		f.NewSheet("Pool Draw")

		PrintPoolMatches(f, pools, 0, 1, 1, false)
		// Header row should be Red vs White
		val, _ := f.GetCellValue("Pool Matches", "A3")
		assert.Equal(t, "Red", val, "expected Red on left (mirror=false)")
		val, _ = f.GetCellValue("Pool Matches", "G3")
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
		f.NewSheet("Elimination Matches")
		f.NewSheet("Pool Results")

		PrintTeamEliminationMatches(f, poolMatchWinners, eliminationMatchRounds, 3, 2, true)
		// Match header row (Red/White labels) should be swapped: White vs Red
		// Round header was removed, first match header at row 3
		val, _ := f.GetCellValue("Elimination Matches", "A3")
		assert.Equal(t, "White", val, "expected White on left (mirror=true)")
		val, _ = f.GetCellValue("Elimination Matches", "G3")
		assert.Equal(t, "Red", val, "expected Red on right (mirror=true)")
	})

	t.Run("mirror = false", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Elimination Matches")
		f.NewSheet("Pool Results")

		PrintTeamEliminationMatches(f, poolMatchWinners, eliminationMatchRounds, 3, 2, false)
		// Match header row should be Red vs White
		val, _ := f.GetCellValue("Elimination Matches", "A3")
		assert.Equal(t, "Red", val, "expected Red on left (mirror=false)")
		val, _ = f.GetCellValue("Elimination Matches", "G3")
		assert.Equal(t, "White", val, "expected White on right (mirror=false)")
	})

	t.Run("multiple courts", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		f.NewSheet("Elimination Matches")
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
		val, _ := f.GetCellValue("Elimination Matches", "A2") // Match 1 title row
		assert.Equal(t, "Round 1 - Match 1", val)

		// Match 2 (Shiaijo B) should be at column 9
		val, _ = f.GetCellValue("Elimination Matches", "I2") // Match 2 title row (Column 9 = I)
		assert.Equal(t, "Round 1 - Match 2", val)

		// Match 3 (Shiaijo B again) should be below Match 2
		// Verify Match 3 starts at row 10
		val, _ = f.GetCellValue("Elimination Matches", "I10")
		assert.Equal(t, "Round 1 - Match 3", val)

		// Verify Shiaijo headers
		val, _ = f.GetCellValue("Elimination Matches", "A1")
		assert.Equal(t, "Shiaijo A", val)
		val, _ = f.GetCellValue("Elimination Matches", "I1")
		assert.Equal(t, "Shiaijo B", val)
	})
}
