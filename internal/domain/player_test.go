package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
	if player.ID != "player1" {
		t.Errorf("Expected player ID to be 'player1', got '%s'", player.ID)
	}

	if player.Name != "John Doe" {
		t.Errorf("Expected player name to be 'John Doe', got '%s'", player.Name)
	}

	if player.DisplayName != "J. Doe" {
		t.Errorf("Expected player display name to be 'J. Doe', got '%s'", player.DisplayName)
	}

	if player.Dojo != "Test Dojo" {
		t.Errorf("Expected player dojo to be 'Test Dojo', got '%s'", player.Dojo)
	}

	if player.PoolPosition != 1 {
		t.Errorf("Expected player pool position to be 1, got %d", player.PoolPosition)
	}
}

func TestMatchWinner(t *testing.T) {
	// Create a test match winner
	winner := domain.MatchWinner{
		PlayerID: "player1",
		MatchID:  "match1",
	}

	// Verify match winner properties
	if winner.PlayerID != "player1" {
		t.Errorf("Expected winner player ID to be 'player1', got '%s'", winner.PlayerID)
	}

	if winner.MatchID != "match1" {
		t.Errorf("Expected winner match ID to be 'match1', got '%s'", winner.MatchID)
	}
}
