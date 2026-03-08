package helper

import (
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

// Made with Bob
