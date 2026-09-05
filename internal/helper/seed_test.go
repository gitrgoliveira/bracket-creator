package helper

import (
	"fmt"
	"math/bits"
	"sort"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestStandardSeedingFull(t *testing.T) {
	// buildBracketFromDraw pairs leaf 2k with 2k+1 in round 1. A bye is an
	// empty leaf. For a correctly seeded draw, every bye must pair with a real
	// player (giving a top seed a bye); never two byes in the same match.
	tests := []struct {
		name        string
		playerCount int
		seedCount   int
		wantSlots   int
		wantByes    int
	}{
		{"24 players, 3 seeds (reproduces user bug)", 24, 3, 32, 8},
		{"24 players, 8 seeds", 24, 8, 32, 8},
		{"6 players, 2 seeds", 6, 2, 8, 2},
		{"16 players (exact power of two)", 16, 4, 16, 0},
		{"15 players, 5 seeds", 15, 5, 16, 1},
		{"5 players, 0 seeds", 5, 0, 8, 3},
		{"1 player", 1, 0, 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			players := make([]Player, tt.playerCount)
			for i := range players {
				if i < tt.seedCount {
					players[i] = Player{Name: fmt.Sprintf("Seed%d", i+1), Seed: i + 1}
				} else {
					players[i] = Player{Name: fmt.Sprintf("Player%d", i)}
				}
			}

			result := StandardSeedingFull(players)

			require.Len(t, result, tt.wantSlots, "result length should be the full bracket size")

			// Every real player appears exactly once; the rest are byes (empty Name).
			byes := 0
			seen := make(map[string]int)
			for _, p := range result {
				if p.Name == "" {
					byes++
					continue
				}
				seen[p.Name]++
			}
			assert.Equal(t, tt.wantByes, byes, "bye count")
			assert.Len(t, seen, tt.playerCount, "every player present exactly once")
			for name, c := range seen {
				assert.Equalf(t, 1, c, "player %s duplicated", name)
			}

			// No round-1 match has two byes (the core fix).
			for k := 0; k+1 < len(result); k += 2 {
				if result[k].Name == "" && result[k+1].Name == "" {
					t.Errorf("empty-vs-empty match at leaves %d,%d, byes not distributed", k, k+1)
				}
			}

			// The top seeds should be the ones drawing byes: every bye's round-1
			// partner is a real, top-ranked player.
			if tt.seedCount > 0 {
				for k := 0; k+1 < len(result); k += 2 {
					a, b := result[k], result[k+1]
					if a.Name == "" {
						assert.NotEmpty(t, b.Name, "bye partner should be a real player")
					}
					if b.Name == "" {
						assert.NotEmpty(t, a.Name, "bye partner should be a real player")
					}
				}
			}
		})
	}
}

// TestStandardSeedingFull_HonorsSeedNumbers verifies that a seeded player claims
// its Seed NUMBER as its bracket rank (matching StandardSeeding / the Excel draw),
// not merely its position among the seeded players. Regression for the
// dense-rank bug found in review: with non-contiguous seeds {1,2,5} the #5 seed
// must land at the rank-5 bracket slot, not the third-from-top.
func TestStandardSeedingFull_HonorsSeedNumbers(t *testing.T) {
	players := []Player{
		{Name: "Alice", Seed: 1},
		{Name: "Bob", Seed: 2},
		{Name: "Charlie", Seed: 5},
		{Name: "Dave"}, {Name: "Eve"}, {Name: "Frank"},
	}
	result := StandardSeedingFull(players) // n=6, pow2=8
	require.Len(t, result, 8)

	// order[slot] = seeding rank at that slot. A rank-k player must sit where
	// order==k. Build slot-for-rank from the same primitive the impl uses.
	order := generateBracketOrder(8)
	slotForRank := map[int]int{}
	for slot, rank := range order {
		slotForRank[rank] = slot
	}

	// Seeded players land at their Seed-number rank.
	assert.Equal(t, "Alice", result[slotForRank[1]].Name, "seed 1 → rank-1 slot")
	assert.Equal(t, "Bob", result[slotForRank[2]].Name, "seed 2 → rank-2 slot")
	assert.Equal(t, "Charlie", result[slotForRank[5]].Name, "seed 5 → rank-5 slot (not rank-3)")

	// Unseeded fill the remaining in-range ranks (3,4,6) in input order.
	assert.Equal(t, "Dave", result[slotForRank[3]].Name)
	assert.Equal(t, "Eve", result[slotForRank[4]].Name)
	assert.Equal(t, "Frank", result[slotForRank[6]].Name)

	// Every player present exactly once; ranks 7,8 are byes.
	seen := map[string]int{}
	byes := 0
	for _, p := range result {
		if p.Name == "" {
			byes++
		} else {
			seen[p.Name]++
		}
	}
	assert.Equal(t, 2, byes)
	assert.Len(t, seen, 6)
	for name, c := range seen {
		assert.Equalf(t, 1, c, "player %s duplicated", name)
	}
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
				assert.Equalf(t, 1, count, "Player %s appears %d times (expected 1)", name, count)
			}

			// Verify all original players are present
			assert.Lenf(t, nameCount, tt.playerCount, "Should have exactly %d unique players", tt.playerCount)

			// Verify all seeded players are present
			seededFound := make(map[int]bool)
			for _, p := range result {
				if p.Seed > 0 {
					seededFound[p.Seed] = true
				}
			}
			assert.Lenf(t, seededFound, tt.seedCount, "Should have exactly %d seeded players", tt.seedCount)

			// Verify correct number of unseeded players
			unseededCount := 0
			for _, p := range result {
				if p.Seed == 0 {
					unseededCount++
				}
			}
			assert.Equalf(t, tt.playerCount-tt.seedCount, unseededCount, "Should have %d unseeded players", tt.playerCount-tt.seedCount)
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
		assert.Equalf(t, 1, count, "Player '%s' appears %d times but should appear exactly once", name, count)
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

func TestPoolSeeding_WithPools_Integration(t *testing.T) {
	// Integration test: Apply seeds, run PoolSeeding, create pools
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

	// Run pool seeding
	seededPlayers := referencePoolSeeding(players, 8, 1)

	// Verify no duplicates
	nameCount := make(map[string]int)
	for _, p := range seededPlayers {
		nameCount[p.Name]++
	}

	for name, count := range nameCount {
		assert.Equalf(t, 1, count, "Player %s appears %d times after StandardSeeding", name, count)
	}

	// Create pools (3 players per pool = 8 pools)
	pools, err := CreatePools(seededPlayers, 3, false)
	assert.NoError(t, err)

	// Verify no duplicates across pools
	allPlayersInPools := make(map[string]int)
	for _, pool := range pools {
		for _, player := range pool.Players {
			allPlayersInPools[player.Name]++
		}
	}

	for name, count := range allPlayersInPools {
		assert.Equalf(t, 1, count, "Player %s appears %d times across all pools", name, count)
	}

	// Verify all 24 players ended up in pools
	assert.Len(t, allPlayersInPools, 24, "All 24 players should be in pools")
}

func TestPoolSeeding_WithBalancedPools_Integration(t *testing.T) {
	// 10 players, 4 seeds
	players := make([]Player, 10)
	for i := 0; i < 10; i++ {
		players[i] = Player{
			Name: fmt.Sprintf("Player%d", i+1),
			Dojo: fmt.Sprintf("Dojo%d", i+1),
		}
	}
	players[0].Seed = 1
	players[1].Seed = 2
	players[2].Seed = 3
	players[3].Seed = 4

	// Use PoolSeeding for pool distribution
	seededPlayers := referencePoolSeeding(players, 4, 1)

	// Create pools with max size 3 -> should create 4 pools (3, 3, 2, 2)
	pools, err := CreatePools(seededPlayers, 3, true)
	assert.NoError(t, err)

	assert.Len(t, pools, 4, "Should have 4 pools")

	poolSizes := make(map[int]int)
	for _, pool := range pools {
		poolSizes[len(pool.Players)]++
	}
	assert.Equal(t, 2, poolSizes[3], "Should have 2 pools of size 3")
	assert.Equal(t, 2, poolSizes[2], "Should have 2 pools of size 2")

	// Verify seeds are in different pools
	seedInPool := make(map[int]int)
	for i, pool := range pools {
		for _, p := range pool.Players {
			if p.Seed > 0 {
				seedInPool[p.Seed] = i
			}
		}
	}

	assert.Len(t, seedInPool, 4, "All 4 seeds should be assigned to pools")

	// Check pool uniqueness for seeds
	poolsWithSeeds := make(map[int]bool)
	for _, poolIdx := range seedInPool {
		poolsWithSeeds[poolIdx] = true
	}
	assert.Len(t, poolsWithSeeds, 4, "Each of the 4 seeds should be in a unique pool")
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
					assert.Equalf(t, 1, count, "Player %s should appear exactly once", name)
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
					assert.Equalf(t, 1, count, "Player %s should appear exactly once", name)
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
					assert.Equalf(t, 1, count, "Player %s should appear exactly once", name)
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

func TestStandardSeeding_DisplacedSeeds(t *testing.T) {
	t.Run("basic displaced seed - 3 players", func(t *testing.T) {
		players := []Player{
			{Name: "Seed1", Seed: 1},
			{Name: "Displaced100", Seed: 100},
			{Name: "Unseeded1", Seed: 0},
		}

		result := StandardSeeding(players)

		assert.Len(t, result, 3)

		// Seed 1 should be at index 0
		assert.Equal(t, "Seed1", result[0].Name)
		// Displaced100 should be at index 2 (furthest from index 0)
		assert.Equal(t, "Displaced100", result[2].Name)
		// Unseeded1 should be at index 1
		assert.Equal(t, "Unseeded1", result[1].Name)

		// Verification of completeness
		names := make(map[string]bool)
		for _, p := range result {
			assert.NotEmpty(t, p.Name, "Result should not contain players with empty names")
			names[p.Name] = true
		}
		assert.Len(t, names, 3)
	})

	t.Run("extreme seed rank - 10 players", func(t *testing.T) {
		players := make([]Player, 10)
		for i := 0; i < 10; i++ {
			players[i] = Player{Name: fmt.Sprintf("Player%d", i+1)}
		}
		players[0] = Player{Name: "Seed1", Seed: 1}
		players[1] = Player{Name: "Seed2", Seed: 2}
		players[2] = Player{Name: "Extreme5000", Seed: 5000}

		result := StandardSeeding(players)

		assert.Len(t, result, 10)

		// Seed 1 should be at index 0 (standard position)
		assert.Equal(t, "Seed1", result[0].Name)
		// Seed 2 should be at index 8 (standard position for 16-player bracket)
		assert.Equal(t, "Seed2", result[8].Name)
		// Extreme5000 should be at index 4 (furthest from 0 and 8 in [1..7, 9])
		assert.Equal(t, "Extreme5000", result[4].Name)

		// Check for blanks and duplicates
		names := make(map[string]int)
		for _, p := range result {
			assert.NotEmpty(t, p.Name, "Result should not contain players with empty names")
			names[p.Name]++
		}
		assert.Len(t, names, 10)
		for name, count := range names {
			assert.Equalf(t, 1, count, "Player %s should appear exactly once", name)
		}
	})

	t.Run("multiple displaced seeds - 8 players", func(t *testing.T) {
		players := make([]Player, 8)
		for i := 0; i < 8; i++ {
			players[i] = Player{Name: fmt.Sprintf("Player%d", i+1)}
		}
		players[0] = Player{Name: "Seed1", Seed: 1}
		players[1] = Player{Name: "Seed2", Seed: 2}
		players[2] = Player{Name: "Disp100", Seed: 100}
		players[3] = Player{Name: "Disp200", Seed: 200}

		result := StandardSeeding(players)

		// 8 players, power of 2 is 8. Order: [1, 8, 4, 5, 2, 7, 3, 6]
		// Seed 1 (rank 1) at index 0.
		// Seed 2 (rank 2) at index 4.
		// Disp100: furthest from {0, 4}. Distances: 1:1, 2:2, 3:1, 5:1, 6:2, 7:3.
		// Furthest is index 7 (dist 3).
		// Disp200: furthest from {0, 4, 7}. Distances: 1:1, 2:2, 3:1, 5:1, 6:1.
		// Max distance 2 at index 2.

		assert.Equal(t, "Seed1", result[0].Name)
		assert.Equal(t, "Seed2", result[4].Name)
		assert.Equal(t, "Disp100", result[7].Name)
		assert.Equal(t, "Disp200", result[2].Name)

		names := make(map[string]bool)
		for _, p := range result {
			names[p.Name] = true
		}
		assert.Len(t, names, 8)
	})
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

func TestGeneratePoolPriority(t *testing.T) {
	tests := []struct {
		n        int
		expected []int
	}{
		{n: 1, expected: []int{0}},
		{n: 2, expected: []int{0, 1}},
		{n: 4, expected: []int{0, 3, 1, 2}},
		{n: 8, expected: []int{0, 7, 3, 4, 1, 5, 2, 6}},
		{n: 12, expected: []int{0, 11, 5, 6, 2, 8, 3, 9, 1, 4, 7, 10}},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			result := generatePoolPriority(tt.n)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPoolSeeding(t *testing.T) {
	t.Run("12 pools 4 seeds", func(t *testing.T) {
		players := make([]Player, 36) // 3 players per pool
		for i := 0; i < 36; i++ {
			players[i] = Player{Name: fmt.Sprintf("P%d", i+1), Dojo: fmt.Sprintf("Dojo%d", i+1)}
		}
		players[0].Seed = 1
		players[1].Seed = 2
		players[2].Seed = 3
		players[3].Seed = 4

		numPools := 12
		result := referencePoolSeeding(players, numPools, 1)

		// Create pools to verify final placement
		pools, err := CreatePools(result, 3, false)
		assert.NoError(t, err)
		assert.Len(t, pools, 12)

		// Seed 1 in Pool 1 (index 0)
		assert.Equal(t, 1, pools[0].Players[0].Seed)
		// Seed 2 in Pool 12 (index 11)
		assert.Equal(t, 2, pools[11].Players[0].Seed)
		// Seed 3 in Pool 6 (index 5)
		assert.Equal(t, 3, pools[5].Players[0].Seed)
		// Seed 4 in Pool 7 (index 6)
		assert.Equal(t, 4, pools[6].Players[0].Seed)
	})

	t.Run("more seeds than pools", func(t *testing.T) {
		numPools := 4
		players := make([]Player, 12)
		for i := 0; i < 12; i++ {
			players[i] = Player{Name: fmt.Sprintf("P%d", i+1), Seed: i + 1, Dojo: fmt.Sprintf("Dojo%d", i+1)}
		}

		result := referencePoolSeeding(players, numPools, 1)
		pools, err := CreatePools(result, 3, false)
		assert.NoError(t, err)

		// Priority for 4 pools: [0, 3, 1, 2]
		// Cyclic Priority Assignment:
		// Pool Index:  0  3  1  2 |  0  3  1  2 |  0  3  1  2
		// Seed Rank:   1  2  3  4 |  5  6  7  8 |  9 10 11 12
		// Result Idx:  0  3  1  2 |  4  7  5  6 |  8 11  9 10
		// (Assuming numPools=4 and linear filling)

		assert.Equal(t, 1, pools[0].Players[0].Seed)
		assert.Equal(t, 5, pools[0].Players[1].Seed)
		assert.Equal(t, 9, pools[0].Players[2].Seed)

		assert.Equal(t, 2, pools[3].Players[0].Seed)
		assert.Equal(t, 6, pools[3].Players[1].Seed)
		assert.Equal(t, 10, pools[3].Players[2].Seed)
	})
}

// seededPoolCourts runs the operator-visible pool chain - PoolSeeding,
// CreatePools, ReorderPoolsForCourts, AssignPoolsToCourts - over a roster of
// numPools*poolSize competitors carrying the given seed RANKS, and reports the
// court index each rank ended up on. Every competitor gets their own dojo so
// CreatePools' dojo-conflict avoidance never moves anyone off the slot the
// seeding chose; only the seeding decides placement here.
func seededPoolCourts(t *testing.T, ranks []int, numPools, poolSize, numCourts int) map[int]int {
	t.Helper()

	players := make([]Player, numPools*poolSize)
	for i := range players {
		players[i] = Player{Name: fmt.Sprintf("p%03d", i), Dojo: fmt.Sprintf("dojo%03d", i)}
	}
	for i, r := range ranks {
		players[i].Seed = r
	}

	pools, err := CreatePools(referencePoolSeeding(players, numPools, numCourts), poolSize, false)
	require.NoError(t, err)
	require.Len(t, pools, numPools)
	pools = ReorderPoolsForCourts(pools, numCourts)

	assignment, err := AssignPoolsToCourts(numPools, numCourts)
	require.NoError(t, err)

	courts := map[int]int{}
	for pi, p := range pools {
		for _, pl := range p.Players {
			if pl.Seed == 0 {
				continue
			}
			_, dup := courts[pl.Seed]
			require.Falsef(t, dup, "seed %d was placed twice", pl.Seed)
			courts[pl.Seed] = assignment[pi]
		}
	}
	require.Len(t, courts, len(ranks), "every seed must survive pool creation")
	return courts
}

// TestPoolSeedingPlacesByRankNotByPosition pins the invariant seedCourtOrder's
// doc comment rests on, at the level the comment is written for.
//
// A GAPPED seed set reaches PoolSeeding in production: domain.ValidateAssignments
// only holds the operator's input to a contiguous 1..N, and after that
// validating load engine.dropSeedAssignments removes the assignments of seeded
// competitors who did not check in. The survivors keep their raw ranks, so
// {1, 2, 3, 4} minus a rank-2 no-show arrives here as {1, 3, 4}.
//
// D6 is stated in RANKS ("seed 1 -> B, seed 2 -> C, seed 3 -> A, seed 4 -> D"),
// and so is helper.SeedPlacementWarnings, which reads (a.rank-b.rank)%2 to decide
// which seeds should share a half. Placement therefore has to read the rank too:
// keyed on the position in the sorted list, rank 3 takes position 1 and lands in
// rank 2's quarter, and the warnings then blame the operator's configuration for
// a spread the check-in drop caused.
func TestPoolSeedingPlacesByRankNotByPosition(t *testing.T) {
	const numPools, poolSize, numCourts = 4, 4, 4

	// Both placements written out rather than derived from seedCourtOrder, so the
	// expectation cannot drift with the code under test. Reading them side by
	// side IS the property an operator would recognise: ranks 1, 3 and 4 hold the
	// same shiaijo in both rows, so a rank-2 no-show does not drag the surviving
	// seeds into other courts, and so into other halves of the draw.
	assert.Equal(t,
		map[int]int{1: 1, 2: 2, 3: 0, 4: 3},
		seededPoolCourts(t, []int{1, 2, 3, 4}, numPools, poolSize, numCourts),
		"D6 with every rank present: seed 1 -> B, 2 -> C, 3 -> A, 4 -> D")
	assert.Equal(t,
		map[int]int{1: 1, 3: 0, 4: 3},
		seededPoolCourts(t, []int{1, 3, 4}, numPools, poolSize, numCourts),
		"ranks 1, 3 and 4 belong on shiaijo B, A and D whether or not rank 2 is present")
}

func TestPoolSeeding_CornerCases(t *testing.T) {
	t.Run("zero pools returns input unchanged", func(t *testing.T) {
		players := []Player{{Name: "A", Seed: 1}, {Name: "B"}}
		result := referencePoolSeeding(players, 0, 1)
		assert.Equal(t, players, result)
	})

	t.Run("negative pools returns input unchanged", func(t *testing.T) {
		players := []Player{{Name: "A", Seed: 1}, {Name: "B"}}
		result := referencePoolSeeding(players, -1, 1)
		assert.Equal(t, players, result)
	})

	t.Run("no seeded players keeps unseeded order", func(t *testing.T) {
		players := []Player{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}}
		result := referencePoolSeeding(players, 2, 1)
		// Unseeded should fill linearly in order.
		assert.Equal(t, "A", result[0].Name)
		assert.Equal(t, "B", result[1].Name)
		assert.Equal(t, "C", result[2].Name)
		assert.Equal(t, "D", result[3].Name)
	})

	t.Run("single pool places all seeds", func(t *testing.T) {
		players := []Player{
			{Name: "S1", Seed: 1},
			{Name: "S2", Seed: 2},
			{Name: "U1"},
		}
		result := referencePoolSeeding(players, 1, 1)
		// All players present, seeds preserved.
		seen := map[string]bool{}
		for _, p := range result {
			seen[p.Name] = true
		}
		assert.Len(t, seen, 3)
	})

	t.Run("preserves total count", func(t *testing.T) {
		players := make([]Player, 17)
		for i := range players {
			players[i] = Player{Name: fmt.Sprintf("P%d", i+1)}
		}
		players[0].Seed = 1
		players[1].Seed = 2
		players[2].Seed = 3
		result := referencePoolSeeding(players, 6, 1)
		assert.Len(t, result, 17)

		nonEmpty := 0
		for _, p := range result {
			if p.Name != "" {
				nonEmpty++
			}
		}
		assert.Equal(t, 17, nonEmpty)
	})
}

func TestPoolSeeding_DistributesSeedsAcrossPools(t *testing.T) {
	// When seeds <= numPools, every seed should land in a distinct pool.
	tests := []struct {
		name     string
		seeds    int
		numPools int
		poolSize int
	}{
		{name: "2 seeds, 4 pools", seeds: 2, numPools: 4, poolSize: 3},
		{name: "3 seeds, 5 pools", seeds: 3, numPools: 5, poolSize: 3},
		{name: "4 seeds, 4 pools", seeds: 4, numPools: 4, poolSize: 3},
		{name: "5 seeds, 8 pools", seeds: 5, numPools: 8, poolSize: 3},
		{name: "8 seeds, 8 pools", seeds: 8, numPools: 8, poolSize: 3},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			total := tt.numPools * tt.poolSize
			players := make([]Player, total)
			for i := range players {
				players[i] = Player{Name: fmt.Sprintf("P%d", i+1), Dojo: fmt.Sprintf("D%d", i+1)}
			}
			for i := 0; i < tt.seeds; i++ {
				players[i].Seed = i + 1
			}

			result := referencePoolSeeding(players, tt.numPools, 1)
			pools, err := CreatePools(result, tt.poolSize, false)
			assert.NoError(t, err)
			assert.Len(t, pools, tt.numPools)

			seedPools := map[int]bool{}
			for i, pool := range pools {
				for _, p := range pool.Players {
					if p.Seed > 0 {
						assert.Falsef(t, seedPools[i], "pool %d already has a seed", i)
						seedPools[i] = true
					}
				}
			}
			assert.Len(t, seedPools, tt.seeds, "each seed should land in a distinct pool")
		})
	}
}

func TestGeneratePoolPriority_Properties(t *testing.T) {
	t.Run("zero returns empty", func(t *testing.T) {
		assert.Empty(t, generatePoolPriority(0))
	})

	t.Run("negative returns empty", func(t *testing.T) {
		assert.Empty(t, generatePoolPriority(-3))
	})

	for n := 1; n <= 64; n++ {
		n := n
		t.Run(fmt.Sprintf("permutation_n=%d", n), func(t *testing.T) {
			p := generatePoolPriority(n)
			assert.Len(t, p, n, "priority length must equal n")

			seen := make(map[int]bool, n)
			for _, v := range p {
				assert.GreaterOrEqual(t, v, 0)
				assert.Less(t, v, n)
				assert.Falsef(t, seen[v], "duplicate value %d", v)
				seen[v] = true
			}
			assert.Len(t, seen, n)

			if n >= 2 {
				assert.Equal(t, 0, p[0], "first priority must be 0 (low extreme)")
				assert.Equal(t, n-1, p[1], "second priority must be n-1 (high extreme)")
			}
		})
	}
}

func TestStandardSeeding_DisplacedSeeds_NoMissingPlayers(t *testing.T) {
	// Stress test: many displaced seeds should never lose players.
	players := make([]Player, 16)
	for i := range players {
		players[i] = Player{Name: fmt.Sprintf("P%d", i+1)}
	}
	// Mix of valid and displaced seed ranks.
	players[0].Seed = 1
	players[1].Seed = 16
	players[2].Seed = 99   // displaced
	players[3].Seed = 200  // displaced
	players[4].Seed = 1000 // displaced

	result := StandardSeeding(players)
	assert.Len(t, result, 16)

	names := map[string]int{}
	for _, p := range result {
		assert.NotEmpty(t, p.Name)
		names[p.Name]++
	}
	assert.Len(t, names, 16)
	for n, c := range names {
		assert.Equalf(t, 1, c, "player %s appears %d times", n, c)
	}
}

func TestPoolSeeding_DojoConflict(t *testing.T) {
	// Regression test: LC2026 6+ mixed category had 5 players from Tora Dojo
	// London and 1 seeded player. The seeded player shifted unseeded filling
	// by one slot, causing the 5th Tora player to find no conflict-free pool
	// and reach the placement fallback (which, before the bc-dojo fix, took
	// the first pool with room and would have placed two Tora players in the
	// same pool).
	players := []Player{
		{Name: "Walter McCahon", Dojo: "Tora Dojo London"},
		{Name: "Ricardo Oliveira", Dojo: "Tora Dojo London"},
		{Name: "Chris Bowden", Dojo: "Ichi Byoshi"},
		{Name: "Ruairidh Pooler", Dojo: "Scotland"},
		{Name: "Royth von Hahn", Dojo: "Kendo Dojo Koln"},
		{Name: "Mykolas Maciulevicius", Dojo: "Iron Wolf Kendo Dojo"},
		{Name: "Jonathan Fitzgerald", Dojo: "Peristeri Kenyukai"},
		{Name: "Dominik Christ", Dojo: "Tora Dojo London"},
		{Name: "Denis Arsenin", Dojo: "Latvian Kendo Federation"},
		{Name: "Masatoshi Kurokawa", Dojo: "PSV MG"}, // "6" in column 4 is a dan grade, not a seed
		{Name: "Barry Straughan", Dojo: "Kadode"},
		{Name: "Andrew Lam", Dojo: "Tora Dojo London"},
		{Name: "Alex Ansell", Dojo: "Shin Sei dojo"},
		{Name: "Zenjiro Hamada", Dojo: "Oxford"},
		{Name: "KAORU FUJITA", Dojo: "Tora Dojo London"},
	}

	result := referencePoolSeeding(players, 5, 2)
	require.Len(t, result, 15)

	pools, err := CreatePools(result, 3, false)
	require.NoError(t, err)
	require.Len(t, pools, 5)

	for _, pool := range pools {
		dojoCount := make(map[string]int)
		for _, p := range pool.Players {
			dojoCount[p.Dojo]++
		}
		for dojo, count := range dojoCount {
			assert.Equal(t, 1, count,
				"dojo %q has %d players in %s, expected at most 1", dojo, count, pool.PoolName)
		}
	}
}

func TestPoolSeeding_LargeSameDojo(t *testing.T) {
	// Regression test: same-dojo players spread throughout the CSV (not grouped
	// at the front) reach the placement fallback, which before the bc-dojo fix
	// took the first pool with room and would have landed two in the same pool.
	// Each case uses dojoSize == numPools (worst case: every pool must absorb
	// exactly one same-dojo player). Players are placed at evenly-spaced
	// positions pos[i] = i*(total-1)/(D-1), which reproduces the adversarial
	// ordering from the LC2026 bug. Grouping same-dojo at the front does NOT
	// trigger the bug; this spread does.
	tests := []struct {
		name      string
		dojoSize  int
		numPools  int
		poolSize  int
		numCourts int
	}{
		{"4 same-dojo, 4 pools", 4, 4, 3, 2},
		{"5 same-dojo, 5 pools", 5, 5, 3, 2},
		{"6 same-dojo, 6 pools", 6, 6, 3, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := tt.numPools * tt.poolSize
			players := make([]Player, total)

			// Spread same-dojo players evenly: pos[i] = i*(total-1)/(dojoSize-1).
			dojoPositions := make(map[int]bool, tt.dojoSize)
			for i := range tt.dojoSize {
				dojoPositions[i*(total-1)/(tt.dojoSize-1)] = true
			}

			dojoIdx, otherIdx := 0, 0
			for i := range total {
				if dojoPositions[i] {
					players[i] = Player{Name: fmt.Sprintf("BigDojo%d", dojoIdx+1), Dojo: "BigDojo"}
					dojoIdx++
				} else {
					otherIdx++
					players[i] = Player{Name: fmt.Sprintf("Other%d", otherIdx), Dojo: fmt.Sprintf("Dojo%d", otherIdx)}
				}
			}

			result := referencePoolSeeding(players, tt.numPools, tt.numCourts)
			pools, err := CreatePools(result, tt.poolSize, false)
			require.NoError(t, err)
			require.Len(t, pools, tt.numPools)

			for _, pool := range pools {
				dojoCount := make(map[string]int)
				for _, p := range pool.Players {
					dojoCount[p.Dojo]++
				}
				assert.LessOrEqual(t, dojoCount["BigDojo"], 1,
					"BigDojo has %d players in %s", dojoCount["BigDojo"], pool.PoolName)
			}
		})
	}
}

func TestPoolSeeding_DojoEdgeCases(t *testing.T) {
	// Helper: build a pool of N players from one dojo plus filler from unique dojos.
	makePlayers := func(dojoGroups map[string]int, fillerCount int) []Player {
		players := []Player{}
		for dojo, n := range dojoGroups {
			for i := range n {
				players = append(players, Player{Name: fmt.Sprintf("%s_%d", dojo, i+1), Dojo: dojo})
			}
		}
		for i := range fillerCount {
			players = append(players, Player{Name: fmt.Sprintf("Solo%d", i+1), Dojo: fmt.Sprintf("SoloDojo%d", i+1)})
		}
		return players
	}

	// Interleave dojoGroups players with filler at evenly-spaced positions per group.
	makePlayersInterleaved := func(dojoGroups []struct {
		Name string
		Size int
	}, fillerCount int) []Player {
		total := fillerCount
		for _, g := range dojoGroups {
			total += g.Size
		}
		players := make([]Player, total)
		used := make(map[int]bool)
		for _, g := range dojoGroups {
			if g.Size == 1 {
				// place at midpoint
				pos := total / 2
				for used[pos] {
					pos = (pos + 1) % total
				}
				used[pos] = true
				players[pos] = Player{Name: fmt.Sprintf("%s_1", g.Name), Dojo: g.Name}
				continue
			}
			for i := range g.Size {
				pos := i * (total - 1) / (g.Size - 1)
				for used[pos] {
					pos = (pos + 1) % total
				}
				used[pos] = true
				players[pos] = Player{Name: fmt.Sprintf("%s_%d", g.Name, i+1), Dojo: g.Name}
			}
		}
		soloIdx := 0
		for i := range total {
			if !used[i] {
				soloIdx++
				players[i] = Player{Name: fmt.Sprintf("Solo%d", soloIdx), Dojo: fmt.Sprintf("SoloDojo%d", soloIdx)}
			}
		}
		return players
	}

	t.Run("two competing large dojos 5+5 in 5 pools", func(t *testing.T) {
		players := makePlayersInterleaved([]struct {
			Name string
			Size int
		}{{"Alpha", 5}, {"Beta", 5}}, 5)
		result := referencePoolSeeding(players, 5, 2)
		pools, err := CreatePools(result, 3, false)
		require.NoError(t, err)
		for _, pool := range pools {
			counts := map[string]int{}
			for _, p := range pool.Players {
				counts[p.Dojo]++
			}
			assert.LessOrEqual(t, counts["Alpha"], 1, "Alpha doubled in %s", pool.PoolName)
			assert.LessOrEqual(t, counts["Beta"], 1, "Beta doubled in %s", pool.PoolName)
		}
	})

	t.Run("dojo larger than pool count - doubling unavoidable but bounded", func(t *testing.T) {
		// 6 from one dojo into 5 pools: at least one pool MUST have 2; expect <= 2.
		players := makePlayersInterleaved([]struct {
			Name string
			Size int
		}{{"Mega", 6}}, 9)
		result := referencePoolSeeding(players, 5, 2)
		pools, err := CreatePools(result, 3, false)
		require.NoError(t, err)
		doublings := 0
		for _, pool := range pools {
			c := 0
			for _, p := range pool.Players {
				if p.Dojo == "Mega" {
					c++
				}
			}
			assert.LessOrEqual(t, c, 2, "Mega tripled in %s", pool.PoolName)
			if c == 2 {
				doublings++
			}
		}
		assert.LessOrEqual(t, doublings, 1, "expected at most 1 pool with 2 Mega players, got %d", doublings)
	})

	t.Run("three medium dojos 3+3+3 in 3 pools", func(t *testing.T) {
		players := makePlayersInterleaved([]struct {
			Name string
			Size int
		}{{"Red", 3}, {"Blue", 3}, {"Green", 3}}, 0)
		result := referencePoolSeeding(players, 3, 2)
		pools, err := CreatePools(result, 3, false)
		require.NoError(t, err)
		for _, pool := range pools {
			counts := map[string]int{}
			for _, p := range pool.Players {
				counts[p.Dojo]++
			}
			for d, c := range counts {
				assert.Equalf(t, 1, c, "dojo %s has %d in %s", d, c, pool.PoolName)
			}
		}
	})

	t.Run("equal-size dojos resolve via stable tiebreak", func(t *testing.T) {
		players := makePlayersInterleaved([]struct {
			Name string
			Size int
		}{{"AAA", 4}, {"BBB", 4}}, 4)
		result := referencePoolSeeding(players, 4, 2)
		pools, err := CreatePools(result, 3, false)
		require.NoError(t, err)
		for _, pool := range pools {
			counts := map[string]int{}
			for _, p := range pool.Players {
				counts[p.Dojo]++
			}
			assert.LessOrEqual(t, counts["AAA"], 1)
			assert.LessOrEqual(t, counts["BBB"], 1)
		}
	})

	t.Run("seeded player from a large dojo", func(t *testing.T) {
		players := makePlayersInterleaved([]struct {
			Name string
			Size int
		}{{"Big", 5}}, 10)
		// Mark one Big player as seeded.
		for i := range players {
			if players[i].Dojo == "Big" {
				players[i].Seed = 1
				break
			}
		}
		result := referencePoolSeeding(players, 5, 2)
		pools, err := CreatePools(result, 3, false)
		require.NoError(t, err)
		for _, pool := range pools {
			c := 0
			for _, p := range pool.Players {
				if p.Dojo == "Big" {
					c++
				}
			}
			assert.LessOrEqual(t, c, 1, "Big doubled in %s", pool.PoolName)
		}
	})

	t.Run("pool size 4 with large dojo", func(t *testing.T) {
		players := makePlayersInterleaved([]struct {
			Name string
			Size int
		}{{"Big", 5}}, 15)
		result := referencePoolSeeding(players, 5, 2)
		pools, err := CreatePools(result, 4, false)
		require.NoError(t, err)
		for _, pool := range pools {
			c := 0
			for _, p := range pool.Players {
				if p.Dojo == "Big" {
					c++
				}
			}
			assert.LessOrEqual(t, c, 1, "Big doubled in %s", pool.PoolName)
		}
	})

	t.Run("isMax with imbalanced sizes", func(t *testing.T) {
		// 13 players, poolSize=3, isMax=true → 5 pools (3,3,3,2,2).
		players := makePlayersInterleaved([]struct {
			Name string
			Size int
		}{{"Big", 5}}, 8)
		result := referencePoolSeeding(players, 5, 2)
		pools, err := CreatePools(result, 3, true)
		require.NoError(t, err)
		for _, pool := range pools {
			c := 0
			for _, p := range pool.Players {
				if p.Dojo == "Big" {
					c++
				}
			}
			assert.LessOrEqual(t, c, 1, "Big doubled in %s", pool.PoolName)
		}
	})

	t.Run("single pool degenerate", func(t *testing.T) {
		players := makePlayers(map[string]int{"X": 2}, 1)
		result := referencePoolSeeding(players, 1, 1)
		pools, err := CreatePools(result, 3, false)
		require.NoError(t, err)
		require.Len(t, pools, 1)
		assert.Len(t, pools[0].Players, 3)
	})

	t.Run("all players from one dojo", func(t *testing.T) {
		// Degenerate: doubling unavoidable; just ensure no crash and all placed.
		players := []Player{}
		for i := range 9 {
			players = append(players, Player{Name: fmt.Sprintf("OnlyOne_%d", i+1), Dojo: "Only"})
		}
		result := referencePoolSeeding(players, 3, 2)
		pools, err := CreatePools(result, 3, false)
		require.NoError(t, err)
		total := 0
		for _, p := range pools {
			total += len(p.Players)
		}
		assert.Equal(t, 9, total)
	})

	t.Run("two large dojos plus one small dojo", func(t *testing.T) {
		players := makePlayersInterleaved([]struct {
			Name string
			Size int
		}{{"Alpha", 4}, {"Beta", 4}, {"Gamma", 2}}, 5)
		result := referencePoolSeeding(players, 5, 2)
		pools, err := CreatePools(result, 3, false)
		require.NoError(t, err)
		for _, pool := range pools {
			counts := map[string]int{}
			for _, p := range pool.Players {
				counts[p.Dojo]++
			}
			assert.LessOrEqual(t, counts["Alpha"], 1)
			assert.LessOrEqual(t, counts["Beta"], 1)
			assert.LessOrEqual(t, counts["Gamma"], 1)
		}
	})

	t.Run("4 courts distribute correctly with large dojo", func(t *testing.T) {
		players := makePlayersInterleaved([]struct {
			Name string
			Size int
		}{{"Big", 4}}, 12)
		result := referencePoolSeeding(players, 4, 4)
		pools, err := CreatePools(result, 4, false)
		require.NoError(t, err)
		for _, pool := range pools {
			c := 0
			for _, p := range pool.Players {
				if p.Dojo == "Big" {
					c++
				}
			}
			assert.LessOrEqual(t, c, 1, "Big doubled in %s", pool.PoolName)
		}
	})
}

// buildOversubscribedDojoRoster builds a deterministic roster of `total`
// players where `dojoSize` of them share dojoName, spread evenly through the
// roster at pos[i] = i*(total-1)/(dojoSize-1) (the same adversarial spacing
// used by TestPoolSeeding_LargeSameDojo above, which reproduces the LC2026
// ordering for fixture realism). Unlike that test's dojoSize == numPools
// case, HERE dojoSize > numPools, so the leastConflictedPool fallback is
// unavoidable regardless of input order: after numPools conflict-free
// placements every pool already holds one member of the oversubscribed dojo,
// and PoolSeeding re-clusters by dojo before CreatePools ever sees the
// roster's order anyway (grouped-at-front and this spread measure to the
// identical per-pool counts). The spacing is kept for fixture realism, not
// because it changes whether the fallback fires. The remaining players each
// get a unique dojo.
//
// This is a thin naming wrapper over newOversubscribedDojoRoster, the
// algorithm shared with drawGoldenDojoRoster (draw_shapes_golden_test.go).
// Only the generated NAME strings differ between the two callers (this
// one's assertions read player.Dojo only, so its names are disposable; the
// golden's names are frozen into testdata/draw_shapes.json byte-for-byte) --
// the placement math itself must never drift between two copies.
func buildOversubscribedDojoRoster(total, dojoSize int, dojoName string) []Player {
	return newOversubscribedDojoRoster(total, dojoSize, dojoName,
		func(i int) string { return fmt.Sprintf("%s Player %d", dojoName, i) },
		func(i int) (name, dojo string) { return fmt.Sprintf("Other%d", i), fmt.Sprintf("Dojo%d", i) },
	)
}

// newOversubscribedDojoRoster is the shared placement algorithm behind
// buildOversubscribedDojoRoster and drawGoldenDojoRoster. memberName formats
// the i-th (1-based) oversubscribed-dojo player's name; filler formats the
// i-th (1-based) filler player's name and unique dojo.
//
// Requires dojoSize >= 2: the position formula divides by dojoSize-1, so a
// smaller value would otherwise divide by zero -- reject it here with a
// clear message rather than let a future caller hit that panic silently.
func newOversubscribedDojoRoster(total, dojoSize int, dojoName string, memberName func(i int) string, filler func(i int) (name, dojo string)) []Player {
	if dojoSize < 2 {
		panic(fmt.Sprintf("newOversubscribedDojoRoster: dojoSize must be >= 2 to oversubscribe %q, got %d", dojoName, dojoSize))
	}

	players := make([]Player, total)

	dojoPositions := make(map[int]bool, dojoSize)
	for i := 0; i < dojoSize; i++ {
		dojoPositions[i*(total-1)/(dojoSize-1)] = true
	}

	dojoIdx, otherIdx := 0, 0
	for i := 0; i < total; i++ {
		if dojoPositions[i] {
			dojoIdx++
			players[i] = Player{Name: memberName(dojoIdx), Dojo: dojoName}
		} else {
			otherIdx++
			name, dojo := filler(otherIdx)
			players[i] = Player{Name: name, Dojo: dojo}
		}
	}
	return players
}

// isSingleDojoPool reports whether pool p has MORE THAN ONE player and they
// all share one dojo. Pools of 0 or 1 player are excluded: a same-dojo
// conflict needs at least two players to exist, so a singleton pool
// trivially satisfying "every player shares a dojo" is not what either
// caller (below, and computeDojoOversubscriptionStats in
// draw_shapes_golden_test.go) means to flag.
func isSingleDojoPool(p Pool) bool {
	if len(p.Players) <= 1 {
		return false
	}
	dojo := p.Players[0].Dojo
	for _, pl := range p.Players[1:] {
		if pl.Dojo != dojo {
			return false
		}
	}
	return true
}

func TestPoolSeeding_DojoSpreadFallback(t *testing.T) {
	// Regression test for bc-dojo: 24 entrants, 10 from one dojo (rest unique
	// dojos), through BuildPoolPhase(players, 4, false, 2) -- poolSize 4,
	// min-mode, 2 courts, which is what actually derives 6 pools of 4 and
	// runs PoolSeeding -> CreatePools -> ReorderPoolsForCourts in the order
	// production uses (BuildPoolPhase's own doc comment: hand-assembling
	// this sequence is exactly the drift it exists to prevent). Before the
	// fix, the fallback (first-pool-with-room) placed overflow into the
	// FIRST pool with room, piling four of the ten Tora players into a
	// single pool -- measured as an entirely single-dojo pool of 4.
	players := buildOversubscribedDojoRoster(24, 10, "Tora Dojo")

	pools, drawCourts, err := BuildPoolPhase(players, 4, false, 2)
	require.NoError(t, err)
	require.Len(t, pools, 6)
	require.Equal(t, 2, drawCourts)

	// Pool SIZES must be unaffected by the fallback fix: this is checked
	// separately from composition so a future change that moves sizes is
	// distinguishable from one that moves membership.
	for i, pool := range pools {
		assert.Len(t, pool.Players, 4, "pool %d (%s) size changed", i, pool.PoolName)
	}

	toraCounts := make([]int, len(pools))
	for i, pool := range pools {
		count := 0
		for _, p := range pool.Players {
			if p.Dojo == "Tora Dojo" {
				count++
			}
		}
		toraCounts[i] = count
		assert.False(t, isSingleDojoPool(pool), "%s is entirely single-dojo", pool.PoolName)
	}

	// Assert the MULTISET of per-pool Tora counts, never the ordered
	// per-pool sequence: the sequence is 2,2,1,2,2,1 for this scenario and is
	// an artifact of the tie-break, not the contract.
	sorted := append([]int(nil), toraCounts...)
	sort.Ints(sorted)
	assert.Equal(t, []int{1, 1, 2, 2, 2, 2}, sorted,
		"Tora Dojo per-pool counts (sorted) should be the multiset {2,2,2,2,1,1}, got %v", toraCounts)

	maxCount := 0
	for _, c := range toraCounts {
		if c > maxCount {
			maxCount = c
		}
	}
	assert.LessOrEqual(t, maxCount, 2, "Tora Dojo should never exceed 2 players in any pool, got per-pool counts %v", toraCounts)
}

func TestPoolSeeding_RealRosterDojoSpread(t *testing.T) {
	// Regression test for bc-dojo, using the real committed roster
	// test-data/individual_men_up_to_2nd_2026.csv (50 players, 15 "Team Rho"),
	// run through BuildPoolPhase(players, 5, false, 2) -- the one function
	// documented to get the PoolSeeding -> CreatePools -> ReorderPoolsForCourts
	// order and the derived pool/court counts right, exactly as the real
	// draw does (BuildPoolPhase's own doc comment names hand-assembling this
	// sequence as the exact drift it exists to prevent).
	//
	// loadDistributionRoster (pool_distribution_invariants_test.go), not a
	// second, near-identical CSV loader (bc-drwx item 13: this file used to
	// carry its own loadCSVPlayers, which only ever differed from
	// loadDistributionRoster in REQUIRING 3+ columns instead of also
	// accepting 2 -- individual_men_up_to_2nd_2026.csv is 4-column, so every
	// row already has len(rec)>=3 and both loaders read the identical
	// rec[2] dojo column for it; the two were never actually testing
	// different parsing behaviour for the file either one was ever called
	// with).
	players := loadDistributionRoster(t, "../../test-data/individual_men_up_to_2nd_2026.csv")
	require.Len(t, players, 50)

	rhoCount := 0
	for _, p := range players {
		if p.Dojo == "Team Rho" {
			rhoCount++
		}
	}
	require.Equal(t, 15, rhoCount, "fixture drifted: expected 15 Team Rho players")

	pools, drawCourts, err := BuildPoolPhase(players, 5, false, 2)
	require.NoError(t, err)
	require.Len(t, pools, 10)
	require.Equal(t, 2, drawCourts)

	// Pool SIZES must be unaffected by the fallback fix: this is checked
	// separately from composition. 50 players / 10 pools = 5 each.
	for i, pool := range pools {
		assert.Len(t, pool.Players, 5, "pool %d (%s) size changed", i, pool.PoolName)
	}

	// Acceptance is "no pool holds more than 2 of ANY one dojo", not only
	// the oversubscribed one: with the fix the worst count anywhere on this
	// roster is 2, so assert every dojo, which also guards the smaller
	// multi-member dojos in the fixture.
	for _, pool := range pools {
		dojoCount := make(map[string]int)
		for _, p := range pool.Players {
			dojoCount[p.Dojo]++
		}
		for dojo, count := range dojoCount {
			assert.LessOrEqual(t, count, 2,
				"%s has %d players in %s, expected at most 2", dojo, count, pool.PoolName)
		}
	}
}

func TestApplySeeds_DuplicateSeedRanks(t *testing.T) {
	t.Run("rejects two assignments with the same rank", func(t *testing.T) {
		players := []Player{
			{Name: "Alice"},
			{Name: "Bob"},
		}
		assignments := []domain.SeedAssignment{
			{Name: "Alice", SeedRank: 1},
			{Name: "Bob", SeedRank: 1},
		}
		err := ApplySeeds(players, assignments)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate seed rank 1")
	})

	t.Run("allows multiple unseeded entries (rank 0)", func(t *testing.T) {
		players := []Player{
			{Name: "Alice"},
			{Name: "Bob"},
			{Name: "Carol"},
		}
		assignments := []domain.SeedAssignment{
			{Name: "Alice", SeedRank: 0},
			{Name: "Bob", SeedRank: 0},
			{Name: "Carol", SeedRank: 1},
		}
		err := ApplySeeds(players, assignments)
		require.NoError(t, err)
	})
}

// TestPoolSeeding_SingleCourt_DojoSpread covers the numCourts == 1 path where
// the court-aware priority logic collapses to a single bracket. Two large
// dojos must still be spread across pools instead of being placed together by
// PoolSeeding's dojo-clustering step.
func TestPoolSeeding_SingleCourt_DojoSpread(t *testing.T) {
	t.Parallel()

	const numPools = 4
	const poolSize = 3
	total := numPools * poolSize

	players := make([]Player, 0, total)
	// Two large dojo groups (4 each) plus 4 unique-dojo fillers.
	for i := 0; i < 4; i++ {
		players = append(players, Player{Name: fmt.Sprintf("Alpha%d", i+1), Dojo: "AlphaDojo"})
	}
	for i := 0; i < 4; i++ {
		players = append(players, Player{Name: fmt.Sprintf("Beta%d", i+1), Dojo: "BetaDojo"})
	}
	for i := 0; i < 4; i++ {
		players = append(players, Player{Name: fmt.Sprintf("Solo%d", i+1), Dojo: fmt.Sprintf("SoloDojo%d", i+1)})
	}

	result := referencePoolSeeding(players, numPools, 1)
	pools, err := CreatePools(result, poolSize, false)
	require.NoError(t, err)
	require.Len(t, pools, numPools)

	for _, pool := range pools {
		dojoCount := make(map[string]int)
		for _, p := range pool.Players {
			dojoCount[p.Dojo]++
		}
		assert.LessOrEqual(t, dojoCount["AlphaDojo"], 1,
			"AlphaDojo overpopulated %s: %v", pool.PoolName, dojoCount)
		assert.LessOrEqual(t, dojoCount["BetaDojo"], 1,
			"BetaDojo overpopulated %s: %v", pool.PoolName, dojoCount)
	}
}

// TestPoolSeeding_SeedsDoNotDegradeDojoSpread pins the pool-side counterpart of
// the knockout's first-round dojo separation.
//
// Seeds occupy slots computed before the unseeded are placed, which shifts the
// dojo-clustered fill and could leave two dojo-mates in one pool on a roster
// that spreads perfectly without seeds. Measured before the repair pass: four
// pools holding four 3-member dojos spread every dojo one-per-pool with no
// seeds, and put two together the moment two seeds were set.
func TestPoolSeeding_SeedsDoNotDegradeDojoSpread(t *testing.T) {
	build := func(numPools, poolSize, nDojos, dojoGroupSize, nSeeds int) []Player {
		n := numPools * poolSize
		r := make([]Player, 0, n)
		for c := 0; c < nDojos; c++ {
			for i := 1; i <= dojoGroupSize; i++ {
				r = append(r, Player{Name: fmt.Sprintf("C%d_%d", c, i), Dojo: fmt.Sprintf("Dojo%d", c)})
			}
		}
		for i := len(r) + 1; i <= n; i++ {
			r = append(r, Player{Name: fmt.Sprintf("O%d", i), Dojo: fmt.Sprintf("D%02d", i)})
		}
		for s := 0; s < nSeeds; s++ {
			r[s].Seed = s + 1
		}
		return r
	}
	worstSameDojo := func(pools []Pool, nDojos int) int {
		m := 0
		for c := 0; c < nDojos; c++ {
			dojo := fmt.Sprintf("Dojo%d", c)
			for _, p := range pools {
				k := 0
				for _, pl := range p.Players {
					if pl.Dojo == dojo {
						k++
					}
				}
				if k > m {
					m = k
				}
			}
		}
		return m
	}

	tests := []struct {
		name                                              string
		numPools, poolSize, courts, nDojos, dojoGroupSize int
		seeds                                             int
	}{
		// Both cases were measured suboptimal before the repair pass, and both
		// spread perfectly with the same roster and no seeds.
		{"four 3-member dojos, two seeds", 4, 3, 1, 4, 3, 2},
		{"four 5-member dojos, four seeds", 7, 3, 2, 4, 5, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			optimum := (tt.dojoGroupSize + tt.numPools - 1) / tt.numPools

			unseeded, _, err := BuildPoolPhase(build(tt.numPools, tt.poolSize, tt.nDojos, tt.dojoGroupSize, 0), tt.poolSize, false, tt.courts)
			require.NoError(t, err)
			require.Equal(t, optimum, worstSameDojo(unseeded, tt.nDojos),
				"baseline: the same roster without seeds must already spread optimally")

			seeded, _, err := BuildPoolPhase(build(tt.numPools, tt.poolSize, tt.nDojos, tt.dojoGroupSize, tt.seeds), tt.poolSize, false, tt.courts)
			require.NoError(t, err)
			assert.Equal(t, optimum, worstSameDojo(seeded, tt.nDojos),
				"setting seeds must not make the dojo spread worse than the same roster without them")

			// Sizes and seed separation are invariants of the repair pass: it
			// swaps competitors between pools, so it must never move a seed,
			// change a pool's size, or lose anybody.
			total := 0
			for _, p := range seeded {
				total += len(p.Players)
				seeds := 0
				for _, pl := range p.Players {
					if pl.Seed > 0 {
						seeds++
					}
				}
				assert.LessOrEqual(t, seeds, 1, "%s holds more than one seed", p.PoolName)
			}
			assert.Equal(t, tt.numPools*tt.poolSize, total, "the repair pass must not lose or duplicate a competitor")
		})
	}
}

// TestStandardSeeding_DelaysDojoMeetings pins the knockout counterpart of the
// pool draw's dojo avoidance: dojo-mates must not meet in the first round, and
// beyond that must meet as LATE as the bracket allows.
//
// Operators paste a roster dojo by dojo, unseeded competitors fill bracket
// slots in roster order, and CreateBalancedTree pairs adjacent slots, so
// before delayDojoMeetings existed a roster of four dojos of four produced a
// first round in which EVERY match was dojo-mate against dojo-mate. Pushing
// them out of round one is only half of it: two dojo-mates left in the same
// half still meet in the semi-final when the draw could have held them apart
// until the final.
func TestStandardSeeding_DelaysDojoMeetings(t *testing.T) {
	countR1Clashes := func(out []Player) int {
		n := 0
		for i := 0; i+1 < len(out); i += 2 {
			a, b := out[i], out[i+1]
			if a.Name != "" && b.Name != "" && a.Dojo == b.Dojo {
				n++
			}
		}
		return n
	}
	dojoGrouped := func(nDojos, dojoGroupSize int) []Player {
		var roster []Player
		for c := 0; c < nDojos; c++ {
			for i := 1; i <= dojoGroupSize; i++ {
				roster = append(roster, Player{
					Name: fmt.Sprintf("C%d_%d", c, i),
					Dojo: fmt.Sprintf("Dojo%d", c),
				})
			}
		}
		return roster
	}

	t.Run("dojo-grouped roster gets no same-dojo first round", func(t *testing.T) {
		assert.Equal(t, 0, countR1Clashes(StandardSeeding(dojoGrouped(4, 4))),
			"no first-round match may be between two members of one dojo when the draw allows otherwise")
	})

	t.Run("dojo-mates meet as late as the bracket allows", func(t *testing.T) {
		// The bracket halves at every round, so N competitors can be kept
		// apart until round N-ceil(log2(dojoGroupSize))+1 and no later: a dojo of
		// two can be split across the halves and meet only in the final, a
		// dojo of four across the quarters and meet in the semi-final.
		// Anything earlier than that bound is a draw that gave up too soon.
		for _, tc := range []struct{ n, nDojos, dojoGroupSize int }{
			{8, 2, 2}, {16, 2, 2}, {16, 3, 2}, {16, 2, 4}, {32, 4, 4},
		} {
			roster := dojoGrouped(tc.nDojos, tc.dojoGroupSize)
			for i := len(roster) + 1; i <= tc.n; i++ {
				roster = append(roster, Player{Name: fmt.Sprintf("O%d", i), Dojo: fmt.Sprintf("D%02d", i)})
			}
			out := StandardSeeding(roster)

			rounds := bits.Len(uint(tc.n)) - 1
			best := rounds - (bits.Len(uint(tc.dojoGroupSize - 1))) + 1
			slots := map[string][]int{}
			for slot, p := range out {
				if p.Name != "" {
					slots[p.Dojo] = append(slots[p.Dojo], slot)
				}
			}
			for dojo, ss := range slots {
				if len(ss) < 2 {
					continue
				}
				earliest := 1 << 30
				for a := range ss {
					for b := a + 1; b < len(ss); b++ {
						if r := dojoMeetRound(ss[a], ss[b]); r < earliest {
							earliest = r
						}
					}
				}
				assert.GreaterOrEqual(t, earliest, best,
					"n=%d dojos=%dx%d: %s meets in round %d, but the bracket allows round %d",
					tc.n, tc.nDojos, tc.dojoGroupSize, dojo, earliest, best)
			}
		}
	})

	t.Run("a roster with no dojo collision is returned untouched", func(t *testing.T) {
		// The pass must be a no-op when there is nothing to repair, which is
		// what keeps existing published draws and the goldens byte-identical.
		roster := make([]Player, 16)
		for i := range roster {
			roster[i] = Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo %03d", i+1)}
		}
		out := StandardSeeding(roster)
		for i := range out {
			assert.Equal(t, roster[i].Name, out[i].Name, "slot %d moved on a roster with no dojo collision", i)
		}
	})

	t.Run("seeds are never moved, on either side of a pair", func(t *testing.T) {
		// Compared against a REFERENCE draw, not against the output itself: an
		// earlier version of this assertion compared out[slot].Name with
		// itself, which holds for any input and pinned nothing. The reference
		// is the same roster with every dojo made unique, so the pass is a
		// no-op on it and its seed slots are the untouched seeding.
		//
		// The sizes are deliberate: a competitor count that is NOT a power of
		// two, with more seeds than the top bracket positions, forces
		// displaced-seed placement, which is the only way a seed reaches an
		// ODD slot and so the only way it could be the side this pass moves.
		//
		// What this does NOT pin, stated so nobody reads more into it:
		// removing the seed-protecting occupied guard from this pass (then
		// named separateFirstRoundDojos; today delayDojoMeetings' movable()
		// check) left this subtest green, verified against that earlier form.
		// No roster was found where a seed sits on an odd slot, shares a dojo
		// with its even partner, AND has a legal swap partner. The guard is
		// defence in depth; this subtest pins that seeds are where the
		// seeding put them, which is the property that actually matters to
		// an operator.
		for _, n := range []int{5, 6, 7, 12, 16} {
			for _, nSeeds := range []int{2, 4, 6} {
				if nSeeds > n {
					continue
				}
				roster := make([]Player, n)
				for i := range roster {
					roster[i] = Player{Name: fmt.Sprintf("P%02d", i+1), Dojo: "OneDojo"}
				}
				for s := 0; s < nSeeds; s++ {
					roster[s].Seed = s + 1
				}
				reference := make([]Player, n)
				copy(reference, roster)
				for i := range reference {
					reference[i].Dojo = fmt.Sprintf("Unique%02d", i+1)
				}
				got, want := StandardSeeding(roster), StandardSeeding(reference)
				for slot := range want {
					if want[slot].Seed > 0 {
						assert.Equal(t, want[slot].Seed, got[slot].Seed,
							"n=%d seeds=%d: slot %d must still hold seed %d", n, nSeeds, slot, want[slot].Seed)
					}
				}
			}
		}
	})

	t.Run("two seeds from one dojo stay drawn against each other", func(t *testing.T) {
		// The counterpart of the rule above, asserted so it reads as a
		// decision rather than an oversight: when both competitors in a pair
		// are seeds from one dojo, neither may be moved, so the pairing
		// survives. That is a seeding outcome, since the operator set those
		// ranks, not a fault in the draw.
		roster := make([]Player, 6)
		for i := range roster {
			roster[i] = Player{Name: fmt.Sprintf("S%02d", i+1), Dojo: "SeedDojo", Seed: i + 1}
		}
		out := StandardSeeding(roster)
		assert.Positive(t, countR1Clashes(out),
			"an all-seeded single-dojo field cannot be separated, and the pass must not pretend otherwise")
		seen := map[int]bool{}
		for _, p := range out {
			if p.Seed > 0 {
				assert.False(t, seen[p.Seed], "seed %d appears twice", p.Seed)
				seen[p.Seed] = true
			}
		}
		assert.Len(t, seen, 6, "every seed must still be in the draw exactly once")
	})

	// TestStandardSeeding_DelayDojoMeetings_SkipsImmovableWorstPair pins FIX 3
	// (bc-dojo-least-conflicted-pool): the worst-pair hill climb must not
	// abandon the WHOLE repair the moment the single globally-worst same-dojo
	// pair turns out to be immovable.
	//
	// Construction (n=8): generateBracketOrder(8) = [1,8,4,5,2,7,3,6], so
	// seed ranks 4 and 5 land at slots 2 and 3 -- adjacent, meeting in round
	// 1, and both SEEDED, hence permanently immovable (a dojo is not a
	// reason to break the seeding contract). Separately, six unseeded
	// players fill the remaining slots (0,1,4,5,6,7) in roster order; the
	// last two of them (DojoX) land at slots 6 and 7, ALSO meeting in round
	// 1, but both unseeded and hence fixable (e.g. swapping either of them
	// with slot 0's occupant pushes DojoX's meeting to round 3, at the cost
	// of moving DojoB's own pair from round 3 to round 2 -- a net
	// improvement the hill climb should take).
	//
	// Before the fix, the worst-pair scan always found the seeded DojoA
	// pair first (it is tied for worst at round 1, and the scan order
	// visits slots 2-3 before 6-7), found no movable member to relocate,
	// and returned immediately -- leaving DojoX's fixable round-1 meeting
	// untouched. RED-verified: reverting to the original early-return
	// reproduces exactly that (see the fix's own commit for the verification
	// transcript).
	t.Run("an immovable worst pair does not strand a separate fixable pair", func(t *testing.T) {
		players := []Player{
			{Name: "S4", Dojo: "DojoA", Seed: 4},
			{Name: "S5", Dojo: "DojoA", Seed: 5},
			{Name: "C1", Dojo: "DojoC"},
			{Name: "B1", Dojo: "DojoB"},
			{Name: "D1", Dojo: "DojoD"},
			{Name: "B2", Dojo: "DojoB"},
			{Name: "X1", Dojo: "DojoX"},
			{Name: "X2", Dojo: "DojoX"},
		}

		out := StandardSeeding(players)
		require.Len(t, out, 8)

		slotOf := map[string]int{}
		for i, p := range out {
			if p.Name != "" {
				slotOf[p.Name] = i
			}
		}

		// Sanity: the construction actually produces the immovable
		// round-1 seeded pairing this test is about. If this fails, the
		// fixture no longer exercises the scenario and needs revisiting,
		// not the production code.
		require.Contains(t, slotOf, "S4")
		require.Contains(t, slotOf, "S5")
		require.Equal(t, 1, dojoMeetRound(slotOf["S4"], slotOf["S5"]),
			"sanity: the seeded DojoA pair must be the immovable round-1 pairing this test is about")

		// The actual assertion: no MOVABLE same-dojo pair (neither member
		// seeded) may meet in round 1. The immovable seeded pair at
		// slots 2-3 is expected and allowed to remain.
		for i := range out {
			for j := i + 1; j < len(out); j++ {
				a, b := out[i], out[j]
				if a.Name == "" || b.Name == "" || a.Dojo == "" || a.Dojo != b.Dojo {
					continue
				}
				if dojoMeetRound(i, j) != 1 {
					continue
				}
				assert.Truef(t, a.Seed > 0 && b.Seed > 0,
					"movable same-dojo pair %s/%s (dojo %s) meets in round 1 at slots %d/%d: a fixable round-1 collision must not be stranded by an unrelated immovable one",
					a.Name, b.Name, a.Dojo, i, j)
			}
		}
	})
}

// referenceDojoSumMeetRoundsTouching is a full O(N^2) scan over EVERY pair in
// the draw, kept only for TestDojoSumMeetRounds_MatchesFullScan: it is the
// pre-P1 semantics of dojoSumMeetRounds(result, slots, x, y) restated
// independently -- sum dojoMeetRound(slots[i], slots[j]) for every same-dojo
// pair where i or j is x or y, counting the {x, y} pair itself exactly once
// -- rather than derived from the two-loop-over-x-and-y shape the real
// function now uses. A bug that double-counts or drops the {x, y} pair, or
// that mis-scopes "touching", would still pass a test built from the same
// two-loop shape; this does not share that shape. slots is
// denseSlotMap(len(result)), matching what the real function is now handed
// (bc-drwx item 1): both the reference and the function under test must read
// the same (correct) tree geometry, or this test would only ever pin the two
// implementations agreeing with EACH OTHER, not with the real tree.
func referenceDojoSumMeetRoundsTouching(result []Player, keys []string, slots []int, x, y int) int {
	sum := 0
	for i := range result {
		for j := i + 1; j < len(result); j++ {
			if i != x && i != y && j != x && j != y {
				continue
			}
			if result[i].Name == "" || result[j].Name == "" || result[i].Dojo == "" {
				continue
			}
			if keys[i] != keys[j] {
				continue
			}
			sum += dojoMeetRound(slots[i], slots[j])
		}
	}
	return sum
}

// referenceFullDrawDojoSum is a full whole-draw meeting-round total,
// independent of both dojoSumMeetRounds and dojoSwapGain, used by
// TestDojoSwapGain_MatchesFullDrawDelta as the "recompute from scratch"
// oracle for a swap's gain. slots is denseSlotMap(len(result)) -- see
// referenceDojoSumMeetRoundsTouching's own doc comment for why this must
// match what the real function is handed.
func referenceFullDrawDojoSum(result []Player, keys []string, slots []int) int {
	sum := 0
	for i := range result {
		for j := i + 1; j < len(result); j++ {
			if result[i].Name == "" || result[j].Name == "" || result[i].Dojo == "" {
				continue
			}
			if keys[i] != keys[j] {
				continue
			}
			sum += dojoMeetRound(slots[i], slots[j])
		}
	}
	return sum
}

// dojoSumTestRosters is shared by TestDojoSumMeetRounds_MatchesFullScan and
// TestDojoSwapGain_MatchesFullDrawDelta: a spread of deterministic shapes
// covering a namesake-free (all-unique-dojo) roster, a clustered-dojo roster
// (a few dojos, several members each, pasted dojo-by-dojo as an operator
// would), a multi-dojo roster with dojos interleaved rather than clustered,
// and a roster with some blank Name slots (byes) and a blank Dojo entry, both
// of which the meeting-round scan must skip.
func dojoSumTestRosters() map[string][]Player {
	namesakeFree := make([]Player, 12)
	for i := range namesakeFree {
		namesakeFree[i] = Player{Name: fmt.Sprintf("U%02d", i+1), Dojo: fmt.Sprintf("Dojo%02d", i+1)}
	}

	clustered := make([]Player, 0, 16)
	for c := 0; c < 4; c++ {
		for i := 0; i < 4; i++ {
			clustered = append(clustered, Player{
				Name: fmt.Sprintf("C%d_%d", c, i),
				Dojo: fmt.Sprintf("Dojo%d", c),
			})
		}
	}

	interleaved := make([]Player, 16)
	dojos := []string{"Alpha", "Beta", "Gamma", "Delta"}
	for i := range interleaved {
		interleaved[i] = Player{Name: fmt.Sprintf("I%02d", i+1), Dojo: dojos[i%len(dojos)]}
	}

	withByesAndBlankDojo := []Player{
		{Name: "A1", Dojo: "DojoA"},
		{Name: "", Dojo: ""}, // bye slot: blank name AND blank dojo
		{Name: "A2", Dojo: "DojoA"},
		{Name: "NoDojo", Dojo: ""}, // named but dojo-less: excluded from every pair
		{Name: "B1", Dojo: "DojoB"},
		{Name: "B2", Dojo: "DojoB"},
		{Name: "A3", Dojo: "DojoA"},
		{Name: "", Dojo: ""},
	}

	return map[string][]Player{
		"namesake-free":            namesakeFree,
		"clustered-dojo":           clustered,
		"multi-dojo-interleaved":   interleaved,
		"byes-and-blank-dojo-rows": withByesAndBlankDojo,
	}
}

// TestDojoSumMeetRounds_MatchesFullScan pins the P1 rewrite of
// dojoSumMeetRounds (walk only the pairs touching x or y, in O(N), rather
// than the old variadic-filtered O(N^2) whole-draw scan) against
// referenceDojoSumMeetRoundsTouching, over every (x, y) pair in each roster
// shape.
func TestDojoSumMeetRounds_MatchesFullScan(t *testing.T) {
	for name, roster := range dojoSumTestRosters() {
		t.Run(name, func(t *testing.T) {
			slots := denseSlotMap(len(roster))
			// keys (string) feeds referenceDojoSumMeetRoundsTouching, the
			// independent string-keyed oracle; ids (int, bc-pnum) feeds the
			// production dojoSumMeetRounds, which now indexes by dense id.
			// Built from the same roster in the same order, so ids[i] ==
			// ids[j] iff keys[i] == keys[j] -- the two stay equivalent even
			// though their concrete values differ.
			keys := make([]string, len(roster))
			for i := range roster {
				keys[i] = dojoKey(roster[i].Dojo)
			}
			ids := make([]int, len(roster))
			idOf := map[string]int{}
			for i := range roster {
				if _, ok := idOf[keys[i]]; !ok {
					idOf[keys[i]] = len(idOf)
				}
				ids[i] = idOf[keys[i]]
			}
			for x := range roster {
				for y := range roster {
					if x == y {
						continue
					}
					want := referenceDojoSumMeetRoundsTouching(roster, keys, slots, x, y)
					got := dojoSumMeetRounds(roster, ids, slots, x, y)
					assert.Equalf(t, want, got, "x=%d y=%d", x, y)
				}
			}
		})
	}
}

// TestDojoSwapGain_MatchesFullDrawDelta pins dojoSwapGain -- built on top of
// the P1-rewritten dojoSumMeetRounds -- against an entirely independent
// oracle: the before/after delta of a full whole-draw recompute
// (referenceFullDrawDojoSum), for every (x, y) swap in each roster shape.
// This is what actually matters to delayDojoMeetings' hill climb: the
// SCOPED sum must still produce the same swap-gain delta the old whole-draw
// sum would have.
func TestDojoSwapGain_MatchesFullDrawDelta(t *testing.T) {
	for name, roster := range dojoSumTestRosters() {
		t.Run(name, func(t *testing.T) {
			slots := denseSlotMap(len(roster))
			// keys (string) feeds referenceFullDrawDojoSum, the independent
			// string-keyed oracle; ids (int, bc-pnum) feeds the production
			// dojoSwapGain, which now indexes by dense id -- see
			// TestDojoSumMeetRounds_MatchesFullScan's own comment for why
			// both are built here.
			keys := make([]string, len(roster))
			for i := range roster {
				keys[i] = dojoKey(roster[i].Dojo)
			}
			ids := make([]int, len(roster))
			idOf := map[string]int{}
			for i := range roster {
				if _, ok := idOf[keys[i]]; !ok {
					idOf[keys[i]] = len(idOf)
				}
				ids[i] = idOf[keys[i]]
			}
			for x := range roster {
				for y := range roster {
					if x == y {
						continue
					}
					before := referenceFullDrawDojoSum(roster, keys, slots)
					roster[x], roster[y] = roster[y], roster[x]
					keys[x], keys[y] = keys[y], keys[x]
					after := referenceFullDrawDojoSum(roster, keys, slots)
					roster[x], roster[y] = roster[y], roster[x]
					keys[x], keys[y] = keys[y], keys[x]
					want := after - before

					got := dojoSwapGain(roster, ids, slots, x, y)
					assert.Equalf(t, want, got, "x=%d y=%d", x, y)
				}
			}
		})
	}
}

// referenceDelayDojoMeetingsUnmemoized is delayDojoMeetings' body as it
// existed immediately before the bc-dojo-least-conflicted-pool wave-2
// slotBest memo: the candidate-relocation scan for worstA/worstB is
// recomputed fresh on every outer iteration, with no per-slot cache. Kept
// ONLY as TestDelayDojoMeetings_MatchesUnmemoizedReference's oracle -- the
// worst-pair selection, tie-break order, exclusion/generation bookkeeping
// and accept condition are copied unchanged, so any drift from the real
// (memoized) function can only be attributed to the memo itself.
func referenceDelayDojoMeetingsUnmemoized(result []Player, occupied map[int]bool) {
	// slots mirrors delayDojoMeetings' own denseSlotMap call (bc-drwx item
	// 1): this reference must use the SAME real-tree geometry the memoized
	// function now uses, or a drift here would be misattributed to the
	// memo when it is really the dense/slot translation.
	slots := denseSlotMap(len(result))
	// ids mirrors delayDojoMeetings' own ids slice (bc-drwx review fix, then
	// bc-pnum's int-id rewrite), kept in lockstep with result on every swap
	// below -- same reason as slots: a drift here would be misattributed to
	// the memo.
	keys := make(dojoKeyCache, len(result))
	idCache := newDojoIDCache(keys, len(result))
	ids := make([]int, len(result))
	for i := range result {
		ids[i] = idCache.of(result[i].Dojo)
	}

	movable := func(i int) bool {
		return !occupied[i] && result[i].Name != "" && result[i].Dojo != ""
	}

	type pairKey struct{ i, j int }
	excluded := map[pairKey]bool{}

	for iter := 0; iter < len(result)*len(result); iter++ {
		worstA, worstB, worstRound := -1, -1, 1<<30
		for i := range result {
			for j := i + 1; j < len(result); j++ {
				if result[i].Name == "" || result[j].Name == "" || result[i].Dojo == "" {
					continue
				}
				if ids[i] != ids[j] {
					continue
				}
				if excluded[pairKey{i, j}] {
					continue
				}
				if r := dojoMeetRound(slots[i], slots[j]); r < worstRound {
					worstA, worstB, worstRound = i, j, r
				}
			}
		}
		if worstA < 0 {
			return
		}

		bestGain, bestX, bestY := 0, -1, -1
		for _, x := range []int{worstA, worstB} {
			if !movable(x) {
				continue
			}
			for y := range result {
				if y == x || !movable(y) || ids[y] == ids[x] {
					continue
				}
				if gain := dojoSwapGain(result, ids, slots, x, y); gain > bestGain {
					bestGain, bestX, bestY = gain, x, y
				}
			}
		}
		if bestGain <= 0 {
			excluded[pairKey{worstA, worstB}] = true
			continue
		}
		result[bestX], result[bestY] = result[bestY], result[bestX]
		ids[bestX], ids[bestY] = ids[bestY], ids[bestX]
		excluded = map[pairKey]bool{}
	}
}

// TestDelayDojoMeetings_MatchesUnmemoizedReference pins the wave-2 slotBest
// generation memo against referenceDelayDojoMeetingsUnmemoized (the
// pre-memo body) across clustered, lopsided (a couple of oversized dojos
// plus many singleton dojos -- the shape most likely to make one slot recur
// as worstA/worstB across several stuck-and-excluded iterations of the same
// generation, which is exactly what the memo is for), and seeded/occupied
// shapes.
func TestDelayDojoMeetings_MatchesUnmemoizedReference(t *testing.T) {
	dojoGrouped := func(nDojos, groupSize int) []Player {
		var out []Player
		for c := 0; c < nDojos; c++ {
			for i := 0; i < groupSize; i++ {
				out = append(out, Player{Name: fmt.Sprintf("C%02d_%03d", c, i), Dojo: fmt.Sprintf("Dojo%02d", c)})
			}
		}
		return out
	}
	lopsided := func(bigDojos, bigGroupSize, singletons int) []Player {
		var out []Player
		for c := 0; c < bigDojos; c++ {
			for i := 0; i < bigGroupSize; i++ {
				out = append(out, Player{Name: fmt.Sprintf("Big%d_%03d", c, i), Dojo: fmt.Sprintf("BigDojo%d", c)})
			}
		}
		for i := 0; i < singletons; i++ {
			out = append(out, Player{Name: fmt.Sprintf("Solo%03d", i), Dojo: fmt.Sprintf("SoloDojo%03d", i)})
		}
		return out
	}

	// Sized to stay well under a second in total: this test's job is
	// output-identity, which the memo logic makes N-independent (a slot's
	// cached answer is exactly what a fresh scan would return, whatever N
	// is) -- the dramatic wall-clock deltas are measured separately
	// (dojoSumMeetRounds' and delayDojoMeetings' own doc comments) on
	// 256-entrant rosters, which would make this correctness pin itself
	// the slowest thing in the package if reused here.
	cases := map[string][]Player{
		"clustered 8 dojos of 8":                       dojoGrouped(8, 8),
		"clustered 16 dojos of 4":                      dojoGrouped(16, 4),
		"lopsided: 2 large dojos + many singletons":    lopsided(2, 10, 20),
		"lopsided: 1 large dojo + many singletons":     lopsided(1, 12, 16),
		"lopsided: 3 large dojos + few singletons":     lopsided(3, 8, 6),
		"small mixed clustered + one unique straggler": append(dojoGrouped(3, 3), Player{Name: "Extra", Dojo: "UniqueDojo"}),
	}

	for name, roster := range cases {
		t.Run(name, func(t *testing.T) {
			memoized := make([]Player, len(roster))
			copy(memoized, roster)
			reference := make([]Player, len(roster))
			copy(reference, roster)
			occupied := map[int]bool{}

			delayDojoMeetings(memoized, occupied)
			referenceDelayDojoMeetingsUnmemoized(reference, occupied)

			require.Len(t, memoized, len(reference))
			for i := range memoized {
				assert.Equalf(t, reference[i].Name, memoized[i].Name,
					"slot %d: memoized=%q reference=%q", i, memoized[i].Name, reference[i].Name)
			}
		})
	}

	// A seeded/occupied variant: two seed slots share a dojo and land
	// adjacent (immovable, mirrors the excluded map's own FIX-3 scenario),
	// alongside an entirely separate fixable same-dojo pair elsewhere, so
	// the memo is exercised alongside the occupied-slot immovability path
	// too, not just the plain unseeded case above.
	t.Run("seeded pair immovable, separate fixable pair elsewhere", func(t *testing.T) {
		roster := dojoGrouped(6, 6) // 36 players, 6 dojos of 6
		roster[2].Seed = 4
		roster[3].Seed = 5
		roster[2].Dojo = "SeededDojo"
		roster[3].Dojo = "SeededDojo"
		occupied := map[int]bool{2: true, 3: true}

		memoized := make([]Player, len(roster))
		copy(memoized, roster)
		reference := make([]Player, len(roster))
		copy(reference, roster)

		delayDojoMeetings(memoized, occupied)
		referenceDelayDojoMeetingsUnmemoized(reference, occupied)

		require.Len(t, memoized, len(reference))
		for i := range memoized {
			assert.Equalf(t, reference[i].Name, memoized[i].Name,
				"slot %d: memoized=%q reference=%q", i, memoized[i].Name, reference[i].Name)
		}
	})
}
