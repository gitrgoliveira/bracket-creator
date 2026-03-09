package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestPlayer(t *testing.T) {
	// Create a test player
	player := domain.Player{
		ID:           "player1",
		Name:         "John Doe",
		DisplayName:  "J. Doe",
		Dojo:         "Test Dojo",
		PoolPosition: 1,
	}

	// Verify player properties
	assert.Equal(t, "player1", player.ID)
	assert.Equal(t, "John Doe", player.Name)
	assert.Equal(t, "J. Doe", player.DisplayName)
	assert.Equal(t, "Test Dojo", player.Dojo)
	assert.Equal(t, int64(1), player.PoolPosition)
}

func TestMatchWinner(t *testing.T) {
	// Create a test match winner
	winner := domain.MatchWinner{
		PlayerID: "player1",
		MatchID:  "match1",
	}

	// Verify match winner properties
	assert.Equal(t, "player1", winner.PlayerID)
	assert.Equal(t, "match1", winner.MatchID)
}
