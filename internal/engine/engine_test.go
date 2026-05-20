package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
	players := make([]domain.Player, len(names))
	for i, n := range names {
		players[i] = domain.Player{Name: n, Dojo: "Dojo" + string(rune('A'+i%5))}
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
	assert.Equal(t, state.CompStatusPools, comp.Status)

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
	players := []domain.Player{
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

// TestStartCompetition_LeagueFormat verifies that a league-format competition
// generates pool matches (not a bracket) and reaches CompStatusPools, and
// that all matches complete transitions it to CompStatusComplete without
// requiring a separate playoff phase.
func TestStartCompetition_LeagueFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-test"

	// League: single pool, full round-robin, no playoffs.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:         compID,
		Name:       "League Test",
		Kind:       "individual",
		Format:     state.CompFormatLeague,
		PoolSize:   5, // all 5 participants in one pool
		RoundRobin: true,
		Courts:     []string{"A"},
		StartTime:  "09:00",
		Status:     "setup",
	}))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave", "Eve"})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "league must enter pools status")

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.Len(t, pools, 1, "league must produce exactly one pool")
	assert.Len(t, pools[0].Players, 5)

	// 5-player round-robin: n*(n-1)/2 = 10 matches.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Len(t, matches, 10, "5-player round-robin must produce 10 matches")

	// Mark all matches completed; MaybeAutoCompletePools should transition to complete.
	for i := range matches {
		matches[i].Status = state.MatchStatusCompleted
		matches[i].Winner = matches[i].SideA
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome)

	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status, "league must complete after all pool matches done, without a playoff phase")
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
	assert.Equal(t, state.CompStatusPlayoffs, comp.Status)

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
	err = eng.RecordMatchResult(compID, firstMatchID, &state.MatchResult{
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
	err = eng.RecordMatchResult(compID, secondMatchID, &state.MatchResult{
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
	require.NoError(t, eng.RecordMatchResult(compID, bracket.Rounds[0][0].ID, &state.MatchResult{
		Winner: bracket.Rounds[0][0].SideA,
		Status: state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, bracket.Rounds[0][1].ID, &state.MatchResult{
		Winner: bracket.Rounds[0][1].SideB,
		Status: state.MatchStatusCompleted,
	}))

	// Score the final
	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)

	finalID := bracket.Rounds[1][0].ID
	require.NoError(t, eng.RecordMatchResult(compID, finalID, &state.MatchResult{
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

	err := eng.RecordMatchResult(compID, "m-nonexistent", &state.MatchResult{
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
	err = eng.RecordMatchResult(compID, matchID, &state.MatchResult{
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
	err := eng.RecordMatchResult(compID, "Pool Z-99", &state.MatchResult{
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

	// All matches are draws (hikiwake)
	for i := range matches {
		matches[i].Status = state.MatchStatusCompleted
		matches[i].Decision = state.DecisionDraw
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

func TestCalculatePoolStandings_WeightedScore(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "weighted-score"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie",
	})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	// Alice: 2 wins, 0 losses, gave 3 ippons, took 1
	// Bob: 1 win, 1 loss, gave 1 ippon, took 2
	// Charlie: 0 wins, 2 losses, gave 1 ippon, took 2
	for i, m := range matches {
		if (m.SideA == "Alice" && m.SideB == "Bob") || (m.SideA == "Bob" && m.SideB == "Alice") {
			matches[i].Winner = "Alice"
			if m.SideA == "Alice" {
				matches[i].IpponsA = []string{"M", "K"}
				matches[i].IpponsB = []string{"D"}
			} else {
				matches[i].IpponsA = []string{"D"}
				matches[i].IpponsB = []string{"M", "K"}
			}
		} else if (m.SideA == "Alice" && m.SideB == "Charlie") || (m.SideA == "Charlie" && m.SideB == "Alice") {
			matches[i].Winner = "Alice"
			if m.SideA == "Alice" {
				matches[i].IpponsA = []string{"M"}
				matches[i].IpponsB = []string{}
			} else {
				matches[i].IpponsA = []string{}
				matches[i].IpponsB = []string{"M"}
			}
		} else {
			matches[i].Winner = "Bob"
			if m.SideA == "Bob" {
				matches[i].IpponsA = []string{"M"}
				matches[i].IpponsB = []string{"T"}
			} else {
				matches[i].IpponsA = []string{"T"}
				matches[i].IpponsB = []string{"M"}
			}
		}
		matches[i].Status = state.MatchStatusCompleted
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)

	for _, poolStandings := range standings {
		if len(poolStandings) != 3 {
			continue
		}
		// Verify Points are computed using the weighted formula:
		// Points = W*100_000_000 - L*1_000_000 + D*10_000 + G*100 - T
		for _, s := range poolStandings {
			expected := s.Wins*100_000_000 - s.Losses*1_000_000 + s.Draws*10_000 + s.IpponsGiven*100 - s.IpponsTaken
			assert.Equal(t, expected, s.Points, "Points should match weighted formula for %s", s.Player.Name)
		}
		// Ranking: Alice (2W) > Bob (1W,1L) > Charlie (0W,2L)
		assert.Equal(t, "Alice", poolStandings[0].Player.Name)
		assert.Equal(t, 2, poolStandings[0].Wins)
		assert.True(t, poolStandings[0].Points > 0, "Alice should have positive Points")
		assert.Equal(t, "Bob", poolStandings[1].Player.Name)
		assert.True(t, poolStandings[0].Points > poolStandings[1].Points, "Alice should have higher Points than Bob")
		assert.True(t, poolStandings[1].Points > poolStandings[2].Points, "Bob should have higher Points than Charlie")
	}
}

func TestCalculatePoolStandings_TeamScoring(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "team-scoring"

	comp := &state.Competition{
		ID:           compID,
		Name:         "Team Competition",
		Kind:         "team",
		Format:       "pools",
		TeamSize:     3,
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{"TeamA", "TeamB", "TeamC"})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	// TeamA vs TeamB: TeamA wins 2-1 in IV, 4-2 in PW
	// TeamA vs TeamC: TeamA wins 3-0 in IV, 5-1 in PW
	// TeamB vs TeamC: TeamB wins 2-1 in IV, 3-2 in PW
	for i, m := range matches {
		matches[i].Status = state.MatchStatusCompleted
		if (m.SideA == "TeamA" && m.SideB == "TeamB") || (m.SideA == "TeamB" && m.SideB == "TeamA") {
			matches[i].Winner = "TeamA"
			a, b := "TeamA", "TeamB"
			if m.SideA == "TeamB" {
				a, b = "TeamB", "TeamA"
			}
			matches[i].SubResults = []state.SubMatchResult{
				{Position: 1, SideA: a, SideB: b, IpponsA: []string{"M", "K"}, IpponsB: []string{"D"}, Winner: "TeamA"},
				{Position: 2, SideA: a, SideB: b, IpponsA: []string{"M"}, IpponsB: []string{"M"}, Winner: "TeamB"},
				{Position: 3, SideA: a, SideB: b, IpponsA: []string{"D"}, IpponsB: []string{}, Winner: "TeamA"},
			}
		} else if (m.SideA == "TeamA" && m.SideB == "TeamC") || (m.SideA == "TeamC" && m.SideB == "TeamA") {
			matches[i].Winner = "TeamA"
			a, c := "TeamA", "TeamC"
			if m.SideA == "TeamC" {
				a, c = "TeamC", "TeamA"
			}
			matches[i].SubResults = []state.SubMatchResult{
				{Position: 1, SideA: a, SideB: c, IpponsA: []string{"M", "K"}, IpponsB: []string{}, Winner: "TeamA"},
				{Position: 2, SideA: a, SideB: c, IpponsA: []string{"M"}, IpponsB: []string{"D"}, Winner: "TeamA"},
				{Position: 3, SideA: a, SideB: c, IpponsA: []string{"M", "D"}, IpponsB: []string{}, Winner: "TeamA"},
			}
		} else {
			matches[i].Winner = "TeamB"
			b, c := "TeamB", "TeamC"
			if m.SideA == "TeamC" {
				b, c = "TeamC", "TeamB"
			}
			matches[i].SubResults = []state.SubMatchResult{
				{Position: 1, SideA: b, SideB: c, IpponsA: []string{"M"}, IpponsB: []string{}, Winner: "TeamB"},
				{Position: 2, SideA: b, SideB: c, IpponsA: []string{"M"}, IpponsB: []string{"K"}, Winner: "TeamC"},
				{Position: 3, SideA: b, SideB: c, IpponsA: []string{"M"}, IpponsB: []string{"D"}, Winner: "TeamB"},
			}
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)

	for _, poolStandings := range standings {
		if len(poolStandings) != 3 {
			continue
		}
		// TeamA: 2W, 0L — IV: 5W 1L, PW: 9 given 2 taken
		// TeamB: 1W, 1L — IV: 3W 2L 1D, PW: 5 given 5 taken
		// TeamC: 0W, 2L — IV: 1W 6L, PW: 4 given 11 taken
		assert.Equal(t, "TeamA", poolStandings[0].Player.Name)
		assert.Equal(t, 2, poolStandings[0].Wins)
		assert.Equal(t, 5, poolStandings[0].IndividualWins)
		assert.Equal(t, 1, poolStandings[0].IndividualLosses)
		assert.Equal(t, 9, poolStandings[0].PointsWon)

		assert.Equal(t, "TeamB", poolStandings[1].Player.Name)
		assert.Equal(t, 1, poolStandings[1].Wins)
		assert.Equal(t, 1, poolStandings[1].Losses)

		assert.Equal(t, "TeamC", poolStandings[2].Player.Name)
		assert.Equal(t, 0, poolStandings[2].Wins)

		// Verify team weighted formula is applied
		for _, s := range poolStandings {
			expected := s.Wins*100_000_000_000 - s.Losses*1_000_000_000 + s.Draws*10_000_000 +
				s.IndividualWins*100_000 - s.IndividualLosses*10_000 + s.IndividualDraws*1_000 +
				s.PointsWon*100 - s.PointsLost
			assert.Equal(t, expected, s.Points, "Team weighted formula mismatch for %s", s.Player.Name)
		}

		assert.True(t, poolStandings[0].Points > poolStandings[1].Points)
		assert.True(t, poolStandings[1].Points > poolStandings[2].Points)
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

// --- Export Tests ---

func TestExportCompetitionXlsx(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "export-test"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	// Simple check for ZIP header (Excel files are ZIPs)
	assert.Equal(t, []byte{0x50, 0x4b, 0x03, 0x04}, data[:4])
}

func TestExportCompetitionXlsx_NotFound(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	data, err := eng.ExportCompetitionXlsx("nonexistent")
	assert.Error(t, err)
	assert.Nil(t, data)
}

// --- Match Court Update Tests ---

func TestUpdateMatchCourt_Pool(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "court-pool"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	matchID := matches[0].ID

	err = eng.UpdateMatchCourt(compID, matchID, "Court Z")
	require.NoError(t, err)

	// Verify persistence in matches
	reloaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	found := false
	for _, m := range reloaded {
		if m.ID == matchID {
			assert.Equal(t, "Court Z", m.Court)
			found = true
			break
		}
	}
	assert.True(t, found)

	// Verify persistence in schedule
	schedule, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	found = false
	for _, s := range schedule {
		if s.MatchRef == matchID {
			assert.Equal(t, "Court Z", s.Court)
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestUpdateMatchCourt_Bracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "court-bracket"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	matchID := bracket.Rounds[0][0].ID

	err = eng.UpdateMatchCourt(compID, matchID, "Court X")
	require.NoError(t, err)

	// Verify persistence in bracket
	reloaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Court X", reloaded.Rounds[0][0].Court)

	// Verify persistence in schedule
	schedule, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	found := false
	for _, s := range schedule {
		if s.MatchRef == "R1-M"+matchID {
			assert.Equal(t, "Court X", s.Court)
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestUpdateMatchCourt_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "court-not-found"
	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{"A", "B", "C"})
	require.NoError(t, eng.StartCompetition(compID))

	err := eng.UpdateMatchCourt(compID, "nonexistent", "Z")
	assert.Error(t, err)
}

// --- Bracket Winner Override Tests ---

func TestOverrideBracketWinner(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-bracket"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	matchID := bracket.Rounds[0][0].ID // Alice vs Bob

	err = eng.OverrideBracketWinner(compID, matchID, "Bob")
	require.NoError(t, err)

	// Verify propagation
	reloaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", reloaded.Rounds[0][0].Winner)
	assert.True(t, reloaded.Rounds[0][0].IsOverridden)
	assert.Equal(t, "Bob", reloaded.Rounds[1][0].SideA)

	// Verify override persistence
	overrides, err := store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", overrides.Winners[matchID])
}

func TestOverrideBracketWinner_AutoPropagation(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-auto"

	createTestCompetition(t, store, compID, "playoffs", 3)
	// 4 players, Alice vs Bob (M1), Charlie vs Dave (M2). Winner M1 vs Winner M2 (Final).
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)

	// Mark second semifinal as Dave winning by bye (if possible) - or just score it normally
	err = eng.RecordMatchResult(compID, bracket.Rounds[0][1].ID, &state.MatchResult{
		Winner: "Dave",
		Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)

	// Now override first semifinal to Bob
	err = eng.OverrideBracketWinner(compID, bracket.Rounds[0][0].ID, "Bob")
	require.NoError(t, err)

	reloaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", reloaded.Rounds[1][0].SideA)
	assert.Equal(t, "Dave", reloaded.Rounds[1][0].SideB)
}

func TestOverrideBracketWinner_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-not-found"
	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"A", "B"})
	require.NoError(t, eng.StartCompetition(compID))

	err := eng.OverrideBracketWinner(compID, "m-999", "A")
	assert.Error(t, err)
}

// --- Scoring and Standing Logic Tests ---

func TestFormatScore_HansokuOnly(t *testing.T) {
	// Legacy disk values: hansoku used to be cumulative, so values >1 still
	// appear when reading old saves. The renderer must keep displaying them.
	score := formatScore([]string{}, 2)
	assert.Equal(t, "(H2)", score)

	score = formatScore([]string{"M"}, 1)
	assert.Equal(t, "M (H1)", score)

	// Post-PR-#110 saves: the discharged foul pair is recorded as an "H" ippon
	// on the opponent's slice and HansokuA resets to 0. No redundant "(H...)"
	// suffix should appear alongside the H ippon.
	score = formatScore([]string{"H"}, 0)
	assert.Equal(t, "H", score)

	score = formatScore([]string{"M", "H"}, 0)
	assert.Equal(t, "MH", score)
}

func TestCalculatePoolStandings_WithManualOverrides(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "standings-override"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))

	pools, _ := store.LoadPools(compID)
	poolName := pools[0].PoolName

	// Manually override Bob to rank 1, Alice to rank 2, Charlie to rank 3
	overrides := &state.Overrides{
		PoolRanks: map[string]map[string]int{
			poolName: {
				"Bob":     1,
				"Alice":   2,
				"Charlie": 3,
			},
		},
	}
	require.NoError(t, store.SaveOverrides(compID, overrides))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)

	poolStandings := standings[poolName]
	assert.Equal(t, "Bob", poolStandings[0].Player.Name)
	assert.Equal(t, 1, poolStandings[0].Rank)
	assert.True(t, poolStandings[0].IsOverridden)

	assert.Equal(t, "Alice", poolStandings[1].Player.Name)
	assert.Equal(t, 2, poolStandings[1].Rank)
	assert.True(t, poolStandings[1].IsOverridden)
}

func TestOverrideBracketWinner_SideB(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-sideb"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"A", "B", "C", "D"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, _ := store.LoadBracket(compID)
	// Match 1 is index 0, Match 2 is index 1.
	// Override Match 2 winner to "D"
	err := eng.OverrideBracketWinner(compID, bracket.Rounds[0][1].ID, "D")
	require.NoError(t, err)

	reloaded, _ := store.LoadBracket(compID)
	assert.Equal(t, "D", reloaded.Rounds[1][0].SideB)
}

func TestOverrideBracketWinner_DeepPropagation(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-deep"

	createTestCompetition(t, store, compID, "playoffs", 3)
	// 5 players ensures padded to 8 slots -> 3 rounds
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "P3", "P4", "P5"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, _ := store.LoadBracket(compID)
	// Find Alice vs Bob match. StandardSeeding will place them.
	matchID := ""
	for _, m := range bracket.Rounds[0] {
		if (m.SideA == "Alice" && m.SideB == "Bob") || (m.SideA == "Bob" && m.SideB == "Alice") {
			matchID = m.ID
			break
		}
	}
	require.NotEmpty(t, matchID)

	err := eng.OverrideBracketWinner(compID, matchID, "Bob")
	require.NoError(t, err)

	reloaded, _ := store.LoadBracket(compID)
	// Verify it reached at least Round 2
	found := false
	for _, m := range reloaded.Rounds[1] {
		if m.SideA == "Bob" || m.SideB == "Bob" {
			found = true
			break
		}
	}
	assert.True(t, found, "Bob should have propagated to Round 2")
}

func TestCalculatePoolStandings_EdgeCases(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "standings-edges"

	createTestCompetition(t, store, compID, "pools", 2)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob"}) // Pool of 2
	require.NoError(t, eng.StartCompetition(compID))

	// 1. Match ID with no hyphen
	err := store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "NoHyphen", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
	})
	require.NoError(t, err)
	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)

	// Find any pool
	var poolName string
	var poolStandings []state.PlayerStanding
	for name, s := range standings {
		if len(s) > 0 {
			poolName = name
			poolStandings = s
			break
		}
	}
	require.NotEmpty(t, poolStandings, "should have at least one pool with players")

	// Alice should have 0 wins because "NoHyphen" was skipped
	for _, s := range poolStandings {
		if s.Player.Name == "Alice" {
			assert.Equal(t, 0, s.Wins)
		}
	}

	// 2. Match with unknown player
	err = store.SavePoolMatches(compID, []state.MatchResult{
		{ID: poolName + "-0", SideA: "Alice", SideB: "Unknown", Status: state.MatchStatusCompleted, Winner: "Alice"},
	})
	require.NoError(t, err)
	standings, _ = eng.CalculatePoolStandings(compID)
	for _, s := range standings {
		for _, ps := range s {
			if ps.Player.Name == "Alice" {
				assert.Equal(t, 0, ps.Wins)
			}
		}
	}
}

func TestStartCompetition_NumberPrefix_Pools(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "prefix-pools"

	comp := &state.Competition{
		ID: compID, Name: "Prefix Test", Kind: "individual", Format: "pools",
		PoolSize: 3, PoolSizeMode: "min", PoolWinners: 2, RoundRobin: true,
		Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
		NumberPrefix: "K",
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})

	require.NoError(t, eng.StartCompetition(compID))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.NotEmpty(t, pools)

	seen := map[string]bool{}
	for _, p := range pools {
		for _, pl := range p.Players {
			assert.NotEmpty(t, pl.Number, "player %s should have a number", pl.Name)
			assert.True(t, len(pl.Number) > 1 && pl.Number[0] == 'K', "number %q should start with K", pl.Number)
			assert.False(t, seen[pl.Number], "duplicate number %q", pl.Number)
			seen[pl.Number] = true
		}
	}
}

func TestStartCompetition_NumberPrefix_Playoffs(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "prefix-playoffs"

	comp := &state.Competition{
		ID: compID, Name: "Prefix Playoffs", Kind: "individual", Format: "playoffs",
		Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
		NumberPrefix: "A",
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})

	require.NoError(t, eng.StartCompetition(compID))

	// Confirm competition started (bracket generated) and NumberPrefix persisted.
	updated, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPlayoffs, updated.Status)
	assert.Equal(t, "A", updated.NumberPrefix)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, bracket.Rounds, "bracket should have rounds")
}

func TestUpdateMatchTime_Pool(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "time-pool"

	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	matchID := matches[0].ID

	err = eng.UpdateMatchTime(compID, matchID, "10:00")
	require.NoError(t, err)

	// Verify persistence in matches
	reloaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	found := false
	for _, m := range reloaded {
		if m.ID == matchID {
			assert.Equal(t, "10:00", m.ScheduledAt)
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestUpdateMatchTime_Bracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "time-bracket"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	matchID := bracket.Rounds[0][0].ID

	err = eng.UpdateMatchTime(compID, matchID, "11:00")
	require.NoError(t, err)

	// Verify persistence in bracket
	reloaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "11:00", reloaded.Rounds[0][0].ScheduledAt)
}

func TestUpdateMatchTime_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "time-not-found"
	createTestCompetition(t, store, compID, "pools", 3)
	saveTestParticipants(t, store, compID, []string{"A", "B", "C"})
	require.NoError(t, eng.StartCompetition(compID))

	err := eng.UpdateMatchTime(compID, "nonexistent", "12:00")
	assert.Error(t, err)
}

func TestStartCompetition_TeamSizeFallback(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "team-comp"

	comp := &state.Competition{
		ID:       compID,
		Name:     "Team Competition",
		Kind:     "team",
		Format:   "pools",
		PoolSize: 3,
		Status:   "setup",
		Courts:   []string{"A"},
	}
	// Note: TeamSize is 0
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{"Team A", "Team B", "Team C"})

	err := eng.StartCompetition(compID)
	require.NoError(t, err)

	// Verify TeamSize was defaulted to 5
	updated, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, 5, updated.TeamSize)
}

func TestRecordMatchResult_PreservesSides(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "preservation-test"

	createTestCompetition(t, store, compID, "pools", 3)
	// Create pool matches
	matchID := "Pool A-0"
	matches := []state.MatchResult{
		{
			ID:     matchID,
			SideA:  "Player A",
			SideB:  "Player B",
			Status: state.MatchStatusScheduled,
		},
	}
	err := store.SavePoolMatches(compID, matches)
	require.NoError(t, err)

	// Update match without SideA/SideB
	update := &state.MatchResult{
		ID:     matchID,
		Winner: "Player A",
		Status: state.MatchStatusCompleted,
	}

	err = eng.RecordMatchResult(compID, matchID, update)
	require.NoError(t, err)

	// Verify that Sides were preserved in the update object (passed as pointer)
	assert.Equal(t, "Player A", update.SideA)
	assert.Equal(t, "Player B", update.SideB)

	// Verify that Sides were preserved in the store
	loadedMatches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loadedMatches, 1)

	m := loadedMatches[0]
	assert.Equal(t, "Player A", m.SideA)
	assert.Equal(t, "Player B", m.SideB)
	assert.Equal(t, "Player A", m.Winner)
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
}

// --- Court Pre-Assignment Tests ---

func TestStartCompetition_PoolCourtDistribution(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "court-dist-pools"

	// createTestCompetition uses Courts: []string{"A", "B"}
	createTestCompetition(t, store, compID, "pools", 3)
	// 6 players → 2 pools. AssignPoolsToCourts(2, 2) → [0, 1]
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	courts := map[string]int{}
	for _, m := range matches {
		courts[m.Court]++
	}
	assert.Greater(t, courts["A"], 0, "court A should have matches")
	assert.Greater(t, courts["B"], 0, "court B should have matches")

	for _, m := range matches {
		if strings.HasPrefix(m.ID, "Pool A-") {
			assert.Equal(t, "A", m.Court, "Pool A match %s should be on court A", m.ID)
		}
		if strings.HasPrefix(m.ID, "Pool B-") {
			assert.Equal(t, "B", m.Court, "Pool B match %s should be on court B", m.ID)
		}
	}
}

func TestStartCompetition_BracketCourtDistribution(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "court-dist-bracket"

	// createTestCompetition uses Courts: []string{"A", "B"}
	createTestCompetition(t, store, compID, "playoffs", 3)
	// 8 players → pow2=8, 4 first-round matches split across 2 courts
	saveTestParticipants(t, store, compID, []string{
		"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8",
	})

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	require.NotEmpty(t, bracket.Rounds)

	// First round: 4 matches. SubtreeCourtIndex(4, 2, slot) assigns:
	// matches 0,1 (slots 0,1) → court A; matches 2,3 (slots 2,3) → court B
	firstRound := bracket.Rounds[0]
	assert.Len(t, firstRound, 4)

	courts := map[string]int{}
	for _, m := range firstRound {
		courts[m.Court]++
	}
	assert.Equal(t, 2, courts["A"], "2 first-round matches should be on court A")
	assert.Equal(t, 2, courts["B"], "2 first-round matches should be on court B")

	// All non-bye matches in any round should have a court assigned
	for rIdx, round := range bracket.Rounds {
		for _, m := range round {
			if m.Status != state.MatchStatusCompleted {
				assert.NotEmpty(t, m.Court, "round %d match %s should have a court assigned", rIdx, m.ID)
			}
		}
	}
}

func TestStartCompetition_SingleCourtFallback(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "single-court"

	comp := &state.Competition{
		ID:           compID,
		Name:         "Single Court",
		Kind:         "individual",
		Format:       "pools",
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{"P1", "P2", "P3", "P4", "P5", "P6"})

	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	for _, m := range matches {
		assert.Equal(t, "A", m.Court, "all matches should be on court A with single court")
	}
}

// TestStartCompetition_AutoDefaultsTeamSize pins the StartCompetition
// transform's TeamSize handling after the /deep-review fix:
//   - The pipeline applies a default of 5 when Kind=="team" and TeamSize==0.
//   - The validation check compares current.TeamSize to the SNAPSHOT
//     loadedTeamSize (the pre-default value), NOT to the
//     post-pipeline comp.TeamSize.
//   - The transform then assigns comp.TeamSize (with the default applied)
//     to current.TeamSize, persisting 5 on disk.
//
// Pre-fix, the validation compared current.TeamSize to comp.TeamSize,
// which would have falsely flagged the auto-default (current=0 vs
// comp=5) as drift and rejected the start.
//
// We pin this by starting a team competition with TeamSize=0 and
// asserting (a) the start succeeds and (b) the persisted TeamSize is
// the default 5.
func TestStartCompetition_AutoDefaultsTeamSize(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "team-default-test"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           compID,
		Name:         "Team Test",
		Kind:         "team",
		Format:       "pools",
		PoolSize:     2,
		PoolSizeMode: "min",
		PoolWinners:  1,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       state.CompStatusSetup,
		TeamSize:     0, // → pipeline default applies (5)
	}))
	saveTestParticipants(t, store, compID, []string{"Team A", "Team B", "Team C", "Team D"})

	require.NoError(t, eng.StartCompetition(compID),
		"team competition with TeamSize=0 should start (pipeline applies default 5; validation must not falsely flag this as drift)")

	stored, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 5, stored.TeamSize, "TeamSize must be defaulted to 5 for team competitions starting with 0")
	assert.Equal(t, state.CompStatusPools, stored.Status)
}

// TestStartCompetition_PreservesExplicitTeamSize pins the "TeamSize
// already set explicitly survives start" path — exercises the merge
// branch where current.TeamSize == loadedTeamSize but != 0, so the
// pipeline's auto-default isn't applied. Pre-fix, the transform
// unconditionally assigned `current.TeamSize = comp.TeamSize` —
// correct for this case (loaded==comp==7) but wrong for the
// concurrent-admin-change case (loaded=0, comp=5, current=9 would
// clobber to 5 instead of preserving admin's 9).
//
// The concurrent-change direction requires engine hooks to simulate
// deterministically; pinning the explicit-TeamSize-no-default path
// at least catches the case where the entire merge block was
// removed by a regression.
func TestStartCompetition_PreservesExplicitTeamSize(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "team-preserve-test"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           compID,
		Name:         "Team Preserve Test",
		Kind:         "team",
		Format:       "pools",
		PoolSize:     2,
		PoolSizeMode: "min",
		PoolWinners:  1,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       state.CompStatusSetup,
		TeamSize:     7,
	}))
	saveTestParticipants(t, store, compID, []string{"Team A", "Team B", "Team C", "Team D"})

	require.NoError(t, eng.StartCompetition(compID))

	stored, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 7, stored.TeamSize,
		"explicit TeamSize=7 must survive start (no auto-default applied)")
}

// TestStartCompetition_PreservesNumberPrefix pins the NumberPrefix /
// StartTime / RoundRobin additions to the StartCompetition transform's
// field-mismatch validation. With these fields in the validation list,
// a happy-path start (no drift) must succeed — these tests are the
// "validation list doesn't reject valid starts" guard. The drift-
// detection direction (concurrent change DOES reject) isn't pinned
// here because reproducing the race deterministically would require
// engine hooks that don't exist. The forward direction is sufficient
// to catch a typo or wrong field reference in the validation block.
func TestStartCompetition_PreservesNumberPrefix(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "prefix-test"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           compID,
		Name:         "Prefix Test",
		Kind:         "individual",
		Format:       "pools",
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		NumberPrefix: "K",
		StartTime:    "10:30",
		Status:       state.CompStatusSetup,
	}))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})

	require.NoError(t, eng.StartCompetition(compID))

	stored, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "K", stored.NumberPrefix, "NumberPrefix must survive the atomic commit")
	assert.Equal(t, "10:30", stored.StartTime, "StartTime must survive the atomic commit")
	assert.True(t, stored.RoundRobin, "RoundRobin must survive the atomic commit")
}
