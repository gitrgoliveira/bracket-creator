package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makePlayers creates a slice of n players named "P0", "P1", …
func makePlayers(n int) []Player {
	players := make([]Player, n)
	for i := range players {
		players[i] = Player{Name: fmt.Sprintf("P%d", i), Dojo: "Dojo"}
	}
	return players
}

// noOverlapPerRound asserts that within each round, every player appears at
// most once (i.e. no two concurrent matches share a player).
func noOverlapPerRound(t *testing.T, matches []Match, players []Player) {
	t.Helper()
	byRound := make(map[int][]Match)
	for _, m := range matches {
		byRound[m.Round] = append(byRound[m.Round], m)
	}
	for r, roundMatches := range byRound {
		seen := make(map[int]bool)
		for _, m := range roundMatches {
			idxA := playerIndex(players, m.SideA)
			idxB := playerIndex(players, m.SideB)
			assert.False(t, seen[idxA], "round %d: player index %d appears more than once", r, idxA)
			assert.False(t, seen[idxB], "round %d: player index %d appears more than once", r, idxB)
			seen[idxA] = true
			seen[idxB] = true
		}
	}
}

func TestCreatePoolRoundRobinMatches_SetsRound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		numPlayers int
	}{
		{"3 players", 3},
		{"4 players", 4},
		{"5 players", 5},
		{"6 players", 6},
		{"7 players", 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			players := makePlayers(tt.numPlayers)
			pools := []Pool{{PoolName: "A", Players: players}}
			CreatePoolRoundRobinMatches(pools)

			require.NotEmpty(t, pools[0].Matches, "expected matches to be generated")

			// All rounds must be non-negative.
			for i, m := range pools[0].Matches {
				assert.GreaterOrEqual(t, m.Round, 0, "match %d has negative round", i)
			}

			// Within each round, no player appears twice.
			noOverlapPerRound(t, pools[0].Matches, pools[0].Players)
		})
	}
}

func TestCreatePartialPoolMatches_SetsRound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		numPlayers int
	}{
		{"2 players", 2},
		{"3 players", 3},
		{"4 players", 4},
		{"5 players", 5},
		{"6 players", 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			players := makePlayers(tt.numPlayers)
			pools := []Pool{{PoolName: "A", Players: players}}
			CreatePartialPoolMatches(pools)

			require.NotEmpty(t, pools[0].Matches, "expected matches to be generated")

			// Expect exactly n-1 matches.
			assert.Len(t, pools[0].Matches, tt.numPlayers-1)

			// All rounds must be non-negative.
			for i, m := range pools[0].Matches {
				assert.GreaterOrEqual(t, m.Round, 0, "match %d has negative round", i)
			}

			// Within each round, no player appears twice.
			noOverlapPerRound(t, pools[0].Matches, pools[0].Players)

			// Verify known round assignment: even-indexed edges → round 0,
			// odd-indexed → round 1.
			for k, m := range pools[0].Matches {
				want := k % 2
				assert.Equal(t, want, m.Round, "match (edge) %d should be round %d", k, want)
			}
		})
	}
}
