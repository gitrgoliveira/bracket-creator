package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
	if match.ID != "match1" {
		t.Errorf("Expected match ID to be 'match1', got '%s'", match.ID)
	}

	if match.SideA.ID != "player1" {
		t.Errorf("Expected SideA player ID to be 'player1', got '%s'", match.SideA.ID)
	}

	if match.SideB.ID != "player2" {
		t.Errorf("Expected SideB player ID to be 'player2', got '%s'", match.SideB.ID)
	}

	if match.Winner != nil {
		t.Errorf("Expected winner to be nil, got player ID '%s'", match.Winner.ID)
	}

	// Set a winner
	match.Winner = &player1

	if match.Winner.ID != "player1" {
		t.Errorf("Expected winner player ID to be 'player1', got '%s'", match.Winner.ID)
	}
}
