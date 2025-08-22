package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
	if tournament.Name != "Test Tournament" {
		t.Errorf("Expected tournament name to be 'Test Tournament', got '%s'", tournament.Name)
	}

	if len(tournament.Pools) != 1 {
		t.Errorf("Expected tournament to have 1 pool, got %d", len(tournament.Pools))
	}

	if tournament.Pools[0].ID != "pool1" {
		t.Errorf("Expected pool ID to be 'pool1', got '%s'", tournament.Pools[0].ID)
	}

	if len(tournament.EliminationMatches) != 1 {
		t.Errorf("Expected tournament to have 1 elimination match, got %d", len(tournament.EliminationMatches))
	}

	if tournament.EliminationMatches[0].ID != "match1" {
		t.Errorf("Expected elimination match ID to be 'match1', got '%s'", tournament.EliminationMatches[0].ID)
	}
}
