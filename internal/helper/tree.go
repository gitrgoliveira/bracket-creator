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

func CreateBalancedTree(leafValues []string, sanitize bool) *Node {
	mid := len(leafValues) / 2
	node := &Node{}

	if len(leafValues) == 1 {
		if sanitize {
			node.LeafVal = sanitizeName(leafValues[0])
		} else {
			node.LeafVal = leafValues[0]
		}
		node.LeafNode = true
		node.Val = 1
		return node
	}

	node.Left = CreateBalancedTree(leafValues[:mid], sanitize)
	node.Right = CreateBalancedTree(leafValues[mid:], sanitize)
	node.LeafNode = false
	node.Val = node.Left.Val + node.Right.Val

	return node
}

func PrintLeafNodes(node *Node, f *excelize.File, sheetName string, startCol int, startRow int, depth int, pools bool) {
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
		writeTreeValue(f, sheetName, startCol, startRow+size, node.LeafVal)
	} else {
		// this collects the cell coordinates for the match number in the tree
		node.LeafVal = CreateTreeBracket(f, sheetName, startCol, startRow+size/2+1, size-1)
		node.SheetName = sheetName
	}

	PrintLeafNodes(node.Left, f, sheetName, startCol-2, startRow, depth-1, pools)
	PrintLeafNodes(node.Right, f, sheetName, startCol-2, startRow+size, depth-1, pools)
}

func treeAdjustment(node *Node) {

	if node.Left.LeafNode && node.Right.LeafNode {

		// Need to ensure pools winners stay on top
		leftPool := strings.Split(node.Left.LeafVal, ".")
		leftPos, _ := strconv.ParseInt(leftPool[1], 10, 64)
		rightPool := strings.Split(node.Right.LeafVal, ".")
		rightPos, _ := strconv.ParseInt(rightPool[1], 10, 64)

		// For that we need to ensure the last character of the left (i.e. top) node is higher than the right
		if leftPos > rightPos {
			node.Left, node.Right = node.Right, node.Left
		}
	}

	// Also need to ensure pool winners are the ones that get a bye
	if node.Left.LeafNode && !node.Right.LeafNode {
		// find a second placed pool winner on the other branch
		leftPool := strings.Split(node.Left.LeafVal, ".")
		leftPos, _ := strconv.ParseInt(leftPool[1], 10, 64)
		rightPool := strings.Split(node.Right.Left.LeafVal, ".")
		rightPos, _ := strconv.ParseInt(rightPool[1], 10, 64)

		// For that we need to ensure the last character of the left (i.e. top) node is higher than the left of the right branch
		if leftPos > rightPos {
			node.Left, node.Right.Left = node.Right.Left, node.Left
		}
	}
}

func GenerateFinals(pools []Pool, poolWinners int) []string {

	finalists := make([][]string, len(pools))
	for i := 0; i < len(pools); i++ {
		for j := 0; j < poolWinners; j++ {
			finalists[i] = append(finalists[i], fmt.Sprintf("%s.%d", pools[i].PoolName, j+1))
		}
	}

	// fmt.Println(finalists)
	matches := make([]string, 0)
	round := -1
	for i := 0; i < len(pools)*poolWinners; i++ {

		poolPos := i % len(pools)

		if poolPos == 0 && len(pools)%poolWinners == 0 {
			// fmt.Println("new round")
			round++
		} else if round < 0 {
			round = 0
		}
		pos := (i + round) % poolWinners
		// fmt.Printf("pool num %d.", poolPos)
		// fmt.Println(pos)
		matches = append(matches, finalists[poolPos][pos])
	}

	// fmt.Println(matches)
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

func RoundToPowerOf2(x, y float64) int {
	quotient := x / y
	absQuotient := math.Abs(quotient)
	roundedLog2 := math.Ceil(math.Log2(absQuotient))
	powerOf2 := math.Pow(2, roundedLog2)
	roundedQuotient := int(powerOf2)
	return roundedQuotient
}
