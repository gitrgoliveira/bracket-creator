package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
	if pool.ID != "pool1" {
		t.Errorf("Expected pool ID to be 'pool1', got '%s'", pool.ID)
	}

	if pool.Name != "Pool A" {
		t.Errorf("Expected pool name to be 'Pool A', got '%s'", pool.Name)
	}

	if len(pool.Players) != 2 {
		t.Errorf("Expected pool to have 2 players, got %d", len(pool.Players))
	}

	if pool.Players[0].ID != "player1" {
		t.Errorf("Expected first player ID to be 'player1', got '%s'", pool.Players[0].ID)
	}

	if pool.Players[1].ID != "player2" {
		t.Errorf("Expected second player ID to be 'player2', got '%s'", pool.Players[1].ID)
	}

	if len(pool.Matches) != 1 {
		t.Errorf("Expected pool to have 1 match, got %d", len(pool.Matches))
	}

	if pool.Matches[0].ID != "match1" {
		t.Errorf("Expected match ID to be 'match1', got '%s'", pool.Matches[0].ID)
	}
}
