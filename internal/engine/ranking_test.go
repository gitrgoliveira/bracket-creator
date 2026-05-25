package engine

import (
	"os"
	"path/filepath"
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

func TestResolveReservedSlots(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-slots-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	// Source competition
	srcID := "source-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: srcID, Name: "Source", Status: "completed"}))
	require.NoError(t, store.SaveParticipants(srcID, []domain.Player{{Name: "Winner"}}))
	require.NoError(t, store.SaveBracket(srcID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{{Winner: "Winner", Status: state.MatchStatusCompleted}}},
	}))

	// Target competition
	targetID := "target-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: targetID, Name: "Target"}))
	slots := []state.ReservedSlot{
		{ParticipantID: "P1", SourceCompID: srcID, SourceRank: 1},
	}
	require.NoError(t, store.SaveReservedSlots(targetID, slots))

	players := []domain.Player{
		{ID: "P1", Name: "Placeholder", Tag: "reserved"},
		{ID: "P2", Name: "Normal"},
	}

	resolved, mutated, err := eng.resolveReservedSlots(targetID, players)
	require.NoError(t, err)
	assert.True(t, mutated, "placeholder was updated in place — mutated must be true")
	assert.Len(t, resolved, 2)
	assert.Equal(t, "Winner", resolved[0].Name)
	assert.Equal(t, "", resolved[0].Tag)
	assert.Equal(t, "Normal", resolved[1].Name)
}

func TestResolveReservedSlots_Errors(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-slots-err-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "test"
	players := []domain.Player{{ID: "P1", Tag: "reserved"}}

	// No slots file - should return players unchanged
	res, mutated, err := eng.resolveReservedSlots(compID, players)
	assert.NoError(t, err)
	assert.False(t, mutated, "no slots file → no mutation possible")
	assert.Equal(t, players, res)

	// Slot with missing source competition
	slots := []state.ReservedSlot{{ParticipantID: "P1", SourceCompID: "missing", SourceRank: 1}}
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Test"}))
	require.NoError(t, store.SaveReservedSlots(compID, slots))
	_, _, err = eng.resolveReservedSlots(compID, players)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Source competition not ready
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "not-ready", Status: "setup"}))
	slots[0].SourceCompID = "not-ready"
	require.NoError(t, store.SaveReservedSlots(compID, slots))
	_, _, err = eng.resolveReservedSlots(compID, players)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not reached playoffs yet")
}

// TestResolveReservedSlots_CorruptSlotsFile pins the invariant that a
// genuine LoadReservedSlots I/O / parse failure surfaces as an error
// from resolveReservedSlots rather than being swallowed into a
// "(players, false, nil)" no-op. Pre-fix, a corrupt reserved-slots.json
// caused StartCompetition to proceed past resolution with the
// placeholder "Reserved: rank N" entries left in the players slice —
// the bracket / pool files would be generated with those placeholders
// as real participants. The fix in resolveReservedSlots propagates
// the error; this test injects a corrupt JSON file directly and
// asserts the resolution call surfaces it.
func TestResolveReservedSlots_CorruptSlotsFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-slots-corrupt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "corrupt-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Corrupt"}))

	// Write malformed JSON directly to reserved-slots.json. The file path
	// matches state.Store.compPath(compID, "reserved-slots.json").
	slotsPath := filepath.Join(store.GetFolder(), "competitions", compID, "reserved-slots.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(slotsPath), 0700))
	require.NoError(t, os.WriteFile(slotsPath, []byte("{ not valid json"), 0600))

	players := []domain.Player{{ID: "P1", Name: "Real", Tag: ""}}
	res, mutated, err := eng.resolveReservedSlots(compID, players)
	require.Error(t, err, "corrupt slots file must surface as error, not silent no-op")
	assert.Contains(t, err.Error(), "cannot load reserved slots")
	assert.Nil(t, res, "error path returns nil players")
	assert.False(t, mutated)
}

func TestResolveReservedSlots_Duplicate(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-slots-dup-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	// Source competition with a winner
	srcID := "source-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: srcID, Name: "Source", Status: "completed"}))
	require.NoError(t, store.SaveParticipants(srcID, []domain.Player{{Name: "Robert Young", Dojo: "Team Alpha"}}))
	require.NoError(t, store.SaveBracket(srcID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{{Winner: "Robert Young", Status: state.MatchStatusCompleted}}},
	}))

	// Target competition that ALREADY has "Robert Young"
	targetID := "target-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: targetID, Name: "Target"}))

	// Two players: one real "Robert Young", one placeholder for Rank 1 of source
	players := []domain.Player{
		{ID: "Existing-ID", Name: "Robert Young", Dojo: "Team Alpha"},
		{ID: "Placeholder-ID", Name: "Reserved: source-comp rank 1", Tag: "reserved"},
	}
	require.NoError(t, store.SaveParticipants(targetID, players))

	slots := []state.ReservedSlot{
		{ID: "Slot-ID", ParticipantID: "Placeholder-ID", SourceCompID: srcID, SourceRank: 1},
	}
	require.NoError(t, store.SaveReservedSlots(targetID, slots))

	// Resolve slots
	resolved, mutated, err := eng.resolveReservedSlots(targetID, players)
	require.NoError(t, err)
	assert.True(t, mutated, "placeholder was removed (duplicate-merge path) — mutated must be true")

	// SHOULD now only have 1 player! (the existing one)
	assert.Len(t, resolved, 1)
	assert.Equal(t, "Robert Young", resolved[0].Name)
	assert.Equal(t, "Existing-ID", resolved[0].ID)

	// The reserved slot should have been UPDATED to point to the existing ID
	updatedSlots, err := store.LoadReservedSlots(targetID)
	require.NoError(t, err)
	require.Len(t, updatedSlots, 1)
	assert.Equal(t, "Existing-ID", updatedSlots[0].ParticipantID)
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

// TestResolveReservedSlots_LeagueSource verifies that when the source
// competition has format "league", resolveReservedSlots resolves the
// placeholder via GetPoolRanking (pool standings) rather than
// GetBracketRanking. A league source has no bracket.json, so calling
// GetBracketRanking would return an error; the test confirms the happy
// path succeeds and the placeholder is replaced with the pool winner.
func TestResolveReservedSlots_LeagueSource(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	// League source competition: complete, all players in one pool.
	srcID := "league-source"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     srcID,
		Name:   "League Source",
		Format: state.CompFormatLeague,
		Status: state.CompStatusComplete,
	}))
	require.NoError(t, store.SaveParticipants(srcID, []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Charlie", Dojo: "DojoC"},
	}))
	require.NoError(t, store.SavePools(srcID, []helper.Pool{
		{
			PoolName: "Pool A",
			Players: []helper.Player{
				{Name: "Alice"},
				{Name: "Bob"},
				{Name: "Charlie"},
			},
		},
	}))
	// Alice beats Bob and Charlie — rank 1.
	require.NoError(t, store.SavePoolMatches(srcID, []state.MatchResult{
		{
			ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			Winner: "Alice", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted,
		},
		{
			ID: "Pool A-1", SideA: "Alice", SideB: "Charlie",
			Winner: "Alice", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted,
		},
		{
			ID: "Pool A-2", SideA: "Bob", SideB: "Charlie",
			Winner: "Bob", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted,
		},
	}))

	// Target competition with one reserved slot pointing at rank 1 of the league source.
	targetID := "league-target"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: targetID, Name: "Target"}))
	players := []domain.Player{
		{ID: "placeholder-1", Name: "Reserved: league-source rank 1", Tag: "reserved"},
		{ID: "real-1", Name: "Dave", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveReservedSlots(targetID, []state.ReservedSlot{
		{ParticipantID: "placeholder-1", SourceCompID: srcID, SourceRank: 1},
	}))

	resolved, mutated, err := eng.resolveReservedSlots(targetID, players)
	require.NoError(t, err)
	assert.True(t, mutated)
	require.Len(t, resolved, 2)

	// Placeholder should now hold Alice's name (pool ranking doesn't
	// re-resolve full participant data, so Dojo is not checked here).
	assert.Equal(t, "Alice", resolved[0].Name)
	assert.Equal(t, "", resolved[0].Tag, "resolved placeholder must not retain 'reserved' tag")
	assert.Equal(t, "Dave", resolved[1].Name)
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
		assert.Equal(t, 1, s.Draws, "%s: team match must be a draw", s.Player.Name)
		assert.Equal(t, 1, s.IndividualDraws, "%s: sub-bout draw must increment IndividualDraws", s.Player.Name)
	}
}
