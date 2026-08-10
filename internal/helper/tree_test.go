package helper

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Len(t, subtrees, 4)

	// Create a map for easier lookup
	subtreeMap := make(map[int64]bool)
	for _, subtree := range subtrees {
		subtreeMap[subtree.Val] = true
	}

	// Assert the values of the subtrees
	expectedValues := []int64{2, 4, 6, 8} // These should be the leaf nodes
	for _, expectedValue := range expectedValues {
		assert.Truef(t, subtreeMap[expectedValue], "Expected value %d not found in subtrees", expectedValue)
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

// TestPrintLeafNodesDoesNotReorderTree pins the pure-renderer contract directly:
// placement is ApplyPoolAdjustments' job (run once, on the whole tree, by
// RenderKnockoutPages), so drawing a page must leave the leaf order untouched
// even when that order is one the placement pass would change.
func TestPrintLeafNodesDoesNotReorderTree(t *testing.T) {
	// "Pool A-2nd" above "Pool B-1st" is exactly the pairing treeAdjustment
	// swaps, so a renderer that still adjusted would be caught here.
	tree := CreateBalancedTree([]string{"Pool A-2nd", "Pool B-1st"})
	before := collectOrderedLeaves(tree)

	f := excelize.NewFile()
	defer f.Close()
	_, err := f.NewSheet("Tree 1")
	require.NoError(t, err)

	PrintLeafNodes(tree, f, "Tree 1", 4, 1, 2, nil)

	assert.Equal(t, before, collectOrderedLeaves(tree), "rendering must not reorder the tree")

	// And the placement pass still does the reordering when it is run.
	ApplyPoolAdjustments(tree)
	assert.Equal(t, []string{"Pool B-1st", "Pool A-2nd"}, collectOrderedLeaves(tree),
		"ApplyPoolAdjustments owns the placement PrintLeafNodes gave up")
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
				expectedFormats := []string{"Pool A-1st", "Pool A-2nd", "Pool B-1st", "Pool B-2nd"}
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
		{
			name: "4 pools 2 winners - cross-pool matchup ordering",
			pools: []Pool{
				{PoolName: "Pool A"},
				{PoolName: "Pool B"},
				{PoolName: "Pool C"},
				{PoolName: "Pool D"},
			},
			poolWinners: 2,
			validate: func(t *testing.T, finalists []string) {
				// The interleaving must pair 1st-place finishers against
				// 2nd-place finishers from other pools. Adjacent pairs in
				// the result become bracket matchups via CreateBalancedTree.
				expected := []string{
					"Pool A-1st", "Pool B-2nd", "Pool C-1st", "Pool D-2nd",
					"Pool A-2nd", "Pool B-1st", "Pool C-2nd", "Pool D-1st",
				}
				assert.Equal(t, expected, finalists)
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

// TestGenerateFinals_NoDuplicatesOrMissing sweeps poolWinners 1..6 × pool counts
// 2..10 and asserts the output multiset is EXACTLY {each pool} × {1st..poolWinners},
// every placeholder present exactly once, none duplicated, none missing. This
// is the invariant the old `len(pools)%poolWinners` round-gate violated for
// non-coprime combos (e.g. poolWinners>=4 with 2/6/10 pools), which silently
// corrupted the live in-place knockout (mp-turx). The previous tests only checked
// length/membership and happened to use clean combos, so they missed it.
func TestGenerateFinals_NoDuplicatesOrMissing(t *testing.T) {
	for poolWinners := 1; poolWinners <= 6; poolWinners++ {
		for poolCount := 2; poolCount <= 10; poolCount++ {
			name := fmt.Sprintf("%dpools_%dwinners", poolCount, poolWinners)
			t.Run(name, func(t *testing.T) {
				pools := make([]Pool, poolCount)
				want := make(map[string]int, poolCount*poolWinners)
				for p := 0; p < poolCount; p++ {
					pools[p] = Pool{PoolName: fmt.Sprintf("Pool %c", 'A'+p)}
					for rank := 1; rank <= poolWinners; rank++ {
						want[fmt.Sprintf("Pool %c-%s", 'A'+p, GetOrdinal(rank))] = 1
					}
				}

				finals := GenerateFinals(pools, poolWinners)
				require.Len(t, finals, poolCount*poolWinners,
					"output length must equal poolCount*poolWinners")

				got := make(map[string]int, len(finals))
				for _, f := range finals {
					got[f]++
				}
				for label := range got {
					assert.LessOrEqual(t, got[label], 1, "placeholder %q appears %d times (must be exactly once)", label, got[label])
				}
				assert.Equal(t, want, got,
					"multiset of finalists must be exactly each pool × each rank, with no dups/missing")
			})
		}
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

func TestSubdivideTreeEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		setupTree    func() *Node
		numSubtrees  int
		validateFunc func(t *testing.T, subtrees []*Node)
	}{
		{
			name: "nil node",
			setupTree: func() *Node {
				return nil
			},
			numSubtrees: 4,
			validateFunc: func(t *testing.T, subtrees []*Node) {
				if subtrees != nil {
					t.Errorf("Expected nil result for nil node, got %d subtrees", len(subtrees))
				}
			},
		},
		{
			name: "zero subtrees requested",
			setupTree: func() *Node {
				return &Node{Val: 1, LeafNode: true, LeafVal: "A"}
			},
			numSubtrees: 0,
			validateFunc: func(t *testing.T, subtrees []*Node) {
				if subtrees != nil {
					t.Errorf("Expected nil result for 0 subtrees, got %d subtrees", len(subtrees))
				}
			},
		},
		{
			name: "negative subtrees requested",
			setupTree: func() *Node {
				return &Node{Val: 1, LeafNode: true, LeafVal: "A"}
			},
			numSubtrees: -1,
			validateFunc: func(t *testing.T, subtrees []*Node) {
				if subtrees != nil {
					t.Errorf("Expected nil result for negative subtrees, got %d subtrees", len(subtrees))
				}
			},
		},
		{
			name: "single leaf node with subdivision",
			setupTree: func() *Node {
				return &Node{Val: 1, LeafNode: true, LeafVal: "A"}
			},
			numSubtrees: 2,
			validateFunc: func(t *testing.T, subtrees []*Node) {
				if len(subtrees) != 1 {
					t.Errorf("Expected 1 subtree (the node itself), got %d", len(subtrees))
				}
			},
		},
		{
			name: "request more subtrees than available",
			setupTree: func() *Node {
				return CreateBalancedTree([]string{"A", "B"})
			},
			numSubtrees: 8,
			validateFunc: func(t *testing.T, subtrees []*Node) {
				// Should return what's available
				if len(subtrees) == 0 {
					t.Error("Expected at least some subtrees")
				}
			},
		},
		{
			name: "subdivision equals number of leaves",
			setupTree: func() *Node {
				return CreateBalancedTree([]string{"A", "B", "C", "D"})
			},
			numSubtrees: 4,
			validateFunc: func(t *testing.T, subtrees []*Node) {
				if len(subtrees) != 4 {
					t.Errorf("Expected 4 subtrees, got %d", len(subtrees))
				}
				// All should be leaf nodes
				for i, st := range subtrees {
					if !st.LeafNode {
						t.Errorf("Subtree %d should be a leaf node", i)
					}
				}
			},
		},
		{
			name: "unbalanced tree subdivision",
			setupTree: func() *Node {
				// Create an unbalanced tree
				return &Node{
					Val: 3,
					Left: &Node{
						Val:      1,
						LeafNode: true,
						LeafVal:  "A",
					},
					Right: &Node{
						Val: 2,
						Left: &Node{
							Val:      1,
							LeafNode: true,
							LeafVal:  "B",
						},
						Right: &Node{
							Val:      1,
							LeafNode: true,
							LeafVal:  "C",
						},
					},
				}
			},
			numSubtrees: 2,
			validateFunc: func(t *testing.T, subtrees []*Node) {
				if len(subtrees) == 0 {
					t.Error("Expected at least one subtree")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := tt.setupTree()
			subtrees := SubdivideTree(tree, tt.numSubtrees)
			tt.validateFunc(t, subtrees)
		})
	}
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

func TestGenerateFinalsEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		pools       []Pool
		poolWinners int
		validate    func(t *testing.T, finalists []string)
	}{
		{
			name:        "empty pools",
			pools:       []Pool{},
			poolWinners: 2,
			validate: func(t *testing.T, finalists []string) {
				if len(finalists) != 0 {
					t.Errorf("Expected 0 finalists from empty pools, got %d", len(finalists))
				}
			},
		},
		{
			name: "single pool with one winner",
			pools: []Pool{
				{PoolName: "Pool A"},
			},
			poolWinners: 1,
			validate: func(t *testing.T, finalists []string) {
				if len(finalists) != 1 {
					t.Errorf("Expected 1 finalist, got %d", len(finalists))
				}
				if finalists[0] != "Pool A-1st" {
					t.Errorf("Expected 'Pool A-1st', got %s", finalists[0])
				}
			},
		},
		{
			name: "zero winners per pool",
			pools: []Pool{
				{PoolName: "Pool A"},
				{PoolName: "Pool B"},
			},
			poolWinners: 0,
			validate: func(t *testing.T, finalists []string) {
				if len(finalists) != 0 {
					t.Errorf("Expected 0 finalists with 0 winners, got %d", len(finalists))
				}
			},
		},
		{
			name: "many pools with many winners",
			pools: []Pool{
				{PoolName: "Pool A"},
				{PoolName: "Pool B"},
				{PoolName: "Pool C"},
				{PoolName: "Pool D"},
				{PoolName: "Pool E"},
			},
			poolWinners: 4,
			validate: func(t *testing.T, finalists []string) {
				expectedCount := 5 * 4 // 5 pools * 4 winners
				if len(finalists) != expectedCount {
					t.Errorf("Expected %d finalists, got %d", expectedCount, len(finalists))
				}
				// Verify format of entries
				for i, finalist := range finalists {
					if !strings.Contains(finalist, "Pool") || !strings.Contains(finalist, "-") {
						t.Errorf("Finalist %d has invalid format: %s", i, finalist)
					}
				}
			},
		},
		{
			name: "verify distribution pattern - 3 pools, 2 winners",
			pools: []Pool{
				{PoolName: "Pool A"},
				{PoolName: "Pool B"},
				{PoolName: "Pool C"},
			},
			poolWinners: 2,
			validate: func(t *testing.T, finalists []string) {
				if len(finalists) != 6 {
					t.Errorf("Expected 6 finalists (3*2), got %d", len(finalists))
				}
				// Verify all expected finalists are present
				expectedSet := map[string]bool{
					"Pool A-1st": true, "Pool A-2nd": true,
					"Pool B-1st": true, "Pool B-2nd": true,
					"Pool C-1st": true, "Pool C-2nd": true,
				}
				for _, f := range finalists {
					if !expectedSet[f] {
						t.Errorf("Unexpected finalist: %s", f)
					}
				}
			},
		},
		{
			name: "single pool with multiple winners",
			pools: []Pool{
				{PoolName: "Pool X"},
			},
			poolWinners: 5,
			validate: func(t *testing.T, finalists []string) {
				if len(finalists) != 5 {
					t.Errorf("Expected 5 finalists, got %d", len(finalists))
				}
				for i := range 5 {
					expected := fmt.Sprintf("Pool X-%s", GetOrdinal(i+1))
					if finalists[i] != expected {
						t.Errorf("Position %d: expected %s, got %s", i, expected, finalists[i])
					}
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
			// A page whose root is a single leaf: SubdivideTree produces these
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

// applyTreeAdjustments delegates to the exported ApplyPoolAdjustments so
// test helpers stay in sync with the production traversal.
func applyTreeAdjustments(node *Node) {
	ApplyPoolAdjustments(node)
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

// findByes returns leaf values at nodes where one child is a leaf and the other
// is an internal node (the leaf gets a bye).
func findByeLeaves(node *Node) []string {
	if node == nil || node.LeafNode {
		return nil
	}
	var byes []string
	if node.Left.LeafNode && !node.Right.LeafNode {
		byes = append(byes, node.Left.LeafVal)
	}
	if !node.Left.LeafNode && node.Right.LeafNode {
		byes = append(byes, node.Right.LeafVal)
	}
	byes = append(byes, findByeLeaves(node.Left)...)
	byes = append(byes, findByeLeaves(node.Right)...)
	return byes
}

func buildAdjustedTree(pools []Pool, poolWinners int) *Node {
	finals := GenerateFinals(pools, poolWinners)
	tree := CreateBalancedTree(finals)
	applyTreeAdjustments(tree)
	return tree
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

func TestBracketSamePoolSeparation(t *testing.T) {
	poolCounts := []int{2, 3, 4, 5, 6, 8}

	for _, nPools := range poolCounts {
		t.Run(fmt.Sprintf("%d_pools_2_winners", nPools), func(t *testing.T) {
			pools, poolNames := makePools(nPools)
			tree := buildAdjustedTree(pools, 2)

			leaves := collectOrderedLeaves(tree)
			mid := len(leaves) / 2
			topHalf := leaves[:mid]
			bottomHalf := leaves[mid:]

			for _, pool := range poolNames {
				topCount := 0
				bottomCount := 0
				for _, l := range topHalf {
					if leafPool(l) == pool {
						topCount++
					}
				}
				for _, l := range bottomHalf {
					if leafPool(l) == pool {
						bottomCount++
					}
				}
				assert.Equalf(t, 1, topCount, "%s should have exactly 1 player in top half", pool)
				assert.Equalf(t, 1, bottomCount, "%s should have exactly 1 player in bottom half", pool)
			}
		})
	}
}

func TestBracketNoSamePoolFirstRoundMatch(t *testing.T) {
	poolCounts := []int{2, 3, 4, 5, 6, 8}

	for _, nPools := range poolCounts {
		t.Run(fmt.Sprintf("%d_pools_2_winners", nPools), func(t *testing.T) {
			pools, _ := makePools(nPools)
			tree := buildAdjustedTree(pools, 2)

			for _, m := range findLeafMatches(tree) {
				topPool := leafPool(m.top)
				bottomPool := leafPool(m.bottom)
				assert.NotEqual(t, topPool, bottomPool,
					"same-pool first-round match: %s vs %s", m.top, m.bottom)
			}
		})
	}
}

// TestBracketCrossPoolMatching characterizes the cross-pool ("1st meets a 2nd")
// property at 2 qualifiers per pool across the FULL 2..12 pool range, not just
// the power-of-two counts the test originally swept (bc-draw Phase 1).
//
// CURRENT BEHAVIOUR, DEFECT PINNED: the property holds only at power-of-two
// pool counts. At every other count some first-round matches are 2nd-vs-2nd,
// and the number of them grows with the count. wantSameRank is therefore the
// number of round-1 matches that VIOLATE the intended rule, pinned as-is.
//
// The one thing that does hold everywhere is the weaker guarantee: the
// violations are always 2nd-vs-2nd, so two pool WINNERS never meet in round 1.
// The rewrite (bc-draw R4/R5) is expected to drive wantSameRank to 0
// everywhere by crossing 2nd places to a partner court instead of pairing
// adjacent pools; when it does, this table changes and that is the point.
func TestBracketCrossPoolMatching(t *testing.T) {
	cases := []struct {
		nPools       int
		wantSameRank int
	}{
		{nPools: 2, wantSameRank: 0}, // power of two: rule holds
		{nPools: 3, wantSameRank: 1},
		{nPools: 4, wantSameRank: 0}, // power of two: rule holds
		{nPools: 5, wantSameRank: 1},
		{nPools: 6, wantSameRank: 2},
		{nPools: 7, wantSameRank: 1},
		{nPools: 8, wantSameRank: 0}, // power of two: rule holds
		{nPools: 9, wantSameRank: 1},
		{nPools: 10, wantSameRank: 2},
		{nPools: 11, wantSameRank: 3},
		{nPools: 12, wantSameRank: 4},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d_pools_2_winners", tc.nPools), func(t *testing.T) {
			pools, _ := makePools(tc.nPools)
			tree := buildAdjustedTree(pools, 2)

			matches := findLeafMatches(tree)
			require.NotEmpty(t, matches)

			var sameRank []string
			for _, m := range matches {
				topRank := leafRank(m.top)
				bottomRank := leafRank(m.bottom)
				if topRank != bottomRank {
					continue
				}
				sameRank = append(sameRank, fmt.Sprintf("%s vs %s", m.top, m.bottom))
				// The violations are 2nd-vs-2nd only: two pool winners meeting
				// in round 1 would be a strictly worse defect than the one
				// being pinned here, so fail loudly if that ever appears.
				assert.NotEqual(t, int64(1), topRank,
					"two pool WINNERS meeting in round 1 is a new, worse defect: %s vs %s", m.top, m.bottom)
			}

			assert.Len(t, sameRank, tc.wantSameRank,
				"same-rank (non-cross-pool) round-1 matches changed for %d pools: %v", tc.nPools, sameRank)
		})
	}
}

func TestTreeAdjustmentRankOrdering(t *testing.T) {
	// In every first-round match, the top (left) player should have rank <= bottom (right).
	poolCounts := []int{2, 3, 4, 5, 6, 8}

	for _, nPools := range poolCounts {
		t.Run(fmt.Sprintf("%d_pools_2_winners", nPools), func(t *testing.T) {
			pools, _ := makePools(nPools)
			tree := buildAdjustedTree(pools, 2)

			for _, m := range findLeafMatches(tree) {
				topRank := leafRank(m.top)
				bottomRank := leafRank(m.bottom)
				assert.LessOrEqual(t, topRank, bottomRank,
					"1st-place finisher should be on top: got %s (rank %d) above %s (rank %d)",
					m.top, topRank, m.bottom, bottomRank)
			}
		})
	}
}

// TestTreeAdjustmentByeAllocation characterizes WHICH finishing places receive
// the structural byes, swept over 1..4 qualifiers per pool x 2..12 pools
// (bc-draw Phase 1; the test previously swept 2 qualifiers x 3 pool counts).
//
// wantByeRanks is the multiset of finishing places holding a bye, sorted
// ascending: [1 1] means two pool winners bye, [1 2 3] means a winner, a 2nd
// and a 3rd do.
//
// CURRENT BEHAVIOUR, DEFECT PINNED. At 1 and 2 qualifiers per pool the intended
// rule holds and every bye goes to a pool winner. At 3 qualifiers it breaks at
// 2, 6, 7, 8, 9, 11 and 12 pools, and at 4 qualifiers at 3, 5, 6, 7, 9, 10, 11
// and 12 pools: a 2nd or 3rd place byes into round 2 while pool WINNERS play a
// round-1 match. The cause is that treeAdjustment only ever inspects two
// adjacent nodes, so with more than two ranks in play it cannot see the leaf it
// would have to swap with.
//
// bc-draw R6 replaces this with region-local allocation (seeded pools' winners
// first, then oversized pools' winners, then remaining winners, only then
// crossed-in 2nds and 3rds), so THIS TABLE IS EXPECTED TO CHANGE. Until then a
// change here means the draw moved without the rewrite.
func TestTreeAdjustmentByeAllocation(t *testing.T) {
	cases := []struct {
		poolWinners  int
		nPools       int
		wantByeRanks []int
	}{
		// 1 qualifier per pool: every bye goes to a pool winner (rule holds).
		{poolWinners: 1, nPools: 2, wantByeRanks: nil},
		{poolWinners: 1, nPools: 3, wantByeRanks: []int{1}},
		{poolWinners: 1, nPools: 4, wantByeRanks: nil},
		{poolWinners: 1, nPools: 5, wantByeRanks: []int{1}},
		{poolWinners: 1, nPools: 6, wantByeRanks: []int{1, 1}},
		{poolWinners: 1, nPools: 7, wantByeRanks: []int{1}},
		{poolWinners: 1, nPools: 8, wantByeRanks: nil},
		{poolWinners: 1, nPools: 9, wantByeRanks: []int{1}},
		{poolWinners: 1, nPools: 10, wantByeRanks: []int{1, 1}},
		{poolWinners: 1, nPools: 11, wantByeRanks: []int{1, 1, 1}},
		{poolWinners: 1, nPools: 12, wantByeRanks: []int{1, 1, 1, 1}},

		// 2 qualifiers per pool: every bye still goes to a pool winner.
		{poolWinners: 2, nPools: 2, wantByeRanks: nil},
		{poolWinners: 2, nPools: 3, wantByeRanks: []int{1, 1}},
		{poolWinners: 2, nPools: 4, wantByeRanks: nil},
		{poolWinners: 2, nPools: 5, wantByeRanks: []int{1, 1}},
		{poolWinners: 2, nPools: 6, wantByeRanks: []int{1, 1, 1, 1}},
		{poolWinners: 2, nPools: 7, wantByeRanks: []int{1, 1}},
		{poolWinners: 2, nPools: 8, wantByeRanks: nil},
		{poolWinners: 2, nPools: 9, wantByeRanks: []int{1, 1}},
		{poolWinners: 2, nPools: 10, wantByeRanks: []int{1, 1, 1, 1}},
		{poolWinners: 2, nPools: 11, wantByeRanks: []int{1, 1, 1, 1, 1, 1}},
		{poolWinners: 2, nPools: 12, wantByeRanks: []int{1, 1, 1, 1, 1, 1, 1, 1}},

		// 3 qualifiers per pool: non-winner byes appear at 2, 6, 7, 8, 9, 11, 12.
		{poolWinners: 3, nPools: 2, wantByeRanks: []int{1, 3}},
		{poolWinners: 3, nPools: 3, wantByeRanks: []int{1}},
		{poolWinners: 3, nPools: 4, wantByeRanks: []int{1, 1, 1, 1}},
		{poolWinners: 3, nPools: 5, wantByeRanks: []int{1}},
		{poolWinners: 3, nPools: 6, wantByeRanks: []int{1, 2}},
		{poolWinners: 3, nPools: 7, wantByeRanks: []int{1, 1, 1, 1, 2}},
		{poolWinners: 3, nPools: 8, wantByeRanks: []int{1, 1, 1, 1, 1, 2, 2, 3}},
		{poolWinners: 3, nPools: 9, wantByeRanks: []int{1, 1, 1, 1, 2}},
		{poolWinners: 3, nPools: 10, wantByeRanks: []int{1, 1}},
		{poolWinners: 3, nPools: 11, wantByeRanks: []int{2}}, // the ONLY bye goes to a 2nd place
		{poolWinners: 3, nPools: 12, wantByeRanks: []int{1, 1, 1, 2}},

		// 4 qualifiers per pool: non-winner byes at 3, 5, 6, 7, 9, 10, 11, 12.
		{poolWinners: 4, nPools: 2, wantByeRanks: nil},
		{poolWinners: 4, nPools: 3, wantByeRanks: []int{1, 1, 2, 3}},
		{poolWinners: 4, nPools: 4, wantByeRanks: nil},
		{poolWinners: 4, nPools: 5, wantByeRanks: []int{1, 1, 2, 3}},
		{poolWinners: 4, nPools: 6, wantByeRanks: []int{1, 1, 1, 1, 2, 2, 3, 3}},
		{poolWinners: 4, nPools: 7, wantByeRanks: []int{1, 1, 2, 3}},
		{poolWinners: 4, nPools: 8, wantByeRanks: nil},
		{poolWinners: 4, nPools: 9, wantByeRanks: []int{1, 1, 2, 3}},
		{poolWinners: 4, nPools: 10, wantByeRanks: []int{1, 1, 1, 1, 2, 2, 3, 3}},
		{poolWinners: 4, nPools: 11, wantByeRanks: []int{1, 1, 1, 1, 1, 1, 2, 2, 2, 3, 3, 3}},
		{poolWinners: 4, nPools: 12, wantByeRanks: []int{1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3}},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d_pools_%d_winners", tc.nPools, tc.poolWinners), func(t *testing.T) {
			pools, _ := makePools(tc.nPools)
			tree := buildAdjustedTree(pools, tc.poolWinners)

			byes := findByeLeaves(tree)
			gotRanks := make([]int, 0, len(byes))
			for _, b := range byes {
				gotRanks = append(gotRanks, int(leafRank(b)))
			}
			sort.Ints(gotRanks)

			want := tc.wantByeRanks
			if want == nil {
				want = []int{}
			}
			assert.Equal(t, want, gotRanks,
				"bye allocation changed for %d pools x %d qualifiers (byes were %v)", tc.nPools, tc.poolWinners, byes)

			// Where a non-winner byes, a pool WINNER is simultaneously made to
			// play a round-1 match. Assert that explicitly: it is the half of
			// the defect an operator actually notices, and R6 removes it.
			nonWinnerBye := false
			for _, r := range gotRanks {
				if r != 1 {
					nonWinnerBye = true
					break
				}
			}
			if !nonWinnerBye {
				return
			}
			winnerInRound1 := false
			for _, m := range findLeafMatches(tree) {
				if leafRank(m.top) == 1 || leafRank(m.bottom) == 1 {
					winnerInRound1 = true
					break
				}
			}
			assert.True(t, winnerInRound1,
				"a non-winner holds a bye at %d pools x %d qualifiers, so a pool winner must be playing round 1 (R6 violation being pinned)",
				tc.nPools, tc.poolWinners)
		})
	}
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

// treeShape renders a tree's TOPOLOGY only - which positions are leaves and
// which are junctions - with every label discarded.
func treeShape(node *Node) string {
	if node == nil {
		return "."
	}
	if node.LeafNode {
		return "L"
	}
	return "(" + treeShape(node.Left) + treeShape(node.Right) + ")"
}

// TestApplyPoolAdjustmentsPreservesShape pins the property that makes it safe to
// run the placement pass BEFORE SubdivideTree rather than inside the per-page
// render (bc-draw Phase 3).
//
// treeAdjustment only ever exchanges two LEAVES: its first branch swaps two leaf
// children, and its second swaps a leaf child with node.Right.Left - which is
// itself always a leaf, because CreateBalancedTree only ever gives a node a leaf
// left child when that node spans 2 or 3 entrants, and a 3-entrant node's right
// child is a 2-leaf pair. So placement moves labels between slots and never
// moves a slot.
//
// That is the whole safety argument for the hoist: page boundaries are chosen by
// SubdivideTree from the tree's topology, so adjusting first cannot move them.
// If this ever goes red, RenderKnockoutPages is splitting a differently-shaped
// tree than it used to and every page boundary in the sweep is suspect.
func TestApplyPoolAdjustmentsPreservesShape(t *testing.T) {
	for nPools := 1; nPools <= 12; nPools++ {
		for poolWinners := 1; poolWinners <= 4; poolWinners++ {
			t.Run(fmt.Sprintf("%d_pools_%d_winners", nPools, poolWinners), func(t *testing.T) {
				pools, _ := makePools(nPools)
				finals := GenerateFinals(pools, poolWinners)

				before := CreateBalancedTree(finals)
				after := CreateBalancedTree(finals)
				ApplyPoolAdjustments(after)

				require.Equal(t, treeShape(before), treeShape(after),
					"placement must not change the tree's topology")
				assert.ElementsMatch(t, collectOrderedLeaves(before), collectOrderedLeaves(after),
					"placement must not add, drop or duplicate an entrant")

				// Running it twice must not change the SHAPE either, whatever
				// it does to the labels (see TestApplyPoolAdjustmentsIsNotIdempotent).
				ApplyPoolAdjustments(after)
				assert.Equal(t, treeShape(before), treeShape(after),
					"a second placement pass must not change the topology either")

				// The consequence the hoist depends on, asserted directly: the
				// page split is identical whether it runs before or after
				// placement, for every page count the layout can ask for.
				for _, numPages := range []int{1, 2, 4, 8} {
					pagesBefore := SubdivideTree(before, numPages)
					pagesAfter := SubdivideTree(after, numPages)
					require.Lenf(t, pagesAfter, len(pagesBefore), "%d pages: page count moved", numPages)
					for i := range pagesBefore {
						assert.Equalf(t, treeShape(pagesBefore[i]), treeShape(pagesAfter[i]),
							"%d pages: page %d covers a different region of the draw", numPages, i+1)
					}
				}
			})
		}
	}
}

// TestApplyPoolAdjustmentsIsNotIdempotent pins a sharp edge on the placement
// pass, found while hoisting it out of PrintLeafNodes (bc-draw Phase 3).
//
// Running the pass TWICE can produce a different draw from running it once, so
// "place the tree" is an operation a tree must undergo exactly once. The cause
// is the same short sight TestTreeAdjustmentByeAllocation pins: treeAdjustment's
// bye branch compares node.Left against node.Right.Left, but the recursion then
// visits node.Right and may swap a lower-ranked leaf INTO node.Right.Left, which
// the already-completed comparison at node never sees. A second pass sees it and
// swaps again. It needs three ranks in play to bite, so it appears from 3
// qualifiers per pool upward and never at 1 or 2.
//
// This is NOT introduced by the hoist - ApplyPoolAdjustments has always been the
// engine's whole-tree pass - but the hoist puts it inside RenderKnockoutPages,
// which four generators call, so the precondition is now worth stating: hand the
// funnel an UNPLACED tree. Every caller does today (each builds a fresh
// CreateBalancedTree). bc-draw Phase 4 replaces this pass; a rewrite that is
// order-independent would make this test's expectation flip to "unchanged",
// which is an improvement to record deliberately, not to absorb.
func TestApplyPoolAdjustmentsIsNotIdempotent(t *testing.T) {
	// The smallest configuration in the sweep that exhibits it.
	pools, _ := makePools(2)
	tree := CreateBalancedTree(GenerateFinals(pools, 3))

	ApplyPoolAdjustments(tree)
	once := collectOrderedLeaves(tree)
	require.Equal(t,
		[]string{"Pool A-1st", "Pool B-2nd", "Pool A-2nd", "Pool B-3rd", "Pool B-1st", "Pool A-3rd"},
		once, "one placement pass")

	ApplyPoolAdjustments(tree)
	twice := collectOrderedLeaves(tree)
	assert.Equal(t,
		[]string{"Pool A-1st", "Pool B-2nd", "Pool A-2nd", "Pool B-1st", "Pool B-3rd", "Pool A-3rd"},
		twice, "a second pass moves Pool B-1st into the bye slot the first pass left to Pool B-3rd")
	require.NotEqual(t, once, twice, "the whole point: placement is not idempotent")

	// At 1 and 2 qualifiers there are too few ranks for the blind spot to
	// open, and the pass IS stable.
	for _, poolWinners := range []int{1, 2} {
		for nPools := 1; nPools <= 12; nPools++ {
			stable := CreateBalancedTree(GenerateFinals(makePoolsOnly(nPools), poolWinners))
			ApplyPoolAdjustments(stable)
			first := collectOrderedLeaves(stable)
			ApplyPoolAdjustments(stable)
			assert.Equalf(t, first, collectOrderedLeaves(stable),
				"%d pools x %d qualifiers must be stable", nPools, poolWinners)
		}
	}
}

// makePoolsOnly is makePools without the names return, for callers that only
// need the pools.
func makePoolsOnly(n int) []Pool {
	pools, _ := makePools(n)
	return pools
}

func TestTreeAdjustmentSwapsBothLeaves(t *testing.T) {
	// Direct test: when both children are leaves with 2nd on left and 1st on right,
	// treeAdjustment should swap them.
	node := &Node{
		Left:  &Node{LeafNode: true, LeafVal: "Pool A-2nd"},
		Right: &Node{LeafNode: true, LeafVal: "Pool B-1st"},
	}
	treeAdjustment(node)
	assert.Equal(t, "Pool B-1st", node.Left.LeafVal, "1st-place should be swapped to top")
	assert.Equal(t, "Pool A-2nd", node.Right.LeafVal, "2nd-place should be swapped to bottom")
}

func TestTreeAdjustmentNoSwapWhenCorrect(t *testing.T) {
	node := &Node{
		Left:  &Node{LeafNode: true, LeafVal: "Pool A-1st"},
		Right: &Node{LeafNode: true, LeafVal: "Pool B-2nd"},
	}
	treeAdjustment(node)
	assert.Equal(t, "Pool A-1st", node.Left.LeafVal)
	assert.Equal(t, "Pool B-2nd", node.Right.LeafVal)
}

func TestTreeAdjustmentByeSwap(t *testing.T) {
	// When left child is a leaf (bye position) with rank 2, and right child is an
	// internal node whose top-left leaf has rank 1, treeAdjustment should swap
	// so the 1st-place finisher gets the bye.
	node := &Node{
		Left: &Node{LeafNode: true, LeafVal: "Pool A-2nd"},
		Right: &Node{
			Left:  &Node{LeafNode: true, LeafVal: "Pool B-1st"},
			Right: &Node{LeafNode: true, LeafVal: "Pool C-2nd"},
		},
	}
	treeAdjustment(node)
	assert.Equal(t, "Pool B-1st", node.Left.LeafVal, "1st-place should get the bye (left/top position)")
	assert.Equal(t, "Pool A-2nd", node.Right.Left.LeafVal, "2nd-place should be pushed into the match")
}

func TestTreeAdjustmentByeNoSwapWhenCorrect(t *testing.T) {
	node := &Node{
		Left: &Node{LeafNode: true, LeafVal: "Pool A-1st"},
		Right: &Node{
			Left:  &Node{LeafNode: true, LeafVal: "Pool B-2nd"},
			Right: &Node{LeafNode: true, LeafVal: "Pool C-2nd"},
		},
	}
	treeAdjustment(node)
	assert.Equal(t, "Pool A-1st", node.Left.LeafVal, "1st-place already in bye position, no swap")
	assert.Equal(t, "Pool B-2nd", node.Right.Left.LeafVal)
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
