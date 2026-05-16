package pool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
