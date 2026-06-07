package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoolGeneration_SinglePoolMultiCourt_RoundAwareCourts verifies that when a
// single league pool is generated with multiple courts, matches within the same
// round never share a participant — so no two matches that could run
// concurrently involve the same player.
func TestPoolGeneration_SinglePoolMultiCourt_RoundAwareCourts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		numPlayers int
		courts     []string
	}{
		{"4 players 2 courts", 4, []string{"A", "B"}},
		{"6 players 2 courts", 6, []string{"A", "B"}},
		{"6 players 3 courts", 6, []string{"A", "B", "C"}},
		{"8 players 2 courts", 8, []string{"A", "B"}},
		{"8 players 3 courts", 8, []string{"A", "B", "C"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eng, store, _ := setupTestEngine(t)
			compID := "league-multi-court"

			comp := &state.Competition{
				ID:           compID,
				Name:         "League Multi Court",
				Kind:         "individual",
				Format:       state.CompFormatLeague,
				PoolSize:     tt.numPlayers, // single pool: pool size = all players
				PoolSizeMode: "min",
				PoolWinners:  1,
				RoundRobin:   true,
				Courts:       tt.courts,
				StartTime:    "09:00",
				Status:       "setup",
			}
			require.NoError(t, store.SaveCompetition(comp))

			players := make([]domain.Player, tt.numPlayers)
			for i := range players {
				players[i] = domain.Player{
					Name: string(rune('A' + i)),
					Dojo: "Dojo",
				}
			}
			require.NoError(t, store.SaveParticipants(compID, players))

			require.NoError(t, eng.StartCompetition(compID))

			matches, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			require.NotEmpty(t, matches)

			// Group by round.
			byRound := make(map[int][]state.MatchResult)
			for _, m := range matches {
				byRound[m.Round] = append(byRound[m.Round], m)
			}

			// Within each round, no player should appear more than once.
			for r, roundMatches := range byRound {
				seen := make(map[string]bool)
				for _, m := range roundMatches {
					assert.False(t, seen[m.SideA], "round %d: player %q appears in multiple concurrent matches", r, m.SideA)
					assert.False(t, seen[m.SideB], "round %d: player %q appears in multiple concurrent matches", r, m.SideB)
					seen[m.SideA] = true
					seen[m.SideB] = true
				}
			}
		})
	}
}

func TestPoolGeneration_SinglePoolMultiCourt_RejectsTooManyCourts(t *testing.T) {
	t.Parallel()

	eng, store, _ := setupTestEngine(t)
	compID := "league-too-many-courts"

	comp := &state.Competition{
		ID:           compID,
		Name:         "League Too Many Courts",
		Kind:         "individual",
		Format:       state.CompFormatLeague,
		PoolSize:     6,
		PoolSizeMode: "min",
		PoolWinners:  1,
		RoundRobin:   true,
		Courts:       []string{"A", "B", "C", "D"},
		StartTime:    "09:00",
		Status:       "setup",
	}
	require.NoError(t, store.SaveCompetition(comp))

	players := make([]domain.Player, 6)
	for i := range players {
		players[i] = domain.Player{
			Name: string(rune('A' + i)),
			Dojo: "Dojo",
		}
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	err := eng.StartCompetition(compID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many courts")
}
