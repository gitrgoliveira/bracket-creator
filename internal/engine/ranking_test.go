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

func TestGetBracketRanking(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-ranking-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "test-comp"
	comp := &state.Competition{
		ID:   compID,
		Name: "Test Comp",
	}
	require.NoError(t, store.SaveCompetition(comp))

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Charlie", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "M1", SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted},
				{ID: "M2", SideA: "Charlie", SideB: "Dave", Winner: "Charlie", Status: state.MatchStatusCompleted},
			},
			{
				{ID: "M3", SideA: "Alice", SideB: "Charlie", Winner: "Alice", Status: state.MatchStatusCompleted},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	tests := []struct {
		rank     int
		wantName string
		wantErr  bool
	}{
		{rank: 1, wantName: "Alice", wantErr: false},
		{rank: 2, wantName: "Charlie", wantErr: false},
		{rank: 3, wantName: "Bob", wantErr: false},
		{rank: 4, wantName: "Dave", wantErr: false},
		{rank: 5, wantErr: true},
	}

	for _, tt := range tests {
		player, err := eng.GetBracketRanking(compID, tt.rank)
		if tt.wantErr {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, player.Name)
		}
	}
}

func TestGetBracketRanking_Errors(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-ranking-err-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	// No bracket
	_, err = eng.GetBracketRanking("nonexistent", 1)
	assert.Error(t, err)

	// Empty bracket
	compID := "empty"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Empty"}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{}}))
	_, err = eng.GetBracketRanking(compID, 1)
	assert.Error(t, err)
}

// TestGetPoolRanking_Basic verifies that rank 1 returns the winner of
// pool 1, rank 2 the winner of pool 2, rank 3 the runner-up of pool 1, etc.
func TestGetPoolRanking_Basic(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-ranking"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Pool Ranking",
		Format: state.CompFormatMixed,
		Status: state.CompStatusComplete,
	}))

	// Two players so we get one pool and one match.
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
	}))

	// Save pool structure so CalculatePoolStandings has pool info.
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{
			PoolName: "Pool A",
			Players: []helper.Player{
				{Name: "Alice"},
				{Name: "Bob"},
			},
		},
	}))

	// Alice beats Bob.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:      "Pool A-0",
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Alice",
			IpponsA: []string{"M"},
			Status:  state.MatchStatusCompleted,
		},
	}))

	p, err := eng.GetPoolRanking(compID, 1)
	require.NoError(t, err)
	assert.Equal(t, "Alice", p.Name, "rank 1 must be the pool winner")

	p, err = eng.GetPoolRanking(compID, 2)
	require.NoError(t, err)
	assert.Equal(t, "Bob", p.Name, "rank 2 must be the pool runner-up")
}

// TestGetPoolRanking_NotFound verifies that a competition with no pool
// data returns a not-found error.
func TestGetPoolRanking_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-ranking-empty"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Empty",
		Format: state.CompFormatMixed,
	}))

	_, err := eng.GetPoolRanking(compID, 1)
	assert.Error(t, err)
}

// TestGetPoolRanking_OutOfRange verifies that requesting a rank beyond
// the pool's depth returns an error.
func TestGetPoolRanking_OutOfRange(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-ranking-oob"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "OOB",
		Format: state.CompFormatMixed,
		Status: state.CompStatusComplete,
	}))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted},
	}))

	// Pool has 2 players, so rank 100 should not be found.
	_, err := eng.GetPoolRanking(compID, 100)
	assert.Error(t, err)
}

// TestCalculatePoolStandings_TeamSubDraw covers the sub.Winner=="" branch in
// computeStandings (lines 341-343). In a best-of-3 team kendo match each
// position fights individually; a position where both fighters score 2 ippons
// each is impossible in normal play (the bout ends when one side reaches 2)
// but valid to construct in tests to exercise the IndividualDraws counter.
func TestCalculatePoolStandings_TeamSubDraw(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "team-sub-draw"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Team Sub Draw",
		Kind:     "team",
		Format:   state.CompFormatMixed,
		Status:   state.CompStatusPools,
		TeamSize: 3,
	}))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "TeamA"}, {Name: "TeamB"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "TeamA"}, {Name: "TeamB"},
		}},
	}))

	// Team match is a draw (Winner==""), one sub-bout is also a draw:
	// 1-1 ippons with time expired — valid in best-of-3 (neither side
	// reached 2 before the clock ran out).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:     "Pool A-0",
			SideA:  "TeamA",
			SideB:  "TeamB",
			Winner: "",
			Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{
				{Position: 0, SideA: "A1", SideB: "B1",
					IpponsA: []string{"M"}, IpponsB: []string{"M"},
					Winner: ""},
			},
		},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolStandings := standings["Pool A"]
	require.Len(t, poolStandings, 2)

	// Both teams drew the match, each sub-bout is also a draw.
	for _, s := range poolStandings {
		assert.Equalf(t, 1, s.Draws, "%s: team match must be a draw", s.Player.Name)
		assert.Equalf(t, 1, s.IndividualDraws, "%s: sub-bout draw must increment IndividualDraws", s.Player.Name)
	}
}
