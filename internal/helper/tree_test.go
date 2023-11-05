package helper

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestSubdivideTree(t *testing.T) {
	// Create a sample tree
	root := &Node{
		Val: 5,
		Left: &Node{
			Val: 3,
			Left: &Node{
				Val: 2,
			},
			Right: &Node{
				Val: 4,
			},
		},
		Right: &Node{
			Val: 7,
			Left: &Node{
				Val: 6,
			},
			Right: &Node{
				Val: 8,
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

	// Assert the values of the subtrees
	expectedValues := []int{2, 3, 4, 5, 6, 7, 8}
	for _, expectedValue := range expectedValues {
		found := false
		for _, subtree := range subtrees {
			if subtree.Val == expectedValue {
				found = true
				break
			}
		}
		if !found {
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
	// Test case 1: node is nil
	node1 := &Node{}
	f1 := excelize.NewFile()
	sheetName1 := "Sheet1"
	startCol1 := 1
	startRow1 := 1
	depth1 := 1
	pools1 := true
	PrintLeafNodes(node1, f1, sheetName1, startCol1, startRow1, depth1, pools1)

	// Test case 2: node is a leaf node
	node2 := &Node{LeafNode: true, LeafVal: "LeafValue"}
	f2 := excelize.NewFile()
	sheetName2 := "Sheet2"
	startCol2 := 2
	startRow2 := 2
	depth2 := 2
	pools2 := false
	PrintLeafNodes(node2, f2, sheetName2, startCol2, startRow2, depth2, pools2)

	// Test case 3: node is not a leaf node
	node3 := &Node{LeafNode: false, Left: &Node{LeafNode: true, LeafVal: "LeftLeafValue"}, Right: &Node{LeafNode: true, LeafVal: "RightLeafValue"}}
	f3 := excelize.NewFile()
	sheetName3 := "Sheet3"
	startCol3 := 3
	startRow3 := 3
	depth3 := 3
	pools3 := true
	PrintLeafNodes(node3, f3, sheetName3, startCol3, startRow3, depth3, pools3)
}
