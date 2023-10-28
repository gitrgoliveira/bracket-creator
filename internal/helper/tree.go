package helper

import (
	"fmt"
	"math"
	"strings"

	"github.com/xuri/excelize/v2"
)

type EliminationMatch struct {
	// Match Number
	Number int

	// Pool Number winner or Cell value
	Left string

	// Pool Number winner or Cell value
	Right string
}

type Node struct {
	LeafNode bool

	// Pool Number or Cell value
	LeafVal string
	Val     int
	Left    *Node
	Right   *Node
}

func CreateBalancedTree(leafValues []string, sanatize bool) *Node {
	mid := len(leafValues) / 2
	node := &Node{}

	if len(leafValues) == 1 {
		if sanatize {
			node.LeafVal = sanatizeName(leafValues[0])
		} else {
			node.LeafVal = leafValues[0]
		}
		node.LeafNode = true
		node.Val = 1
		return node
	}

	node.Left = CreateBalancedTree(leafValues[:mid], sanatize)
	node.Right = CreateBalancedTree(leafValues[mid:], sanatize)
	node.LeafNode = false
	node.Val = node.Left.Val + node.Right.Val

	return node
}

func Walk(t *Node, ch chan int) {
	if t.Left != nil {
		Walk(t.Left, ch)
	}
	ch <- t.Val
	if t.Right != nil {
		Walk(t.Right, ch)
	}
}

func PrintLeafNodes(node *Node, f *excelize.File, sheetName string, startCol int, startRow int, depth int) {
	if node == nil {
		return
	}
	emptyRows := int(math.Pow(2, float64(depth))) - 1

	// Need to ensure pools winners stay on top
	// For that we need to ensure the last charater of the left (i.e. top) node is the number 1
	if !node.LeafNode && node.Left.LeafNode && node.Right.LeafNode {
		if node.Left.LeafNode && strings.HasSuffix(node.Left.LeafVal, "2") {
			node.Left, node.Right = node.Right, node.Left
		}
	}

	// Need to ensure pools winners are the ones that get a bye
	if !node.LeafNode && node.Left.LeafNode && !node.Right.LeafNode {
		if strings.HasSuffix(node.Left.LeafVal, "2") {
			// find a second placed pool winner on the other branch
			node.Left, node.Right.Left = node.Right.Left, node.Left
		}
	}

	if node.LeafNode {
		writeTreeValue(f, sheetName, startCol, emptyRows+startRow-1, node.LeafVal)
	} else {
		// this collects the cell coordinates for the match number in the tree
		node.LeafVal = CreateTreeBracket(f, sheetName, startCol, emptyRows/2+startRow, emptyRows, false, fmt.Sprintf("%d", depth))
	}

	PrintLeafNodes(node.Left, f, sheetName, startCol-2, startRow, depth-1)
	PrintLeafNodes(node.Right, f, sheetName, startCol-2, startRow+emptyRows+1, depth-1)
}

func GenerateFinals(pools []Pool) []string {
	finals := make([]string, 0)

	for i, j := 0, len(pools)-1; j > i; i, j = i+1, j-1 {
		finals = append(finals, fmt.Sprintf("%s.1", pools[i].PoolName))
		finals = append(finals, fmt.Sprintf("%s.2", pools[j].PoolName))
	}
	for i, j := 0, len(pools)-1; i < j; i, j = i+1, j-1 {
		finals = append(finals, fmt.Sprintf("%s.1", pools[j].PoolName))
		finals = append(finals, fmt.Sprintf("%s.2", pools[i].PoolName))
	}
	// for an odd number of pools, add the middle pool to the finals
	if len(pools)%2 != 0 {
		finals = append(finals, fmt.Sprintf("%s.1", pools[len(pools)/2].PoolName))
		finals = append(finals, fmt.Sprintf("%s.2", pools[len(pools)/2].PoolName))
	}

	return finals
}

// Function to calculate the depth of a balanced tree for a given number of leaf nodes
func CalculateDepthForLeafs(leafs int) int {
	// Formula to calculate the depth of a balanced tree
	depth := int(math.Ceil(math.Log2(float64(leafs + 1))))

	return depth
}
func CalculateDepth(node *Node) int {
	if node == nil {
		return 0
	}

	leftDepth := CalculateDepth(node.Left)
	rightDepth := CalculateDepth(node.Right)

	return int(math.Max(float64(leftDepth), float64(rightDepth))) + 1
}

func CalculateNodesForLeafs(leafs int) int {
	// Formula to calculate the number of nodes in a balanced tree
	nodes := 2*leafs - 1
	return nodes
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

func InOrderTraversal(root *Node) []string {
	if root == nil {
		return []string{}
	}

	matches := make([]string, 0)

	stack := Stack{}
	curr := root

	for curr != nil || !stack.IsEmpty() {
		for curr != nil {
			stack.Push(curr)
			curr = curr.Left
		}

		curr = stack.Pop()
		if curr.Left != nil || curr.Right != nil {
			matches = append(matches, curr.LeafVal)
		}

		curr = curr.Right
	}

	return matches
}

/////////////################################################################

func TraverseRounds(node *Node, depth int, maxDepth int, matchMapping map[string]int) []EliminationMatch {
	if node == nil || node.Left == nil || node.Right == nil {
		return []EliminationMatch{}
	}

	var matches []EliminationMatch

	// if depth == maxDepth &&
	// 	(node.Left.LeafNode || node.Right.LeafNode) {

	if depth == maxDepth {
		//LeafVal
		// fmt.Printf("%s ", node.LeafVal)
		EliminationMatchs := EliminationMatch{
			Number: matchMapping[node.LeafVal],
			Left:   node.Left.LeafVal,
			Right:  node.Right.LeafVal,
		}
		matches = append(matches, EliminationMatchs)
	}

	// Then traverse the left subtree
	leftMatches := TraverseRounds(node.Left, depth+1, maxDepth, matchMapping)

	// Traverse the right subtree first
	rightMatches := TraverseRounds(node.Right, depth+1, maxDepth, matchMapping)

	matches = append(matches, leftMatches...)
	matches = append(matches, rightMatches...)

	return matches

}

func FindMaxDepth(node *Node) int {
	if node == nil {
		return 0
	}

	leftDepth := FindMaxDepth(node.Left)
	rightDepth := FindMaxDepth(node.Right)

	if leftDepth > rightDepth {
		return leftDepth + 1
	} else {
		return rightDepth + 1
	}
}
