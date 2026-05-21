package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSingleParticipantAddAndReplace(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-test-p"
	comp := state.Competition{
		ID:     compID,
		Name:   "Test Competition",
		Status: state.CompStatusSetup,
	}
	err := store.SaveCompetition(&comp)
	require.NoError(t, err)

	// 1. POST single participant (happy path)
	payload := map[string]interface{}{
		"name":        "Test Player",
		"displayName": "T. Player",
		"dojo":        "Test Dojo",
		"danGrade":    "3 Dan",
	}
	bodyBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var addedPlayer domain.Player
	err = json.Unmarshal(w.Body.Bytes(), &addedPlayer)
	require.NoError(t, err)

	assert.NotEmpty(t, addedPlayer.ID)
	// Bead spec + project convention: minted participant IDs must be UUIDv4
	// so the format-sniffer in loadParticipantsNoLock stays on a single
	// contract. Lock the format here to catch a future "compID-pX" regression.
	assert.True(t, helper.IsUUIDv4(addedPlayer.ID),
		"AddParticipant must mint UUIDv4 IDs, got %q", addedPlayer.ID)
	assert.Equal(t, "Test Player", addedPlayer.Name)
	assert.Equal(t, "T. Player", addedPlayer.DisplayName)
	assert.Equal(t, "Test Dojo", addedPlayer.Dojo)
	assert.Equal(t, []string{"3 Dan"}, addedPlayer.Metadata)

	// Verify player is stored in participants.csv
	storedPlayers, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, storedPlayers, 1)
	assert.Equal(t, addedPlayer.ID, storedPlayers[0].ID)

	// 2. PUT replace participant (happy path)
	replacePayload := map[string]interface{}{
		"name":        "Updated Player Name",
		"displayName": "U. Player",
		"dojo":        "Updated Dojo",
		"danGrade":    "4 Dan",
	}
	replaceBytes, _ := json.Marshal(replacePayload)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+addedPlayer.ID, bytes.NewBuffer(replaceBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var updatedPlayer domain.Player
	err = json.Unmarshal(w.Body.Bytes(), &updatedPlayer)
	require.NoError(t, err)

	assert.Equal(t, addedPlayer.ID, updatedPlayer.ID)
	assert.Equal(t, "Updated Player Name", updatedPlayer.Name)
	assert.Equal(t, "U. Player", updatedPlayer.DisplayName)
	assert.Equal(t, "Updated Dojo", updatedPlayer.Dojo)
	assert.Equal(t, []string{"4 Dan"}, updatedPlayer.Metadata)

	// 3. Test 409 Conflict when started
	startedComp := comp
	startedComp.Status = state.CompStatusPools
	err = store.SaveCompetition(&startedComp)
	require.NoError(t, err)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+addedPlayer.ID, bytes.NewBuffer(replaceBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	// 4. Test 404 when player does not exist (change status back to setup first)
	startedComp.Status = state.CompStatusSetup
	err = store.SaveCompetition(&startedComp)
	require.NoError(t, err)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/nonexistent-id", bytes.NewBuffer(replaceBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSeedRenamingUnderReplace(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-seed-rename"
	comp := state.Competition{
		ID:     compID,
		Name:   "Seed Rename Test",
		Status: state.CompStatusSetup,
	}
	err := store.SaveCompetition(&comp)
	require.NoError(t, err)

	// 1. Add participant
	player := domain.Player{
		Name: "Alice",
		Dojo: "Original Dojo",
	}
	added, err := store.AddParticipant(compID, player, false)
	require.NoError(t, err)

	// 2. Set seed for Alice
	seeds := []domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1},
	}
	err = store.SaveSeeds(compID, seeds)
	require.NoError(t, err)

	// 3. PUT replace participant renaming Alice -> Alice Cooper
	replacePayload := map[string]interface{}{
		"name": "Alice Cooper",
		"dojo": "Cooper Dojo",
	}
	replaceBytes, _ := json.Marshal(replacePayload)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+added.ID, bytes.NewBuffer(replaceBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// 4. Verify seed is renamed in seeds.csv
	storedSeeds, err := store.LoadSeeds(compID)
	require.NoError(t, err)
	require.Len(t, storedSeeds, 1)
	assert.Equal(t, "Alice Cooper", storedSeeds[0].Name)
}

// TestDuplicateNameRejection pins the bead acceptance criterion that
// add/replace must reject a name already in the roster with 409. Without
// the guard, name-keyed lookups (seeds, lineups) would silently key on
// whichever row happens to come first in the CSV.
func TestDuplicateNameRejection(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-dup-name"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Dup Name Test",
		Status: state.CompStatusSetup,
	}))

	// Seed two participants directly via the store helper.
	first, err := store.AddParticipant(compID, domain.Player{Name: "Alice", Dojo: "Dojo A"}, false)
	require.NoError(t, err)
	_, err = store.AddParticipant(compID, domain.Player{Name: "Bob", Dojo: "Dojo B"}, false)
	require.NoError(t, err)

	// 1. POST add duplicate → 409.
	dupAdd, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo X"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(dupAdd))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code, "POST add of duplicate name must return 409")

	// 2. PUT replace of Bob renaming to Alice → 409.
	dupReplace, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo B"})
	bobID := ""
	for _, p := range mustLoad(t, store, compID) {
		if p.Name == "Bob" {
			bobID = p.ID
			break
		}
	}
	require.NotEmpty(t, bobID)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+bobID, bytes.NewBuffer(dupReplace))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code, "PUT replace renaming Bob→Alice must return 409")

	// 3. PUT renaming Alice to her OWN current name is a no-op rename and must succeed.
	sameName, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo A2"})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+first.ID, bytes.NewBuffer(sameName))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "PUT with unchanged name (dojo edit) must succeed")
}

func mustLoad(t *testing.T, store *state.Store, compID string) []domain.Player {
	t.Helper()
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	return players
}
