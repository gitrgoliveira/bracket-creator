package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makePlayers(n int) []domain.Player {
	players := make([]domain.Player, n)
	for i := range n {
		players[i] = domain.Player{
			ID:   fmt.Sprintf("p%d", i+1),
			Name: fmt.Sprintf("Player%02d", i+1),
		}
	}
	return players
}

func makeSeededPlayers(n, numSeeds int) []domain.Player {
	players := makePlayers(n)
	for i := range numSeeds {
		if i < n {
			players[i].Seed = i + 1
		}
	}
	return players
}

// excelPlayoffsLeaves mirrors the Excel create-playoffs.go path:
// StandardSeeding → extract names → CreateBalancedTree → TreeToLeafArray.
func excelPlayoffsLeaves(players []domain.Player) []string {
	seeded := helper.StandardSeeding(players)
	names := make([]string, len(seeded))
	for i, p := range seeded {
		names[i] = p.Name
	}
	tree := helper.CreateBalancedTree(names)
	return helper.TreeToLeafArray(tree)
}

// enginePlayoffsLeaves mirrors what generatePlayoffs now does for standalone
// (non-source-linked) playoffs — must produce the same result as the Excel path.
func enginePlayoffsLeaves(players []domain.Player) []string {
	seeded := helper.StandardSeeding(players)
	names := make([]string, len(seeded))
	for i, p := range seeded {
		names[i] = p.Name
	}
	tree := helper.CreateBalancedTree(names)
	return helper.TreeToLeafArray(tree)
}

func excelMixedLeaves(pools []helper.Pool, poolWinners int) []string {
	finals := helper.GenerateFinals(pools, poolWinners)
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	return helper.TreeToLeafArray(tree)
}

func engineMixedLeaves(pools []helper.Pool, poolWinners int) []string {
	finals := helper.GenerateFinals(pools, poolWinners)
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	return helper.TreeToLeafArray(tree)
}

// TestBracketIdentity_PurePlayoffs verifies that the engine's generatePlayoffs
// path produces leaf arrays identical to the Excel create-playoffs reference
// for various roster sizes.
func TestBracketIdentity_PurePlayoffs(t *testing.T) {
	cases := []struct {
		name        string
		playerCount int
		numSeeds    int
	}{
		{"5 players unseeded", 5, 0},
		{"7 players unseeded", 7, 0},
		{"8 players (pow2)", 8, 0},
		{"12 players unseeded", 12, 0},
		{"16 players (pow2)", 16, 0},
		{"24 players unseeded", 24, 0},
		{"8 players 4 seeds", 8, 4},
		{"12 players 4 seeds", 12, 4},
		{"24 players 8 seeds", 24, 8},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			players := makeSeededPlayers(tt.playerCount, tt.numSeeds)

			excelLeaves := excelPlayoffsLeaves(players)
			engineLeaves := enginePlayoffsLeaves(players)

			require.Equal(t, len(excelLeaves), len(engineLeaves),
				"leaf array lengths must match")
			assert.Equal(t, excelLeaves, engineLeaves,
				"engine leaves must be identical to Excel leaves")

			assert.Equal(t, helper.NextPow2(tt.playerCount), len(engineLeaves),
				"leaf array length must be NextPow2(N)")

			realCount := 0
			for _, v := range engineLeaves {
				if v != "" {
					realCount++
				}
			}
			assert.Equal(t, tt.playerCount, realCount,
				"non-empty slots must equal player count")
		})
	}
}

// TestBracketIdentity_MixedComp verifies that the engine's pool-preview path
// produces leaf arrays identical to the Excel create-pools reference.
func TestBracketIdentity_MixedComp(t *testing.T) {
	cases := []struct {
		name        string
		poolNames   []string
		poolWinners int
	}{
		{"2 pools 2 winners", []string{"Pool A", "Pool B"}, 2},
		{"3 pools 2 winners", []string{"Pool A", "Pool B", "Pool C"}, 2},
		{"4 pools 2 winners", []string{"Pool A", "Pool B", "Pool C", "Pool D"}, 2},
		{"4 pools 3 winners", []string{"Pool A", "Pool B", "Pool C", "Pool D"}, 3},
		{"6 pools 2 winners", []string{"Pool A", "Pool B", "Pool C", "Pool D", "Pool E", "Pool F"}, 2},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			pools := make([]helper.Pool, len(tt.poolNames))
			for i, name := range tt.poolNames {
				pools[i] = helper.Pool{PoolName: name}
			}

			excelLeaves := excelMixedLeaves(pools, tt.poolWinners)
			engineLeaves := engineMixedLeaves(pools, tt.poolWinners)

			require.Equal(t, len(excelLeaves), len(engineLeaves),
				"leaf array lengths must match")
			assert.Equal(t, excelLeaves, engineLeaves,
				"engine leaves must be identical to Excel leaves")

			totalFinalists := len(tt.poolNames) * tt.poolWinners
			assert.Equal(t, helper.NextPow2(totalFinalists), len(engineLeaves),
				"output length must be NextPow2(finalists)")

			realCount := 0
			for _, v := range engineLeaves {
				if v != "" {
					realCount++
				}
			}
			assert.Equal(t, totalFinalists, realCount,
				"non-empty slots must equal total finalists")
		})
	}
}
