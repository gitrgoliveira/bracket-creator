package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
)

func TestStaleSeedsBug(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "stale-seeds-comp"

	// 1. Create competition with seeds
	compWithSeeds := state.Competition{
		ID:     compID,
		Name:   "Seeded Competition",
		Format: "playoffs",
		Courts: []string{"Court A"},
		Players: []helper.Player{
			{Name: "Alice", Seed: 1, Dojo: "Dojo A"},
			{Name: "Bob", Seed: 2, Dojo: "Dojo B"},
		},
	}
	body, _ := json.Marshal(compWithSeeds)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Verify seeds were saved to disk
	seeds, err := store.LoadSeeds(compID)
	assert.NoError(t, err)
	assert.Len(t, seeds, 2, "Expected 2 seeds to be saved")

	// 2. Update competition with players but NO seeds
	compNoSeeds := state.Competition{
		ID:     compID,
		Name:   "Seeded Competition",
		Format: "playoffs",
		Courts: []string{"Court A"},
		Players: []helper.Player{
			{Name: "Alice", Seed: 0, Dojo: "Dojo A"},
			{Name: "Bob", Seed: 0, Dojo: "Dojo B"},
			{Name: "Charlie", Seed: 0, Dojo: "Dojo C"},
		},
	}
	body, _ = json.Marshal(compNoSeeds)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify seeds were CLEARED from disk
	seeds, err = store.LoadSeeds(compID)
	assert.NoError(t, err)
	assert.Len(t, seeds, 0, "Expected 0 seeds after clearing them in the participant list")

	// 3. Verify starting the competition doesn't fail with "participant not found"
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/"+compID+"/start", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Competition should start successfully after seeds are cleared")
}
