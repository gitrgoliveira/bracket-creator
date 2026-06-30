package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPUTCompetition_EngiPreservedOnCosmetic pins the fix for Finding 1: a
// cosmetic-only PUT (name change) must not silently reset Engi to false.
// Before the fix, current.Engi was never copied from comp.Engi, so any PUT
// zeroed out the field.
func TestPUTCompetition_EngiPreservedOnCosmetic(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	// Create an engi competition.
	seed := state.Competition{
		ID:       "engi-preserve",
		Name:     "Engi Cup",
		Kind:     "individual",
		Format:   "playoffs",
		PoolSize: 3,
		Courts:   []string{"A"},
		Engi:     true,
	}
	require.NoError(t, store.SaveCompetition(&seed))

	// PUT with a cosmetic name change — Engi is still true in the body.
	update := seed
	update.Name = "Engi Cup Renamed"
	body, _ := json.Marshal(update)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/engi-preserve", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "cosmetic PUT must succeed")

	stored, err := store.LoadCompetition("engi-preserve")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.True(t, stored.Engi, "Engi must remain true after a cosmetic PUT")
	assert.Equal(t, "Engi Cup Renamed", stored.Name)
}

// TestPUTCompetition_EngiCanBeDisabled verifies that a PUT explicitly setting
// Engi=false correctly persists the change (the merge must not freeze the
// field to its stored value — it must copy whatever the request sends).
func TestPUTCompetition_EngiCanBeDisabled(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	seed := state.Competition{
		ID:       "engi-disable",
		Name:     "Engi Cup",
		Kind:     "individual",
		Format:   "playoffs",
		PoolSize: 3,
		Courts:   []string{"A"},
		Engi:     true,
	}
	require.NoError(t, store.SaveCompetition(&seed))

	// PUT with Engi explicitly false.
	update := seed
	update.Engi = false
	body, _ := json.Marshal(update)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/engi-disable", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	stored, err := store.LoadCompetition("engi-disable")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.False(t, stored.Engi, "Engi must be false after explicit PUT with Engi=false")
}
