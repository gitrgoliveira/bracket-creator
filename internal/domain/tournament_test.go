package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestTournament(t *testing.T) {
	// Create test players
	player1 := domain.Player{
		ID:           "player1",
		Name:         "John Doe",
		DisplayName:  "J. Doe",
		Dojo:         "Test Dojo",
		PoolPosition: 1,
	}

	player2 := domain.Player{
		ID:           "player2",
		Name:         "Jane Smith",
		DisplayName:  "J. Smith",
		Dojo:         "Another Dojo",
		PoolPosition: 2,
	}

	// Create a test match
	match := domain.Match{
		ID:    "match1",
		SideA: &player1,
		SideB: &player2,
	}

	// Create a test pool
	pool := domain.Pool{
		ID:      "pool1",
		Name:    "Pool A",
		Players: []domain.Player{player1, player2},
		Matches: []domain.Match{match},
	}

	// Create a test tournament
	tournament := domain.Tournament{
		Name:               "Test Tournament",
		Pools:              []domain.Pool{pool},
		EliminationMatches: []domain.Match{match},
	}

	// Verify tournament properties
	assert.Equal(t, "Test Tournament", tournament.Name)
	assert.Len(t, tournament.Pools, 1)
	assert.Equal(t, "pool1", tournament.Pools[0].ID)
	assert.Len(t, tournament.EliminationMatches, 1)
	assert.Equal(t, "match1", tournament.EliminationMatches[0].ID)
}
