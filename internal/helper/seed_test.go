package helper

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestGenerateBracketOrder(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		expected []int
	}{
		{
			name:     "single player",
			n:        1,
			expected: []int{1},
		},
		{
			name:     "two players",
			n:        2,
			expected: []int{1, 2},
		},
		{
			name:     "four players",
			n:        4,
			expected: []int{1, 4, 2, 3},
		},
		{
			name:     "eight players",
			n:        8,
			expected: []int{1, 8, 4, 5, 2, 7, 3, 6},
		},
		{
			name:     "sixteen players",
			n:        16,
			expected: []int{1, 16, 8, 9, 4, 13, 5, 12, 2, 15, 7, 10, 3, 14, 6, 11},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateBracketOrder(tt.n)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateBracketOrder_SeedToPoolMappingForEightPools(t *testing.T) {
	order := generateBracketOrder(8)
	poolLabels := []string{"A", "B", "C", "D", "E", "F", "G", "H"}

	seedToPool := make(map[int]string, len(order))
	for seedRank, poolNumber := range order {
		seedToPool[seedRank+1] = poolLabels[poolNumber-1]
	}

	assert.Equal(t, "A", seedToPool[1], "seed #1 should map to pool A")
	assert.Equal(t, "H", seedToPool[2], "seed #2 should map to pool H")
	assert.Equal(t, "D", seedToPool[3], "seed #3 should map to pool D")
	assert.Equal(t, "E", seedToPool[4], "seed #4 should map to pool E")

	assert.Equal(t, map[int]string{
		1: "A",
		2: "H",
		3: "D",
		4: "E",
		5: "B",
		6: "G",
		7: "C",
		8: "F",
	}, seedToPool)
}

func TestStandardSeeding(t *testing.T) {
	tests := []struct {
		name     string
		players  []Player
		validate func(t *testing.T, result []Player)
	}{
		{
			name: "all unseeded players",
			players: []Player{
				{Name: "Player1", Seed: 0},
				{Name: "Player2", Seed: 0},
				{Name: "Player3", Seed: 0},
				{Name: "Player4", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 4)
				// All players should be present
				names := make(map[string]bool)
				for _, p := range result {
					names[p.Name] = true
				}
				assert.True(t, names["Player1"])
				assert.True(t, names["Player2"])
				assert.True(t, names["Player3"])
				assert.True(t, names["Player4"])
			},
		},
		{
			name: "two seeded players in 4-player bracket",
			players: []Player{
				{Name: "Seed1", Seed: 1},
				{Name: "Seed2", Seed: 2},
				{Name: "Player3", Seed: 0},
				{Name: "Player4", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 4)
				// Seed 1 should be at position 0
				assert.Equal(t, "Seed1", result[0].Name)
				assert.Equal(t, 1, result[0].Seed)
				// Verify all players are present
				names := make(map[string]bool)
				for _, p := range result {
					names[p.Name] = true
				}
				assert.True(t, names["Seed1"])
				assert.True(t, names["Seed2"])
				// Verify seeds are properly assigned
				seedCount := 0
				for _, p := range result {
					if p.Seed > 0 {
						seedCount++
					}
				}
				assert.Equal(t, 2, seedCount)
			},
		},
		{
			name: "four seeded players in 8-player bracket",
			players: []Player{
				{Name: "Seed1", Seed: 1},
				{Name: "Seed2", Seed: 2},
				{Name: "Seed3", Seed: 3},
				{Name: "Seed4", Seed: 4},
				{Name: "Player5", Seed: 0},
				{Name: "Player6", Seed: 0},
				{Name: "Player7", Seed: 0},
				{Name: "Player8", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 8)
				// Seed 1 should be first
				assert.Equal(t, 1, result[0].Seed)
				// Verify all 4 seeds are present
				seedCount := 0
				for _, p := range result {
					if p.Seed > 0 {
						seedCount++
					}
				}
				assert.Equal(t, 4, seedCount)
				// Verify all players are present
				names := make(map[string]bool)
				for _, p := range result {
					names[p.Name] = true
				}
				assert.Len(t, names, 8)
			},
		},
		{
			name: "non-power-of-2 bracket with seeds",
			players: []Player{
				{Name: "Seed1", Seed: 1},
				{Name: "Seed2", Seed: 2},
				{Name: "Player3", Seed: 0},
				{Name: "Player4", Seed: 0},
				{Name: "Player5", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 5)
				// Seed 1 should be first
				assert.Equal(t, 1, result[0].Seed)
				// All players should be present
				names := make(map[string]bool)
				for _, p := range result {
					names[p.Name] = true
				}
				assert.Len(t, names, 5)
			},
		},
		{
			name: "single player",
			players: []Player{
				{Name: "OnlyPlayer", Seed: 1},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 1)
				assert.Equal(t, "OnlyPlayer", result[0].Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StandardSeeding(tt.players)
			tt.validate(t, result)
		})
	}
}

func TestApplySeeds(t *testing.T) {
	tests := []struct {
		name        string
		players     []Player
		assignments []domain.SeedAssignment
		wantErr     bool
		errContains string
		validate    func(t *testing.T, players []Player)
	}{
		{
			name: "successful seed assignment",
			players: []Player{
				{Name: "Alice", Seed: 0},
				{Name: "Bob", Seed: 0},
				{Name: "Charlie", Seed: 0},
			},
			assignments: []domain.SeedAssignment{
				{Name: "Alice", SeedRank: 1},
				{Name: "Bob", SeedRank: 2},
			},
			wantErr: false,
			validate: func(t *testing.T, players []Player) {
				// Find Alice and Bob
				var alice, bob *Player
				for i := range players {
					if players[i].Name == "Alice" {
						alice = &players[i]
					}
					if players[i].Name == "Bob" {
						bob = &players[i]
					}
				}
				assert.NotNil(t, alice)
				assert.NotNil(t, bob)
				assert.Equal(t, 1, alice.Seed)
				assert.Equal(t, 2, bob.Seed)
			},
		},
		{
			name: "seed collision - swap existing seed",
			players: []Player{
				{Name: "Alice", Seed: 1},
				{Name: "Bob", Seed: 0},
			},
			assignments: []domain.SeedAssignment{
				{Name: "Bob", SeedRank: 1},
			},
			wantErr: false,
			validate: func(t *testing.T, players []Player) {
				var alice, bob *Player
				for i := range players {
					if players[i].Name == "Alice" {
						alice = &players[i]
					}
					if players[i].Name == "Bob" {
						bob = &players[i]
					}
				}
				assert.NotNil(t, alice)
				assert.NotNil(t, bob)
				// Bob should get seed 1, Alice's seed should be swapped to 0
				assert.Equal(t, 1, bob.Seed)
				assert.Equal(t, 0, alice.Seed)
			},
		},
		{
			name: "participant not found",
			players: []Player{
				{Name: "Alice", Seed: 0},
				{Name: "Bob", Seed: 0},
			},
			assignments: []domain.SeedAssignment{
				{Name: "Charlie", SeedRank: 1},
			},
			wantErr:     true,
			errContains: "seeded participant not found in main list: Charlie",
		},
		{
			name: "empty assignments",
			players: []Player{
				{Name: "Alice", Seed: 0},
				{Name: "Bob", Seed: 0},
			},
			assignments: []domain.SeedAssignment{},
			wantErr:     false,
			validate: func(t *testing.T, players []Player) {
				// All seeds should remain 0
				for _, p := range players {
					assert.Equal(t, 0, p.Seed)
				}
			},
		},
		{
			name: "multiple seed assignments",
			players: []Player{
				{Name: "Alice", Seed: 0},
				{Name: "Bob", Seed: 0},
				{Name: "Charlie", Seed: 0},
				{Name: "David", Seed: 0},
			},
			assignments: []domain.SeedAssignment{
				{Name: "Alice", SeedRank: 1},
				{Name: "Bob", SeedRank: 2},
				{Name: "Charlie", SeedRank: 3},
			},
			wantErr: false,
			validate: func(t *testing.T, players []Player) {
				seedMap := make(map[string]int)
				for _, p := range players {
					seedMap[p.Name] = p.Seed
				}
				assert.Equal(t, 1, seedMap["Alice"])
				assert.Equal(t, 2, seedMap["Bob"])
				assert.Equal(t, 3, seedMap["Charlie"])
				assert.Equal(t, 0, seedMap["David"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of players to avoid modifying the test data
			playersCopy := make([]Player, len(tt.players))
			copy(playersCopy, tt.players)

			err := ApplySeeds(playersCopy, tt.assignments)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, playersCopy)
				}
			}
		})
	}
}

func TestStandardSeeding_Integration(t *testing.T) {
	// Test a realistic tournament scenario
	players := []Player{
		{Name: "Champion", Seed: 1, Dojo: "Dojo A"},
		{Name: "Runner-up", Seed: 2, Dojo: "Dojo B"},
		{Name: "Third Place", Seed: 3, Dojo: "Dojo C"},
		{Name: "Fourth Place", Seed: 4, Dojo: "Dojo D"},
		{Name: "Player 5", Seed: 0, Dojo: "Dojo E"},
		{Name: "Player 6", Seed: 0, Dojo: "Dojo F"},
		{Name: "Player 7", Seed: 0, Dojo: "Dojo G"},
		{Name: "Player 8", Seed: 0, Dojo: "Dojo H"},
	}

	result := StandardSeeding(players)

	// Verify all players are present
	assert.Len(t, result, 8)

	// Verify seed 1 is first
	assert.Equal(t, 1, result[0].Seed, "Seed 1 should be at position 0")

	// Verify all 4 seeds are present in the result
	seedsFound := make(map[int]bool)
	for _, p := range result {
		if p.Seed > 0 {
			seedsFound[p.Seed] = true
		}
	}
	assert.True(t, seedsFound[1], "Seed 1 should be present")
	assert.True(t, seedsFound[2], "Seed 2 should be present")
	assert.True(t, seedsFound[3], "Seed 3 should be present")
	assert.True(t, seedsFound[4], "Seed 4 should be present")

	// Verify unseeded players fill remaining slots
	unseededCount := 0
	for _, p := range result {
		if p.Seed == 0 {
			unseededCount++
		}
	}
	assert.Equal(t, 4, unseededCount, "Should have 4 unseeded players")
}

func TestStandardSeeding_NoDuplicates(t *testing.T) {
	tests := []struct {
		name        string
		playerCount int
		seedCount   int
	}{
		{
			name:        "24 players with 3 seeds (reproduces user bug)",
			playerCount: 24,
			seedCount:   3,
		},
		{
			name:        "24 players with 8 seeds",
			playerCount: 24,
			seedCount:   8,
		},
		{
			name:        "20 players with 4 seeds",
			playerCount: 20,
			seedCount:   4,
		},
		{
			name:        "12 players with 2 seeds",
			playerCount: 12,
			seedCount:   2,
		},
		{
			name:        "15 players with 5 seeds",
			playerCount: 15,
			seedCount:   5,
		},
		{
			name:        "7 players with 3 seeds",
			playerCount: 7,
			seedCount:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create players with seeds
			players := make([]Player, tt.playerCount)
			for i := 0; i < tt.playerCount; i++ {
				name := ""
				seed := 0
				if i < tt.seedCount {
					name = "Seed" + string(rune('A'+i))
					seed = i + 1
				} else {
					name = "Player" + string(rune('A'+i))
				}
				players[i] = Player{
					Name: name,
					Seed: seed,
					Dojo: "Dojo" + string(rune('A'+i)),
				}
			}

			result := StandardSeeding(players)

			// CRITICAL: Verify no duplicates - each player should appear exactly once
			nameCount := make(map[string]int)
			for _, p := range result {
				nameCount[p.Name]++
			}

			for name, count := range nameCount {
				assert.Equal(t, 1, count, "Player %s appears %d times (expected 1)", name, count)
			}

			// Verify all original players are present
			assert.Len(t, nameCount, tt.playerCount, "Should have exactly %d unique players", tt.playerCount)

			// Verify all seeded players are present
			seededFound := make(map[int]bool)
			for _, p := range result {
				if p.Seed > 0 {
					seededFound[p.Seed] = true
				}
			}
			assert.Len(t, seededFound, tt.seedCount, "Should have exactly %d seeded players", tt.seedCount)

			// Verify correct number of unseeded players
			unseededCount := 0
			for _, p := range result {
				if p.Seed == 0 {
					unseededCount++
				}
			}
			assert.Equal(t, tt.playerCount-tt.seedCount, unseededCount, "Should have %d unseeded players", tt.playerCount-tt.seedCount)
		})
	}
}

func TestStandardSeeding_24PlayersWithSeeds_NoMissingPlayers(t *testing.T) {
	// This test specifically reproduces the issue from the user's CSV:
	// 24 players with 3 seeds should not result in any player being duplicated or missing

	// Create 24 distinct players
	players := []Player{
		{Name: "Cersei Lannister", Seed: 1, DisplayName: "LANNISTER", Dojo: "Team Gamma"},
		{Name: "Daenerys Targaryen", Seed: 2, DisplayName: "TARGARYEN", Dojo: "Team Delta"},
		{Name: "Eddard Stark", Seed: 3, DisplayName: "STARK", Dojo: "Team Epsilon"},
		{Name: "Frodo Baggins", Seed: 0, DisplayName: "BAGGINS", Dojo: "Team Zeta"},
		{Name: "Gandalf The Grey", Seed: 0, DisplayName: "GANDALF", Dojo: "Team Eta"},
		{Name: "Hermione Granger", Seed: 0, DisplayName: "GRANGER", Dojo: "Team Theta"},
		{Name: "Inigo Montoya", Seed: 0, DisplayName: "MONTOYA", Dojo: "Team Iota"},
		{Name: "Jon Snow", Seed: 0, DisplayName: "SNOW", Dojo: "Team Kappa"},
		{Name: "Katniss Everdeen", Seed: 0, DisplayName: "EVERDEEN", Dojo: "Team Lambda"},
		{Name: "Legolas Greenleaf", Seed: 0, DisplayName: "GREENLEAF", Dojo: "Team Mu"},
		{Name: "Moby Dick", Seed: 0, DisplayName: "DICK", Dojo: "Team Nu"},
		{Name: "Neville Longbottom", Seed: 0, DisplayName: "LONGBOTTOM", Dojo: "Team Xi"},
		{Name: "Othello", Seed: 0, DisplayName: "OTHELLO", Dojo: "Team Omicron"},
		{Name: "Petyr Baelish", Seed: 0, DisplayName: "BAELISH", Dojo: "Team Pi"},
		{Name: "Quirinus Quirrell", Seed: 0, DisplayName: "QUIRRELL", Dojo: "Team Rho"},
		{Name: "Ron Weasley", Seed: 0, DisplayName: "WEASLEY", Dojo: "Team Sigma"},
		{Name: "Samwise Gamgee", Seed: 0, DisplayName: "GAMGEE", Dojo: "Team Tau"},
		{Name: "Tyrion Lannister", Seed: 0, DisplayName: "LANNISTER", Dojo: "Team Upsilon"},
		{Name: "Ulysses", Seed: 0, DisplayName: "ULYSSES", Dojo: "Team Phi"},
		{Name: "Voldemort", Seed: 0, DisplayName: "VOLDEMORT", Dojo: "Team Chi"},
		{Name: "Willy Wonka", Seed: 0, DisplayName: "WONKA", Dojo: "Team Psi"},
		{Name: "Xaro Xhoan Daxos", Seed: 0, DisplayName: "DAXOS", Dojo: "Team Omega"},
		{Name: "Ygritte", Seed: 0, DisplayName: "YGRITTE", Dojo: "Team Alpha"},
		{Name: "Zeus", Seed: 0, DisplayName: "ZEUS", Dojo: "Team Beta"},
	}

	result := StandardSeeding(players)

	// Verify exactly 24 players returned
	assert.Len(t, result, 24, "Should return exactly 24 players")

	// CRITICAL: Check for duplicates
	namesSeen := make(map[string]int)
	for _, p := range result {
		namesSeen[p.Name]++
	}

	// Each player should appear exactly once
	for name, count := range namesSeen {
		assert.Equal(t, 1, count, "Player '%s' appears %d times but should appear exactly once", name, count)
	}

	// Verify all original players are present
	assert.Len(t, namesSeen, 24, "Should have all 24 unique players")

	// Specifically check for the problematic players from the bug report
	assert.Equal(t, 1, namesSeen["Cersei Lannister"], "Cersei Lannister should appear exactly once")
	assert.Equal(t, 1, namesSeen["Eddard Stark"], "Eddard Stark should not be missing")
	assert.Equal(t, 1, namesSeen["Daenerys Targaryen"], "Daenerys Targaryen should appear exactly once")

	// Verify all 3 seeds are present
	seedsPresent := make(map[int]bool)
	for _, p := range result {
		if p.Seed > 0 {
			seedsPresent[p.Seed] = true
		}
	}
	assert.True(t, seedsPresent[1], "Seed 1 should be present")
	assert.True(t, seedsPresent[2], "Seed 2 should be present")
	assert.True(t, seedsPresent[3], "Seed 3 should be present")
	assert.Len(t, seedsPresent, 3, "Should have exactly 3 seeded players")
}

func TestStandardSeeding_WithPools_Integration(t *testing.T) {
	// Integration test: Apply seeds, run StandardSeeding, create pools
	// Verify no duplicates end up in pools

	// Create 24 players
	players := make([]Player, 24)
	for i := 0; i < 24; i++ {
		players[i] = Player{
			Name:        "Player" + string(rune('A'+i)),
			DisplayName: string(rune('A' + i)),
			Dojo:        "Dojo" + string(rune('A'+i)),
			Seed:        0,
		}
	}

	// Apply seeds to first 3 players
	assignments := []domain.SeedAssignment{
		{Name: "PlayerA", SeedRank: 1},
		{Name: "PlayerB", SeedRank: 2},
		{Name: "PlayerC", SeedRank: 3},
	}

	err := ApplySeeds(players, assignments)
	assert.NoError(t, err)

	// Run standard seeding
	seededPlayers := StandardSeeding(players)

	// Verify no duplicates
	nameCount := make(map[string]int)
	for _, p := range seededPlayers {
		nameCount[p.Name]++
	}

	for name, count := range nameCount {
		assert.Equal(t, 1, count, "Player %s appears %d times after StandardSeeding", name, count)
	}

	// Create pools (3 players per pool = 8 pools)
	pools := CreatePools(seededPlayers, 3)

	// Verify no duplicates across pools
	allPlayersInPools := make(map[string]int)
	for _, pool := range pools {
		for _, player := range pool.Players {
			allPlayersInPools[player.Name]++
		}
	}

	for name, count := range allPlayersInPools {
		assert.Equal(t, 1, count, "Player %s appears %d times across all pools", name, count)
	}

	// Verify all 24 players ended up in pools
	assert.Len(t, allPlayersInPools, 24, "All 24 players should be in pools")
}

func TestStandardSeeding_CornerCases(t *testing.T) {
	tests := []struct {
		name     string
		players  []Player
		validate func(t *testing.T, result []Player)
	}{
		{
			name:    "empty player list",
			players: []Player{},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 0)
			},
		},
		{
			name: "single player unseeded",
			players: []Player{
				{Name: "OnlyPlayer", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 1)
				assert.Equal(t, "OnlyPlayer", result[0].Name)
			},
		},
		{
			name: "single player seeded",
			players: []Player{
				{Name: "Champion", Seed: 1},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 1)
				assert.Equal(t, "Champion", result[0].Name)
				assert.Equal(t, 1, result[0].Seed)
			},
		},
		{
			name: "all players seeded",
			players: []Player{
				{Name: "Seed1", Seed: 1},
				{Name: "Seed2", Seed: 2},
				{Name: "Seed3", Seed: 3},
				{Name: "Seed4", Seed: 4},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 4)
				// Verify no duplicates
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				for name, count := range names {
					assert.Equal(t, 1, count, "Player %s should appear exactly once", name)
				}
				// All should be seeded
				for _, p := range result {
					assert.Greater(t, p.Seed, 0, "All players should be seeded")
				}
			},
		},
		{
			name: "seeds with gaps (non-sequential)",
			players: []Player{
				{Name: "Seed1", Seed: 1},
				{Name: "Seed5", Seed: 5},
				{Name: "Seed10", Seed: 10},
				{Name: "Player4", Seed: 0},
				{Name: "Player5", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 5)
				// Verify no duplicates
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				assert.Len(t, names, 5)
				for name, count := range names {
					assert.Equal(t, 1, count, "Player %s should appear exactly once", name)
				}
			},
		},
		{
			name: "more seeds than bracket can hold",
			players: []Player{
				{Name: "Seed1", Seed: 1},
				{Name: "Seed2", Seed: 2},
				{Name: "Seed3", Seed: 3},
				{Name: "Seed100", Seed: 100}, // Way beyond bracket positions
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 4)
				// Verify no duplicates
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				assert.Len(t, names, 4, "All 4 unique players should be present")
			},
		},
		{
			name: "seed rank equals player count",
			players: []Player{
				{Name: "Seed8", Seed: 8},
				{Name: "Player2", Seed: 0},
				{Name: "Player3", Seed: 0},
				{Name: "Player4", Seed: 0},
				{Name: "Player5", Seed: 0},
				{Name: "Player6", Seed: 0},
				{Name: "Player7", Seed: 0},
				{Name: "Player8", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 8)
				// Seed8 should be at position 7 (last) in an 8-player bracket
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				assert.Len(t, names, 8)
				assert.Equal(t, 1, names["Seed8"])
			},
		},
		{
			name: "two players both seeded",
			players: []Player{
				{Name: "Seed1", Seed: 1},
				{Name: "Seed2", Seed: 2},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 2)
				assert.Equal(t, 1, result[0].Seed)
				assert.Equal(t, 2, result[1].Seed)
			},
		},
		{
			name: "power of 2 boundary - 16 players with 4 seeds",
			players: func() []Player {
				players := make([]Player, 16)
				for i := 0; i < 4; i++ {
					players[i] = Player{Name: "Seed" + string(rune('A'+i)), Seed: i + 1}
				}
				for i := 4; i < 16; i++ {
					players[i] = Player{Name: "Player" + string(rune('A'+i))}
				}
				return players
			}(),
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 16)
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				assert.Len(t, names, 16)
				// Verify seed 1 is first
				assert.Equal(t, 1, result[0].Seed)
			},
		},
		{
			name: "non-power of 2 - 17 players",
			players: func() []Player {
				players := make([]Player, 17)
				for i := 0; i < 2; i++ {
					players[i] = Player{Name: "Seed" + string(rune('A'+i)), Seed: i + 1}
				}
				for i := 2; i < 17; i++ {
					players[i] = Player{Name: "Player" + string(rune('A'+i))}
				}
				return players
			}(),
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 17)
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				assert.Len(t, names, 17, "All 17 players should be unique")
			},
		},
		{
			name: "3 players with 2 seeds",
			players: []Player{
				{Name: "Seed1", Seed: 1},
				{Name: "Seed2", Seed: 2},
				{Name: "Player3", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 3)
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				assert.Len(t, names, 3)
			},
		},
		{
			name: "seeds in reverse order",
			players: []Player{
				{Name: "Seed4", Seed: 4},
				{Name: "Seed3", Seed: 3},
				{Name: "Seed2", Seed: 2},
				{Name: "Seed1", Seed: 1},
				{Name: "Player5", Seed: 0},
				{Name: "Player6", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 6)
				// Seed 1 should still be first after seeding
				assert.Equal(t, 1, result[0].Seed)
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				assert.Len(t, names, 6)
			},
		},
		{
			name: "many players with one seed",
			players: func() []Player {
				players := make([]Player, 32)
				players[0] = Player{Name: "Champion", Seed: 1}
				for i := 1; i < 32; i++ {
					players[i] = Player{Name: "Player" + string(rune('A'+i))}
				}
				return players
			}(),
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 32)
				assert.Equal(t, "Champion", result[0].Name)
				assert.Equal(t, 1, result[0].Seed)
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				assert.Len(t, names, 32)
			},
		},
		{
			name: "duplicate seed ranks (allowed by ApplySeeds swap logic)",
			players: []Player{
				{Name: "PlayerA", Seed: 1},
				{Name: "PlayerB", Seed: 1}, // Duplicate seed rank
				{Name: "PlayerC", Seed: 0},
				{Name: "PlayerD", Seed: 0},
			},
			validate: func(t *testing.T, result []Player) {
				assert.Len(t, result, 4)
				names := make(map[string]int)
				for _, p := range result {
					names[p.Name]++
				}
				// Even with duplicate seeds, no player should be duplicated
				assert.Len(t, names, 4, "All players should be unique")
				for name, count := range names {
					assert.Equal(t, 1, count, "Player %s should appear exactly once", name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StandardSeeding(tt.players)
			tt.validate(t, result)
		})
	}
}

func TestStandardSeeding_LargeTournaments(t *testing.T) {
	tests := []struct {
		name        string
		playerCount int
		seedCount   int
	}{
		{name: "32 players with 8 seeds", playerCount: 32, seedCount: 8},
		{name: "64 players with 16 seeds", playerCount: 64, seedCount: 16},
		{name: "128 players with 32 seeds", playerCount: 128, seedCount: 32},
		{name: "50 players with 10 seeds (non-power-of-2)", playerCount: 50, seedCount: 10},
		{name: "100 players with 20 seeds", playerCount: 100, seedCount: 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			players := make([]Player, tt.playerCount)
			for i := 0; i < tt.seedCount; i++ {
				players[i] = Player{
					Name: fmt.Sprintf("Seed%d", i+1),
					Seed: i + 1,
					Dojo: fmt.Sprintf("Dojo%d", i+1),
				}
			}
			for i := tt.seedCount; i < tt.playerCount; i++ {
				players[i] = Player{
					Name: fmt.Sprintf("Player%d", i+1),
					Dojo: fmt.Sprintf("Dojo%d", i+1),
				}
			}

			result := StandardSeeding(players)

			// Verify correct count
			assert.Len(t, result, tt.playerCount)

			// Verify no duplicates
			names := make(map[string]int)
			for _, p := range result {
				names[p.Name]++
			}

			for name, count := range names {
				assert.LessOrEqual(t, count, 1, "Player %s appears %d times", name, count)
			}

			// Verify all seeds present
			seedsFound := make(map[int]bool)
			for _, p := range result {
				if p.Seed > 0 && p.Seed <= tt.seedCount {
					seedsFound[p.Seed] = true
				}
			}

			// Seed 1 should always be first
			if tt.seedCount > 0 {
				assert.Equal(t, 1, result[0].Seed, "Seed 1 should be at position 0")
			}
		})
	}
}

func TestApplySeeds_CornerCases(t *testing.T) {
	tests := []struct {
		name        string
		players     []Player
		assignments []domain.SeedAssignment
		wantErr     bool
		errContains string
		validate    func(t *testing.T, players []Player)
	}{
		{
			name:        "empty players with assignments",
			players:     []Player{},
			assignments: []domain.SeedAssignment{{Name: "Ghost", SeedRank: 1}},
			wantErr:     true,
			errContains: "seeded participant not found",
		},
		{
			name:        "empty assignments with players",
			players:     []Player{{Name: "Alice", Seed: 0}},
			assignments: []domain.SeedAssignment{},
			wantErr:     false,
			validate: func(t *testing.T, players []Player) {
				assert.Equal(t, 0, players[0].Seed)
			},
		},
		{
			name:        "single player single assignment",
			players:     []Player{{Name: "Champion", Seed: 0}},
			assignments: []domain.SeedAssignment{{Name: "Champion", SeedRank: 1}},
			wantErr:     false,
			validate: func(t *testing.T, players []Player) {
				assert.Equal(t, 1, players[0].Seed)
			},
		},
		{
			name: "case sensitive name matching",
			players: []Player{
				{Name: "Alice", Seed: 0},
				{Name: "alice", Seed: 0},
			},
			assignments: []domain.SeedAssignment{{Name: "Alice", SeedRank: 1}},
			wantErr:     false,
			validate: func(t *testing.T, players []Player) {
				// Only "Alice" should get seed 1, not "alice"
				aliceSeeded := false
				aliceLowerUnseeded := true
				for _, p := range players {
					if p.Name == "Alice" && p.Seed == 1 {
						aliceSeeded = true
					}
					if p.Name == "alice" && p.Seed != 0 {
						aliceLowerUnseeded = false
					}
				}
				assert.True(t, aliceSeeded, "Alice should be seeded")
				assert.True(t, aliceLowerUnseeded, "alice (lowercase) should remain unseeded")
			},
		},
		{
			name: "assign all players as seeds",
			players: []Player{
				{Name: "P1", Seed: 0},
				{Name: "P2", Seed: 0},
				{Name: "P3", Seed: 0},
			},
			assignments: []domain.SeedAssignment{
				{Name: "P1", SeedRank: 1},
				{Name: "P2", SeedRank: 2},
				{Name: "P3", SeedRank: 3},
			},
			wantErr: false,
			validate: func(t *testing.T, players []Player) {
				for _, p := range players {
					assert.Greater(t, p.Seed, 0, "All players should be seeded")
				}
			},
		},
		{
			name: "multiple collisions requiring swaps",
			players: []Player{
				{Name: "A", Seed: 1},
				{Name: "B", Seed: 2},
				{Name: "C", Seed: 3},
				{Name: "D", Seed: 0},
			},
			assignments: []domain.SeedAssignment{
				{Name: "D", SeedRank: 1}, // Collision with A
			},
			wantErr: false,
			validate: func(t *testing.T, players []Player) {
				var dPlayer, aPlayer *Player
				for i := range players {
					if players[i].Name == "D" {
						dPlayer = &players[i]
					}
					if players[i].Name == "A" {
						aPlayer = &players[i]
					}
				}
				assert.Equal(t, 1, dPlayer.Seed, "D should have seed 1")
				assert.Equal(t, 0, aPlayer.Seed, "A's seed should be swapped to 0")
			},
		},
		{
			name: "assign seed to already seeded player",
			players: []Player{
				{Name: "Champion", Seed: 5},
				{Name: "Player2", Seed: 0},
			},
			assignments: []domain.SeedAssignment{
				{Name: "Champion", SeedRank: 1},
			},
			wantErr: false,
			validate: func(t *testing.T, players []Player) {
				for _, p := range players {
					if p.Name == "Champion" {
						assert.Equal(t, 1, p.Seed, "Champion's seed should be updated to 1")
					}
				}
			},
		},
		{
			name: "partial name match should not work",
			players: []Player{
				{Name: "Alice Smith", Seed: 0},
			},
			assignments: []domain.SeedAssignment{
				{Name: "Alice", SeedRank: 1},
			},
			wantErr:     true,
			errContains: "seeded participant not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			playersCopy := make([]Player, len(tt.players))
			copy(playersCopy, tt.players)

			err := ApplySeeds(playersCopy, tt.assignments)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, playersCopy)
				}
			}
		})
	}
}

func BenchmarkApplySeeds(b *testing.B) {
	for n := 10; n <= 10000; n *= 10 {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			players := make([]Player, n)
			assignments := make([]domain.SeedAssignment, n/2)
			for i := 0; i < n; i++ {
				players[i] = Player{
					Name: fmt.Sprintf("Player%d", i),
					Seed: i + 1, // initialize with some seeds
				}
			}
			for i := 0; i < n/2; i++ {
				assignments[i] = domain.SeedAssignment{
					Name:     fmt.Sprintf("Player%d", i),
					SeedRank: (n / 2) - i, // swap seeds around
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// We need to copy players so we don't skew the benchmark, but copying is O(N)
				pCopy := make([]Player, n)
				copy(pCopy, players)
				_ = ApplySeeds(pCopy, assignments)
			}
		})
	}
}
