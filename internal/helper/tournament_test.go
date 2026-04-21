package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePlayersWithZekkenName(t *testing.T) {
	entries := []string{
		"John Smith, クレスワェル, Tokyo Kendo Club",
		"Yuki Tanaka, 田中, Osaka Dojo",
	}

	players, err := CreatePlayers(entries, true)
	require.NoError(t, err)
	require.Len(t, players, 2)

	assert.Equal(t, "John Smith", players[0].Name)
	assert.Equal(t, "クレスワェル", players[0].DisplayName)
	assert.Equal(t, "Tokyo Kendo Club", players[0].Dojo)

	assert.Equal(t, "Yuki Tanaka", players[1].Name)
	assert.Equal(t, "田中", players[1].DisplayName)
	assert.Equal(t, "Osaka Dojo", players[1].Dojo)
}

func TestCreatePlayersWithZekkenNameFallback(t *testing.T) {
	entries := []string{
		"John Smith, , Tokyo Kendo Club",
	}

	players, err := CreatePlayers(entries, true)
	require.NoError(t, err)
	assert.Equal(t, "J. SMITH", players[0].DisplayName)
}

func TestCreatePlayersWithoutZekkenName(t *testing.T) {
	entries := []string{
		"John Smith, Tokyo Kendo Club",
		"Yuki Tanaka",
	}

	players, err := CreatePlayers(entries, false)
	require.NoError(t, err)

	assert.Equal(t, "Tokyo Kendo Club", players[0].Dojo)
	assert.Equal(t, "J. SMITH", players[0].DisplayName)
	assert.Equal(t, "NA", players[1].Dojo)
}

func TestCreatePlayersZekkenMissingDojo(t *testing.T) {
	entries := []string{
		"John Smith, クレスワェル",
	}

	_, err := CreatePlayers(entries, true)
	require.Error(t, err)
}

func TestCreatePlayersWithMetadata(t *testing.T) {
	// Case 1: Zekken ON
	entriesOn := []string{
		"John Smith, クレスワェル, Tokyo Kendo Club, Extra1, Extra2",
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
		"John Smith, Tokyo Kendo Club, Extra1, Extra2",
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
	createPlayers := func(n int, dojoModulo int) []Player {
		players := make([]Player, n)
		for i := 0; i < n; i++ {
			dojoN := i
			if dojoModulo > 0 {
				dojoN = i % dojoModulo
			}
			players[i] = Player{
				Name: fmt.Sprintf("Player%03d", i+1),
				Dojo: fmt.Sprintf("Dojo %02d", dojoN+1),
			}
		}
		return players
	}

	tests := []struct {
		name      string
		players   []Player
		poolSize  int
		isMax     bool
		wantPanic bool
		validate  func(t *testing.T, pools []Pool)
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
		{
			name:     "zero players returns zero pools",
			players:  []Player{},
			poolSize: 4,
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 0 {
					t.Errorf("Expected 0 pools, got %d", len(pools))
				}
			},
		},
		{
			name:     "single player per pool",
			players:  createPlayers(5, 5),
			poolSize: 1,
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 5 {
					t.Errorf("Expected 5 pools, got %d", len(pools))
				}

				totalPlayers := 0
				for i, pool := range pools {
					if len(pool.Players) != 1 {
						t.Errorf("Expected pool %d to have exactly 1 player, got %d", i, len(pool.Players))
					}
					totalPlayers += len(pool.Players)
				}

				if totalPlayers != 5 {
					t.Errorf("Expected 5 total players, got %d", totalPlayers)
				}
			},
		},
		{
			name:     "uneven division keeps all players",
			players:  createPlayers(10, 10),
			poolSize: 4,
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 2 {
					t.Errorf("Expected 2 pools, got %d", len(pools))
				}

				totalPlayers := 0
				for _, pool := range pools {
					totalPlayers += len(pool.Players)
				}
				if totalPlayers != 10 {
					t.Errorf("Expected 10 total players, got %d", totalPlayers)
				}
			},
		},
		{
			name:     "high volume 64 players",
			players:  createPlayers(64, 16),
			poolSize: 4,
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 16 {
					t.Errorf("Expected 16 pools, got %d", len(pools))
				}

				seen := make(map[string]int)
				totalPlayers := 0
				for _, pool := range pools {
					totalPlayers += len(pool.Players)
					for _, p := range pool.Players {
						seen[p.Name]++
					}
				}

				if totalPlayers != 64 {
					t.Errorf("Expected 64 total players, got %d", totalPlayers)
				}
				if len(seen) != 64 {
					t.Errorf("Expected 64 unique assigned players, got %d", len(seen))
				}
				for name, count := range seen {
					if count != 1 {
						t.Errorf("Player %s assigned %d times", name, count)
					}
				}
			},
		},
		{
			name:     "pool naming includes AA after Z",
			players:  createPlayers(54, 54),
			poolSize: 2,
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 27 {
					t.Errorf("Expected 27 pools, got %d", len(pools))
				}

				if pools[0].PoolName != "Pool A" {
					t.Errorf("Expected Pool A, got %s", pools[0].PoolName)
				}
				if pools[25].PoolName != "Pool Z" {
					t.Errorf("Expected Pool Z, got %s", pools[25].PoolName)
				}
				if pools[26].PoolName != "Pool AA" {
					t.Errorf("Expected Pool AA, got %s", pools[26].PoolName)
				}
			},
		},
		{
			name:      "panics when pool size is zero",
			players:   createPlayers(4, 4),
			poolSize:  0,
			wantPanic: true,
		},
		{
			name:      "panics when pool size larger than player count",
			players:   createPlayers(3, 3),
			poolSize:  4,
			wantPanic: true,
		},
		{
			name:     "max pool size mode creates exact number of pools",
			players:  createPlayers(11, 11),
			poolSize: 3,
			isMax:    true,
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 4 {
					t.Errorf("Expected 4 pools, got %d", len(pools))
				}
				for i, pool := range pools {
					if len(pool.Players) > 3 {
						t.Errorf("Expected pool %d to have at most 3 players, got %d", i, len(pool.Players))
					}
				}
			},
		},
		{
			name:     "10 players with max 3 players creates 4 pools (3, 3, 2, 2)",
			players:  createPlayers(10, 10),
			poolSize: 3,
			isMax:    true,
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 4 {
					t.Errorf("Expected 4 pools, got %d", len(pools))
				}

				poolSizes := make(map[int]int)
				for _, pool := range pools {
					poolSizes[len(pool.Players)]++
				}

				if poolSizes[3] != 2 {
					t.Errorf("Expected 2 pools of size 3, got %d", poolSizes[3])
				}
				if poolSizes[2] != 2 {
					t.Errorf("Expected 2 pools of size 2, got %d", poolSizes[2])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Expected panic, got nil")
					}
				}()
			}

			pools := CreatePools(tt.players, tt.poolSize, tt.isMax)
			if tt.validate != nil {
				tt.validate(t, pools)
			}
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
		poolIdx := discoverPool(pools, player, []int{2, 2})

		if poolIdx != 0 {
			t.Errorf("Expected pool 0, got %d", poolIdx)
		}
	})

	t.Run("avoids same dojo", func(t *testing.T) {
		player := Player{Name: "P3", Dojo: "Dojo A"}
		poolIdx := discoverPool(pools, player, []int{2, 2})

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
		poolIdx := discoverPool(fullPools, player, []int{2})

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

		poolIdx := forceSameDojo(pools, []int{2, 2})
		if poolIdx != 1 {
			t.Errorf("Expected pool 1, got %d", poolIdx)
		}
	})

	t.Run("returns -1 when all pools full", func(t *testing.T) {
		pools := []Pool{
			{Players: []Player{{Name: "P1"}, {Name: "P2"}}},
			{Players: []Player{{Name: "P3"}, {Name: "P4"}}},
		}

		poolIdx := forceSameDojo(pools, []int{2, 2})
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

		poolIdx := forcePoolSize(pools, []int{2, 2})
		// Should return 0 or 1 (first pool with space for poolSize+1)
		if poolIdx < 0 || poolIdx > 1 {
			t.Errorf("Expected 0 or 1, got %d", poolIdx)
		}
	})

	t.Run("handles empty pools", func(t *testing.T) {
		pools := []Pool{
			{Players: []Player{}},
		}

		poolIdx := forcePoolSize(pools, []int{3, 3})
		if poolIdx != 0 {
			t.Errorf("Expected 0, got %d", poolIdx)
		}
	})

	t.Run("returns first pool when all full", func(t *testing.T) {
		pools := []Pool{
			{Players: []Player{{Name: "P1"}, {Name: "P2"}, {Name: "P3"}}},
			{Players: []Player{{Name: "P4"}, {Name: "P5"}, {Name: "P6"}}},
		}

		poolIdx := forcePoolSize(pools, []int{2, 2})
		// Returns 0 when all pools are at capacity
		if poolIdx != 0 {
			t.Errorf("Expected 0, got %d", poolIdx)
		}
	})
}

func TestSanitizeNameExtended(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "two names",
			input:    "John Doe",
			expected: "J. DOE",
		},
		{
			name:     "three names - middle name ignored",
			input:    "John Michael Doe",
			expected: "J. DOE",
		},
		{
			name:     "single name",
			input:    "Madonna",
			expected: "MADONNA",
		},
		{
			name:     "extra whitespace",
			input:    "  John   Doe  ",
			expected: "J. DOE",
		},
		{
			name:     "lowercase input",
			input:    "john doe",
			expected: "J. DOE",
		},
		{
			name:     "uppercase input",
			input:    "JOHN DOE",
			expected: "J. DOE",
		},
		{
			name:     "hyphenated last name",
			input:    "Mary Smith-Jones",
			expected: "M. SMITH-JONES",
		},
		{
			name:     "four names",
			input:    "Mary Jane Anne Smith",
			expected: "M. SMITH",
		},
		{
			name:     "unicode characters",
			input:    "José García",
			expected: "J. GARCÍA",
		},
		{
			name:     "mixed case with spaces",
			input:    "JoHn   SmItH",
			expected: "J. SMITH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCreatePlayersEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		entries     []string
		withZekken  bool
		wantErr     bool
		errContains string
		validate    func(t *testing.T, players []Player)
	}{
		{
			name:       "empty entries list",
			entries:    []string{},
			withZekken: false,
			wantErr:    false,
			validate: func(t *testing.T, players []Player) {
				if len(players) != 0 {
					t.Errorf("Expected 0 players, got %d", len(players))
				}
			},
		},
		{
			name:       "entries with only whitespace",
			entries:    []string{"   ", "\t", "\n", ""},
			withZekken: false,
			wantErr:    false,
			validate: func(t *testing.T, players []Player) {
				if len(players) != 0 {
					t.Errorf("Expected 0 players from whitespace entries, got %d", len(players))
				}
			},
		},
		{
			name:       "mixed empty and valid entries",
			entries:    []string{"", "John Doe, Dojo A", "", "Jane Smith, Dojo B", "   "},
			withZekken: false,
			wantErr:    false,
			validate: func(t *testing.T, players []Player) {
				if len(players) != 2 {
					t.Errorf("Expected 2 players, got %d", len(players))
				}
			},
		},
		{
			name:        "zekken mode with only 2 columns",
			entries:     []string{"John Doe, JD"},
			withZekken:  true,
			wantErr:     true,
			errContains: "invalid entry: expected format 'Name, ZekkenName, Dojo'",
		},
		{
			name:        "zekken mode with empty dojo",
			entries:     []string{"John Doe, JD, "},
			withZekken:  true,
			wantErr:     true,
			errContains: "missing dojo in column 3",
		},
		{
			name:       "non-zekken mode with single column",
			entries:    []string{"John Doe"},
			withZekken: false,
			wantErr:    false,
			validate: func(t *testing.T, players []Player) {
				if len(players) != 1 {
					t.Fatalf("Expected 1 player, got %d", len(players))
				}
				if players[0].Name != "John Doe" {
					t.Errorf("Expected name 'John Doe', got %s", players[0].Name)
				}
				if players[0].Dojo != "NA" {
					t.Errorf("Expected dojo 'NA', got %s", players[0].Dojo)
				}
			},
		},
		{
			name:       "unicode names and dojos",
			entries:    []string{"田中太郎, 東京道場", "José García, México Dojo"},
			withZekken: false,
			wantErr:    false,
			validate: func(t *testing.T, players []Player) {
				if len(players) != 2 {
					t.Fatalf("Expected 2 players, got %d", len(players))
				}
				if players[0].Name != "田中太郎" {
					t.Errorf("Expected unicode name preserved, got %s", players[0].Name)
				}
			},
		},
		{
			name:       "metadata with multiple extra columns",
			entries:    []string{"John Doe, Dojo A, Rank1, Rank2, Rank3, Extra1"},
			withZekken: false,
			wantErr:    false,
			validate: func(t *testing.T, players []Player) {
				if len(players) != 1 {
					t.Fatalf("Expected 1 player, got %d", len(players))
				}
				if len(players[0].Metadata) != 4 {
					t.Errorf("Expected 4 metadata fields, got %d", len(players[0].Metadata))
				}
				if players[0].Metadata[0] != "Rank1" {
					t.Errorf("Expected first metadata 'Rank1', got %s", players[0].Metadata[0])
				}
			},
		},
		{
			name:       "very long names",
			entries:    []string{"Christopher Alexander Montgomery Wellington III, Very Long Dojo Name Association"},
			withZekken: false,
			wantErr:    false,
			validate: func(t *testing.T, players []Player) {
				if len(players) != 1 {
					t.Fatalf("Expected 1 player, got %d", len(players))
				}
				if players[0].DisplayName != "C. III" {
					t.Errorf("Expected displayName 'C. III', got %s", players[0].DisplayName)
				}
			},
		},
		{
			name:       "leading and trailing commas",
			entries:    []string{",John Doe, Dojo A,", "Jane Smith, Dojo B,"},
			withZekken: false,
			wantErr:    false,
			validate: func(t *testing.T, players []Player) {
				if len(players) != 2 {
					t.Fatalf("Expected 2 players, got %d", len(players))
				}
				// First entry has empty first field, so name becomes ""
				// This might be a valid test case or might need error handling
			},
		},
		{
			name:        "multiple validation errors",
			entries:     []string{"John Doe, Dojo A", "John Doe, Dojo A", "Jane Smith, JS"},
			withZekken:  true,
			wantErr:     true,
			errContains: "CSV validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			players, err := CreatePlayers(tt.entries, tt.withZekken)

			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, players)
			}
		})
	}
}

func TestCreatePoolMatchesEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		pools    []Pool
		validate func(t *testing.T, pools []Pool)
	}{
		{
			name:  "empty pools slice",
			pools: []Pool{},
			validate: func(t *testing.T, pools []Pool) {
				if len(pools) != 0 {
					t.Error("Expected empty pools slice to remain empty")
				}
			},
		},
		{
			name: "pool with single player",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players:  []Player{{Name: "Solo"}},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				if len(pools[0].Matches) != 1 {
					t.Errorf("Expected 1 match for single player, got %d", len(pools[0].Matches))
				}
				// Single player should match against themselves (position 0)
				if pools[0].Matches[0].SideA.Name != "Solo" || pools[0].Matches[0].SideB.Name != "Solo" {
					t.Error("Single player should match against themselves")
				}
			},
		},
		{
			name: "pool with two players",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players:  []Player{{Name: "P1"}, {Name: "P2"}},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				if len(pools[0].Matches) != 1 {
					t.Errorf("Expected 1 match, got %d", len(pools[0].Matches))
				}
			},
		},
		{
			name: "multiple pools - verify independence",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players:  []Player{{Name: "A1"}, {Name: "A2"}},
				},
				{
					PoolName: "Pool B",
					Players:  []Player{{Name: "B1"}, {Name: "B2"}, {Name: "B3"}},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				if len(pools[0].Matches) != 1 {
					t.Errorf("Pool A: Expected 1 match, got %d", len(pools[0].Matches))
				}
				if len(pools[1].Matches) != 3 {
					t.Errorf("Pool B: Expected 3 matches, got %d", len(pools[1].Matches))
				}
				// Verify no cross-pool contamination
				for _, match := range pools[0].Matches {
					if match.SideA.Name[0] != 'A' || match.SideB.Name[0] != 'A' {
						t.Error("Pool A match contains player from wrong pool")
					}
				}
			},
		},
		{
			name: "verify no duplicate match pairings in pool of 3",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players:  []Player{{Name: "P1"}, {Name: "P2"}, {Name: "P3"}},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				seen := make(map[string]bool)
				for _, match := range pools[0].Matches {
					key := fmt.Sprintf("%s-%s", match.SideA.Name, match.SideB.Name)
					reverseKey := fmt.Sprintf("%s-%s", match.SideB.Name, match.SideA.Name)
					if seen[key] || seen[reverseKey] {
						t.Errorf("Duplicate match pairing detected: %s", key)
					}
					seen[key] = true
				}
			},
		},
		{
			name: "pool of 4 - each player fights twice",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players:  []Player{{Name: "P1"}, {Name: "P2"}, {Name: "P3"}, {Name: "P4"}},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				playerMatches := make(map[string]int)
				for _, match := range pools[0].Matches {
					playerMatches[match.SideA.Name]++
					playerMatches[match.SideB.Name]++
				}
				for player, count := range playerMatches {
					if count != 2 {
						t.Errorf("Player %s appears in %d matches, expected 2", player, count)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			CreatePoolMatches(tt.pools)
			tt.validate(t, tt.pools)
		})
	}
}

func TestCreatePoolRoundRobinMatchesEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		pools    []Pool
		validate func(t *testing.T, pools []Pool)
	}{
		{
			name: "pool of 2 - minimal round robin",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players:  []Player{{Name: "P1"}, {Name: "P2"}},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				if len(pools[0].Matches) != 1 {
					t.Errorf("Expected 1 match for 2 players, got %d", len(pools[0].Matches))
				}
			},
		},
		{
			name: "pool of 6 - larger round robin",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players: []Player{
						{Name: "P1"}, {Name: "P2"}, {Name: "P3"},
						{Name: "P4"}, {Name: "P5"}, {Name: "P6"},
					},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				expectedMatches := (6 * 5) / 2 // n(n-1)/2
				if len(pools[0].Matches) != expectedMatches {
					t.Errorf("Expected %d matches (6 choose 2), got %d", expectedMatches, len(pools[0].Matches))
				}
				// Verify each player appears correct number of times
				playerMatches := make(map[string]int)
				for _, match := range pools[0].Matches {
					playerMatches[match.SideA.Name]++
					playerMatches[match.SideB.Name]++
				}
				for player, count := range playerMatches {
					if count != 5 { // Each player should face 5 others
						t.Errorf("Player %s appears in %d matches, expected 5", player, count)
					}
				}
			},
		},
		{
			name: "verify pool of 4 special case swapping",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players: []Player{
						{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"},
					},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				if len(pools[0].Matches) != 6 {
					t.Errorf("Expected 6 matches (4 choose 2), got %d", len(pools[0].Matches))
				}
				// The special swapping logic should ensure proper round-robin order
				// Just verify structure integrity
				for i, match := range pools[0].Matches {
					if match.SideA == nil || match.SideB == nil {
						t.Errorf("Match %d has nil player", i)
					}
					if match.SideA.Name == match.SideB.Name {
						t.Errorf("Match %d has player against themselves", i)
					}
				}
			},
		},
		{
			name: "empty pool",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players:  []Player{},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				if len(pools[0].Matches) != 0 {
					t.Errorf("Expected 0 matches for empty pool, got %d", len(pools[0].Matches))
				}
			},
		},
		{
			name: "single player pool",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players:  []Player{{Name: "Solo"}},
				},
			},
			validate: func(t *testing.T, pools []Pool) {
				if len(pools[0].Matches) != 0 {
					t.Errorf("Expected 0 matches for single player, got %d", len(pools[0].Matches))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			CreatePoolRoundRobinMatches(tt.pools)
			tt.validate(t, tt.pools)
		})
	}
}

func TestConvertPlayersToWinnersEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		players   []Player
		sanitized bool
		validate  func(t *testing.T, winners map[string]MatchWinner)
	}{
		{
			name:      "empty players list",
			players:   []Player{},
			sanitized: true,
			validate: func(t *testing.T, winners map[string]MatchWinner) {
				if len(winners) != 0 {
					t.Errorf("Expected empty map, got %d entries", len(winners))
				}
			},
		},
		{
			name: "duplicate display names with sanitized=true",
			players: []Player{
				{Name: "John Doe", DisplayName: "J. DOE", sheetName: "Sheet1", cell: "A1"},
				{Name: "Jane Doe", DisplayName: "J. DOE", sheetName: "Sheet1", cell: "A2"},
			},
			sanitized: true,
			validate: func(t *testing.T, winners map[string]MatchWinner) {
				// Last one wins due to map overwrite
				if len(winners) != 1 {
					t.Errorf("Expected 1 entry (overwritten), got %d", len(winners))
				}
				if winner, ok := winners["J. DOE"]; !ok {
					t.Error("Expected key 'J. DOE' in map")
				} else if winner.cell != "A2" {
					t.Errorf("Expected last player's cell 'A2', got %s", winner.cell)
				}
			},
		},
		{
			name: "duplicate names with sanitized=false",
			players: []Player{
				{Name: "John Smith", DisplayName: "J. SMITH", sheetName: "Sheet1", cell: "A1"},
				{Name: "John Smith", DisplayName: "J. SMITH", sheetName: "Sheet2", cell: "B1"},
			},
			sanitized: false,
			validate: func(t *testing.T, winners map[string]MatchWinner) {
				// Last one wins due to map overwrite
				if len(winners) != 1 {
					t.Errorf("Expected 1 entry (overwritten), got %d", len(winners))
				}
				if winner, ok := winners["John Smith"]; !ok {
					t.Error("Expected key 'John Smith' in map")
				} else if winner.sheetName != "Sheet2" {
					t.Errorf("Expected last player's sheet 'Sheet2', got %s", winner.sheetName)
				}
			},
		},
		{
			name: "single player",
			players: []Player{
				{Name: "Alice", DisplayName: "ALICE", sheetName: "Main", cell: "C5"},
			},
			sanitized: true,
			validate: func(t *testing.T, winners map[string]MatchWinner) {
				if len(winners) != 1 {
					t.Errorf("Expected 1 entry, got %d", len(winners))
				}
				if winner, ok := winners["ALICE"]; !ok {
					t.Error("Expected key 'ALICE' in map")
				} else {
					if winner.sheetName != "Main" {
						t.Errorf("Expected sheetName 'Main', got %s", winner.sheetName)
					}
					if winner.cell != "C5" {
						t.Errorf("Expected cell 'C5', got %s", winner.cell)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			winners := ConvertPlayersToWinners(tt.players, tt.sanitized)
			tt.validate(t, winners)
		})
	}
}

// TestCreatePoolMatchesOrder verifies the specific match ordering for non-round-robin pools.
// Non-round-robin creates n matches for n players where each player fights exactly twice.
func TestCreatePoolMatchesOrder(t *testing.T) {
	tests := []struct {
		name          string
		players       []Player
		expectedPairs []struct{ sideA, sideB string }
		description   string
	}{
		{
			name: "pool of 2 - minimal case",
			players: []Player{
				{Name: "P1"},
				{Name: "P2"},
			},
			expectedPairs: []struct{ sideA, sideB string }{
				{"P1", "P2"}, // Match 0: single match between the two players
			},
			description: "2 players create 1 match",
		},
		{
			name: "pool of 3 - verifies circular pairing",
			players: []Player{
				{Name: "P1"},
				{Name: "P2"},
				{Name: "P3"},
			},
			expectedPairs: []struct{ sideA, sideB string }{
				{"P1", "P2"}, // Match 0
				{"P1", "P3"}, // Match 1
				{"P2", "P3"}, // Match 2
			},
			description: "3 players: each fights twice in defined order",
		},
		{
			name: "pool of 4 - defined order",
			players: []Player{
				{Name: "P1"},
				{Name: "P2"},
				{Name: "P3"},
				{Name: "P4"},
			},
			expectedPairs: []struct{ sideA, sideB string }{
				{"P1", "P2"}, // Match 0
				{"P3", "P2"}, // Match 1
				{"P3", "P4"}, // Match 2
				{"P1", "P4"}, // Match 3
			},
			description: "4 players: each fights twice in defined order",
		},
		{
			name: "pool of 5 - larger pool",
			players: []Player{
				{Name: "A"},
				{Name: "B"},
				{Name: "C"},
				{Name: "D"},
				{Name: "E"},
			},
			expectedPairs: []struct{ sideA, sideB string }{
				{"A", "B"}, // Match 0
				{"C", "B"}, // Match 1
				{"C", "D"}, // Match 2
				{"E", "D"}, // Match 3
				{"E", "A"}, // Match 4
			},
			description: "5 players: cycle order keeps two matches per player",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pools := []Pool{
				{
					PoolName: "Test Pool",
					Players:  tt.players,
				},
			}

			CreatePoolMatches(pools)

			if len(pools[0].Matches) != len(tt.expectedPairs) {
				t.Fatalf("Expected %d matches, got %d", len(tt.expectedPairs), len(pools[0].Matches))
			}

			for i, match := range pools[0].Matches {
				expected := tt.expectedPairs[i]
				if match.SideA.Name != expected.sideA || match.SideB.Name != expected.sideB {
					t.Errorf("Match %d: expected %s vs %s, got %s vs %s",
						i, expected.sideA, expected.sideB, match.SideA.Name, match.SideB.Name)
				}
			}
		})
	}
}

// TestCreatePoolRoundRobinMatchesOrder verifies the specific match ordering for round-robin pools.
// Round-robin creates all possible pairings (n choose 2) with specific ordering logic including
// side swapping and special handling for pools of 4.
func TestCreatePoolRoundRobinMatchesOrder(t *testing.T) {
	tests := []struct {
		name          string
		players       []Player
		expectedPairs []struct{ sideA, sideB string }
		description   string
	}{
		{
			name: "pool of 3 - all pairings",
			players: []Player{
				{Name: "A"},
				{Name: "B"},
				{Name: "C"},
			},
			expectedPairs: []struct{ sideA, sideB string }{
				{"A", "B"},
				{"A", "C"},
				{"B", "C"},
			},
			description: "3 players: 3 matches covering all pairings",
		},
		{
			name: "pool of 4 - special case handling",
			players: []Player{
				{Name: "A"},
				{Name: "B"},
				{Name: "C"},
				{Name: "D"},
			},
			expectedPairs: []struct{ sideA, sideB string }{
				{"A", "B"},
				{"C", "B"},
				{"C", "D"},
				{"A", "D"},
				{"A", "C"},
				{"B", "D"},
			},
			description: "4 players: 6 matches in defined order",
		},
		{
			name: "pool of 5 - standard round-robin",
			players: []Player{
				{Name: "P1"},
				{Name: "P2"},
				{Name: "P3"},
				{Name: "P4"},
				{Name: "P5"},
			},
			expectedPairs: []struct{ sideA, sideB string }{
				{"P1", "P2"},
				{"P3", "P2"},
				{"P3", "P4"},
				{"P5", "P4"},
				{"P1", "P3"},
				{"P2", "P4"},
				{"P3", "P5"},
				{"P1", "P4"},
				{"P2", "P5"},
				{"P1", "P5"},
			},
			description: "5 players: 10 matches with no side switching for consecutive fighters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pools := []Pool{
				{
					PoolName: "Test Pool",
					Players:  tt.players,
				},
			}

			CreatePoolRoundRobinMatches(pools)

			if len(pools[0].Matches) != len(tt.expectedPairs) {
				t.Fatalf("Expected %d matches, got %d", len(tt.expectedPairs), len(pools[0].Matches))
			}

			for i, match := range pools[0].Matches {
				expected := tt.expectedPairs[i]
				if match.SideA.Name != expected.sideA || match.SideB.Name != expected.sideB {
					t.Errorf("Match %d: expected %s vs %s, got %s vs %s",
						i, expected.sideA, expected.sideB, match.SideA.Name, match.SideB.Name)
				}
			}
		})
	}
}

// TestPoolMatchOrderingComparison verifies that round-robin and non-round-robin produce
// different match structures for the same pool.
func TestPoolMatchOrderingComparison(t *testing.T) {
	players := []Player{
		{Name: "A"},
		{Name: "B"},
		{Name: "C"},
		{Name: "D"},
	}

	t.Run("non-round-robin vs round-robin match counts", func(t *testing.T) {
		// Non-round-robin: n matches for n players
		nonRRPools := []Pool{{PoolName: "Pool A", Players: players}}
		CreatePoolMatches(nonRRPools)

		// Round-robin: n(n-1)/2 matches for n players
		rrPools := []Pool{{PoolName: "Pool A", Players: players}}
		CreatePoolRoundRobinMatches(rrPools)

		if len(nonRRPools[0].Matches) != 4 {
			t.Errorf("Non-round-robin: expected 4 matches, got %d", len(nonRRPools[0].Matches))
		}
		if len(rrPools[0].Matches) != 6 {
			t.Errorf("Round-robin: expected 6 matches, got %d", len(rrPools[0].Matches))
		}
	})

	t.Run("round-robin covers all pairings", func(t *testing.T) {
		rrPools := []Pool{{PoolName: "Pool A", Players: players}}
		CreatePoolRoundRobinMatches(rrPools)

		// Track all pairings (unordered)
		pairings := make(map[string]bool)
		for _, match := range rrPools[0].Matches {
			// Create canonical pairing key (alphabetically sorted)
			p1, p2 := match.SideA.Name, match.SideB.Name
			if p1 > p2 {
				p1, p2 = p2, p1
			}
			key := p1 + "-" + p2
			if pairings[key] {
				t.Errorf("Duplicate pairing: %s", key)
			}
			pairings[key] = true
		}

		// Should have exactly 6 unique pairings for 4 players
		if len(pairings) != 6 {
			t.Errorf("Expected 6 unique pairings, got %d", len(pairings))
		}

		// Verify all expected pairings exist
		expectedPairings := []string{"A-B", "A-C", "A-D", "B-C", "B-D", "C-D"}
		for _, expected := range expectedPairings {
			if !pairings[expected] {
				t.Errorf("Missing expected pairing: %s", expected)
			}
		}
	})

	t.Run("non-round-robin does not cover all pairings", func(t *testing.T) {
		nonRRPools := []Pool{{PoolName: "Pool A", Players: players}}
		CreatePoolMatches(nonRRPools)

		// Track all pairings
		pairings := make(map[string]bool)
		for _, match := range nonRRPools[0].Matches {
			p1, p2 := match.SideA.Name, match.SideB.Name
			if p1 > p2 {
				p1, p2 = p2, p1
			}
			pairings[p1+"-"+p2] = true
		}

		// Non-round-robin should have fewer unique pairings than round-robin
		// For 4 players: non-RR has 4 matches but may have duplicate pairings
		if len(pairings) >= 6 {
			t.Errorf("Non-round-robin should have fewer than 6 unique pairings, got %d", len(pairings))
		}
	})
}
