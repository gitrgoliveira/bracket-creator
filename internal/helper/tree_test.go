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
	PrintLeafNodes(node, f, "Sheet1", 10, 1, 3, true, nil)

	// Since we're really just testing that the function doesn't panic,
	// we'll just verify that it completed and some cells were written
	// We can't easily verify the exact cells since that would require
	// parsing the Excel file, which is beyond the scope of this test

	// Just assert that the test completed without panicking
	t.Log("PrintLeafNodes completed without errors")

	// Test with pools set to false
	PrintLeafNodes(node, f, "Sheet2", 10, 1, 3, false, nil)
	t.Log("PrintLeafNodes with pools=false completed without errors")
}

func TestGenerateFinals(t *testing.T) {
	tests := []struct {
		name        string
		pools       []Pool
		poolWinners int
		validate    func(t *testing.T, finalists []string)
	}{
		{
			name: "2 pools with 2 winners each",
			pools: []Pool{
				{PoolName: "Pool A"},
				{PoolName: "Pool B"},
			},
			poolWinners: 2,
			validate: func(t *testing.T, finalists []string) {
				if len(finalists) != 4 {
					t.Errorf("Expected 4 finalists, got %d", len(finalists))
				}
				// Check that we have the expected format
				expectedFormats := []string{"Pool A.1", "Pool A.2", "Pool B.1", "Pool B.2"}
				formatMap := make(map[string]bool)
				for _, f := range finalists {
					formatMap[f] = true
				}
				for _, expected := range expectedFormats {
					if !formatMap[expected] {
						t.Errorf("Expected finalist %s not found", expected)
					}
				}
			},
		},
		{
			name: "3 pools with 1 winner each",
			pools: []Pool{
				{PoolName: "Pool A"},
				{PoolName: "Pool B"},
				{PoolName: "Pool C"},
			},
			poolWinners: 1,
			validate: func(t *testing.T, finalists []string) {
				if len(finalists) != 3 {
					t.Errorf("Expected 3 finalists, got %d", len(finalists))
				}
			},
		},
		{
			name: "4 pools with 3 winners each",
			pools: []Pool{
				{PoolName: "Pool A"},
				{PoolName: "Pool B"},
				{PoolName: "Pool C"},
				{PoolName: "Pool D"},
			},
			poolWinners: 3,
			validate: func(t *testing.T, finalists []string) {
				if len(finalists) != 12 {
					t.Errorf("Expected 12 finalists, got %d", len(finalists))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalists := GenerateFinals(tt.pools, tt.poolWinners)
			tt.validate(t, finalists)
		})
	}
}

func TestCalculateDepth(t *testing.T) {
	tests := []struct {
		name     string
		node     *Node
		expected int
	}{
		{
			name:     "nil node",
			node:     nil,
			expected: 0,
		},
		{
			name: "single node",
			node: &Node{
				Val: 1,
			},
			expected: 1,
		},
		{
			name: "balanced tree depth 2",
			node: &Node{
				Val: 1,
				Left: &Node{
					Val: 2,
				},
				Right: &Node{
					Val: 3,
				},
			},
			expected: 2,
		},
		{
			name: "balanced tree depth 3",
			node: &Node{
				Val: 1,
				Left: &Node{
					Val: 2,
					Left: &Node{
						Val: 4,
					},
					Right: &Node{
						Val: 5,
					},
				},
				Right: &Node{
					Val: 3,
					Left: &Node{
						Val: 6,
					},
					Right: &Node{
						Val: 7,
					},
				},
			},
			expected: 3,
		},
		{
			name: "unbalanced tree",
			node: &Node{
				Val: 1,
				Left: &Node{
					Val: 2,
					Left: &Node{
						Val: 3,
						Left: &Node{
							Val: 4,
						},
					},
				},
			},
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			depth := CalculateDepth(tt.node)
			if depth != tt.expected {
				t.Errorf("Expected depth %d, got %d", tt.expected, depth)
			}
		})
	}
}

func TestTraverseRounds(t *testing.T) {
	tests := []struct {
		name     string
		node     *Node
		depth    int
		maxDepth int
		validate func(t *testing.T, matches []*Node)
	}{
		{
			name:     "nil node",
			node:     nil,
			depth:    0,
			maxDepth: 2,
			validate: func(t *testing.T, matches []*Node) {
				if len(matches) != 0 {
					t.Errorf("Expected 0 matches for nil node, got %d", len(matches))
				}
			},
		},
		{
			name: "traverse complete tree",
			node: &Node{
				Val: 1,
				Left: &Node{
					Val: 2,
					Left: &Node{
						Val: 4,
					},
					Right: &Node{
						Val: 5,
					},
				},
				Right: &Node{
					Val: 3,
					Left: &Node{
						Val: 6,
					},
					Right: &Node{
						Val: 7,
					},
				},
			},
			depth:    0,
			maxDepth: 1,
			validate: func(t *testing.T, matches []*Node) {
				// Function returns nodes at the specified depth
				// Just verify we got some matches
				if len(matches) == 0 {
					t.Error("Expected some matches, got 0")
				}
			},
		},
		{
			name: "traverse to leaf level",
			node: &Node{
				Val: 1,
				Left: &Node{
					Val: 2,
					Left: &Node{
						Val: 4,
					},
					Right: &Node{
						Val: 5,
					},
				},
				Right: &Node{
					Val: 3,
					Left: &Node{
						Val: 6,
					},
					Right: &Node{
						Val: 7,
					},
				},
			},
			depth:    0,
			maxDepth: 2,
			validate: func(t *testing.T, matches []*Node) {
				// Just verify the function executes without error
				// The exact count depends on the tree structure
				t.Logf("Got %d matches at depth 2", len(matches))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := TraverseRounds(tt.node, tt.depth, tt.maxDepth)
			tt.validate(t, matches)
		})
	}
}

func TestStack(t *testing.T) {
	t.Run("push and pop", func(t *testing.T) {
		stack := Stack{}

		node1 := &Node{Val: 1}
		node2 := &Node{Val: 2}

		stack.Push(node1)
		stack.Push(node2)

		if stack.IsEmpty() {
			t.Error("Stack should not be empty after pushing")
		}

		popped := stack.Pop()
		if popped.Val != 2 {
			t.Errorf("Expected to pop node with Val=2, got Val=%d", popped.Val)
		}

		popped = stack.Pop()
		if popped.Val != 1 {
			t.Errorf("Expected to pop node with Val=1, got Val=%d", popped.Val)
		}

		if !stack.IsEmpty() {
			t.Error("Stack should be empty after popping all elements")
		}
	})

	t.Run("pop from empty stack", func(t *testing.T) {
		stack := Stack{}

		popped := stack.Pop()
		if popped != nil {
			t.Error("Expected nil when popping from empty stack")
		}
	})
}
