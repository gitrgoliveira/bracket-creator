package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makePlayers(n int) []domain.Player {
	players := make([]domain.Player, n)
	for i := range n {
		players[i] = domain.Player{
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

// enginePlayoffsLeaves runs the REAL engine path (StartCompetition) and
// extracts the round-0 leaf ordering from the generated bracket. This exercises
// generatePlayoffs end-to-end so any drift in the engine path (seeding,
// tree construction, leaf flattening, bye resolution) is caught.
func enginePlayoffsLeaves(t *testing.T, players []domain.Player) []string {
	t.Helper()
	eng, store, _ := setupTestEngine(t)

	compID := fmt.Sprintf("identity-%d", len(players))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        compID,
		Format:    state.CompFormatPlayoffs,
		Kind:      "individual",
		Courts:    []string{"A"},
		StartTime: "09:00",
		Status:    state.CompStatusSetup,
	}))
	// Strip IDs before saving — players with non-UUID IDs confuse the CSV
	// hasIDs detector and corrupt names on reload. Let the store mint UUIDs.
	stripped := make([]domain.Player, len(players))
	for i, p := range players {
		stripped[i] = domain.Player{Name: p.Name, Dojo: p.Dojo}
	}
	require.NoError(t, store.SaveParticipants(compID, stripped))
	// Seeds are stored separately; extract from the player Seed field.
	var seeds []domain.SeedAssignment
	for _, p := range players {
		if p.Seed > 0 {
			seeds = append(seeds, domain.SeedAssignment{Name: p.Name, SeedRank: p.Seed})
		}
	}
	if len(seeds) > 0 {
		require.NoError(t, store.SaveSeeds(compID, seeds))
	}
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	require.NotEmpty(t, bracket.Rounds)

	// Round 0 is the first round (closest to leaves). Collect the two sides
	// of every match in order — this reconstructs the pow2 leaf array.
	pow2 := helper.NextPow2(len(players))
	leaves := make([]string, pow2)
	for i, m := range bracket.Rounds[0] {
		sideA := m.SideA
		sideB := m.SideB
		// Strip "Winner of…" placeholders — those are non-leaf slots that arose
		// from latent byes; treat them as "" (structural bye) for leaf comparison.
		if strings.HasPrefix(sideA, "Winner of") {
			sideA = ""
		}
		if strings.HasPrefix(sideB, "Winner of") {
			sideB = ""
		}
		leaves[i*2] = sideA
		leaves[i*2+1] = sideB
	}
	return leaves
}

func excelMixedLeaves(pools []helper.Pool, poolWinners int) []string {
	finals := helper.GenerateFinals(pools, poolWinners)
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	return helper.TreeToLeafArray(tree)
}

// engineMixedLeaves runs the REAL engine path (StartCompetition on a mixed
// comp) and extracts the preview bracket's round-0 leaf ordering. This
// exercises generatePoolPreviewBracket end-to-end.
func engineMixedLeaves(t *testing.T, pools []helper.Pool, poolWinners int) []string {
	t.Helper()
	eng, store, _ := setupTestEngine(t)

	compID := fmt.Sprintf("identity-mixed-%d-%d", len(pools), poolWinners)
	// PoolSize == poolWinners so each pool is populated with exactly PoolWinners
	// participants — a valid mixed config (every pool can supply PoolWinners
	// finishers). This still produces len(pools) pools, so the preview bracket's
	// placeholder topology ("Pool A-1st" …) is identical to the Excel reference.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           compID,
		Format:       state.CompFormatMixed,
		Kind:         "individual",
		PoolSize:     poolWinners,
		PoolSizeMode: "min",
		PoolWinners:  poolWinners,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       state.CompStatusSetup,
	}))

	// Populate PoolWinners participants per pool so every pool can supply its
	// finishers (and pools get created).
	names := make([]string, len(pools)*poolWinners)
	for i := range names {
		names[i] = fmt.Sprintf("P%02d", i+1)
	}
	players := make([]domain.Player, len(names))
	for i, n := range names {
		players[i] = domain.Player{Name: n, Dojo: "D"}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	require.NotEmpty(t, bracket.Rounds)

	totalFinalists := len(pools) * poolWinners
	pow2 := helper.NextPow2(totalFinalists)
	leaves := make([]string, pow2)
	for i, m := range bracket.Rounds[0] {
		sideA, sideB := m.SideA, m.SideB
		if strings.HasPrefix(sideA, "Winner of") {
			sideA = ""
		}
		if strings.HasPrefix(sideB, "Winner of") {
			sideB = ""
		}
		leaves[i*2] = sideA
		leaves[i*2+1] = sideB
	}
	return leaves
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
			engineLeaves := enginePlayoffsLeaves(t, players)

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

// enginePlayoffsBracket runs the real engine path (StartCompetition) and
// returns the generated bracket so display metadata (mp-7f2w) can be asserted.
func enginePlayoffsBracket(t *testing.T, players []domain.Player) *state.Bracket {
	t.Helper()
	eng, store, _ := setupTestEngine(t)

	compID := fmt.Sprintf("display-%d", len(players))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        compID,
		Format:    state.CompFormatPlayoffs,
		Kind:      "individual",
		Courts:    []string{"A"},
		StartTime: "09:00",
		Status:    state.CompStatusSetup,
	}))
	stripped := make([]domain.Player, len(players))
	for i, p := range players {
		stripped[i] = domain.Player{Name: p.Name, Dojo: p.Dojo}
	}
	require.NoError(t, store.SaveParticipants(compID, stripped))
	// Seeds are stored separately (seeds.csv) and merged at load — extract them
	// from the player Seed field so the seeded test cases actually exercise
	// seeded generation rather than silently running unseeded.
	var seeds []domain.SeedAssignment
	for _, p := range players {
		if p.Seed > 0 {
			seeds = append(seeds, domain.SeedAssignment{Name: p.Name, SeedRank: p.Seed})
		}
	}
	if len(seeds) > 0 {
		require.NoError(t, store.SaveSeeds(compID, seeds))
	}
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	return bracket
}

// excelRoundSizes returns the Excel Tree sheet's elimination-round match counts,
// ordered earliest → latest (e.g. [QF, SF, Final]), via the same unbalanced-tree
// depth traversal create-playoffs.go uses.
func excelRoundSizes(players []domain.Player) []int {
	seeded := helper.StandardSeeding(players)
	names := make([]string, len(seeded))
	for i, p := range seeded {
		names[i] = p.Name
	}
	tree := helper.CreateBalancedTree(names)
	depth := helper.CalculateDepth(tree)
	var sizes []int
	for i := depth; i > 1; i-- {
		sizes = append(sizes, len(helper.TraverseRounds(tree, 1, i-1)))
	}
	return sizes
}

// TestBracketDisplayMetadata_MatchesExcelRounds verifies that the engine's
// DisplayRound / Hidden metadata groups real matches into the SAME effective
// rounds as the Excel Tree sheet (mp-7f2w): structural byes skip a column, no
// phantom matches are shown, and exactly N-1 real matches exist.
func TestBracketDisplayMetadata_MatchesExcelRounds(t *testing.T) {
	cases := []struct {
		name        string
		playerCount int
		numSeeds    int
	}{
		{"5 players", 5, 0},
		{"7 players", 7, 0},
		{"8 players (pow2)", 8, 0},
		{"12 players", 12, 0},
		{"16 players (pow2)", 16, 0},
		{"24 players", 24, 0},
		{"24 players 8 seeds", 24, 8},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			players := makeSeededPlayers(tt.playerCount, tt.numSeeds)
			bracket := enginePlayoffsBracket(t, players)

			// Group real (non-hidden) matches by DisplayRound.
			byDR := map[int]int{}
			realTotal := 0
			maxDR := 0
			for _, round := range bracket.Rounds {
				for _, m := range round {
					if m.Hidden {
						assert.Zero(t, m.DisplayRound, "hidden match must have DisplayRound 0")
						// A hidden (bye) match always has an empty side.
						assert.True(t, m.SideA == "" || m.SideB == "",
							"hidden match must have a structural-bye empty side")
						continue
					}
					assert.GreaterOrEqual(t, m.DisplayRound, 1,
						"real match must have DisplayRound >= 1")
					byDR[m.DisplayRound]++
					realTotal++
					if m.DisplayRound > maxDR {
						maxDR = m.DisplayRound
					}
				}
			}

			// N-1 real matches in a single-elimination bracket.
			assert.Equal(t, tt.playerCount-1, realTotal,
				"real-match count must be N-1")

			// engineSizes ordered earliest → latest (highest DisplayRound first).
			engineSizes := make([]int, maxDR)
			for dr := 1; dr <= maxDR; dr++ {
				engineSizes[maxDR-dr] = byDR[dr]
			}
			assert.Equal(t, excelRoundSizes(players), engineSizes,
				"engine effective-round sizes must match the Excel Tree rounds")
		})
	}
}

// TestBracketDisplayMetadata_Feeders verifies the feeder graph is well formed
// across roster sizes (including deep multi-level bye chains, e.g. 24 players):
// every feeder ID resolves to a real, non-hidden match one DisplayRound deeper,
// and every real match except the final is referenced exactly once as a feeder.
func TestBracketDisplayMetadata_Feeders(t *testing.T) {
	cases := []struct {
		name        string
		playerCount int
		numSeeds    int
	}{
		{"5 players", 5, 0},
		{"7 players", 7, 0},
		{"8 players (pow2)", 8, 0},
		{"12 players", 12, 0},
		{"16 players (pow2)", 16, 0},
		{"24 players", 24, 0},
		{"24 players 8 seeds", 24, 8},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			bracket := enginePlayoffsBracket(t, makeSeededPlayers(tt.playerCount, tt.numSeeds))

			byID := map[string]state.BracketMatch{}
			var real []state.BracketMatch
			for _, round := range bracket.Rounds {
				for _, m := range round {
					byID[m.ID] = m
					if !m.Hidden {
						real = append(real, m)
					}
				}
			}

			refCount := map[string]int{}
			for _, m := range real {
				for _, fid := range m.Feeders {
					if fid == "" {
						continue
					}
					f, ok := byID[fid]
					require.True(t, ok, "feeder %s must exist", fid)
					assert.False(t, f.Hidden, "feeder %s must be a real match", fid)
					assert.Equal(t, m.DisplayRound+1, f.DisplayRound,
						"feeder must be one DisplayRound deeper than its parent")
					refCount[fid]++
				}
			}
			// Each real match except the final (DisplayRound 1) is fed into once.
			for _, m := range real {
				if m.DisplayRound == 1 {
					assert.Zero(t, refCount[m.ID], "final must not be a feeder")
					continue
				}
				assert.Equal(t, 1, refCount[m.ID],
					"real match %s must be referenced as a feeder exactly once", m.ID)
			}
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
			engineLeaves := engineMixedLeaves(t, pools, tt.poolWinners)

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
