package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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
		Format:   "knockout",
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
		Format:   "knockout",
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

// engiSoloCreate POSTs a competition and answers the stored copy.
func engiSoloCreate(t *testing.T, r *gin.Engine, store *state.Store, comp state.Competition) *state.Competition {
	t.Helper()
	body, err := json.Marshal(comp)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusCreated, w.Code, "create: %s", w.Body.String())
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.ID)
	stored, err := store.LoadCompetition(created.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	return stored
}

// engiSoloSeed saves an engi competition at the given status and answers it
// as stored.
func engiSoloSeed(t *testing.T, store *state.Store, id string, solo bool, status state.CompetitionStatus) *state.Competition {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: id, Name: "Iaido " + id, Kind: "individual", Format: state.CompFormatKnockout,
		PoolSize: 3, Courts: []string{"A"}, Engi: true, EngiSolo: solo, Status: status,
	}))
	stored, err := store.LoadCompetition(id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	return stored
}

// The create POST keeps "one competitor per side" on an engi competition and
// stores it off on any other: it means something only with engi.
func TestCreateCompetition_EngiSolo(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	solo := engiSoloCreate(t, r, store, state.Competition{
		Name: "Iaido Open", Kind: "individual", Format: state.CompFormatKnockout,
		Date: "12-07-2026", StartTime: "09:00", Engi: true, EngiSolo: true,
	})
	assert.True(t, solo.Engi)
	assert.True(t, solo.EngiSolo, "an engi competition created solo keeps it")

	plain := engiSoloCreate(t, r, store, state.Competition{
		Name: "Kendo Open", Kind: "individual", Format: state.CompFormatKnockout,
		Date: "12-07-2026", StartTime: "09:00", EngiSolo: true,
	})
	assert.False(t, plain.Engi)
	assert.False(t, plain.EngiSolo, "solo without engi is cleared on create")
}

// The settings PUT stores EngiSolo only beside Engi: a save without engi, or
// one turning engi off, clears it, and a save that leaves the key out stores
// false (an omitted bool decodes as false, which is why the client sends it
// back on every save).
func TestPUTCompetition_EngiSolo(t *testing.T) {
	t.Run("turning it on is stored", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		stored := engiSoloSeed(t, store, "solo-on", false, state.CompStatusSetup)
		w := cvPut(t, r, stored.ID, cvSettingsBody(t, stored, "", map[string]any{"engiSolo": true}))
		require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
		got, err := store.LoadCompetition(stored.ID)
		require.NoError(t, err)
		assert.True(t, got.EngiSolo)
	})
	t.Run("solo without engi is cleared", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "solo-plain", Name: "Plain", Kind: "individual", Format: state.CompFormatKnockout,
			PoolSize: 3, Courts: []string{"A"},
		}))
		stored, err := store.LoadCompetition("solo-plain")
		require.NoError(t, err)
		w := cvPut(t, r, stored.ID, cvSettingsBody(t, stored, "", map[string]any{"engiSolo": true}))
		require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
		got, err := store.LoadCompetition(stored.ID)
		require.NoError(t, err)
		assert.False(t, got.EngiSolo)
	})
	t.Run("turning engi off clears it", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		stored := engiSoloSeed(t, store, "solo-off", true, state.CompStatusSetup)
		w := cvPut(t, r, stored.ID, cvSettingsBody(t, stored, "", map[string]any{"engi": false}))
		require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
		got, err := store.LoadCompetition(stored.ID)
		require.NoError(t, err)
		assert.False(t, got.Engi)
		assert.False(t, got.EngiSolo)
	})
	t.Run("a save that leaves the key out clears it", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		stored := engiSoloSeed(t, store, "solo-omit", true, state.CompStatusSetup)
		raw := cvSettingsBody(t, stored, "", nil)
		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m))
		delete(m, "engiSolo")
		body, err := json.Marshal(m)
		require.NoError(t, err)
		w := cvPut(t, r, stored.ID, body)
		require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
		got, err := store.LoadCompetition(stored.ID)
		require.NoError(t, err)
		assert.True(t, got.Engi)
		assert.False(t, got.EngiSolo, "an omitted engiSolo decodes as false and is stored so")
	})
}

// Locked after the draw like Engi (operator decision D8): once a draw is
// generated the setting is refused with a sentence saying to discard the
// draw, while a save that sends it back unchanged still goes through.
func TestPUTCompetition_EngiSoloLockedAfterTheDraw(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	stored := engiSoloSeed(t, store, "solo-drawn", false, state.CompStatusDrawReady)

	w := cvPut(t, r, stored.ID, cvSettingsBody(t, stored, "", map[string]any{"engiSolo": true}))
	require.Equalf(t, http.StatusBadRequest, w.Code, "%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "one competitor per side can only be changed before the draw is made")
	got, err := store.LoadCompetition(stored.ID)
	require.NoError(t, err)
	assert.False(t, got.EngiSolo, "a refused save stores nothing")

	w = cvPut(t, r, stored.ID, cvSettingsBody(t, stored, "", map[string]any{"name": "Iaido Renamed"}))
	require.Equalf(t, http.StatusOK, w.Code, "an echo of the stored setting is not a change: %s", w.Body.String())

	running := engiSoloSeed(t, store, "solo-running", true, state.CompStatusKnockout)
	w = cvPut(t, r, running.ID, cvSettingsBody(t, running, "", map[string]any{"engiSolo": false}))
	require.Equalf(t, http.StatusBadRequest, w.Code, "%s", w.Body.String())
	got, err = store.LoadCompetition(running.ID)
	require.NoError(t, err)
	assert.True(t, got.EngiSolo)
}

// config.md keeps the setting across a save and a load, writes no key for
// pairs (so a competition stored before the setting existed reads exactly as
// it did), and a config.md with no key reads as pairs.
func TestEngiSolo_ConfigRoundTrip(t *testing.T) {
	_, store, _, _, dir := setupTestRouter(t)

	solo := engiSoloSeed(t, store, "solo-disk", true, state.CompStatusSetup)
	assert.True(t, solo.EngiSolo)
	assert.Contains(t, string(cvConfigBytes(t, dir, "solo-disk")), "engi_solo: true")

	pairs := engiSoloSeed(t, store, "pairs-disk", false, state.CompStatusSetup)
	assert.False(t, pairs.EngiSolo)
	assert.NotContains(t, string(cvConfigBytes(t, dir, "pairs-disk")), "engi_solo",
		"pairs write no key, so a config.md from before the setting is the same file")
}

// The viewer and public competition reads carry engiSolo, which the public
// standings header reads ("Player" for a solo engi table).
func TestEngiSolo_OnTheViewerAndPublicReads(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	stored := engiSoloSeed(t, store, "solo-read", true, state.CompStatusSetup)

	var admin struct {
		EngiSolo bool `json:"engiSolo"`
	}
	require.NoError(t, json.Unmarshal(cvGet(t, r, "/api/competitions/"+stored.ID).Body.Bytes(), &admin))
	assert.True(t, admin.EngiSolo, "GET /api/competitions/:id")

	var single struct {
		Config struct {
			EngiSolo bool `json:"engiSolo"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal(cvGet(t, r, "/api/viewer/competitions/"+stored.ID).Body.Bytes(), &single))
	assert.True(t, single.Config.EngiSolo, "GET /api/viewer/competitions/:id")

	var list []struct {
		Config struct {
			ID       string `json:"id"`
			EngiSolo bool   `json:"engiSolo"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal(cvGet(t, r, "/api/viewer/competitions").Body.Bytes(), &list))
	found := false
	for _, item := range list {
		if item.Config.ID == stored.ID {
			found = true
			assert.True(t, item.Config.EngiSolo, "GET /api/viewer/competitions")
		}
	}
	assert.True(t, found, "the viewer list names the competition")
}
