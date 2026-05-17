package pool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateAdjacentPairings_ZeroOrOne verifies that nil is returned for
// fewer than 2 players.
func TestGenerateAdjacentPairings_ZeroOrOne(t *testing.T) {
	assert.Nil(t, GenerateAdjacentPairings(nil))
	assert.Nil(t, GenerateAdjacentPairings([]Player{}))
	assert.Nil(t, GenerateAdjacentPairings([]Player{{ID: "only"}}))
}

// TestGenerateAdjacentPairings_TwoPlayers verifies the minimal case:
// exactly 1 match (0v1) with correct side assignments.
func TestGenerateAdjacentPairings_TwoPlayers(t *testing.T) {
	players := []Player{{ID: "a"}, {ID: "b"}}
	matches := GenerateAdjacentPairings(players)
	require.Len(t, matches, 1)
	assert.Equal(t, "a", matches[0].SideA.ID)
	assert.Equal(t, "b", matches[0].SideB.ID)
}

// TestGenerateAdjacentPairings_OddNumber verifies a 5-player pool produces
// 4 adjacent matches (N-1).
func TestGenerateAdjacentPairings_OddNumber(t *testing.T) {
	players := []Player{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"}}
	matches := GenerateAdjacentPairings(players)
	require.Len(t, matches, 4)
	for i, m := range matches {
		assert.Equal(t, players[i].ID, m.SideA.ID)
		assert.Equal(t, players[i+1].ID, m.SideB.ID)
	}
}

// TestPartialRoundRobin verifies FR-052 / R7: an 8-player pool with
// poolFormat=partial generates exactly 7 matches in adjacent-neighbour
// order (1v2, 2v3, 3v4, ..., 7v8).
//
// This is a Red test — pool.Player, pool.Match, and
// pool.GenerateAdjacentPairings do not yet exist. The build must fail
// until the Green implementation (T034) lands.
func TestPartialRoundRobin(t *testing.T) {
	players := []Player{
		{ID: "p1"},
		{ID: "p2"},
		{ID: "p3"},
		{ID: "p4"},
		{ID: "p5"},
		{ID: "p6"},
		{ID: "p7"},
		{ID: "p8"},
	}

	matches := GenerateAdjacentPairings(players)

	require.Len(t, matches, 7, "partial round-robin on 8 players yields N-1 matches")

	for i := 0; i < len(matches); i++ {
		assert.Equal(t, players[i].ID, matches[i].SideA.ID,
			"match %d SideA should be player[%d]", i, i)
		assert.Equal(t, players[i+1].ID, matches[i].SideB.ID,
			"match %d SideB should be player[%d]", i, i+1)
	}
}
