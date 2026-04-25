package helper

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	excelize "github.com/xuri/excelize/v2"
)

type Node struct {
	LeafNode bool

	// sheet for Cell Values
	SheetName string

	// match number
	matchNum int64

	// Pool Number or Cell value
	LeafVal string
	Val     int64
	Left    *Node
	Right   *Node
}

func CreateBalancedTree(leafValues []string) *Node {
	mid := len(leafValues) / 2
	node := &Node{}

	if len(leafValues) == 1 {
		node.LeafVal = leafValues[0]
		node.LeafNode = true
		node.Val = 1
		return node
	}

	node.Left = CreateBalancedTree(leafValues[:mid])
	node.Right = CreateBalancedTree(leafValues[mid:])
	node.LeafNode = false
	node.Val = node.Left.Val + node.Right.Val

	return node
}

func PrintLeafNodes(node *Node, f *excelize.File, sheetName string, startCol int, startRow int, depth int, pools bool, matchWinners map[string]MatchWinner) {
	if node == nil {
		return
	}

	if pools && !node.LeafNode {
		// Need to ensure pools winners stay on top and pool winners are the ones that get a bye
		treeAdjustment(node)
	}
	// emptyRows := 2 * (depth + 1) //int(math.Pow(2, float64(depth))) - 3
	// fmt.Println(emptyRows)

	size := int(math.Pow(2, float64(depth-1)))

	if node.LeafNode {
		writeTreeValue(f, sheetName, startCol, startRow+size, node.LeafVal, matchWinners)
	} else {
		// this collects the cell coordinates for the match number in the tree
		node.LeafVal = CreateTreeBracket(f, sheetName, startCol, startRow+size/2+1, size-1)
		node.SheetName = sheetName // How is this used?
	}

	PrintLeafNodes(node.Left, f, sheetName, startCol-2, startRow, depth-1, pools, matchWinners)
	PrintLeafNodes(node.Right, f, sheetName, startCol-2, startRow+size, depth-1, pools, matchWinners)
}

// treeAdjustment repositions leaf nodes within a two-level subtree so that
// the lower-position pool finalist (e.g. "-1st" beats "-2nd") appears at the top
// of a match pair.  In the Excel layout a smaller row number is the preferred
// / seeded side that receives a bye when there is an odd number of players,
// so putting the first-place finisher on top is necessary for correct seeding.
//
// Two cases are handled:
//  1. Both children are leaf nodes → swap them so the lower-position value
//     is on the left (top) child.
//  2. Left child is a leaf and right child is an internal node → swap the leaf
//     with the right node's top-left leaf if the incoming leaf has a lower
//     position, ensuring the first-place finisher gets the bye at this level.
func treeAdjustment(node *Node) {

	if node.Left.LeafNode && node.Right.LeafNode {

		// Need to ensure pools winners stay on top
		_, leftRankStr := splitPoolNameAndRank(node.Left.LeafVal)
		leftPos := parsePoolRank(leftRankStr)
		_, rightRankStr := splitPoolNameAndRank(node.Right.LeafVal)
		rightPos := parsePoolRank(rightRankStr)

		// For that we need to ensure the last character of the left (i.e. top) node is higher than the right
		if leftPos > rightPos {
			node.Left, node.Right = node.Right, node.Left
		}
	}

	// Also need to ensure pool winners are the ones that get a bye
	if node.Left.LeafNode && !node.Right.LeafNode {
		// find a second placed pool winner on the other branch
		_, leftRankStr := splitPoolNameAndRank(node.Left.LeafVal)
		leftPos := parsePoolRank(leftRankStr)
		_, rightRankStr := splitPoolNameAndRank(node.Right.Left.LeafVal)
		rightPos := parsePoolRank(rightRankStr)

		// For that we need to ensure the last character of the left (i.e. top) node is higher than the left of the right branch
		if leftPos > rightPos {
			node.Left, node.Right.Left = node.Right.Left, node.Left
		}
	}
}

func splitPoolNameAndRank(val string) (string, string) {
	idx := strings.LastIndex(val, "-")
	if idx == -1 {
		return val, ""
	}
	return val[:idx], val[idx+1:]
}

func parsePoolRank(rankStr string) int64 {
	if rankStr == "" {
		return 0
	}
	// Remove ordinal suffix (st, nd, rd, th)
	s := rankStr
	if len(s) > 2 {
		s = s[:len(s)-2]
	}
	pos, _ := strconv.ParseInt(s, 10, 64)
	return pos
}

// GenerateFinals interleaves pool finalists so that when CreateBalancedTree
// distributes them into bracket slots, the first-place finisher of one pool
// is paired against the second-place finisher of another pool.
//
// The algorithm cycles through all pools, advancing a round counter every
// time a full set of pools has been placed.  The position rank for each slot
// is computed as (i + round) % poolWinners, which shifts the rank picked for
// successive pools so that adjacent bracket slots always contain different
// finishing positions.
//
// Example with 4 pools and 2 winners per pool:
//
//	result = [Pool_A-1st, Pool_B-2nd, Pool_C-1st, Pool_D-2nd,
//	          Pool_A-2nd, Pool_B-1st, Pool_C-2nd, Pool_D-1st]
func GenerateFinals(pools []Pool, poolWinners int) []string {
	if poolWinners <= 0 || len(pools) == 0 {
		return nil
	}

	finalists := make([][]string, len(pools))
	for i := 0; i < len(pools); i++ {
		for j := 0; j < poolWinners; j++ {
			finalists[i] = append(finalists[i], fmt.Sprintf("%s-%s", pools[i].PoolName, getOrdinal(j+1)))
		}
	}

	matches := make([]string, 0, len(pools)*poolWinners)
	round := -1
	for i := 0; i < len(pools)*poolWinners; i++ {

		poolPos := i % len(pools)

		if poolPos == 0 && len(pools)%poolWinners == 0 {
			round++
		} else if round < 0 {
			round = 0
		}
		pos := (i + round) % poolWinners
		matches = append(matches, finalists[poolPos][pos])
	}

	return matches
}

func CalculateDepth(node *Node) int {
	if node == nil {
		return 0
	}

	leftDepth := CalculateDepth(node.Left)
	rightDepth := CalculateDepth(node.Right)

	return int(math.Max(float64(leftDepth), float64(rightDepth))) + 1
}

type Stack []*Node

func (s *Stack) Push(node *Node) {
	*s = append(*s, node)
}

func (s *Stack) Pop() *Node {
	if s.IsEmpty() {
		return nil
	}
	index := len(*s) - 1
	node := (*s)[index]
	*s = (*s)[:index]
	return node
}

func (s *Stack) IsEmpty() bool {
	return len(*s) == 0
}

func TraverseRounds(node *Node, depth int, maxDepth int) []*Node {
	if node == nil || node.Left == nil || node.Right == nil {
		return []*Node{}
	}

	var matches []*Node

	if depth == maxDepth {
		matches = append(matches, node)
	}

	// Then traverse the left subtree
	leftMatches := TraverseRounds(node.Left, depth+1, maxDepth)

	// Traverse the right subtree first
	rightMatches := TraverseRounds(node.Right, depth+1, maxDepth)

	matches = append(matches, leftMatches...)
	matches = append(matches, rightMatches...)

	return matches

}

// function that subdivides a tree into a specified number of subtrees
func SubdivideTree(node *Node, numSubtrees int) []*Node {
	if node == nil || numSubtrees <= 0 {
		return nil
	}
	subtrees := []*Node{}
	if node.Left != nil {
		subtrees = append(subtrees, SubdivideTree(node.Left, numSubtrees/2)...)
	}
	if node.Right != nil {
		subtrees = append(subtrees, SubdivideTree(node.Right, numSubtrees/2)...)
	}
	if len(subtrees) < numSubtrees {
		subtrees = append(subtrees, node)
	}
	return subtrees
}

func RoundToPowerOf2(x, y float64) (int, error) {
	if y == 0 {
		return 0, fmt.Errorf("divisor cannot be zero")
	}

	quotient := x / y

	if math.IsInf(quotient, 0) {
		return 0, fmt.Errorf("quotient is infinite")
	}
	if math.IsNaN(quotient) {
		return 0, fmt.Errorf("quotient is NaN")
	}

	absQuotient := math.Abs(quotient)
	roundedLog2 := math.Ceil(math.Log2(absQuotient))
	powerOf2 := math.Pow(2, roundedLog2)
	roundedQuotient := int(powerOf2)
	return roundedQuotient, nil
}

// NextPow2 returns the smallest power of 2 that is >= n. Returns 1 for n <= 1.
func NextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

func getOrdinal(n int) string {
	if n <= 0 {
		return strconv.Itoa(n)
	}
	switch n % 100 {
	case 11, 12, 13:
		return strconv.Itoa(n) + "th"
	}
	switch n % 10 {
	case 1:
		return strconv.Itoa(n) + "st"
	case 2:
		return strconv.Itoa(n) + "nd"
	case 3:
		return strconv.Itoa(n) + "rd"
	default:
		return strconv.Itoa(n) + "th"
	}
}
