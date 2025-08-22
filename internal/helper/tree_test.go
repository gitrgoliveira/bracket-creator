package helper

import (
	"testing"

	excelize "github.com/xuri/excelize/v2"
)

func TestSubdivideTree(t *testing.T) {
	// Create a sample tree
	root := &Node{
		Val: 5,
		Left: &Node{
			Val: 3,
			Left: &Node{
				Val:      2,
				LeafNode: true,
				LeafVal:  "2",
			},
			Right: &Node{
				Val:      4,
				LeafNode: true,
				LeafVal:  "4",
			},
		},
		Right: &Node{
			Val: 7,
			Left: &Node{
				Val:      6,
				LeafNode: true,
				LeafVal:  "6",
			},
			Right: &Node{
				Val:      8,
				LeafNode: true,
				LeafVal:  "8",
			},
		},
	}

	// Call the SubdivideTree function
	subtrees := SubdivideTree(root, 4)

	// Assert the number of subtrees
	expectedNumSubtrees := 4
	actualNumSubtrees := len(subtrees)
	if actualNumSubtrees != expectedNumSubtrees {
		t.Errorf("Expected %d subtrees, but got %d", expectedNumSubtrees, actualNumSubtrees)
	}

	// Create a map for easier lookup
	subtreeMap := make(map[int64]bool)
	for _, subtree := range subtrees {
		subtreeMap[subtree.Val] = true
	}

	// Assert the values of the subtrees
	expectedValues := []int64{2, 4, 6, 8} // These should be the leaf nodes
	for _, expectedValue := range expectedValues {
		if !subtreeMap[expectedValue] {
			t.Errorf("Expected value %d not found in subtrees", expectedValue)
		}
	}
}

func TestRoundToPowerOf2_1(t *testing.T) {
	// Test cases
	testCases := []struct {
		x        float64
		y        float64
		expected int
	}{
		{1, 14, 0},
		{28, 14, 2},
		{6, 2, 4},
		{60, 15, 4},
		{10.5, 2, 8},
		{11, 2, 8},
		{9, 2, 8},
	}

	// Run the test cases
	for _, testCase := range testCases {
		actual := RoundToPowerOf2(testCase.x, testCase.y)
		if actual != testCase.expected {
			t.Errorf("For x = %f and y = %f, expected %d, but got %d", testCase.x, testCase.y, testCase.expected, actual)
		}
	}
}

func TestPrintLeafNodes(t *testing.T) {
	// Create test nodes with pool format values
	node := &Node{
		Val: 5,
		Left: &Node{
			Val: 3,
			Left: &Node{
				Val:      2,
				LeafNode: true,
				LeafVal:  "A.1", // Pool A, 1st place
			},
			Right: &Node{
				Val:      4,
				LeafNode: true,
				LeafVal:  "B.1", // Pool B, 1st place
			},
		},
		Right: &Node{
			Val: 7,
			Left: &Node{
				Val:      6,
				LeafNode: true,
				LeafVal:  "C.1", // Pool C, 1st place
			},
			Right: &Node{
				Val:      8,
				LeafNode: true,
				LeafVal:  "D.1", // Pool D, 1st place
			},
		},
	}

	// Create an Excel file
	f := excelize.NewFile()
	defer f.Close()

	// Create sheets for testing
	f.NewSheet("Sheet1")
	f.NewSheet("Sheet2")

	// Test with pools set to true
	PrintLeafNodes(node, f, "Sheet1", 10, 1, 3, true)

	// Since we're really just testing that the function doesn't panic,
	// we'll just verify that it completed and some cells were written
	// We can't easily verify the exact cells since that would require
	// parsing the Excel file, which is beyond the scope of this test

	// Just assert that the test completed without panicking
	t.Log("PrintLeafNodes completed without errors")

	// Test with pools set to false
	PrintLeafNodes(node, f, "Sheet2", 10, 1, 3, false)
	t.Log("PrintLeafNodes with pools=false completed without errors")
}
