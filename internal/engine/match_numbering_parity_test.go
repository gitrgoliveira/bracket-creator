package engine

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// matchSignature canonically identifies a bracket bout by the SORTED set of real
// leaf (entrant) names that feed into it. The same bout has the same leaf set in
// BOTH numbering paths even though the two paths walk different tree structures
// (the Excel path uses the unbalanced CreateBalancedTree(names); the engine path
// uses a power-of-two-padded tree). Comparing leaf sets — not positions — lets the
// test assert "this physical bout got the same Match N in both paths", which is the
// AC1 user-facing invariant (on-screen Match N == printed Excel Match N).
func matchSignature(leaves []string) string {
	cp := append([]string(nil), leaves...)
	sort.Strings(cp)
	return strings.Join(cp, "|")
}

// excelLeafNames returns every real leaf name beneath an unbalanced Excel tree
// node (the matchup that node represents on the printed Tree sheet).
func excelLeafNames(n *helper.Node) []string {
	if n == nil {
		return nil
	}
	if n.LeafNode {
		if n.LeafVal == "" {
			return nil
		}
		return []string{n.LeafVal}
	}
	return append(excelLeafNames(n.Left), excelLeafNames(n.Right)...)
}

// excelNumberBySignature reproduces the AUTHORITATIVE printed-sheet numbering:
// build the unbalanced tree the Excel create-playoffs path builds, collect the
// elimination rounds in the exact eliminationMatchRounds order, run the shared
// helper.AssignMatchNumbers, then map each numbered node to its leaf-set signature.
func excelNumberBySignature(players []domain.Player) map[string]int {
	seeded := helper.StandardSeeding(players)
	names := make([]string, len(seeded))
	for i, p := range seeded {
		names[i] = p.Name
	}
	tree := helper.CreateBalancedTree(names)
	depth := helper.CalculateDepth(tree)

	// Same construction as cmd/create-playoffs.go:
	// eliminationMatchRounds[depth-i] = TraverseRounds(tree, 1, i-1).
	rounds := make([][]*helper.Node, depth-1)
	for i := depth; i > 1; i-- {
		rounds[depth-i] = helper.TraverseRounds(tree, 1, i-1)
	}

	helper.AssignMatchNumbers(rounds)

	out := map[string]int{}
	for _, round := range rounds {
		for _, node := range round {
			if node == nil {
				continue
			}
			num := node.MatchNum()
			if num == 0 {
				continue
			}
			out[matchSignature(excelLeafNames(node))] = int(num)
		}
	}
	return out
}

// engineNumberBySignature returns the engine bracket's MatchNumber for each real
// match, keyed by the same leaf-set signature. Leaf sets are computed bottom-up
// over the validated Feeders graph: a real match's leaves are its resolved
// (non-"Winner of") sides plus the leaves of each real feeder match.
func engineNumberBySignature(t *testing.T, players []domain.Player) map[string]int {
	t.Helper()
	bracket := enginePlayoffsBracket(t, players)

	byID := map[string]*state.BracketMatch{}
	for r := range bracket.Rounds {
		for i := range bracket.Rounds[r] {
			m := &bracket.Rounds[r][i]
			byID[m.ID] = m
		}
	}

	// Memoized leaf-set resolver over the real feeder graph.
	leafCache := map[string][]string{}
	var leavesOf func(m *state.BracketMatch) []string
	leavesOf = func(m *state.BracketMatch) []string {
		if cached, ok := leafCache[m.ID]; ok {
			return cached
		}
		var acc []string
		// Direct resolved entrants (a side that is not a "Winner of" placeholder
		// and not empty is a real leaf feeding this match directly).
		for _, side := range []string{m.SideA, m.SideB} {
			if side != "" && !strings.HasPrefix(side, "Winner of") {
				acc = append(acc, side)
			}
		}
		// Real feeders contribute their own leaf sets.
		for _, fid := range m.Feeders {
			if fid == "" {
				continue
			}
			if f, ok := byID[fid]; ok {
				acc = append(acc, leavesOf(f)...)
			}
		}
		leafCache[m.ID] = acc
		return acc
	}

	out := map[string]int{}
	for r := range bracket.Rounds {
		for i := range bracket.Rounds[r] {
			m := &bracket.Rounds[r][i]
			if m.Hidden {
				continue
			}
			require.NotZerof(t, m.MatchNumber,
				"real match %s must have a non-zero MatchNumber", m.ID)
			out[matchSignature(leavesOf(m))] = m.MatchNumber
		}
	}
	return out
}

// TestMatchNumberingParity_ExcelVsWeb is the equal-by-contract proof for AC1/AC8:
// the web bracket's MatchNumber equals the printed Excel Tree sheet's "Match N"
// for every real bout, INCLUDING bye-producing non-power-of-two sizes where the
// Excel tree inserts shorter branches and the web bracket marks matches Hidden —
// the very case where the two numbering schemes could drift.
//
// Both paths are exercised end-to-end (the engine path runs StartCompetition;
// the Excel path runs the real CreateBalancedTree/TraverseRounds/AssignMatchNumbers
// pipeline), then every bout is matched by its leaf-descendant set and the assigned
// numbers are compared. If a future change drifts the web path, this test fails and
// the web path must be corrected to match the authoritative Excel numbering.
func TestMatchNumberingParity_ExcelVsWeb(t *testing.T) {
	// Sizes chosen to span every bye topology: 3/5/6/7 give shallow byes,
	// 11/13 give multi-level bye chains, 4/8/16 are clean powers of two
	// (no byes — the easy baseline).
	sizes := []int{3, 4, 5, 6, 7, 8, 11, 13, 16}

	for _, n := range sizes {
		t.Run(fmt.Sprintf("%d entrants", n), func(t *testing.T) {
			players := makePlayers(n)

			excel := excelNumberBySignature(players)
			web := engineNumberBySignature(t, players)

			// N-1 real matches in single elimination.
			assert.Equalf(t, n-1, len(excel),
				"excel must have N-1 numbered matches (n=%d)", n)
			assert.Equalf(t, n-1, len(web),
				"web must have N-1 numbered matches (n=%d)", n)

			// Numbers must be the contiguous range 1..N-1 in each path.
			assertContiguous(t, excel, n-1, "excel")
			assertContiguous(t, web, n-1, "web")

			// Position-for-position: the same bout (leaf set) gets the same number.
			require.Equal(t, len(excel), len(web),
				"both paths must produce the same number of bouts")
			for sig, excelNum := range excel {
				webNum, ok := web[sig]
				require.Truef(t, ok,
					"bout %q numbered %d in Excel is missing from the web bracket",
					sig, excelNum)
				assert.Equalf(t, excelNum, webNum,
					"bout %q: Excel Match %d but web Match %d — numbering drifted",
					sig, excelNum, webNum)
			}
		})
	}
}

// assertContiguous verifies the numbers in m form the set {1..count}.
func assertContiguous(t *testing.T, m map[string]int, count int, label string) {
	t.Helper()
	seen := make([]bool, count+1)
	for _, v := range m {
		require.GreaterOrEqualf(t, v, 1, "%s: match number must be >= 1", label)
		require.LessOrEqualf(t, v, count, "%s: match number must be <= %d", label, count)
		require.Falsef(t, seen[v], "%s: duplicate match number %d", label, v)
		seen[v] = true
	}
}
