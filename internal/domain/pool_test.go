package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestPool(t *testing.T) {
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

	// Verify pool properties
	assert.Equal(t, "pool1", pool.ID)
	assert.Equal(t, "Pool A", pool.Name)
	assert.Len(t, pool.Players, 2)
	assert.Equal(t, "player1", pool.Players[0].ID)
	assert.Equal(t, "player2", pool.Players[1].ID)
	assert.Len(t, pool.Matches, 1)
	assert.Equal(t, "match1", pool.Matches[0].ID)
}
