package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestMatch(t *testing.T) {
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

	// Create a test match with no winner
	match := domain.Match{
		ID:    "match1",
		SideA: &player1,
		SideB: &player2,
	}

	// Verify match properties
	assert.Equal(t, "match1", match.ID)
	assert.Equal(t, "player1", match.SideA.ID)
	assert.Equal(t, "player2", match.SideB.ID)
	assert.Nil(t, match.Winner)

	// Set a winner
	match.Winner = &player1

	assert.Equal(t, "player1", match.Winner.ID)
}
