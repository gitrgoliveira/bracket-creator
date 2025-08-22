package test

import (
	"testing"
)

func TestCreateTestFS(t *testing.T) {
	fs := CreateTestFS(t)

	// Check that we can read the template.xlsx file
	file, err := fs.Open("template.xlsx")
	if err != nil {
		t.Fatalf("Failed to open template.xlsx: %v", err)
	}
	defer file.Close()

	// The fact that we can open the file is enough for this test
	t.Log("Successfully created and accessed test filesystem")
}

func TestCreateTestPlayers(t *testing.T) {
	players := CreateTestPlayers()

	// Check that we have the expected number of players
	if len(players) != 2 {
		t.Fatalf("Expected 2 players, got %d", len(players))
	}

	// Check the first player
	if players[0].ID != "player1" {
		t.Errorf("Expected player1 ID to be 'player1', got '%s'", players[0].ID)
	}
	if players[0].Name != "John Doe" {
		t.Errorf("Expected player1 Name to be 'John Doe', got '%s'", players[0].Name)
	}

	// Check the second player
	if players[1].ID != "player2" {
		t.Errorf("Expected player2 ID to be 'player2', got '%s'", players[1].ID)
	}
	if players[1].Name != "Jane Smith" {
		t.Errorf("Expected player2 Name to be 'Jane Smith', got '%s'", players[1].Name)
	}
}

func TestCreateTestPools(t *testing.T) {
	pools := CreateTestPools()

	// Check that we have pools
	if len(pools) == 0 {
		t.Fatalf("Expected at least one pool, got none")
	}

	// Check the first pool
	if pools[0].ID != "pool1" {
		t.Errorf("Expected pool ID to be 'pool1', got '%s'", pools[0].ID)
	}

	// Check the pool has players
	if len(pools[0].Players) == 0 {
		t.Errorf("Expected players in pool, got none")
	}

	// Check the pool has matches
	if len(pools[0].Matches) == 0 {
		t.Errorf("Expected matches in pool, got none")
	}
}

func TestCreateTestTournament(t *testing.T) {
	tournament := CreateTestTournament()

	// Check the tournament name
	if tournament.Name != "Test Tournament" {
		t.Errorf("Expected tournament name to be 'Test Tournament', got '%s'", tournament.Name)
	}

	// Check that we have pools
	if len(tournament.Pools) == 0 {
		t.Fatalf("Expected at least one pool in tournament, got none")
	}

	// Check that we have elimination matches
	if len(tournament.EliminationMatches) == 0 {
		t.Fatalf("Expected at least one elimination match in tournament, got none")
	}
}
