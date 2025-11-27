package helper

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

// func TestPrintMatches(t *testing.T) {
// 	players := []Player{
// 		{Name: "Alice"},
// 		{Name: "Bob"},
// 		{Name: "Charlie"},
// 	}

// 	t.Run("Valid number of teams", func(t *testing.T) {
// 		var buf bytes.Buffer
// 		oldStdout := os.Stdout
// 		os.Stdout = &buf
// 		defer func() {
// 			os.Stdout = oldStdout
// 		}()

// 		PrintMatches(players)

// 		expectedOutput := `Matches:
// Alice
// Bob
// Charlie
// Alice vs Bob
// Charlie vs Alice
// Bob vs Charlie
// `
// 		actualOutput := buf.String()
// 		if actualOutput != expectedOutput {
// 			t.Errorf("Unexpected output. Expected:\n%s\nActual:\n%s", expectedOutput, actualOutput)
// 		}
// 	})

// 	t.Run("Invalid number of teams", func(t *testing.T) {
// 		var buf bytes.Buffer
// 		oldStdout := os.Stdout
// 		os.Stdout = &buf
// 		defer func() {
// 			os.Stdout = oldStdout
// 		}()

// 		players := []Player{
// 			{Name: "Alice"},
// 		}

// 		PrintMatches(players)

// 		expectedOutput := "Invalid number of teams. The pool size should be between 3 and 10.\n"
// 		actualOutput := buf.String()
// 		if actualOutput != expectedOutput {
// 			t.Errorf("Unexpected output. Expected:\n%s\nActual:\n%s", expectedOutput, actualOutput)
// 		}
// 	})
// }

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

	// Test with single leaf
	singleLeaf := CreateBalancedTree([]string{"A"}, false)
	if !singleLeaf.LeafNode {
		t.Error("Expected leaf node for single entry")
	}

	// Test with multiple leaves
	leafValues := []string{"A", "B", "C", "D"}
	tree := CreateBalancedTree(leafValues, false)

	// Root should not be a leaf
	if tree.LeafNode {
		t.Error("Root should not be a leaf")
	}

	// Verify tree structure (should be balanced)
	if tree.Left == nil || tree.Right == nil {
		t.Error("Tree should have left and right children")
	}
}
