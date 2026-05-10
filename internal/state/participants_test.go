package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParticipants(t *testing.T) {
	dir, err := os.MkdirTemp("", "participants-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "comp-participants"
	err = os.MkdirAll(filepath.Join(dir, "competitions", compID), 0700)
	require.NoError(t, err)

	// 1. Load empty participants (doesn't exist)
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	assert.Empty(t, players)

	// 2. Save participants
	playersToSave := []helper.Player{
		{Name: "Alice", Dojo: "Dojo A", Tag: "manual"},
		{Name: "Bob", Dojo: "Dojo B"},
	}
	err = store.SaveParticipants(compID, playersToSave)
	require.NoError(t, err)

	// 3. Load participants
	loadedPlayers, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedPlayers, 2)
	assert.NotEmpty(t, loadedPlayers[0].ID) // UUID generated
	assert.Equal(t, "Alice", loadedPlayers[0].Name)
	assert.Equal(t, "ALICE", loadedPlayers[0].DisplayName)
	assert.Equal(t, "Dojo A", loadedPlayers[0].Dojo)
	assert.Equal(t, "manual", loadedPlayers[0].Tag)

	assert.NotEmpty(t, loadedPlayers[1].ID) // UUID generated
	assert.Equal(t, "Bob", loadedPlayers[1].Name)
	assert.Equal(t, "BOB", loadedPlayers[1].DisplayName)
	assert.Equal(t, "Dojo B", loadedPlayers[1].Dojo)
	assert.Empty(t, loadedPlayers[1].Tag)

	// 4. Save and load participants with existing IDs
	playersToSaveWithID := []helper.Player{
		{ID: "00000000-0000-4000-8000-000000000000", Name: "Charlie", Dojo: "Dojo C"},
	}
	err = store.SaveParticipants(compID, playersToSaveWithID)
	require.NoError(t, err)

	loadedPlayersWithID, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedPlayersWithID, 1)
	assert.Equal(t, "00000000-0000-4000-8000-000000000000", loadedPlayersWithID[0].ID)
	assert.Equal(t, "Charlie", loadedPlayersWithID[0].Name)

	// 5. Test merging seeds
	seedsPath := filepath.Join(dir, "competitions", compID, "seeds.csv")
	err = os.WriteFile(seedsPath, []byte("Name,Rank\nCharlie,1\n"), 0600)
	require.NoError(t, err)

	loadedWithSeeds, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedWithSeeds, 1)
	assert.Equal(t, 1, loadedWithSeeds[0].Seed)

	// 6. Test with old format (no IDs)
	participantsPath := filepath.Join(dir, "competitions", compID, "participants.csv")
	err = os.WriteFile(participantsPath, []byte("Dave, Dojo D\nEve, Dojo E\n"), 0600)
	require.NoError(t, err)

	loadedOldFormat, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedOldFormat, 2)
	assert.Empty(t, loadedOldFormat[0].ID) // No UUID
	assert.Equal(t, "Dave", loadedOldFormat[0].Name)
	assert.Empty(t, loadedOldFormat[1].ID) // No UUID
	assert.Equal(t, "Eve", loadedOldFormat[1].Name)
}

func TestParticipantsWithZekkenNameRoundTrip(t *testing.T) {
	// Regression: SaveParticipants writes 2 columns when DisplayName==Name or is empty.
	// LoadParticipants with withZekkenName=true must tolerate this and not error.
	dir, err := os.MkdirTemp("", "participants-zekken-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "comp-zekken"
	err = os.MkdirAll(filepath.Join(dir, "competitions", compID), 0700)
	require.NoError(t, err)

	// Players where DisplayName is empty (will be omitted by SaveParticipants → 2-col row)
	playersToSave := []helper.Player{
		{Name: "Alice Smith", Dojo: "Dojo A"},                         // no DisplayName
		{Name: "Bob Jones", DisplayName: "Bob Jones", Dojo: "Dojo B"}, // DisplayName == Name
		{Name: "Carol", DisplayName: "C. CAROL", Dojo: "Dojo C"},      // distinct DisplayName
	}
	err = store.SaveParticipants(compID, playersToSave)
	require.NoError(t, err)

	// Loading with withZekkenName=true must succeed (no "validation failed" error)
	loaded, err := store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, loaded, 3)

	// Alice: 2-col row → DisplayName derived from Name
	assert.Equal(t, "Alice Smith", loaded[0].Name)
	assert.NotEmpty(t, loaded[0].DisplayName)
	assert.Equal(t, "Dojo A", loaded[0].Dojo)

	// Bob: 2-col row (DisplayName == Name) → DisplayName derived from Name
	assert.Equal(t, "Bob Jones", loaded[1].Name)
	assert.NotEmpty(t, loaded[1].DisplayName)
	assert.Equal(t, "Dojo B", loaded[1].Dojo)

	// Carol: 3-col row → DisplayName preserved
	assert.Equal(t, "Carol", loaded[2].Name)
	assert.Equal(t, "C. CAROL", loaded[2].DisplayName)
	assert.Equal(t, "Dojo C", loaded[2].Dojo)
}
