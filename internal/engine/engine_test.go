package engine

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestEngine(t *testing.T) (*Engine, *state.Store, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "engine-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	eng := New(store)
	return eng, store, dir
}

func createTestCompetition(t *testing.T, store *state.Store, id string, format string, poolSize int) {
	t.Helper()
	comp := &state.Competition{
		ID:           id,
		Name:         "Test Competition",
		Kind:         "individual",
		Format:       format,
		PoolSize:     poolSize,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A", "B"},
		StartTime:    "09:00",
		Status:       "setup",
	}
	require.NoError(t, store.SaveCompetition(comp))
}

func saveTestParticipants(t *testing.T, store *state.Store, compID string, names []string) {
	t.Helper()
	players := make([]helper.Player, len(names))
	for i, n := range names {
		players[i] = helper.Player{Name: n, Dojo: "Dojo" + string(rune('A'+i%5))}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
}

// --- Pool Generation Tests ---

func TestStartCompetition_PoolsFormat_BasicGeneration(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mens-individual"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	// Verify competition status updated
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, "pools", comp.Status)

	// Verify pools were generated
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, pools)

	// With 6 players and poolSize=3 (min mode), we should get 2 pools
	assert.Len(t, pools, 2)

	// Each pool should have 3 players
	totalPlayers := 0
	for _, p := range pools {
		totalPlayers += len(p.Players)
	}
	assert.Equal(t, 6, totalPlayers)

	// Verify pool matches were generated
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, matches)

	// Round-robin with 3 players = 3 matches per pool = 6 total
	assert.Len(t, matches, 6)

	// All matches should be scheduled
	for _, m := range matches {
		assert.Equal(t, state.MatchStatusScheduled, m.Status)
		assert.NotEmpty(t, m.SideA)
		assert.NotEmpty(t, m.SideB)
	}
}

func TestStartCompetition_PoolsFormat_WithSeeds(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "seeded-pools"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
		"Grace", "Hank", "Ivy",
	})

	// Seed the top 3 players
	seeds := []domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1},
		{Name: "Bob", SeedRank: 2},
		{Name: "Charlie", SeedRank: 3},
	}
	require.NoError(t, store.SaveSeeds(compID, seeds))

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.NotEmpty(t, pools)
	// 9 players, pool size 3 min → 3 pools
	assert.Len(t, pools, 3)

	// The seeded players should have been distributed — verify they're not
	// all in the same pool. PoolSeeding with 3 seeds and 3 pools puts one seed per pool.
	seedsByPool := map[string][]string{}
	for _, p := range pools {
		for _, player := range p.Players {
			if player.Seed > 0 {
				seedsByPool[p.PoolName] = append(seedsByPool[p.PoolName], player.Name)
			}
		}
	}

	// Each pool should have at most 1 seed
	for pool, seededPlayers := range seedsByPool {
		assert.LessOrEqualf(t, len(seededPlayers), 1,
			"pool %s has too many seeds: %v", pool, seededPlayers)
	}
	// All 3 seeds should be placed
	totalSeeds := 0
	for _, s := range seedsByPool {
		totalSeeds += len(s)
	}
	assert.Equal(t, 3, totalSeeds, "all seeds should be placed in pools")
}

func TestStartCompetition_PoolsFormat_DojoConflictAvoidance(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "dojo-conflict"

	createTestCompetition(t, store, compID, "pools", 3)

	// Create players with same-dojo groups
	players := []helper.Player{
		{Name: "A1", Dojo: "Mumeishi"},
		{Name: "A2", Dojo: "Mumeishi"},
		{Name: "A3", Dojo: "Mumeishi"},
		{Name: "B1", Dojo: "Sanshukai"},
		{Name: "B2", Dojo: "Sanshukai"},
		{Name: "B3", Dojo: "Sanshukai"},
		{Name: "C1", Dojo: "Tora"},
		{Name: "C2", Dojo: "Tora"},
		{Name: "C3", Dojo: "Tora"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.Len(t, pools, 3)

	// Verify no pool has two players from the same dojo
	for _, p := range pools {
		dojos := map[string]int{}
		for _, player := range p.Players {
			dojos[player.Dojo]++
		}
		for dojo, count := range dojos {
			assert.LessOrEqualf(t, count, 1, "pool %s has %d players from %s", p.PoolName, count, dojo)
		}
	}
}

func TestStartCompetition_PoolsFormat_MaxMode(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "max-mode"

	comp := &state.Competition{
		ID:           compID,
		Name:         "Max Mode Test",
		Kind:         "individual",
		Format:       "pools",
		PoolSize:     4,
		PoolSizeMode: "max",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{
		"P1", "P2", "P3", "P4", "P5", "P6", "P7",
	})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)

	// 7 players, max pool size 4 → ceil(7/4) = 2 pools (4+3)
	assert.Len(t, pools, 2)

	totalPlayers := 0
	for _, p := range pools {
		totalPlayers += len(p.Players)
	}
	assert.Equal(t, 7, totalPlayers)
}

func TestStartCompetition_PoolsFormat_NonRoundRobin(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "non-rr"

	comp := &state.Competition{
		ID:           compID,
		Name:         "Non RR Test",
		Kind:         "individual",
		Format:       "pools",
		PoolSize:     4,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   false,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{
		"P1", "P2", "P3", "P4",
	})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	// Non-round-robin with 4 players in 1 pool generates sequential pairs
	assert.NotEmpty(t, matches)
}

// --- Bracket/Playoffs Generation Tests ---

func TestStartCompetition_PlayoffsFormat_Basic(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "playoffs"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave",
	})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, "playoffs", comp.Status)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)

	// 4 players → 2 rounds (semifinals + final)
	assert.Len(t, bracket.Rounds, 2)
	assert.Len(t, bracket.Rounds[0], 2) // 2 semifinal matches
	assert.Len(t, bracket.Rounds[1], 1) // 1 final
}

func TestStartCompetition_PlayoffsFormat_WithByes(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "playoffs-byes"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve",
	})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)

	// 5 players → padded to 8 slots → 3 rounds
	assert.Len(t, bracket.Rounds, 3)
	assert.Len(t, bracket.Rounds[0], 4) // 4 first-round matches
	assert.Len(t, bracket.Rounds[1], 2) // 2 semifinal matches
	assert.Len(t, bracket.Rounds[2], 1) // 1 final

	// Bye matches (where at least one side is empty) should be auto-completed
	byeCount := 0
	for _, m := range bracket.Rounds[0] {
		if m.SideA == "" || m.SideB == "" {
			byeCount++
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
		}
	}
	// 3 empty slots in 8 positions: m2 has one bye (P5 vs ""), m3 has double bye ("" vs "")
	assert.GreaterOrEqual(t, byeCount, 2)

	// Matches with one real player and one bye should have a winner
	for _, m := range bracket.Rounds[0] {
		if (m.SideA == "" && m.SideB != "") || (m.SideA != "" && m.SideB == "") {
			assert.NotEmpty(t, m.Winner, "single-bye match should have a winner")
		}
	}
}

func TestStartCompetition_PlayoffsFormat_PowerOf2(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "playoffs-8"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{
		"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8",
	})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)

	// 8 players = power of 2 → no byes
	assert.Len(t, bracket.Rounds, 3)
	assert.Len(t, bracket.Rounds[0], 4)
	assert.Len(t, bracket.Rounds[1], 2)
	assert.Len(t, bracket.Rounds[2], 1)

	// No byes — all first-round matches should have both sides
	for _, m := range bracket.Rounds[0] {
		assert.NotEmpty(t, m.SideA)
		assert.NotEmpty(t, m.SideB)
		assert.Equal(t, state.MatchStatusScheduled, m.Status)
	}
}

func TestStartCompetition_PlayoffsFormat_WithSeeds(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "playoffs-seeded"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{
		"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8",
	})

	seeds := []domain.SeedAssignment{
		{Name: "P1", SeedRank: 1},
		{Name: "P2", SeedRank: 2},
	}
	require.NoError(t, store.SaveSeeds(compID, seeds))

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)

	// Seed 1 and 2 should be on opposite halves of the bracket
	// Find P1 and P2 in the first round
	var p1Match, p2Match int
	for i, m := range bracket.Rounds[0] {
		if m.SideA == "P1" || m.SideB == "P1" {
			p1Match = i
		}
		if m.SideA == "P2" || m.SideB == "P2" {
			p2Match = i
		}
	}

	// In a 8-player bracket, opposite halves means one is in matches 0-1 and other in 2-3
	p1Half := p1Match / 2
	p2Half := p2Match / 2
	assert.NotEqual(t, p1Half, p2Half, "Seed 1 and 2 should be in opposite halves")
}

func TestStartCompetition_PlayoffsFormat_SinglePlayer(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "single"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice"})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	// Single player → NextPow2(1)=1, tree depth=1, loop doesn't produce rounds
	// The bracket is created but with no rounds (trivial case)
	assert.Empty(t, bracket.Rounds)
}

func TestStartCompetition_PlayoffsFormat_TwoPlayers(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "two"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob"})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)

	assert.Len(t, bracket.Rounds, 1)
	assert.Len(t, bracket.Rounds[0], 1)
	assert.Equal(t, "Alice", bracket.Rounds[0][0].SideA)
	assert.Equal(t, "Bob", bracket.Rounds[0][0].SideB)
}

// --- Error Cases ---

func TestStartCompetition_NotFound(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	err := eng.StartCompetition("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStartCompetition_InvalidID(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	err := eng.StartCompetition("../evil")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid competition ID")
}

func TestStartCompetition_AlreadyStarted(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "started"

	comp := &state.Competition{
		ID:     compID,
		Status: "pools",
		Courts: []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(comp))

	err := eng.StartCompetition(compID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")
}

func TestStartCompetition_NoParticipants(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "empty"

	createTestCompetition(t, store, compID, "pools", 3)
	// Don't save any participants file

	err := eng.StartCompetition(compID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no participants")
}

func TestStartCompetition_InvalidSeedName(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bad-seed"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})

	seeds := []domain.SeedAssignment{
		{Name: "Nonexistent", SeedRank: 1},
	}
	require.NoError(t, store.SaveSeeds(compID, seeds))

	err := eng.StartCompetition(compID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in main list")
}

// --- Scoring Tests ---

func TestRecordBracketMatchResult_PropagatesWinner(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bracket-score"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)

	// Score first semifinal: Alice beats Bob
	firstMatchID := bracket.Rounds[0][0].ID
	err = eng.RecordMatchResult(compID, firstMatchID, state.MatchResult{
		Winner:  "Alice",
		IpponsA: []string{"M", "K"},
		Status:  state.MatchStatusCompleted,
	})
	require.NoError(t, err)

	// Verify propagation to the final
	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", bracket.Rounds[1][0].SideA)
	assert.Equal(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status)
}

func TestRecordBracketMatchResult_SecondMatch_PropagatesAsSideB(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bracket-sideb"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)

	// Score second semifinal: Dave beats Charlie
	secondMatchID := bracket.Rounds[0][1].ID
	err = eng.RecordMatchResult(compID, secondMatchID, state.MatchResult{
		Winner: "Dave",
		Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Dave", bracket.Rounds[1][0].SideB)
}

func TestRecordBracketMatchResult_FullTournament(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "full-tourney"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)

	// Score both semifinals
	require.NoError(t, eng.RecordMatchResult(compID, bracket.Rounds[0][0].ID, state.MatchResult{
		Winner: bracket.Rounds[0][0].SideA,
		Status: state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, bracket.Rounds[0][1].ID, state.MatchResult{
		Winner: bracket.Rounds[0][1].SideB,
		Status: state.MatchStatusCompleted,
	}))

	// Score the final
	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)

	finalID := bracket.Rounds[1][0].ID
	require.NoError(t, eng.RecordMatchResult(compID, finalID, state.MatchResult{
		Winner: bracket.Rounds[1][0].SideA,
		Status: state.MatchStatusCompleted,
	}))

	// Verify final has winner
	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, bracket.Rounds[1][0].Winner)
	assert.Equal(t, state.MatchStatusCompleted, bracket.Rounds[1][0].Status)
}

func TestRecordBracketMatchResult_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "not-found-match"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	err := eng.RecordMatchResult(compID, "m-nonexistent", state.MatchResult{
		Winner: "Alice",
		Status: state.MatchStatusCompleted,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- Pool Scoring Tests ---

func TestRecordPoolMatchResult(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-score"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie",
	})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	// Record a result for the first match
	matchID := matches[0].ID
	err = eng.RecordMatchResult(compID, matchID, state.MatchResult{
		ID:      matchID,
		SideA:   matches[0].SideA,
		SideB:   matches[0].SideB,
		Winner:  matches[0].SideA,
		IpponsA: []string{"M", "K"},
		IpponsB: []string{"D"},
		Status:  state.MatchStatusCompleted,
	})
	require.NoError(t, err)

	// Verify the match was saved
	reloaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	var found bool
	for _, m := range reloaded {
		if m.ID == matchID {
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
			assert.Equal(t, matches[0].SideA, m.Winner)
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestRecordPoolMatchResult_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-not-found"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))

	// Pool match IDs contain "Pool" in them
	err := eng.RecordMatchResult(compID, "Pool Z-99", state.MatchResult{
		Winner: "Alice",
		Status: state.MatchStatusCompleted,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- Pool Standings Tests ---

func TestCalculatePoolStandings_Basic(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "standings"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie",
	})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	// Score all matches: Alice beats everyone, Bob beats Charlie
	for i, m := range matches {
		var winner string
		switch {
		case (m.SideA == "Alice" || m.SideB == "Alice"):
			winner = "Alice"
		case (m.SideA == "Bob" || m.SideB == "Bob") && m.SideA != "Alice" && m.SideB != "Alice":
			winner = "Bob"
		default:
			winner = m.SideA
		}
		matches[i].Winner = winner
		matches[i].Status = state.MatchStatusCompleted
		matches[i].IpponsA = []string{"M"}
		matches[i].IpponsB = []string{}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, standings)

	// Find the pool standings
	for _, poolStandings := range standings {
		if len(poolStandings) == 3 {
			// Alice should be first (2 wins)
			assert.Equal(t, "Alice", poolStandings[0].Player.Name)
			assert.Equal(t, 2, poolStandings[0].Wins)
			assert.Equal(t, 0, poolStandings[0].Losses)
			assert.Equal(t, 1, poolStandings[0].Rank)
		}
	}
}

func TestCalculatePoolStandings_AllDraws(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "all-draws"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie",
	})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	// All matches are draws (hikewake)
	for i := range matches {
		matches[i].Status = state.MatchStatusCompleted
		matches[i].Decision = "hikewake"
		matches[i].Winner = ""
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)

	for _, poolStandings := range standings {
		for _, s := range poolStandings {
			assert.Equal(t, 0, s.Wins)
			assert.Equal(t, 0, s.Losses)
			assert.Equal(t, 2, s.Draws) // Each player plays 2 matches in a pool of 3
		}
	}
}

func TestCalculatePoolStandings_IpponDifferentialTiebreak(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tiebreak"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie",
	})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	// Create a scenario where Alice and Bob both have 1 win and 1 loss
	// but Alice has more ippons given
	for i, m := range matches {
		if (m.SideA == "Alice" && m.SideB == "Bob") || (m.SideA == "Bob" && m.SideB == "Alice") {
			matches[i].Winner = "Alice"
			matches[i].IpponsA = []string{"M", "K"}
			matches[i].IpponsB = []string{}
		} else if (m.SideA == "Charlie" && m.SideB == "Alice") || (m.SideA == "Alice" && m.SideB == "Charlie") {
			matches[i].Winner = "Charlie"
			matches[i].IpponsA = []string{"M"}
			matches[i].IpponsB = []string{}
		} else {
			matches[i].Winner = "Bob"
			matches[i].IpponsA = []string{"M"}
			matches[i].IpponsB = []string{}
		}
		matches[i].Status = state.MatchStatusCompleted
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)

	for _, poolStandings := range standings {
		if len(poolStandings) == 3 {
			// All have 1 win, 1 loss — sort by ippons given
			// Alice: gave 2 ippons (M, K vs Bob)
			// Bob: gave 1 ippon (M vs Charlie)
			// Charlie: gave 1 ippon (M vs Alice)
			// Alice should rank higher due to more ippons given
			aliceRank := -1
			for _, s := range poolStandings {
				if s.Player.Name == "Alice" {
					aliceRank = s.Rank
				}
			}
			assert.Equal(t, 1, aliceRank, "Alice should be ranked 1st due to ippon differential")
		}
	}
}

func TestCalculatePoolStandings_IncompleteMatches(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "incomplete"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie",
	})
	require.NoError(t, eng.StartCompetition(compID))

	// Don't score any matches — all still scheduled
	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)

	for _, poolStandings := range standings {
		for _, s := range poolStandings {
			assert.Equal(t, 0, s.Wins)
			assert.Equal(t, 0, s.Losses)
			assert.Equal(t, 0, s.Draws)
		}
	}
}

// --- Schedule Tests ---

func TestGenerateSchedule_Pools(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "schedule-pools"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{"A", "B", "C", "D", "E", "F"})
	require.NoError(t, eng.StartCompetition(compID))

	schedule, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, schedule)

	for _, s := range schedule {
		assert.Equal(t, "pool", s.MatchType)
	}
}

func TestGenerateSchedule_Bracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "schedule-bracket"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"A", "B", "C", "D"})
	require.NoError(t, eng.StartCompetition(compID))

	schedule, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, schedule)

	for _, s := range schedule {
		assert.Equal(t, "bracket", s.MatchType)
	}
}

// --- Bracket with Larger Player Counts ---

func TestStartCompetition_PlayoffsFormat_16Players(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "playoffs-16"

	createTestCompetition(t, store, compID, "playoffs", 3)
	names := make([]string, 16)
	for i := range names {
		names[i] = "Player" + string(rune('A'+i))
	}
	saveTestParticipants(t, store, compID, names)

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)

	// 16 players → 4 rounds
	assert.Len(t, bracket.Rounds, 4)
	assert.Len(t, bracket.Rounds[0], 8) // Round of 16
	assert.Len(t, bracket.Rounds[1], 4) // Quarterfinals
	assert.Len(t, bracket.Rounds[2], 2) // Semifinals
	assert.Len(t, bracket.Rounds[3], 1) // Final
}

func TestStartCompetition_PlayoffsFormat_LargeWithByes(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "playoffs-12"

	createTestCompetition(t, store, compID, "playoffs", 3)
	names := make([]string, 12)
	for i := range names {
		names[i] = "Player" + string(rune('A'+i))
	}
	saveTestParticipants(t, store, compID, names)

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)

	// 12 players → padded to 16 → 4 rounds
	assert.Len(t, bracket.Rounds, 4)
	assert.Len(t, bracket.Rounds[0], 8) // 8 first-round matches

	// 4 empty slots means some matches have byes
	byeCount := 0
	for _, m := range bracket.Rounds[0] {
		if m.SideA == "" || m.SideB == "" {
			byeCount++
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
		}
	}
	assert.GreaterOrEqual(t, byeCount, 2, "should have bye matches")

	// Total real players across all first-round matches
	realPlayers := map[string]bool{}
	for _, m := range bracket.Rounds[0] {
		if m.SideA != "" {
			realPlayers[m.SideA] = true
		}
		if m.SideB != "" {
			realPlayers[m.SideB] = true
		}
	}
	assert.Equal(t, 12, len(realPlayers), "all 12 players should appear in the bracket")
}

// --- Match ID Consistency ---

func TestBracketMatchIDs_AreUnique(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "unique-ids"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{
		"A", "B", "C", "D", "E", "F", "G", "H",
	})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, round := range bracket.Rounds {
		for _, m := range round {
			assert.False(t, ids[m.ID], "duplicate match ID: %s", m.ID)
			ids[m.ID] = true
		}
	}
}

func TestPoolMatchIDs_AreUnique(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "unique-pool-ids"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{
		"A", "B", "C", "D", "E", "F",
	})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, m := range matches {
		assert.False(t, ids[m.ID], "duplicate pool match ID: %s", m.ID)
		ids[m.ID] = true
	}
}
