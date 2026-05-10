package engine

import (
	"os"
	"testing"

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

	players := []helper.Player{
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
	require.NoError(t, store.SaveParticipants(srcID, []helper.Player{{Name: "Winner"}}))
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

	players := []helper.Player{
		{ID: "P1", Name: "Placeholder", Tag: "reserved"},
		{ID: "P2", Name: "Normal"},
	}

	resolved, err := eng.resolveReservedSlots(targetID, players)
	require.NoError(t, err)
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
	players := []helper.Player{{ID: "P1", Tag: "reserved"}}

	// No slots file - should return players unchanged
	res, err := eng.resolveReservedSlots(compID, players)
	assert.NoError(t, err)
	assert.Equal(t, players, res)

	// Slot with missing source competition
	slots := []state.ReservedSlot{{ParticipantID: "P1", SourceCompID: "missing", SourceRank: 1}}
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Test"}))
	require.NoError(t, store.SaveReservedSlots(compID, slots))
	_, err = eng.resolveReservedSlots(compID, players)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Source competition not ready
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "not-ready", Status: "setup"}))
	slots[0].SourceCompID = "not-ready"
	require.NoError(t, store.SaveReservedSlots(compID, slots))
	_, err = eng.resolveReservedSlots(compID, players)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not reached playoffs yet")
}
