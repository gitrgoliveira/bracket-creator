package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_ReservedSlots_RoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	slots := []ReservedSlot{
		{ID: "slot1", ParticipantID: "part1", SourceCompID: "source1", SourceRank: 1},
		{ID: "slot2", ParticipantID: "part2", SourceCompID: "source1", SourceRank: 2},
	}

	err = store.SaveReservedSlots(compID, slots)
	require.NoError(t, err)

	loaded, err := store.LoadReservedSlots(compID)
	require.NoError(t, err)
	assert.Equal(t, slots, loaded)
}

func TestStore_ReservedSlots_NotExists(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := store.LoadReservedSlots("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestStore_AddRemoveReservedSlot(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Add a slot
	slot, err := store.AddReservedSlot(compID, "source-comp", 1, false)
	require.NoError(t, err)
	assert.NotNil(t, slot)
	assert.NotEmpty(t, slot.ID)
	assert.NotEmpty(t, slot.ParticipantID)
	assert.Equal(t, "source-comp", slot.SourceCompID)
	assert.Equal(t, 1, slot.SourceRank)

	// Verify participants updated
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, slot.ParticipantID, players[0].ID)
	assert.Contains(t, strings.ToLower(players[0].Name), "source-comp")
	assert.Equal(t, "reserved", players[0].Tag)

	// Add another slot
	slot2, err := store.AddReservedSlot(compID, "source-comp", 2, false)
	require.NoError(t, err)
	assert.NotEqual(t, slot.ID, slot2.ID)

	players, err = store.LoadParticipants(compID, false)
	require.NoError(t, err)
	assert.Len(t, players, 2)

	// Remove first slot
	err = store.RemoveReservedSlot(compID, slot.ID, false)
	require.NoError(t, err)

	// Verify remaining
	slots, err := store.LoadReservedSlots(compID)
	require.NoError(t, err)
	require.Len(t, slots, 1)
	assert.Equal(t, slot2.ID, slots[0].ID)

	players, err = store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, slot2.ParticipantID, players[0].ID)
}

func TestStore_RemoveReservedSlot_NotFound(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	err = store.RemoveReservedSlot("comp", "nonexistent-slot", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStore_ReservedSlots_LoadParticipantsLocked_WithSeeds(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "seeded-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Create participants.csv manually to test loadParticipantsLocked via AddReservedSlot
	participantsPath := filepath.Join(dir, "competitions", compID, "participants.csv")
	err = os.WriteFile(participantsPath, []byte("550e8400-e29b-41d4-a716-446655440000, Alice, DojoA\n"), 0600)
	require.NoError(t, err)

	// Create seeds.csv
	seedsPath := filepath.Join(dir, "competitions", compID, "seeds.csv")
	err = os.WriteFile(seedsPath, []byte("Name, Rank\nAlice, 1\n"), 0600)
	require.NoError(t, err)

	// AddReservedSlot will trigger loadParticipantsLocked
	slot, err := store.AddReservedSlot(compID, "source", 1, false)
	require.NoError(t, err)

	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, players, 2)

	// Alice should have seed 1
	for _, p := range players {
		if p.Name == "Alice" {
			assert.Equal(t, 1, p.Seed)
		}
	}
	assert.NotNil(t, slot)
}
