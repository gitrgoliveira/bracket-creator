package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDelayDojoMeetings_RealTreeRound1_NonPowerOfTwo pins bc-drwx item 1:
// dojoMeetRound used to score meeting rounds by XORing DENSE StandardSeeding
// indices directly, but every production consumer (cmd/create-playoffs.go,
// internal/engine/bracket.go, internal/engine/playoff_skeleton.go) feeds that
// dense slice to CreateBalancedTree, whose recursion splits the leaf list in
// half at EVERY level rather than padding byes onto the tail -- so for any
// non-power-of-two entrant count the dense-index XOR scores pairs that are
// not real matches and misses real round-1 pairs. delayDojoMeetings, whose
// entire job is to push same-dojo pairs OUT of round 1, was therefore
// checking the wrong geometry whenever len(players) was not a power of two.
//
// This test builds the REAL tree exactly the way engine/bracket.go and
// cmd/create-playoffs.go do (StandardSeeding -> names -> CreateBalancedTree)
// and counts genuine round-1 same-dojo pairs by walking the actual tree
// leaves, rather than trusting dojoMeetRound's own (buggy) arithmetic as the
// oracle. Every roster here pastes entrants dojo-by-dojo (the operator
// workflow that first motivated delayDojoMeetings) with every dojo well
// under half the field, so a zero-round-1-clash draw is always reachable and
// the assertion is not fighting an unavoidable collision.
func TestDelayDojoMeetings_RealTreeRound1_NonPowerOfTwo(t *testing.T) {
	cases := []struct {
		name  string
		sizes []int // dojo sizes, pasted dojo by dojo in this order
	}{
		{"n=6, three dojos of two", []int{2, 2, 2}},
		{"n=10, 4/3/3", []int{4, 3, 3}},
		{"n=12, 4/5/3 (bc-drwx repro)", []int{4, 5, 3}},
		{"n=20, 7/7/6", []int{7, 7, 6}},
		{"n=24, 8/8/8", []int{8, 8, 8}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roster := dojoByDojoRoster(tc.sizes)
			out := StandardSeeding(roster)

			clashes := realTreeRound1DojoClashes(out)
			assert.Equal(t, 0, clashes,
				"n=%d: %d avoidable same-dojo round-1 pairing(s) found in the REAL tree "+
					"(CreateBalancedTree over StandardSeeding's own dense output, exactly as "+
					"engine/bracket.go and cmd/create-playoffs.go build it)", len(roster), clashes)
		})
	}
}

// dojoByDojoRoster builds a roster with len(sizes) dojos, each contributing
// sizes[i] members, pasted one dojo after another -- the paste-order shape
// that motivated delayDojoMeetings in the first place.
func dojoByDojoRoster(sizes []int) []Player {
	var roster []Player
	for d, n := range sizes {
		dojo := fmt.Sprintf("Dojo%d", d)
		for i := 0; i < n; i++ {
			roster = append(roster, Player{
				Name: fmt.Sprintf("D%d_%02d", d, i+1),
				Dojo: dojo,
			})
		}
	}
	return roster
}

// realTreeRound1DojoClashes builds the tree exactly as engine/bracket.go and
// cmd/create-playoffs.go do (StandardSeeding's own dense output fed straight
// into CreateBalancedTree) and counts genuine round-1 same-dojo pairs by
// walking the tree's actual leaves: a node counts as a round-1 match only
// when BOTH its children are leaves (a leaf paired against an internal
// subtree is a bye that meets its opponent's WINNER no earlier than round 2,
// never a round-1 meeting). This is independent of dojoMeetRound, which is
// exactly the function under test here, so it must not be used as the
// oracle.
func realTreeRound1DojoClashes(seededPlayers []Player) int {
	names := make([]string, len(seededPlayers))
	dojoOf := make(map[string]string, len(seededPlayers))
	for i, p := range seededPlayers {
		names[i] = p.Name
		if p.Name != "" {
			dojoOf[p.Name] = p.Dojo
		}
	}
	tree := CreateBalancedTree(names)

	clashes := 0
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil || n.LeafNode {
			return
		}
		if n.Left != nil && n.Right != nil && n.Left.LeafNode && n.Right.LeafNode {
			a, b := n.Left.LeafVal, n.Right.LeafVal
			if a != "" && b != "" && dojoOf[a] != "" && dojoOf[a] == dojoOf[b] {
				clashes++
			}
		}
		walk(n.Left)
		walk(n.Right)
	}
	walk(tree)
	return clashes
}
