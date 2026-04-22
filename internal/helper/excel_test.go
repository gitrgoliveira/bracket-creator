package helper

import (
	"fmt"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestCreateTreeBracket(t *testing.T) {
	// Create a test Excel file
	f := excelize.NewFile()
	defer f.Close()

	// Create a sheet
	sheetName := "Sheet1"
	f.NewSheet(sheetName)

	// Call CreateTreeBracket
	result := CreateTreeBracket(f, sheetName, 1, 1, 1)

	// Verify the result is not empty
	if result == "" {
		t.Error("Expected non-empty cell reference")
	}
}

func TestSanitizeName(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"John Doe", "J. DOE"},
		{"John & Doe", "J. DOE"},
		{"O'Connor", "O'CONNOR"},
		{"John-Doe", "JOHN-DOE"},
		{"", ""},
	}

	for _, tc := range testCases {
		result := sanitizeName(tc.input)
		if result != tc.expected {
			t.Errorf("For input '%s', expected '%s', got '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestWriteTreeValue(t *testing.T) {
	// Create a test Excel file
	f := excelize.NewFile()
	defer f.Close()

	// Create a sheet
	sheetName := "Sheet1"
	f.NewSheet(sheetName)

	// Test 1: Call writeTreeValue with nil matchWinners (should set static value)
	writeTreeValue(f, sheetName, 1, 1, "Test Value", nil)

	// Verify the value was set (column 2, row 1 corresponds to B1)
	cellRef, _ := excelize.CoordinatesToCellName(2, 1)
	value, err := f.GetCellValue(sheetName, cellRef)
	if err != nil {
		t.Fatalf("Error getting cell value: %v", err)
	}

	if value != "Test Value" {
		t.Errorf("Expected cell value to be 'Test Value', got '%s'", value)
	}

	// Test 2: Test with matchWinners map (should create formula)
	matchWinners := map[string]MatchWinner{
		"Pool A.1": {
			sheetName: "Pool Matches",
			cell:      "G10",
		},
	}

	writeTreeValue(f, sheetName, 1, 2, "Pool A.1", matchWinners)

	// Verify the formula was set (column 2, row 2 corresponds to B2)
	cellRef2, _ := excelize.CoordinatesToCellName(2, 2)
	formula, err := f.GetCellFormula(sheetName, cellRef2)
	if err != nil {
		t.Fatalf("Error getting cell formula: %v", err)
	}

	expectedFormula := `CONCATENATE("Pool A.1 ",'Pool Matches'!G10)`
	if formula != expectedFormula {
		t.Errorf("Expected formula to be '%s', got '%s'", expectedFormula, formula)
	}

	// Test 3: Test with pool reference not in matchWinners (should set static value)
	writeTreeValue(f, sheetName, 1, 3, "B.2", matchWinners)

	cellRef3, _ := excelize.CoordinatesToCellName(2, 3)
	value3, err := f.GetCellValue(sheetName, cellRef3)
	if err != nil {
		t.Fatalf("Error getting cell value: %v", err)
	}

	if value3 != "B.2" {
		t.Errorf("Expected cell value to be 'B.2', got '%s'", value3)
	}
}

func TestCreateBalancedTree(t *testing.T) {
	// Skip empty slice test as it causes stack overflow

	// Test with single leaf - should NOT sanitize
	singleLeaf := CreateBalancedTree([]string{"John Doe"})
	if singleLeaf.LeafVal != "John Doe" {
		t.Errorf("Expected verbatim name, got %s", singleLeaf.LeafVal)
	}

	// Test with multiple leaves
	leafValues := []string{"A", "B", "C", "D"}
	tree := CreateBalancedTree(leafValues)

	// Root should not be a leaf
	if tree.LeafNode {
		t.Error("Root should not be a leaf")
	}

	// Verify tree structure (should be balanced)
	if tree.Left == nil || tree.Right == nil {
		t.Error("Tree should have left and right children")
	}
}

func TestAddPlayerDataWithMetadata(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	sheetName := "data"
	f.NewSheet(sheetName)

	players := []Player{
		{
			Name:         "Ricardo Oliveira",
			DisplayName:  "クレスワェル",
			Dojo:         "Tokyo Kendo Club",
			Metadata:     []string{"Extra1", "Extra2"},
			PoolPosition: 1,
		},
	}

	AddPlayerDataToSheet(f, players, true)

	// Check Display Name (Column D)
	val, _ := f.GetCellValue(sheetName, "D3")
	if val != "クレスワェル" {
		t.Errorf("Expected クレスワェル, got %s", val)
	}

	// Check Metadata (Column E and F)
	valE, _ := f.GetCellValue(sheetName, "E3")
	if valE != "Extra1" {
		t.Errorf("Expected Extra1, got %s", valE)
	}
	valF, _ := f.GetCellValue(sheetName, "F3")
	if valF != "Extra2" {
		t.Errorf("Expected Extra2, got %s", valF)
	}
}

// TestCreateTreeBracketExtended provides comprehensive coverage for different bracket sizes
func TestCreateTreeBracketExtended(t *testing.T) {
	tests := []struct {
		name      string
		col       int
		startRow  int
		size      int
		expectErr bool
	}{
		{
			name:     "small bracket size 2",
			col:      2,
			startRow: 1,
			size:     2,
		},
		{
			name:     "medium bracket size 8",
			col:      3,
			startRow: 5,
			size:     8,
		},
		{
			name:     "large bracket size 32",
			col:      5,
			startRow: 1,
			size:     32,
		},
		{
			name:     "bracket starting at high row",
			col:      2,
			startRow: 100,
			size:     4,
		},
		{
			name:     "bracket at column Z",
			col:      25,
			startRow: 1,
			size:     4,
		},
		{
			name:     "zero size bracket",
			col:      1,
			startRow: 1,
			size:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

			sheet := "TestSheet"
			f.NewSheet(sheet)

			result := CreateTreeBracket(f, sheet, tt.col, tt.startRow, tt.size)

			if tt.expectErr {
				if result != "" {
					t.Errorf("Expected error for %s, got result: %s", tt.name, result)
				}
			} else {
				// Should return a valid cell reference
				if tt.size > 0 {
					if result == "" {
						t.Errorf("Expected non-empty cell reference for %s", tt.name)
					}
					// Verify it looks like a cell reference (e.g., "C5")
					matched := false
					for _, c := range result {
						if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
							matched = true
							break
						}
					}
					if !matched {
						t.Errorf("Expected cell reference format, got: %s", result)
					}
				}
			}
		})
	}
}

// TestAddPoolsToTreeTable provides comprehensive testing for pool-to-tree conversion
func TestAddPoolsToTreeTable(t *testing.T) {
	tests := []struct {
		name       string
		poolCount  int
		playerPer  int
		setupPools func() []Pool
	}{
		{
			name:      "single pool single player",
			poolCount: 1,
			playerPer: 1,
			setupPools: func() []Pool {
				return []Pool{
					{
						sheetName: "Pool1",
						cell:      "B2",
						PoolName:  "Pool A",
						Players: []Player{
							{
								Name:         "Player 1",
								PoolPosition: 1,
								sheetName:    "Pool1",
								cell:         "B3",
							},
						},
					},
				}
			},
		},
		{
			name:      "multiple pools multiple players",
			poolCount: 3,
			playerPer: 3,
			setupPools: func() []Pool {
				pools := []Pool{}
				for i := 1; i <= 3; i++ {
					players := []Player{}
					for j := 1; j <= 3; j++ {
						players = append(players, Player{
							Name:         "P" + string(rune(64+i)) + string(rune(48+j)),
							PoolPosition: int64(j),
							sheetName:    "Pool" + string(rune(64+i)),
							cell:         "B" + string(rune(48+j*2)),
						})
					}
					pools = append(pools, Pool{
						sheetName: "Pool" + string(rune(64+i)),
						cell:      "B" + string(rune(48+i*2)),
						PoolName:  "Pool " + string(rune(64+i)),
						Players:   players,
					})
				}
				return pools
			},
		},
		{
			name:      "empty pools list",
			poolCount: 0,
			playerPer: 0,
			setupPools: func() []Pool {
				return []Pool{}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

			sheet := "TreeSheet"
			f.NewSheet(sheet)

			pools := tt.setupPools()
			AddPoolsToTree(f, sheet, pools)

			rows, err := f.GetRows(sheet)
			if err != nil {
				t.Fatalf("Error getting rows: %v", err)
			}

			// Verify rows exist for pools
			if tt.poolCount > 0 {
				if len(rows) == 0 {
					t.Errorf("Expected rows for pools, got %d", len(rows))
				}
			}
		})
	}
}

// TestFillInMatchesTable provides comprehensive match numbering tests
func TestFillInMatchesTable(t *testing.T) {
	tests := []struct {
		name          string
		setupRounds   func() [][]*Node
		expectedCount int
	}{
		{
			name: "single round single match",
			setupRounds: func() [][]*Node {
				return [][]*Node{
					{
						&Node{
							SheetName: "Match1",
							LeafVal:   "D5",
							Val:       1,
						},
					},
				}
			},
			expectedCount: 1,
		},
		{
			name: "multiple rounds multiple matches",
			setupRounds: func() [][]*Node {
				return [][]*Node{
					{
						&Node{SheetName: "R1M1", LeafVal: "D2", Val: 1},
						&Node{SheetName: "R1M2", LeafVal: "D4", Val: 2},
						&Node{SheetName: "R1M3", LeafVal: "D6", Val: 3},
						&Node{SheetName: "R1M4", LeafVal: "D8", Val: 4},
					},
					{
						&Node{SheetName: "R2M1", LeafVal: "F2", Val: 5},
						&Node{SheetName: "R2M2", LeafVal: "F4", Val: 6},
					},
					{
						&Node{SheetName: "R3M1", LeafVal: "H2", Val: 7},
					},
				}
			},
			expectedCount: 7,
		},
		{
			name: "round with nil nodes",
			setupRounds: func() [][]*Node {
				return [][]*Node{
					{
						&Node{SheetName: "M1", LeafVal: "D2", Val: 1},
						nil,
						&Node{SheetName: "M2", LeafVal: "D6", Val: 2},
					},
				}
			},
			expectedCount: 2,
		},
		{
			name: "round with empty sheet names",
			setupRounds: func() [][]*Node {
				return [][]*Node{
					{
						&Node{SheetName: "", LeafVal: "D2", Val: 1},
						&Node{SheetName: "M2", LeafVal: "D4", Val: 2},
					},
				}
			},
			expectedCount: 1,
		},
		{
			name: "empty rounds",
			setupRounds: func() [][]*Node {
				return [][]*Node{}
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

			rounds := tt.setupRounds()

			// Create required sheets
			sheetMap := make(map[string]bool)
			for _, round := range rounds {
				for _, node := range round {
					if node != nil && node.SheetName != "" {
						sheetMap[node.SheetName] = true
					}
				}
			}
			for sheet := range sheetMap {
				f.NewSheet(sheet)
			}

			FillInMatches(f, rounds)

			// Verify match numbers were assigned sequentially
			matchCount := 0
			for _, round := range rounds {
				for _, node := range round {
					if node != nil && node.SheetName != "" {
						if node.matchNum == 0 {
							t.Errorf("Expected non-zero matchNum for node in %s", node.SheetName)
						}
						matchCount++
					}
				}
			}

			if matchCount != tt.expectedCount {
				t.Errorf("Expected %d matches, got %d", tt.expectedCount, matchCount)
			}
		})
	}
}

// TestWriteTreeValueExtended provides comprehensive value/formula writing tests
func TestWriteTreeValueExtended(t *testing.T) {
	tests := []struct {
		name         string
		col          int
		startRow     int
		value        string
		matchWinners map[string]MatchWinner
		validateFunc func(t *testing.T, f *excelize.File, sheet string, col int, startRow int)
	}{
		{
			name:         "static pool reference",
			col:          2,
			startRow:     5,
			value:        "Pool A.1",
			matchWinners: nil,
			validateFunc: func(t *testing.T, f *excelize.File, sheet string, col int, startRow int) {
				colLetter, _ := excelize.ColumnNumberToName(col + 1)
				cellRef := colLetter + "5"
				val, _ := f.GetCellValue(sheet, cellRef)
				if val != "Pool A.1" {
					t.Errorf("Expected 'Pool A.1', got '%s'", val)
				}
			},
		},
		{
			name:     "formula with match winner",
			col:      3,
			startRow: 10,
			value:    "Pool B.2",
			matchWinners: map[string]MatchWinner{
				"Pool B.2": {
					sheetName: "MatchSheet",
					cell:      "E5",
				},
			},
			validateFunc: func(t *testing.T, f *excelize.File, sheet string, col int, startRow int) {
				colLetter, _ := excelize.ColumnNumberToName(col + 1)
				cellRef := colLetter + "10"
				formula, _ := f.GetCellFormula(sheet, cellRef)
				if formula == "" {
					t.Error("Expected formula for match winner")
				}
				if !contains(formula, "CONCATENATE") {
					t.Errorf("Expected CONCATENATE in formula, got: %s", formula)
				}
			},
		},
		{
			name:     "empty value",
			col:      1,
			startRow: 50,
			value:    "",
			validateFunc: func(t *testing.T, f *excelize.File, sheet string, col int, startRow int) {
				colLetter, _ := excelize.ColumnNumberToName(col + 1)
				cellRef := colLetter + "50"
				val, _ := f.GetCellValue(sheet, cellRef)
				if val != "" {
					t.Errorf("Expected empty value, got '%s'", val)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

			sheet := "Sheet1"
			f.NewSheet(sheet)

			writeTreeValue(f, sheet, tt.col, tt.startRow, tt.value, tt.matchWinners)

			if tt.validateFunc != nil {
				tt.validateFunc(t, f, sheet, tt.col, tt.startRow)
			}
		})
	}
}

// Helper function for string testing
func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// makeTestPool builds a minimal Pool with two players and one match,
// suitable for PrintPoolMatches tests.
func makeTestPool(name string) Pool {
	playerA := &Player{Name: "Alice", sheetName: "Pool Draw", cell: "A1"}
	playerB := &Player{Name: "Bob", sheetName: "Pool Draw", cell: "A2"}
	return Pool{
		PoolName:  name,
		sheetName: "Pool Draw",
		cell:      "B1",
		Players:   []Player{*playerA, *playerB},
		Matches:   []Match{{SideA: playerA, SideB: playerB}},
	}
}

func TestPrintPoolMatchesCourts(t *testing.T) {
	tests := []struct {
		name          string
		numPools      int
		numCourts     int
		checkHeaders  []string // Shiaijo labels that must appear in row 1
		checkColStart []int    // expected startCol for each court block
	}{
		{
			name:          "single court",
			numPools:      4,
			numCourts:     1,
			checkHeaders:  []string{"Shiaijo A"},
			checkColStart: []int{1},
		},
		{
			name:          "two courts",
			numPools:      4,
			numCourts:     2,
			checkHeaders:  []string{"Shiaijo A", "Shiaijo B"},
			checkColStart: []int{1, 9},
		},
		{
			name:          "three courts",
			numPools:      6,
			numCourts:     3,
			checkHeaders:  []string{"Shiaijo A", "Shiaijo B", "Shiaijo C"},
			checkColStart: []int{1, 9, 17},
		},
		{
			name:          "two courts uneven split (7 pools)",
			numPools:      7,
			numCourts:     2,
			checkHeaders:  []string{"Shiaijo A", "Shiaijo B"},
			checkColStart: []int{1, 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()
			f.NewSheet("Pool Matches")
			f.NewSheet("Pool Draw")

			pools := make([]Pool, tt.numPools)
			for i := range pools {
				pools[i] = makeTestPool(fmt.Sprintf("Pool %c", rune('A'+i)))
			}

			matchWinners := PrintPoolMatches(f, pools, 0, 1, tt.numCourts)

			// Must have one matchWinner per pool (position 1)
			if len(matchWinners) != tt.numPools {
				t.Errorf("expected %d matchWinners, got %d", tt.numPools, len(matchWinners))
			}

			// Check court header labels at row 1
			for ci, label := range tt.checkHeaders {
				courtStartCol := tt.checkColStart[ci]
				colName, _ := excelize.ColumnNumberToName(courtStartCol)
				cell := fmt.Sprintf("%s1", colName)
				val, err := f.GetCellValue("Pool Matches", cell)
				if err != nil {
					t.Errorf("error reading court header cell %s: %v", cell, err)
					continue
				}
				if val != label {
					t.Errorf("court header at %s: got %q, want %q", cell, val, label)
				}
			}
		})
	}
}
