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

// MatchNum returns the sequential match number assigned to this node by
// AssignMatchNumbers (0 if not yet assigned). Exported so cross-package callers,
// notably the engine-vs-Excel numbering-parity test, can read the authoritative
// printed-sheet number without reaching into the unexported field.
func (n *Node) MatchNum() int64 {
	if n == nil {
		return 0
	}
	return n.matchNum
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

// PrintLeafNodes draws one bracket page: it writes every leaf's label and every
// junction's bracket lines, and stamps each internal node's sheet/cell
// coordinates so FillInMatches can write the match numbers into them.
//
// It is a PURE RENDERER - it never reorders the tree. Placement is decided when
// the draw is BUILT (BuildKnockoutDraw, draw.go), so by the time a page is
// drawn its leaves are already final. It used to run a placement fix-up here,
// per page subtree and as a side effect of drawing, which meant the Excel path
// applied placement to a DIFFERENT scope than the engine path applied it to.
// Do not reintroduce a mutation here.
func PrintLeafNodes(node *Node, f *excelize.File, sheetName string, startCol int, startRow int, depth int, matchWinners map[string]MatchWinner) {
	if node == nil {
		return
	}

	size := int(math.Pow(2, float64(depth-1)))

	if node.LeafNode {
		writeTreeValue(f, sheetName, startCol, startRow+size, node.LeafVal, matchWinners)
	} else {
		// this collects the cell coordinates for the match number in the tree
		node.LeafVal = CreateTreeBracket(f, sheetName, startCol, startRow+size/2+1, size-1)
		node.SheetName = sheetName // How is this used?
	}

	PrintLeafNodes(node.Left, f, sheetName, startCol-2, startRow, depth-1, matchWinners)
	PrintLeafNodes(node.Right, f, sheetName, startCol-2, startRow+size, depth-1, matchWinners)
}

// splitPoolNameAndRank splits a pool-finalist placeholder ("Pool A-1st") into
// its pool name and ordinal suffix at the LAST hyphen. Pool names may contain
// hyphens; a rank never does.
func splitPoolNameAndRank(val string) (string, string) {
	idx := strings.LastIndex(val, "-")
	if idx == -1 {
		return val, ""
	}
	return val[:idx], val[idx+1:]
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

// BuildEliminationMatchRounds collects the tree's internal (match) nodes into
// per-round slices ordered earliest round first: index 0 is the deepest
// (first-played) round and the last index is the final, matching what
// FillInMatches and PrintTeamEliminationMatches expect. Round number =
// len(result) - index. A tree too shallow for any match (single leaf) yields
// an empty slice. This is the single implementation behind all four workbook
// generators - the loop used to be copied at each call site, like the
// tree-page rendering loop before RenderTreePages.
func BuildEliminationMatchRounds(tree *Node) [][]*Node {
	depth := CalculateDepth(tree)
	rounds := make([][]*Node, 0, max(depth-1, 0))
	for i := depth; i > 1; i-- {
		rounds = append(rounds, TraverseRounds(tree, 1, i-1))
	}
	return rounds
}

// SemifinalMatchNumbers derives the two semifinal match numbers from the
// final's children so a bronze (3rd-place) block can reference the "2."
// loser lines via CONCATENATE formulas. Returns (0, 0) when the bracket has
// no real semifinal (fewer than two rounds, e.g. a 2-player bracket) or the
// final's child slots are absent; a zero keeps the corresponding entrant
// slot hand-fillable.
func SemifinalMatchNumbers(rounds [][]*Node) (semiA, semiB int) {
	if len(rounds) < 2 {
		return 0, 0
	}
	lastRound := rounds[len(rounds)-1]
	if len(lastRound) == 0 || lastRound[0] == nil {
		return 0, 0
	}
	if lastRound[0].Left != nil {
		semiA = int(lastRound[0].Left.MatchNum())
	}
	if lastRound[0].Right != nil {
		semiB = int(lastRound[0].Right.MatchNum())
	}
	return semiA, semiB
}

// NeedsBronzeBlock reports whether a naginata playoffs bracket should carry a
// bronze (3rd-place) block: naginata only, and only when a real semifinal round
// exists (a 2-player bracket is a single round with no semifinal, so no bronze).
// This is the single source of truth for the rule expressed at every render/build
// site (cmd create-pools/playoffs, internal/engine/bracket.go).
func NeedsBronzeBlock(naginata bool, numRounds int) bool {
	return naginata && numRounds >= 2
}

// SubdivideRegions cuts a draw's shiaijo regions into Excel tree pages: each
// region contributes exactly pagesPerCourt pages, in court order, so page
// (c*pagesPerCourt + i) always belongs to shiaijo c (R8).
//
// A 1-page court prints its whole region, a 2-page court its region's two child
// subtrees and a 4-page court its four grandchildren. Every page is therefore a
// genuine subtree, which is what a bracket has to be to print at all.
//
// pagesPerCourt is validated by KnockoutPagesPerCourt, which never asks for a
// split a region cannot honour; asking anyway yields the deepest split that
// exists, padded with the region itself, so the page count stays an exact
// multiple rather than silently drifting.
//
// This replaces the old count-based SubdivideTree, which split by TREE POSITION
// and could not express a non-power-of-two page count at all: asked for 3 pages
// on a 12-leaf tree it returned [left half, right half, WHOLE TREE], so page 3
// reprinted every match on pages 1 and 2.
func SubdivideRegions(regions []*Node, pagesPerCourt int) []*Node {
	if pagesPerCourt < 1 {
		pagesPerCourt = 1
	}
	pages := make([]*Node, 0, len(regions)*pagesPerCourt)
	for _, r := range regions {
		pages = append(pages, regionPages(r, pagesPerCourt)...)
	}
	return pages
}

// regionPages splits one region into exactly want pages (1, 2 or 4).
func regionPages(region *Node, want int) []*Node {
	pages := []*Node{region}
	for len(pages) < want {
		next := make([]*Node, 0, len(pages)*2)
		split := false
		for _, p := range pages {
			if p != nil && !p.LeafNode && p.Left != nil && p.Right != nil {
				next = append(next, p.Left, p.Right)
				split = true
			} else {
				next = append(next, p, p)
			}
		}
		if !split {
			// Nothing left to cut: repeat the region rather than emit a page
			// count that is not a multiple of the shiaijo count.
			for len(pages) < want {
				pages = append(pages, region)
			}
			break
		}
		pages = next
	}
	return pages[:want]
}

// TreeToLeafArray converts a tree built by CreateBalancedTree into a
// power-of-two leaf array suitable for buildBracketFromDraw. Internal nodes
// recurse into left and right subtrees, padding each side to
// NextPow2(max(len(left), len(right))) with "" (bye slots) before
// concatenating. The result length is always NextPow2(N) where N is the
// number of real leaves, and bye positions mirror the tree's structural
// asymmetry so the same matchups produced by the Excel bracket are reproduced.
func TreeToLeafArray(node *Node) []string {
	if node == nil {
		return nil
	}
	if node.LeafNode {
		return []string{node.LeafVal}
	}
	left := TreeToLeafArray(node.Left)
	right := TreeToLeafArray(node.Right)
	target := NextPow2(max(len(left), len(right)))
	for len(left) < target {
		left = append(left, "")
	}
	for len(right) < target {
		right = append(right, "")
	}
	return append(left, right...)
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

// KnockoutPagesPerCourt returns how many Excel tree pages each shiaijo gets:
// 1, 2 or 4 (R8), the smallest power of two such that no page carries more than
// MaxPlayersPerTree entrants.
//
// The result is clamped down to the deepest split every region can actually
// honour, so SubdivideRegions never has to invent a subtree: a court whose
// region is a single match cannot print two pages, and the page count must stay
// an exact multiple of the shiaijo count for the page-to-court mapping
// (SubtreeCourtIndex, PoolBoundsForSubtree) to be exact. In practice the clamp
// is inert, because AssignPoolsToCourts keeps region sizes within one pool of
// each other; it only bites on a draw whose regions are wildly uneven.
//
// An oversized region gets MORE PAGES, never an error (R8), and the cap at 4
// means a very large region may still exceed MaxPlayersPerTree per page. That
// is the stated trade: a page too dense to read beats a draw that refuses to
// print during a live event.
func KnockoutPagesPerCourt(regions []*Node) int {
	if len(regions) == 0 {
		return 1
	}
	widest := 0
	splittable := 4
	for _, r := range regions {
		if l := CountLeaves(r); l > widest {
			widest = l
		}
		if s := maxRegionSplit(r); s < splittable {
			splittable = s
		}
	}
	pages := 1
	for pages < 4 && ceilDiv(widest, pages) > MaxPlayersPerTree {
		pages *= 2
	}
	if pages > splittable {
		pages = splittable
	}
	if pages < 1 {
		pages = 1
	}
	return pages
}

// maxRegionSplit is how many genuine page subtrees a region can be cut into:
// 1 for a lone leaf, 2 when either child is a leaf, 4 otherwise.
func maxRegionSplit(region *Node) int {
	if region == nil || region.LeafNode || region.Left == nil || region.Right == nil {
		return 1
	}
	for _, c := range []*Node{region.Left, region.Right} {
		if c.LeafNode || c.Left == nil || c.Right == nil {
			return 2
		}
	}
	return 4
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return a
	}
	return (a + b - 1) / b
}

// KnockoutPageSubtrees returns the subtrees RenderKnockoutPages will print, one
// per Excel tree page, in page order: len(regions) x KnockoutPagesPerCourt
// (R8), so the page count is always a multiple of the shiaijo count and page
// (c*p + i) belongs to shiaijo c.
//
// singleTree (the CLI --single-tree flag) forces the whole bracket onto ONE
// page and wins outright. It used to be silently overridden by the court
// expansion below it, so "--single-tree" on a 4-court event still printed four
// pages.
//
// This replaced TreePageLayout, which returned the page COUNT from the same
// inputs. Once RenderKnockoutPages needed the subtrees themselves it derived
// them inline and stopped calling the count function, which left the count
// function with no production caller and the tests asserting against a formula
// nothing shipped. A test that pins --single-tree has to pin the path that
// actually prints the pages, or the flag can break in the renderer with the
// suite still green.
func KnockoutPageSubtrees(draw *KnockoutDraw, singleTree bool) []*Node {
	if draw == nil || draw.Root == nil {
		return nil
	}
	if singleTree {
		return []*Node{draw.Root}
	}
	return SubdivideRegions(draw.Regions, KnockoutPagesPerCourt(draw.Regions))
}

func GetOrdinal(n int) string {
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
