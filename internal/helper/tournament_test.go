package helper

import (
	"strings"
	"testing"
)

func TestCreatePlayersWithZekkenName(t *testing.T) {
	entries := []string{
		"Ricardo Oliveira, クレスワェル, Tokyo Kendo Club",
		"Yuki Tanaka, 田中, Osaka Dojo",
	}

	players, err := CreatePlayers(entries, true)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(players) != 2 {
		t.Fatalf("Expected 2 players, got %d", len(players))
	}

	if players[0].Name != "Ricardo Oliveira" {
		t.Errorf("Expected Ricardo Oliveira, got %s", players[0].Name)
	}
	if players[0].DisplayName != "クレスワェル" {
		t.Errorf("Expected クレスワェル, got %s", players[0].DisplayName)
	}
	if players[0].Dojo != "Tokyo Kendo Club" {
		t.Errorf("Expected Tokyo Kendo Club, got %s", players[0].Dojo)
	}

	if players[1].Name != "Yuki Tanaka" {
		t.Errorf("Expected Yuki Tanaka, got %s", players[1].Name)
	}
	if players[1].DisplayName != "田中" {
		t.Errorf("Expected 田中, got %s", players[1].DisplayName)
	}
	if players[1].Dojo != "Osaka Dojo" {
		t.Errorf("Expected Osaka Dojo, got %s", players[1].Dojo)
	}
}

func TestCreatePlayersWithZekkenNameFallback(t *testing.T) {
	entries := []string{
		"Ricardo Oliveira, , Tokyo Kendo Club",
	}

	players, err := CreatePlayers(entries, true)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if players[0].DisplayName != "R. OLIVEIRA" {
		t.Errorf("Expected R. OLIVEIRA, got %s", players[0].DisplayName)
	}
}

func TestCreatePlayersWithoutZekkenName(t *testing.T) {
	entries := []string{
		"Ricardo Oliveira, Tokyo Kendo Club",
		"Yuki Tanaka",
	}

	players, err := CreatePlayers(entries, false)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if players[0].Dojo != "Tokyo Kendo Club" {
		t.Errorf("Expected Tokyo Kendo Club, got %s", players[0].Dojo)
	}
	if players[0].DisplayName != "R. OLIVEIRA" {
		t.Errorf("Expected R. OLIVEIRA, got %s", players[0].DisplayName)
	}

	if players[1].Dojo != "NA" {
		t.Errorf("Expected NA, got %s", players[1].Dojo)
	}
}

func TestCreatePlayersZekkenMissingDojo(t *testing.T) {
	entries := []string{
		"Ricardo Oliveira, クレスワェル",
	}

	_, err := CreatePlayers(entries, true)
	if err == nil {
		t.Fatal("Expected error for missing dojo, got nil")
	}
}

func TestCreatePlayersWithMetadata(t *testing.T) {
	// Case 1: Zekken ON
	entriesOn := []string{
		"Ricardo Oliveira, クレスワェル, Tokyo Kendo Club, Extra1, Extra2",
	}
	playersOn, err := CreatePlayers(entriesOn, true)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(playersOn[0].Metadata) != 2 {
		t.Fatalf("Expected 2 metadata fields, got %d", len(playersOn[0].Metadata))
	}
	if playersOn[0].Metadata[0] != "Extra1" || playersOn[0].Metadata[1] != "Extra2" {
		t.Errorf("Unexpected metadata values: %v", playersOn[0].Metadata)
	}

	// Case 2: Zekken OFF
	entriesOff := []string{
		"Ricardo Oliveira, Tokyo Kendo Club, Extra1, Extra2",
	}
	playersOff, err := CreatePlayers(entriesOff, false)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(playersOff[0].Metadata) != 2 {
		t.Fatalf("Expected 2 metadata fields, got %d", len(playersOff[0].Metadata))
	}
	if playersOff[0].Metadata[0] != "Extra1" || playersOff[0].Metadata[1] != "Extra2" {
		t.Errorf("Unexpected metadata values: %v", playersOff[0].Metadata)
	}
}

func TestCreatePlayersDuplicates(t *testing.T) {
	// Case 1: Identical entries (Duplicate Name + Dojo/Zekken)
	entries := []string{
		"John Doe, Dojo A",
		"Jane Doe, Dojo B",
		"John Doe, Dojo A",
	}
	_, err := CreatePlayers(entries, false)
	if err == nil {
		t.Fatal("Expected error for identical entry, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate entry for participant 'John Doe'") {
		t.Errorf("Unexpected error message: %v", err)
	}

	// Case 2: Same Zekken for different people (ALLOWED)
	entriesSameZekken := []string{
		"John Doe, JD, Dojo A",
		"Jane Doe, JD, Dojo B",
	}
	players, err := CreatePlayers(entriesSameZekken, true)
	if err != nil {
		t.Fatalf("Expected no error for different people with same Zekken, got %v", err)
	}
	if len(players) != 2 {
		t.Errorf("Expected 2 players, got %d", len(players))
	}

	// Case 3: Same Name but different Dojo (ALLOWED)
	entriesSameNameDifferentDojo := []string{
		"John Doe, Dojo A",
		"John Doe, Dojo B",
	}
	players2, err := CreatePlayers(entriesSameNameDifferentDojo, false)
	if err != nil {
		t.Fatalf("Expected no error for same name in different dojos, got %v", err)
	}
	if len(players2) != 2 {
		t.Errorf("Expected 2 players, got %d", len(players2))
	}
}

func TestCreatePools(t *testing.T) {
	tests := []struct {
		name     string
		players  []Player
		poolSize int
		validate func(t *testing.T, pools []Pool)
	}{
		{
			name: "8 players into pools of 4",
			players: []Player{
				{Name: "Player1", Dojo: "Dojo A"},
				{Name: "Player2", Dojo: "Dojo B"},
				{Name: "Player3", Dojo: "Dojo C"},
				{Name: "Player4", Dojo: "Dojo D"},
				{Name: "Player5", Dojo: "Dojo E"},
				{Name: "Player6", Dojo: "Dojo F"},
				{Name: "Player7", Dojo: "Dojo G"},
				{Name: "Player8", Dojo: "Dojo H"},
			},
			poolSize: 4,
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 2 {
					t.Errorf("Expected 2 pools, got %d", len(pools))
				}
				if pools[0].PoolName != "Pool A" {
					t.Errorf("Expected Pool A, got %s", pools[0].PoolName)
				}
				if pools[1].PoolName != "Pool B" {
					t.Errorf("Expected Pool B, got %s", pools[1].PoolName)
				}
				// Verify all players are assigned
				totalPlayers := len(pools[0].Players) + len(pools[1].Players)
				if totalPlayers != 8 {
					t.Errorf("Expected 8 total players, got %d", totalPlayers)
				}
			},
		},
		{
			name: "6 players into pools of 3",
			players: []Player{
				{Name: "P1", Dojo: "D1"},
				{Name: "P2", Dojo: "D2"},
				{Name: "P3", Dojo: "D3"},
				{Name: "P4", Dojo: "D4"},
				{Name: "P5", Dojo: "D5"},
				{Name: "P6", Dojo: "D6"},
			},
			poolSize: 3,
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 2 {
					t.Errorf("Expected 2 pools, got %d", len(pools))
				}
			},
		},
		{
			name: "same dojo players distributed",
			players: []Player{
				{Name: "P1", Dojo: "SameDojo"},
				{Name: "P2", Dojo: "SameDojo"},
				{Name: "P3", Dojo: "DifferentDojo"},
				{Name: "P4", Dojo: "DifferentDojo"},
			},
			poolSize: 2,
			validate: func(t *testing.T, pools []Pool) {
				// Verify pools were created
				if len(pools) != 2 {
					t.Errorf("Expected 2 pools, got %d", len(pools))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pools := CreatePools(tt.players, tt.poolSize)
			tt.validate(t, pools)
		})
	}
}

func TestCreatePoolMatches(t *testing.T) {
	players := []Player{
		{Name: "P1"},
		{Name: "P2"},
		{Name: "P3"},
		{Name: "P4"},
	}

	pools := []Pool{
		{
			PoolName: "Pool A",
			Players:  players,
		},
	}

	CreatePoolMatches(pools)

	if len(pools[0].Matches) != 4 {
		t.Errorf("Expected 4 matches, got %d", len(pools[0].Matches))
	}

	// Verify matches are created
	for i, match := range pools[0].Matches {
		if match.SideA == nil || match.SideB == nil {
			t.Errorf("Match %d has nil player", i)
		}
	}
}

func TestCreatePoolRoundRobinMatches(t *testing.T) {
	tests := []struct {
		name            string
		poolSize        int
		expectedMatches int
	}{
		{
			name:            "pool of 3",
			poolSize:        3,
			expectedMatches: 3, // 3 choose 2 = 3
		},
		{
			name:            "pool of 4",
			poolSize:        4,
			expectedMatches: 6, // 4 choose 2 = 6
		},
		{
			name:            "pool of 5",
			poolSize:        5,
			expectedMatches: 10, // 5 choose 2 = 10
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			players := make([]Player, tt.poolSize)
			for i := 0; i < tt.poolSize; i++ {
				players[i] = Player{Name: string(rune('A' + i))}
			}

			pools := []Pool{
				{
					PoolName: "Pool A",
					Players:  players,
				},
			}

			CreatePoolRoundRobinMatches(pools)

			if len(pools[0].Matches) != tt.expectedMatches {
				t.Errorf("Expected %d matches, got %d", tt.expectedMatches, len(pools[0].Matches))
			}

			// Verify all matches have valid players
			for i, match := range pools[0].Matches {
				if match.SideA == nil || match.SideB == nil {
					t.Errorf("Match %d has nil player", i)
				}
				if match.SideA.Name == match.SideB.Name {
					t.Errorf("Match %d has same player on both sides", i)
				}
			}
		})
	}
}

func TestConvertPlayersToWinners(t *testing.T) {
	players := []Player{
		{Name: "Alice", DisplayName: "A. SMITH", sheetName: "Sheet1", cell: "A1"},
		{Name: "Bob", DisplayName: "B. JONES", sheetName: "Sheet1", cell: "A2"},
	}

	t.Run("with sanitized names", func(t *testing.T) {
		winners := ConvertPlayersToWinners(players, true)

		if len(winners) != 2 {
			t.Errorf("Expected 2 winners, got %d", len(winners))
		}

		if _, ok := winners["A. SMITH"]; !ok {
			t.Error("Expected winner with DisplayName 'A. SMITH'")
		}

		if _, ok := winners["B. JONES"]; !ok {
			t.Error("Expected winner with DisplayName 'B. JONES'")
		}
	})

	t.Run("without sanitized names", func(t *testing.T) {
		winners := ConvertPlayersToWinners(players, false)

		if len(winners) != 2 {
			t.Errorf("Expected 2 winners, got %d", len(winners))
		}

		if _, ok := winners["Alice"]; !ok {
			t.Error("Expected winner with Name 'Alice'")
		}

		if _, ok := winners["Bob"]; !ok {
			t.Error("Expected winner with Name 'Bob'")
		}
	})
}

func TestDiscoverPool(t *testing.T) {
	pools := []Pool{
		{
			PoolName: "Pool A",
			Players: []Player{
				{Name: "P1", Dojo: "Dojo A"},
			},
		},
		{
			PoolName: "Pool B",
			Players:  []Player{},
		},
	}

	t.Run("finds empty pool", func(t *testing.T) {
		player := Player{Name: "P2", Dojo: "Dojo B"}
		poolIdx := discoverPool(pools, player, 2)

		if poolIdx != 0 {
			t.Errorf("Expected pool 0, got %d", poolIdx)
		}
	})

	t.Run("avoids same dojo", func(t *testing.T) {
		player := Player{Name: "P3", Dojo: "Dojo A"}
		poolIdx := discoverPool(pools, player, 2)

		// Should find pool 1 since pool 0 has same dojo
		if poolIdx != 1 {
			t.Errorf("Expected pool 1, got %d", poolIdx)
		}
	})

	t.Run("returns -1 when no suitable pool", func(t *testing.T) {
		fullPools := []Pool{
			{
				PoolName: "Pool A",
				Players: []Player{
					{Name: "P1", Dojo: "D1"},
					{Name: "P2", Dojo: "D2"},
				},
			},
		}
		player := Player{Name: "P3", Dojo: "D3"}
		poolIdx := discoverPool(fullPools, player, 2)

		if poolIdx != -1 {
			t.Errorf("Expected -1, got %d", poolIdx)
		}
	})
}

func TestForceSameDojo(t *testing.T) {
	t.Run("finds pool with space", func(t *testing.T) {
		pools := []Pool{
			{Players: []Player{{Name: "P1"}, {Name: "P2"}}},
			{Players: []Player{{Name: "P3"}}},
		}

		poolIdx := forceSameDojo(pools, 2)
		if poolIdx != 1 {
			t.Errorf("Expected pool 1, got %d", poolIdx)
		}
	})

	t.Run("returns -1 when all pools full", func(t *testing.T) {
		pools := []Pool{
			{Players: []Player{{Name: "P1"}, {Name: "P2"}}},
			{Players: []Player{{Name: "P3"}, {Name: "P4"}}},
		}

		poolIdx := forceSameDojo(pools, 2)
		if poolIdx != -1 {
			t.Errorf("Expected -1, got %d", poolIdx)
		}
	})
}

func TestForcePoolSize(t *testing.T) {
	t.Run("finds pool with room for extra player", func(t *testing.T) {
		pools := []Pool{
			{Players: []Player{{Name: "P1"}, {Name: "P2"}}},
			{Players: []Player{{Name: "P3"}, {Name: "P4"}}},
		}

		poolIdx := forcePoolSize(pools, 2)
		// Should return 0 or 1 (first pool with space for poolSize+1)
		if poolIdx < 0 || poolIdx > 1 {
			t.Errorf("Expected 0 or 1, got %d", poolIdx)
		}
	})
}
