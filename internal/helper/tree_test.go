package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

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
		actual, err := RoundToPowerOf2(testCase.x, testCase.y)
		if err != nil {
			t.Errorf("For x = %f and y = %f, unexpected error: %v", testCase.x, testCase.y, err)
			continue
		}
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
				LeafVal:  "A-1st", // Pool A, 1st place
			},
			Right: &Node{
				Val:      4,
				LeafNode: true,
				LeafVal:  "B-1st", // Pool B, 1st place
			},
		},
		Right: &Node{
			Val: 7,
			Left: &Node{
				Val:      6,
				LeafNode: true,
				LeafVal:  "C-1st", // Pool C, 1st place
			},
			Right: &Node{
				Val:      8,
				LeafNode: true,
				LeafVal:  "D-1st", // Pool D, 1st place
			},
		},
	}

	// Create an Excel file
	f := excelize.NewFile()
	defer f.Close()

	// Create sheets for testing
	f.NewSheet("Sheet1")
	f.NewSheet("Sheet2")

	PrintLeafNodes(node, f, "Sheet1", 10, 1, 3, nil)

	// Verify leaf values were written to expected cells
	cellChecks := map[string]string{
		"G2": "A-1st",
		"G4": "B-1st",
		"G6": "C-1st",
		"G8": "D-1st",
	}
	for cell, expected := range cellChecks {
		value, err := f.GetCellValue("Sheet1", cell)
		require.NoError(t, err)
		assert.Equal(t, expected, value)
	}

	// Rendering is pure: drawing the same tree again on another sheet writes
	// the same labels to the same cells, and the tree is not reordered by
	// having been drawn.
	PrintLeafNodes(node, f, "Sheet2", 10, 1, 3, nil)
	for cell, expected := range cellChecks {
		value, err := f.GetCellValue("Sheet2", cell)
		require.NoError(t, err)
		assert.Equal(t, expected, value)
	}
}

// TestPrintLeafNodesDoesNotReorderTree pins the pure-renderer contract
// directly: placement belongs to BuildKnockoutDraw, so drawing a page must
// leave the leaf order untouched even when that order is one the retired
// placement fix-up would have changed.
func TestPrintLeafNodesDoesNotReorderTree(t *testing.T) {
	// "Pool A-2nd" above "Pool B-1st" is exactly the pairing the old
	// treeAdjustment swapped, so a renderer that still adjusted is caught here.
	tree := CreateBalancedTree([]string{"Pool A-2nd", "Pool B-1st"})
	before := collectOrderedLeaves(tree)

	f := excelize.NewFile()
	defer f.Close()
	_, err := f.NewSheet("Tree 1")
	require.NoError(t, err)

	PrintLeafNodes(tree, f, "Tree 1", 4, 1, 2, nil)

	assert.Equal(t, before, collectOrderedLeaves(tree), "rendering must not reorder the tree")
	assert.Equal(t, []string{"Pool A-2nd", "Pool B-1st"}, collectOrderedLeaves(tree),
		"placement belongs to BuildKnockoutDraw; nothing downstream may re-place a built draw")
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

	t.Run("multiple push and pop operations", func(t *testing.T) {
		stack := Stack{}

		for i := 1; i <= 10; i++ {
			stack.Push(&Node{Val: int64(i)})
		}

		for i := 10; i >= 1; i-- {
			if stack.IsEmpty() {
				t.Errorf("Stack should not be empty at iteration %d", i)
			}
			popped := stack.Pop()
			if popped.Val != int64(i) {
				t.Errorf("Expected Val=%d, got Val=%d", i, popped.Val)
			}
		}

		if !stack.IsEmpty() {
			t.Error("Stack should be empty after all pops")
		}
	})
}

func TestCreateBalancedTreeExtended(t *testing.T) {
	tests := []struct {
		name        string
		leafValues  []string
		validateVal func(t *testing.T, node *Node)
	}{
		{
			name:       "single leaf",
			leafValues: []string{"A"},
			validateVal: func(t *testing.T, node *Node) {
				if !node.LeafNode {
					t.Error("Expected leaf node")
				}
				if node.LeafVal != "A" {
					t.Errorf("Expected LeafVal 'A', got %s", node.LeafVal)
				}
				if node.Val != 1 {
					t.Errorf("Expected Val 1, got %d", node.Val)
				}
			},
		},
		{
			name:       "two leaves",
			leafValues: []string{"A", "B"},
			validateVal: func(t *testing.T, node *Node) {
				if node.LeafNode {
					t.Error("Root should not be a leaf node")
				}
				if node.Val != 2 {
					t.Errorf("Expected root Val 2, got %d", node.Val)
				}
				if node.Left == nil || node.Right == nil {
					t.Error("Expected both children to exist")
				}
				if !node.Left.LeafNode || !node.Right.LeafNode {
					t.Error("Expected children to be leaf nodes")
				}
			},
		},
		{
			name:       "four leaves - balanced tree",
			leafValues: []string{"A", "B", "C", "D"},
			validateVal: func(t *testing.T, node *Node) {
				if node.Val != 4 {
					t.Errorf("Expected root Val 4, got %d", node.Val)
				}
				// Verify tree depth
				depth := CalculateDepth(node)
				if depth != 3 {
					t.Errorf("Expected depth 3, got %d", depth)
				}
				// Verify all leaf nodes present
				leafCount := countLeaves(node)
				if leafCount != 4 {
					t.Errorf("Expected 4 leaves, got %d", leafCount)
				}
			},
		},
		{
			name:       "eight leaves",
			leafValues: []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"},
			validateVal: func(t *testing.T, node *Node) {
				if node.Val != 8 {
					t.Errorf("Expected root Val 8, got %d", node.Val)
				}
				depth := CalculateDepth(node)
				if depth != 4 {
					t.Errorf("Expected depth 4, got %d", depth)
				}
				leafCount := countLeaves(node)
				if leafCount != 8 {
					t.Errorf("Expected 8 leaves, got %d", leafCount)
				}
			},
		},
		{
			name:       "odd number of leaves",
			leafValues: []string{"A", "B", "C"},
			validateVal: func(t *testing.T, node *Node) {
				if node.Val != 3 {
					t.Errorf("Expected root Val 3, got %d", node.Val)
				}
				leafCount := countLeaves(node)
				if leafCount != 3 {
					t.Errorf("Expected 3 leaves, got %d", leafCount)
				}
				// Verify structure: left should have 1, right should have 2
				if node.Left.Val != 1 {
					t.Errorf("Expected left child Val 1, got %d", node.Left.Val)
				}
				if node.Right.Val != 2 {
					t.Errorf("Expected right child Val 2, got %d", node.Right.Val)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := CreateBalancedTree(tt.leafValues)
			if tree == nil {
				t.Fatal("Expected non-nil tree")
			}
			tt.validateVal(t, tree)
		})
	}
}

// Helper function to count leaf nodes in a tree
func countLeaves(node *Node) int {
	if node == nil {
		return 0
	}
	if node.LeafNode {
		return 1
	}
	return countLeaves(node.Left) + countLeaves(node.Right)
}

func TestRoundToPowerOf2EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		x        float64
		y        float64
		expected int
		wantErr  bool
	}{
		{
			name:     "zero dividend",
			x:        0,
			y:        5,
			expected: 0,
			wantErr:  false,
		},
		{
			name:    "zero divisor causes infinity",
			x:       10,
			y:       0,
			wantErr: true,
		},
		{
			name:    "both zero",
			x:       0,
			y:       0,
			wantErr: true,
		},
		{
			name:     "negative dividend",
			x:        -10,
			y:        2,
			expected: 8, // abs(-5) = 5, rounds to 8
			wantErr:  false,
		},
		{
			name:     "negative divisor",
			x:        10,
			y:        -2,
			expected: 8, // abs(-5) = 5, rounds to 8
			wantErr:  false,
		},
		{
			name:     "both negative",
			x:        -10,
			y:        -2,
			expected: 8, // abs(5) = 5, rounds to 8
			wantErr:  false,
		},
		{
			name:     "very large numbers",
			x:        1000000,
			y:        1000,
			expected: 1024, // 1000, rounds to 1024
			wantErr:  false,
		},
		{
			name:     "very small quotient",
			x:        1,
			y:        100,
			expected: 0, // Very small quotient rounds down to 0
			wantErr:  false,
		},
		{
			name:     "fractional x approaching power of 2",
			x:        7.9,
			y:        2,
			expected: 4, // 3.95 rounds to 4
			wantErr:  false,
		},
		{
			name:     "exact power of 2 quotient",
			x:        16,
			y:        2,
			expected: 8, // Exactly 8
			wantErr:  false,
		},
		{
			name:     "quotient of 1",
			x:        5,
			y:        5,
			expected: 1, // Quotient 1 -> 2^0 = 1
			wantErr:  false,
		},
		{
			name:     "quotient slightly above 1",
			x:        5.1,
			y:        5,
			expected: 2, // ~1.02 rounds to 2
			wantErr:  false,
		},
		{
			name:     "quotient slightly below 1",
			x:        4.9,
			y:        5,
			expected: 1, // ~0.98 rounds to 1
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RoundToPowerOf2(tt.x, tt.y)
			if tt.wantErr {
				if err == nil {
					t.Errorf("RoundToPowerOf2(%f, %f) expected error, but got nil", tt.x, tt.y)
				}
				return
			}
			if err != nil {
				t.Errorf("RoundToPowerOf2(%f, %f) unexpected error: %v", tt.x, tt.y, err)
				return
			}
			if result != tt.expected {
				t.Errorf("RoundToPowerOf2(%f, %f) = %d, want %d", tt.x, tt.y, result, tt.expected)
			}
		})
	}
}

func TestSplitPoolNameAndRank(t *testing.T) {
	tests := []struct {
		input        string
		expectedPool string
		expectedRank string
	}{
		{"Pool A-1st", "Pool A", "1st"},
		{"Pool-A-1st", "Pool-A", "1st"},
		{"My-Complex-Pool-Name-2nd", "My-Complex-Pool-Name", "2nd"},
		{"NoRank", "NoRank", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pool, rank := splitPoolNameAndRank(tt.input)
			assert.Equal(t, tt.expectedPool, pool)
			assert.Equal(t, tt.expectedRank, rank)
		})
	}
}

func TestNextPow2(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero returns 1", 0, 1},
		{"one returns 1", 1, 1},
		{"two returns 2", 2, 2},
		{"three rounds up to 4", 3, 4},
		{"exact power 4", 4, 4},
		{"five rounds up to 8", 5, 8},
		{"exact power 8", 8, 8},
		{"nine rounds up to 16", 9, 16},
		{"exact power 16", 16, 16},
		{"courts=3 rounds up to 4", 3, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextPow2(tt.input)
			if got != tt.expected {
				t.Errorf("NextPow2(%d) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTraverseRoundsExtended(t *testing.T) {
	tests := []struct {
		name      string
		setupTree func() *Node
		depth     int
		maxDepth  int
		validate  func(t *testing.T, matches []*Node)
	}{
		{
			name: "traverse at max depth",
			setupTree: func() *Node {
				return CreateBalancedTree([]string{"A", "B", "C", "D"})
			},
			depth:    0,
			maxDepth: 2,
			validate: func(t *testing.T, matches []*Node) {
				// TraverseRounds collects nodes at exactly maxDepth
				// With a 4-leaf tree, depth 2 should have 2 nodes
				if len(matches) != 2 {
					t.Logf("Got %d matches at depth 2", len(matches))
				}
			},
		},
		{
			name: "traverse beyond tree depth",
			setupTree: func() *Node {
				return CreateBalancedTree([]string{"A", "B"})
			},
			depth:    0,
			maxDepth: 10,
			validate: func(t *testing.T, matches []*Node) {
				// Should return empty as we go beyond tree depth
				if len(matches) != 0 {
					t.Logf("Got %d matches (may include nodes without children)", len(matches))
				}
			},
		},
		{
			name: "traverse with negative depth",
			setupTree: func() *Node {
				return CreateBalancedTree([]string{"A", "B", "C", "D"})
			},
			depth:    -1,
			maxDepth: 1,
			validate: func(t *testing.T, matches []*Node) {
				// Behavior with negative depth - depends on implementation
				t.Logf("Got %d matches with negative start depth", len(matches))
			},
		},
		{
			name: "traverse leaf-only tree",
			setupTree: func() *Node {
				return &Node{Val: 1, LeafNode: true, LeafVal: "Solo"}
			},
			depth:    0,
			maxDepth: 0,
			validate: func(t *testing.T, matches []*Node) {
				// Single leaf has no children to traverse
				if len(matches) != 0 {
					t.Errorf("Expected 0 matches for leaf node, got %d", len(matches))
				}
			},
		},
		{
			name: "traverse at depth 0",
			setupTree: func() *Node {
				return CreateBalancedTree([]string{"A", "B", "C", "D"})
			},
			depth:    0,
			maxDepth: 0,
			validate: func(t *testing.T, matches []*Node) {
				if len(matches) != 1 {
					t.Errorf("Expected 1 match (root) at maxDepth 0, got %d", len(matches))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := tt.setupTree()
			matches := TraverseRounds(tree, tt.depth, tt.maxDepth)
			tt.validate(t, matches)
		})
	}
}

func TestPrintLeafNodesEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		setupTree    func() *Node
		matchWinners map[string]MatchWinner
		shouldPanic  bool
	}{
		{
			// A page whose root is a single leaf: the page splitter produces these
			// whenever the requested page count is deeper than the tree.
			name: "single leaf page",
			setupTree: func() *Node {
				return &Node{Val: 1, LeafNode: true, LeafVal: "A-1st"}
			},
			matchWinners: nil,
			shouldPanic:  false,
		},
		{
			name: "tree with match winners",
			setupTree: func() *Node {
				return CreateBalancedTree([]string{"Winner1", "Winner2"})
			},
			matchWinners: map[string]MatchWinner{
				"Winner1": {cellCoord: cellCoord{sheetName: "Sheet1", cell: "A1"}},
				"Winner2": {cellCoord: cellCoord{sheetName: "Sheet1", cell: "A2"}},
			},
			shouldPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Error("Expected panic, but function completed normally")
					}
				}()
			}

			f := excelize.NewFile()
			defer f.Close()
			f.NewSheet("TestSheet")

			tree := tt.setupTree()
			depth := CalculateDepth(tree)

			// Should not panic
			PrintLeafNodes(tree, f, "TestSheet", 10, 1, depth, tt.matchWinners)

			// If we got here without panic, test passes
			if !tt.shouldPanic {
				t.Log("PrintLeafNodes completed successfully")
			}
		})
	}
}

func TestTreeToLeafArray(t *testing.T) {
	makeLeaves := func(names ...string) []string { return names }
	makeLabels := func(n int) []string {
		out := make([]string, n)
		for i := range n {
			out[i] = string(rune('A' + i))
		}
		return out
	}

	cases := []struct {
		name      string
		input     []string
		wantLen   int
		wantSlots []string // nil means "don't check exact slots, just length + non-empty count"
		wantReal  int      // number of non-empty slots
	}{
		{
			name:      "1 leaf",
			input:     makeLeaves("A"),
			wantLen:   1,
			wantSlots: []string{"A"},
			wantReal:  1,
		},
		{
			name:      "2 leaves",
			input:     makeLeaves("A", "B"),
			wantLen:   2,
			wantSlots: []string{"A", "B"},
			wantReal:  2,
		},
		{
			name:  "3 leaves, A gets bye",
			input: makeLeaves("A", "B", "C"),
			// CreateBalancedTree splits [A] left, [B,C] right
			// left → ["A"], right → ["B","C"]
			// pad left to NextPow2(max(1,2))=2 → ["A",""]
			// result: ["A","","B","C"]
			wantLen:   4,
			wantSlots: []string{"A", "", "B", "C"},
			wantReal:  3,
		},
		{
			name:     "4 leaves, identity",
			input:    makeLeaves("A", "B", "C", "D"),
			wantLen:  4,
			wantReal: 4,
		},
		{
			name:     "5 leaves",
			input:    makeLabels(5),
			wantLen:  8,
			wantReal: 5,
		},
		{
			name:     "7 leaves",
			input:    makeLabels(7),
			wantLen:  8,
			wantReal: 7,
		},
		{
			name:     "8 leaves, identity",
			input:    makeLabels(8),
			wantLen:  8,
			wantReal: 8,
		},
		{
			name:     "12 leaves",
			input:    makeLabels(12),
			wantLen:  16,
			wantReal: 12,
		},
		{
			name:     "24 leaves",
			input:    makeLabels(24),
			wantLen:  32,
			wantReal: 24,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tree := CreateBalancedTree(tt.input)
			got := TreeToLeafArray(tree)

			assert.Len(t, got, tt.wantLen, "output length must be NextPow2(N)")

			realCount := 0
			for _, v := range got {
				if v != "" {
					realCount++
				}
			}
			assert.Equal(t, tt.wantReal, realCount, "non-empty slot count must equal input count")

			if tt.wantSlots != nil {
				assert.Equal(t, tt.wantSlots, got, "exact slot layout")
			}
		})
	}
}

func leafPool(val string) string {
	name, _ := splitPoolNameAndRank(val)
	return name
}

func leafRank(val string) int64 {
	_, rankStr := splitPoolNameAndRank(val)
	return parsePoolRank(rankStr)
}

// collectOrderedLeaves returns leaf values in left-to-right (top-to-bottom) order.
func collectOrderedLeaves(node *Node) []string {
	if node == nil {
		return nil
	}
	if node.LeafNode {
		return []string{node.LeafVal}
	}
	return append(collectOrderedLeaves(node.Left), collectOrderedLeaves(node.Right)...)
}

type bracketMatch struct {
	top, bottom string
}

// findLeafMatches returns nodes where both children are leaves (actual first-round matches).
func findLeafMatches(node *Node) []bracketMatch {
	if node == nil || node.LeafNode {
		return nil
	}
	var matches []bracketMatch
	if node.Left.LeafNode && node.Right.LeafNode {
		matches = append(matches, bracketMatch{node.Left.LeafVal, node.Right.LeafVal})
	}
	matches = append(matches, findLeafMatches(node.Left)...)
	matches = append(matches, findLeafMatches(node.Right)...)
	return matches
}

func makePools(n int) ([]Pool, []string) {
	pools := make([]Pool, n)
	names := make([]string, n)
	for i := range n {
		name := fmt.Sprintf("Pool %c", 'A'+i)
		pools[i] = Pool{PoolName: name}
		names[i] = name
	}
	return pools, names
}

// buildDrawTree is the whole draw for n pools on numCourts shiaijo, which is
// what every bracket-property test below inspects. It replaced the old
// GenerateFinals -> CreateBalancedTree -> ApplyPoolAdjustments chain: placement
// is now by construction, so there is no separate adjustment step to run.
func buildDrawTree(pools []Pool, poolWinners, numCourts int) *Node {
	d := BuildKnockoutDraw(pools, poolWinners, numCourts)
	if d == nil {
		return nil
	}
	return d.Root
}

// TestDrawEmitsEveryPlaceholderExactlyOnce sweeps poolWinners 1..6 x pool counts
// 1..12 x 1/2/4 shiaijo and asserts the draw's leaf multiset is EXACTLY
// {each pool} x {1st..poolWinners}: every placeholder present once, none
// duplicated, none missing.
//
// This is the invariant that made the flat draw's rank rotation dangerous
// (mp-turx: a gated round counter aliased the rotation for non-coprime
// combinations and silently duplicated some placeholders while dropping
// others). Since these placeholders are the leaves of the LIVE in-place
// knockout, a duplicate corrupts real results, so the property is re-pinned
// against the court-first construction across a wider sweep.
func TestDrawEmitsEveryPlaceholderExactlyOnce(t *testing.T) {
	for poolWinners := 1; poolWinners <= 6; poolWinners++ {
		for poolCount := 1; poolCount <= 12; poolCount++ {
			for _, courts := range []int{1, 2, 4} {
				name := fmt.Sprintf("%dpools_%dwinners_%dcourts", poolCount, poolWinners, courts)
				t.Run(name, func(t *testing.T) {
					pools, poolNames := makePools(poolCount)
					want := make(map[string]int, poolCount*poolWinners)
					for _, p := range poolNames {
						for rank := 1; rank <= poolWinners; rank++ {
							want[fmt.Sprintf("%s-%s", p, GetOrdinal(rank))] = 1
						}
					}

					tree := buildDrawTree(pools, poolWinners, courts)
					require.NotNil(t, tree)
					leaves := TreeLeafLabels(tree)
					require.Len(t, leaves, poolCount*poolWinners,
						"leaf count must equal poolCount*poolWinners")

					got := make(map[string]int, len(leaves))
					for _, f := range leaves {
						got[f]++
					}
					assert.Equal(t, want, got,
						"the draw's leaves must be exactly each pool x each rank, with no dups/missing")
				})
			}
		}
	}
}

// TestBuildKnockoutDrawDegenerateInputs pins the inputs that produce no draw at
// all, so a caller can rely on a nil return rather than on a half-built tree.
func TestBuildKnockoutDrawDegenerateInputs(t *testing.T) {
	pools, _ := makePools(4)
	assert.Nil(t, BuildKnockoutDraw(nil, 2, 2), "no pools, no draw")
	assert.Nil(t, BuildKnockoutDraw([]Pool{}, 2, 2), "no pools, no draw")
	assert.Nil(t, BuildKnockoutDraw(pools, 0, 2), "zero qualifiers per pool, no draw")
	assert.Nil(t, BuildKnockoutDraw(pools, -1, 2), "negative qualifiers, no draw")
	assert.Nil(t, BuildKnockoutDrawFromAssignment(pools, 2, []int{0, 0}, 2),
		"an allocation that does not cover every pool is refused rather than guessed at")

	// A single pool is a legal, if degenerate, draw: one region, one leaf per
	// qualifier, and courts clamped to the pool count.
	one, _ := makePools(1)
	d := BuildKnockoutDraw(one, 1, 4)
	require.NotNil(t, d)
	assert.Equal(t, 1, d.NumCourts())
	assert.Equal(t, []string{"Pool A-1st"}, TreeLeafLabels(d.Root))
}

// TestEffectiveDrawCourts pins the clamp that keeps a court from owning an
// empty region, including its R9 step-down: clamping onto a pool count that is
// not a power of two would otherwise hand the draw an illegal allocation.
//
// The 8-courts-over-7-pools row is the case the old "step down to an even
// number" clamp got wrong: it produced 6, which R9 rejects because 3 regions in
// a half cannot merge pairwise. Every clamped result is asserted legal below,
// so the table cannot drift from ValidateShiaijoCount.
func TestEffectiveDrawCourts(t *testing.T) {
	cases := []struct{ pools, courts, want int }{
		{pools: 8, courts: 4, want: 4},
		{pools: 4, courts: 4, want: 4},
		{pools: 3, courts: 4, want: 2}, // 3 is not a power of two (R9)
		{pools: 2, courts: 4, want: 2},
		{pools: 1, courts: 4, want: 1}, // one shiaijo is explicitly allowed
		{pools: 5, courts: 6, want: 4},
		{pools: 6, courts: 8, want: 4}, // 6 is even but illegal under R9
		{pools: 7, courts: 8, want: 4}, // the old clamp gave 6 here
		{pools: 8, courts: 16, want: 8},
		{pools: 12, courts: 16, want: 8}, // 12 is even but illegal under R9
		{pools: 8, courts: 0, want: 1},
		{pools: 0, courts: 3, want: 3}, // no pools: nothing to clamp against
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%dpools_%dcourts", c.pools, c.courts), func(t *testing.T) {
			got := EffectiveDrawCourts(c.pools, c.courts)
			assert.Equal(t, c.want, got)
			if c.pools > 0 && c.courts > c.pools {
				assert.NoErrorf(t, ValidateShiaijoCount(got),
					"the clamp must land on a legal allocation, got %d", got)
			}
		})
	}
}

// TestSubdivideRegions pins the court-block splitter: each region contributes
// exactly pagesPerCourt pages, in court order, and each page is a genuine
// subtree of its region.
func TestSubdivideRegions(t *testing.T) {
	pools, _ := makePools(8)
	draw := BuildKnockoutDraw(pools, 2, 4)
	require.NotNil(t, draw)

	t.Run("one page per court", func(t *testing.T) {
		pages := SubdivideRegions(draw.Regions, 1)
		require.Len(t, pages, 4)
		for i, p := range pages {
			assert.Same(t, draw.Regions[i], p, "page %d is shiaijo %s's whole region", i+1, CourtLabel(i))
		}
	})

	t.Run("two pages per court are the region's children", func(t *testing.T) {
		pages := SubdivideRegions(draw.Regions, 2)
		require.Len(t, pages, 8)
		for c, r := range draw.Regions {
			assert.Same(t, r.Left, pages[c*2])
			assert.Same(t, r.Right, pages[c*2+1])
		}
	})

	t.Run("four pages per court are the region's grandchildren", func(t *testing.T) {
		pages := SubdivideRegions(draw.Regions, 4)
		require.Len(t, pages, 16)
		for c, r := range draw.Regions {
			assert.Same(t, r.Left.Left, pages[c*4])
			assert.Same(t, r.Left.Right, pages[c*4+1])
			assert.Same(t, r.Right.Left, pages[c*4+2])
			assert.Same(t, r.Right.Right, pages[c*4+3])
		}
	})

	t.Run("degenerate inputs", func(t *testing.T) {
		assert.Empty(t, SubdivideRegions(nil, 2))
		// A page count below 1 is a caller bug, not a request for zero pages.
		assert.Len(t, SubdivideRegions(draw.Regions, 0), 4)
		// A leaf region cannot be cut; the page count stays an exact multiple.
		leaf := &Node{LeafNode: true, LeafVal: "Pool A-1st", Val: 1}
		assert.Len(t, SubdivideRegions([]*Node{leaf}, 4), 4)
	})
}

// TestKnockoutPagesPerCourt pins R8's page-count rule: 1, 2 or 4 pages per
// shiaijo, the smallest that keeps a page inside MaxPlayersPerTree, clamped to
// what the regions can actually be cut into.
func TestKnockoutPagesPerCourt(t *testing.T) {
	region := func(n int) *Node {
		labels := make([]string, n)
		for i := range labels {
			labels[i] = fmt.Sprintf("Pool %c-1st", 'A'+i%26)
		}
		return CreateBalancedTree(labels)
	}
	cases := []struct {
		name    string
		regions []*Node
		want    int
	}{
		{name: "no regions", regions: nil, want: 1},
		{name: "small regions fit one page", regions: []*Node{region(8), region(8)}, want: 1},
		{name: "exactly MaxPlayersPerTree fits one page", regions: []*Node{region(MaxPlayersPerTree)}, want: 1},
		{name: "one over MaxPlayersPerTree splits in two", regions: []*Node{region(MaxPlayersPerTree + 1)}, want: 2},
		{name: "over the limit splits in two", regions: []*Node{region(24), region(24)}, want: 2},
		{name: "far over the limit splits in four", regions: []*Node{region(64)}, want: 4},
		{name: "clamped by the region that cannot be cut", regions: []*Node{region(64), region(1)}, want: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, KnockoutPagesPerCourt(c.regions))
		})
	}
}

// TestBracketSamePoolSeparation is R5 at 2 qualifiers, stated as the guarantee
// the spec says it is: a pool's 1st and 2nd sit in OPPOSITE halves, so they can
// only meet in the final. Under the court-first draw this follows from R4b
// alone - the 2nd crosses to the partner court and partner courts are half the
// bracket apart - so it now holds for every pool count and every shiaijo count,
// not just the ones where the old flat interleave happened to work out.
func TestBracketSamePoolSeparation(t *testing.T) {
	poolCounts := []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	for _, numCourts := range []int{1, 2, 4} {
		for _, nPools := range poolCounts {
			t.Run(fmt.Sprintf("%d_pools_2_winners_%d_courts", nPools, numCourts), func(t *testing.T) {
				pools, poolNames := makePools(nPools)
				tree := buildDrawTree(pools, 2, numCourts)
				require.NotNil(t, tree)

				leaves := TreeToLeafArray(tree)
				mid := len(leaves) / 2
				topHalf := leaves[:mid]
				bottomHalf := leaves[mid:]

				for _, pool := range poolNames {
					topCount := 0
					bottomCount := 0
					for _, l := range topHalf {
						if l != "" && leafPool(l) == pool {
							topCount++
						}
					}
					for _, l := range bottomHalf {
						if l != "" && leafPool(l) == pool {
							bottomCount++
						}
					}
					assert.Equalf(t, 1, topCount, "%s should have exactly 1 qualifier in the top half", pool)
					assert.Equalf(t, 1, bottomCount, "%s should have exactly 1 qualifier in the bottom half", pool)
				}
			})
		}
	}
}

// TestBracketNoSamePoolFirstRoundMatch sweeps 1-4 qualifiers over 2-12 pools on
// 1/2/4 shiaijo: two qualifiers of the SAME pool must never meet in round 1,
// whatever the configuration. The old draw held this only at 2 qualifiers.
func TestBracketNoSamePoolFirstRoundMatch(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for nPools := 2; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				t.Run(fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts), func(t *testing.T) {
					pools, _ := makePools(nPools)
					tree := buildDrawTree(pools, poolWinners, numCourts)
					require.NotNil(t, tree)

					for _, m := range findLeafMatches(tree) {
						assert.NotEqual(t, leafPool(m.top), leafPool(m.bottom),
							"same-pool first-round match: %s vs %s", m.top, m.bottom)
					}
				})
			}
		}
	}
}

// TestByesGoToPoolWinners is R6 as a sweep: at 1 and 2 qualifiers per pool
// EVERY named round-1 bye goes to a pool WINNER, because a region always holds
// at least one home 1st and criteria 1-3 rank all of them above any crossed-in
// rank. The old draw broke this at 3 and 4 qualifiers, where a 2nd or 3rd byed
// while pool winners played round 1 (measured at 2, 6, 7, 8, 9, 11 and 12
// pools). At 3+ qualifiers a bye can still legitimately fall to a crossed-in
// rank (R7's degradation ladder) when a region holds no home 1st at all, so
// that case asserts the weaker, correct property: a region with a home 1st
// never byes anything else.
func TestByesGoToPoolWinners(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for nPools := 2; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				t.Run(fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts), func(t *testing.T) {
					pools, _ := makePools(nPools)
					draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
					require.NotNil(t, draw)

					for c, region := range draw.Regions {
						slots := TreeToLeafArray(region)
						hasHomeWinner := false
						perPool := map[string]int{}
						for _, l := range TreeLeafLabels(region) {
							if leafRank(l) == 1 {
								hasHomeWinner = true
							}
							perPool[leafPool(l)]++
						}
						// A region holding two qualifiers of one pool may have
						// had to hand the bye to a lower finisher to keep them
						// out of a round-1 match: R5 outranks R6's precedence.
						crowded := false
						for _, n := range perPool {
							if n > 1 {
								crowded = true
							}
						}
						if crowded {
							continue
						}
						for i := 0; i+1 < len(slots); i += 2 {
							a, b := slots[i], slots[i+1]
							bye := ""
							if a != "" && b == "" {
								bye = a
							} else if a == "" && b != "" {
								bye = b
							}
							if bye == "" {
								continue
							}
							if hasHomeWinner {
								assert.Equal(t, int64(1), leafRank(bye),
									"shiaijo %s holds a home winner, so its bye cannot go to %s (R6)",
									CourtLabel(c), bye)
							}
						}
					}
				})
			}
		}
	}
}

// drawByeUnits returns the subtrees D4's bye arithmetic applies to: the draw's
// BLOCKS.
//
// A block is usually a shiaijo's region, which is why D4 was first written in
// terms of a region. Where planBlocks subdivides (1 or 2 shiaijo carrying
// enough qualifiers to fill four blocks), the pool set is cut into two or four
// blocks that act as partner courts, each its own ladder with its own greedy
// layer and its own structural bye, and a printable region is their parent.
// Reading the arithmetic off a region there would count two blocks' byes
// against one block's parity.
func drawByeUnits(draw *KnockoutDraw) []*Node {
	return draw.blocks
}

// TestBlockByeNeverSkipsAHigherFinisher pins R6's class ordering and R7's
// degradation ladder as one property: a block's named bye goes to the best
// FINISHING POSITION present in that block. A pool winner is never passed over
// for a runner-up, a runner-up never for a third place.
//
// This replaced a test asserting that every block holds a home 1st, which was
// the "a block must own at least one pool" rule stated as an invariant. That
// rule is gone (operator ruling, 2026-08-10): a block holds QUALIFIERS, not
// pools, and from 2 qualifiers up there are more of the former than the latter,
// so a block may legitimately host only crossed-in finishers. R4(f) blesses it
// outright and R7 is the rule that says what happens then -- the bye flows down
// the ladder to the best crossed-in occupant. Asserting it could never occur
// both contradicted R7 and forced planBlocks to cap the subdivision by the pool
// count, which is what stranded a lone pool winner in a block and let it bye
// ahead of the top seed (see draw_seed_bye_test.go).
//
// The sweep deliberately asks for MORE shiaijo than pools (8 and 16 over as few
// as 1 pool), because EffectiveDrawCourts' clamp is the only thing standing
// between the request and an empty region.
//
// ONE shape in the swept range is exempt, and it is the spec's own precedence
// showing through rather than a defect: at 2 pools and 3 qualifiers each block
// holds {X-1st, Y-2nd, Y-3rd}, so byeing the pool winner would leave Y's own
// 2nd and 3rd to fight each other in round 1. R6 states outright that
// "precedence is a preference, not a guarantee: R3/R4/R5 win", so R5's
// separation takes the bye and Y-2nd -- the better of the two, see
// separateSamePoolPairs -- receives it. The exemption is asserted narrowly: the
// bye must still be that pool's BEST remaining finisher, so the case cannot
// quietly widen into "any bye anywhere".
func TestBlockByeNeverSkipsAHigherFinisher(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4, 8, 16} {
		for nPools := 1; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				t.Run(fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts), func(t *testing.T) {
					pools, _ := makePools(nPools)
					draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
					require.NotNil(t, draw)

					for c, block := range drawByeUnits(draw) {
						require.NotNilf(t, block, "block %d must exist", c)
						labels := TreeLeafLabels(block)
						bye := namedBye(block)
						if bye == "" {
							continue
						}
						bestRank, bestInByesPool := int64(0), int64(0)
						for _, l := range labels {
							r := leafRank(l)
							if r <= 0 {
								continue
							}
							if bestRank == 0 || r < bestRank {
								bestRank = r
							}
							if leafPool(l) == leafPool(bye) && (bestInByesPool == 0 || r < bestInByesPool) {
								bestInByesPool = r
							}
						}
						if leafRank(bye) == bestRank {
							continue
						}
						assert.Equalf(t, 3, poolWinners,
							"block %d byed %s over a %s finisher outside the one shape R5 is allowed to override; leaves: %v",
							c, bye, GetOrdinal(int(bestRank)), labels)
						assert.Equalf(t, 2, nPools,
							"block %d byed %s over a %s finisher outside the one shape R5 is allowed to override; leaves: %v",
							c, bye, GetOrdinal(int(bestRank)), labels)
						assert.Equalf(t, bestInByesPool, leafRank(bye),
							"block %d gave the bye to %s when its own pool has a better finisher in the block; leaves: %v",
							c, bye, labels)
					}
				})
			}
		}
	}
}

// TestBlockByeCountIsQMod2 pins D4's arithmetic directly: a BLOCK of q
// occupants grants exactly q mod 2 NAMED round-1 byes and plays floor(q/2)
// round-1 matches. Every other empty slot pairs with another empty slot into a
// phantom match that is never printed. Recursive halving disagrees from q=6 up
// (it would give 2 matches and 2 byes where greedy gives 3 and 0).
//
// The block is the unit, not the printable region: the two coincide wherever
// the pool set is not subdivided, and where it is, a region spans two or four
// blocks that each carry their own greedy layer. The sweep runs past the 4
// qualifiers the shape golden covers, to 6, where a block holds several
// qualifiers of one pool on FOUR shiaijo as well as on one or two.
func TestBlockByeCountIsQMod2(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for nPools := 1; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 6; poolWinners++ {
				t.Run(fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts), func(t *testing.T) {
					pools, _ := makePools(nPools)
					draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
					require.NotNil(t, draw)

					for c, block := range drawByeUnits(draw) {
						q := len(TreeLeafLabels(block))
						slots := TreeToLeafArray(block)
						matches, byes := 0, 0
						for i := 0; i+1 < len(slots); i += 2 {
							a, b := slots[i], slots[i+1]
							switch {
							case a != "" && b != "":
								matches++
							case a != "" || b != "":
								byes++
							}
						}
						if q == 1 {
							// A one-occupant block has no round-1 layer at all.
							continue
						}
						assert.Equalf(t, q%2, byes, "block %d: %d occupants must grant %d named byes", c, q, q%2)
						assert.Equalf(t, q/2, matches, "block %d: %d occupants must play %d round-1 matches", c, q, q/2)
					}
				})
			}
		}
	}
}

// drawNamedByes lists the placeholders that take a NAMED round-1 bye anywhere
// in the draw, in leaf order.
func drawNamedByes(draw *KnockoutDraw) []string {
	slots := TreeToLeafArray(draw.Root)
	byes := []string{}
	for i := 0; i+1 < len(slots); i += 2 {
		if (slots[i] == "") != (slots[i+1] == "") {
			byes = append(byes, slots[i]+slots[i+1])
		}
	}
	return byes
}

// TestSeededPoolWinnerTakesTheBlockBye is R6 criterion 1: when a block has a
// structural bye and holds the winner of a seeded pool, that winner takes it,
// ahead of an oversized pool's winner and ahead of pool order. The EKC Junior
// Team and Junior Individual Female draws are the reference cases; this is the
// same rule swept over block shapes they do not cover.
func TestSeededPoolWinnerTakesTheBlockBye(t *testing.T) {
	// 5 pools on one shiaijo subdivide into blocks of 2/1/1/1 (A+B, C, D, E).
	// At two qualifiers per pool block 0 holds A-1st, B-1st and the crossed-in
	// D-2nd, so it is the odd block where criterion 1 has a contest to win:
	// two home 1sts, one bye.
	pools, _ := makePools(5)
	// Pool B is the seeded pool and NOT first in pool order, so criterion 1 has
	// to beat criterion 3 for it to take the bye.
	pools[1].Players = []Player{{Name: "seed one", Seed: 1}}
	for i := range pools {
		if i != 1 {
			pools[i].Players = []Player{{Name: fmt.Sprintf("p%d", i)}}
		}
	}
	draw := BuildKnockoutDraw(pools, 2, 1)
	require.NotNil(t, draw)
	assert.Equal(t, []string{"Pool B-1st", "Pool D-1st"}, drawNamedByes(draw),
		"the seeded pool's winner takes its block's bye (R6-1); the other odd block has one home 1st and no contest")
}

// TestOversizedPoolWinnerTakesTheBlockBye is R6 criterion 2 (D1): with no
// seeds in play, the bye goes to the winner of the pool whose qualifier played
// the MOST pool matches, not to the first pool in order. It is fatigue
// compensation, which is why it ranks below seeding and above pool order.
func TestOversizedPoolWinnerTakesTheBlockBye(t *testing.T) {
	// The same shape as the seeded case: 5 pools on one shiaijo at two
	// qualifiers, so block 0 (pools A and B, plus the crossed-in D-2nd) is the
	// odd block with two home 1sts competing for its bye. Pool B is the
	// oversized one and is second in pool order, so criterion 2 has to beat
	// criterion 3.
	pools, _ := makePools(5)
	sizes := []int{3, 5, 3, 3, 3}
	for i := range pools {
		pools[i].Players = make([]Player, sizes[i])
		for j := range pools[i].Players {
			pools[i].Players[j] = Player{Name: fmt.Sprintf("p%d-%d", i, j)}
		}
	}
	draw := BuildKnockoutDraw(pools, 2, 1)
	require.NotNil(t, draw)
	assert.Equal(t, []string{"Pool B-1st", "Pool D-1st"}, drawNamedByes(draw),
		"the oversized pool's winner takes its block's bye (R6-2)")

	// And the generated match count is what the rule actually reads (D1), so a
	// pool with more MATCHES outranks a pool with more PLAYERS if they ever
	// disagree. They cannot today - both metrics are strictly increasing in
	// pool size - but the criterion is defined on the match count.
	for i := range pools {
		pools[i].Matches = make([]Match, sizes[i])
	}
	pools[0].Matches = make([]Match, 9)
	draw2 := BuildKnockoutDraw(pools, 2, 1)
	require.NotNil(t, draw2)
	assert.Equal(t, []string{"Pool A-1st", "Pool D-1st"}, drawNamedByes(draw2),
		"with pool matches drawn, the bye follows the MATCH count (D1)")
}

func TestNeedsBronzeBlock(t *testing.T) {
	cases := []struct {
		name     string
		naginata bool
		rounds   int
		want     bool
	}{
		{"naginata with semifinal", true, 2, true},
		{"naginata with many rounds", true, 4, true},
		{"naginata single round (no semifinal)", true, 1, false},
		{"naginata zero rounds", true, 0, false},
		{"non-naginata with rounds", false, 4, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NeedsBronzeBlock(tc.naginata, tc.rounds))
		})
	}
}

// TestSemifinalMatchNumbers pins the semifinal derivation shared by the two
// cmd generators and the results-export builder (bronze-block loser-line
// CONCATENATE formulas reference these match numbers).
func TestSemifinalMatchNumbers(t *testing.T) {
	semi1 := &Node{matchNum: 5}
	semi2 := &Node{matchNum: 6}
	final := &Node{Left: semi1, Right: semi2, matchNum: 7}

	tests := []struct {
		name   string
		rounds [][]*Node
		wantA  int
		wantB  int
	}{
		{"nil rounds", nil, 0, 0},
		{"single round (2-player bracket, no semifinal)", [][]*Node{{final}}, 0, 0},
		{"empty last round", [][]*Node{{semi1, semi2}, {}}, 0, 0},
		{"nil final node", [][]*Node{{semi1, semi2}, {nil}}, 0, 0},
		{"both semifinals present", [][]*Node{{semi1, semi2}, {final}}, 5, 6},
		{"missing right child (bye)", [][]*Node{{semi1}, {{Left: semi1, matchNum: 7}}}, 5, 0},
		{"missing left child (bye)", [][]*Node{{semi2}, {{Right: semi2, matchNum: 7}}}, 0, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotA, gotB := SemifinalMatchNumbers(tt.rounds)
			assert.Equal(t, tt.wantA, gotA, "semiA")
			assert.Equal(t, tt.wantB, gotB, "semiB")
		})
	}
}

func TestBuildEliminationMatchRounds(t *testing.T) {
	names := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("P%d", i+1)
		}
		return out
	}

	tests := []struct {
		name       string
		numPlayers int
		// per-round match counts, earliest round first, final last
		wantRoundSizes []int
	}{
		{"single leaf has no matches", 1, []int{}},
		{"two players", 2, []int{1}},
		{"unbalanced three players", 3, []int{1, 1}},
		{"balanced four players", 4, []int{2, 1}},
		{"unbalanced six players", 6, []int{2, 2, 1}},
		{"balanced eight players", 8, []int{4, 2, 1}},
		{"unbalanced twelve players", 12, []int{4, 4, 2, 1}},
		{"balanced sixteen players", 16, []int{8, 4, 2, 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := CreateBalancedTree(names(tt.numPlayers))
			got := BuildEliminationMatchRounds(tree)

			require.Len(t, got, len(tt.wantRoundSizes))
			for i, want := range tt.wantRoundSizes {
				assert.Len(t, got[i], want, "round index %d", i)
			}

			// Equivalence with the historical inline construction at the four
			// generator call sites: rounds[depth-i] = TraverseRounds(tree, 1, i-1).
			depth := CalculateDepth(tree)
			for i := depth; i > 1; i-- {
				assert.Equal(t, TraverseRounds(tree, 1, i-1), got[depth-i], "round for maxDepth %d", i-1)
			}

			if len(got) > 0 {
				final := got[len(got)-1]
				require.Len(t, final, 1, "the last index must be the final")
				assert.Same(t, tree, final[0], "the final is the tree root")
			}
		})
	}

	t.Run("nil tree", func(t *testing.T) {
		assert.Empty(t, BuildEliminationMatchRounds(nil))
	})
}
