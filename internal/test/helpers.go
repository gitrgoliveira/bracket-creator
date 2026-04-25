package test

import (
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// CreateTestPlayers returns a slice of players for testing
func CreateTestPlayers() []domain.Player {
	return []domain.Player{
		{
			ID:           "player1",
			Name:         "John Doe",
			DisplayName:  "J. Doe",
			Dojo:         "Test Dojo",
			PoolPosition: 1,
		},
		{
			ID:           "player2",
			Name:         "Jane Smith",
			DisplayName:  "J. Smith",
			Dojo:         "Another Dojo",
			PoolPosition: 2,
		},
	}
}

// CreateTestPools returns a slice of pools for testing
func CreateTestPools() []domain.Pool {
	players := CreateTestPlayers()

	match := domain.Match{
		ID:    "match1",
		SideA: &players[0],
		SideB: &players[1],
	}

	return []domain.Pool{
		{
			ID:      "pool1",
			Name:    "Pool A",
			Players: players,
			Matches: []domain.Match{match},
		},
	}
}

// CreateTestTournament returns a tournament for testing
func CreateTestTournament() domain.Tournament {
	pools := CreateTestPools()

	return domain.Tournament{
		Name:  "Test Tournament",
		Pools: pools,
		EliminationMatches: []domain.Match{
			pools[0].Matches[0],
		},
	}
}
