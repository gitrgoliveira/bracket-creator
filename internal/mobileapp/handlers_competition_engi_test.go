package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// engiShapeRequiredSentence is the refusal both halves share
// (testdata/engi_shape_messages.json; the JS half is
// web-mobile/js/__tests__/competition_shape.node.test.jsx).
func engiShapeRequiredSentence(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "engi_shape_messages.json"))
	require.NoError(t, err)
	var table struct {
		EngiShapeRequired string `json:"engiShapeRequired"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.EngiShapeRequired)
	return table.EngiShapeRequired
}

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

// engiBodyWithout answers a settings PUT body with its engiSolo key taken out.
func engiBodyWithout(t *testing.T, raw []byte) []byte {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	delete(m, "engiSolo")
	body, err := json.Marshal(m)
	require.NoError(t, err)
	return body
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

// The create POST keeps "Single competitors" on an engi competition and
// stores it off on any other: it means something only with engi.
func TestCreateCompetition_EngiSolo(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	solo := engiSoloCreate(t, r, store, state.Competition{
		Name: "Iaido Open", Kind: "individual", Format: state.CompFormatKnockout,
		Date: "12-07-2026", StartTime: "09:00", Engi: true, EngiSolo: true,
	})
	assert.True(t, solo.Engi)
	assert.True(t, solo.EngiSolo, "an engi competition created solo keeps it")

	pairs := engiSoloCreate(t, r, store, state.Competition{
		Name: "Engi Open", Kind: "individual", Format: state.CompFormatKnockout,
		Date: "12-07-2026", StartTime: "09:00", Engi: true, EngiSolo: false,
	})
	assert.True(t, pairs.Engi)
	assert.False(t, pairs.EngiSolo, "an engi competition created with pairs keeps pairs")

	plain := engiSoloCreate(t, r, store, state.Competition{
		Name: "Kendo Open", Kind: "individual", Format: state.CompFormatKnockout,
		Date: "12-07-2026", StartTime: "09:00", EngiSolo: true,
	})
	assert.False(t, plain.Engi)
	assert.False(t, plain.EngiSolo, "solo without engi is cleared on create")
}

// engiCreateRaw POSTs a hand-built create body and answers the recorder.
func engiCreateRaw(t *testing.T, r *gin.Engine, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/competitions", bytes.NewBuffer(raw))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// An engi create must say single competitors or pairs: neither is the default
// (operator decision S1), so a body without engiSolo is refused with the
// sentence the Add competition form shows, and nothing is created. A create
// without engi needs no shape.
func TestCreateCompetition_EngiWithoutAShapeIsRefused(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	base := map[string]any{
		"name": "Engi Cup", "kind": "individual", "format": state.CompFormatKnockout,
		"date": "12-07-2026", "startTime": "09:00", "courts": []string{"A"},
	}
	engi := map[string]any{"engi": true}
	for k, v := range base {
		engi[k] = v
	}
	w := engiCreateRaw(t, r, engi)
	require.Equalf(t, http.StatusBadRequest, w.Code, "%s", w.Body.String())
	var answer struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
	assert.Equal(t, engiShapeRequiredSentence(t), answer.Error)
	ids, err := store.ListCompetitions()
	require.NoError(t, err)
	assert.Empty(t, ids, "a refused create stores nothing")

	w = engiCreateRaw(t, r, base)
	require.Equalf(t, http.StatusCreated, w.Code, "a create without engi needs no shape: %s", w.Body.String())
}

// The settings PUT stores EngiSolo only beside Engi: a save without engi, or
// one turning engi off, clears it. A save that leaves the key out keeps the
// stored shape on an engi competition, and is refused when it turns engi on,
// since neither shape is the default (operator decision S1).
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
	t.Run("a save that leaves the key out keeps it", func(t *testing.T) {
		for _, solo := range []bool{true, false} {
			r, store, _, _, _ := setupTestRouter(t)
			stored := engiSoloSeed(t, store, "solo-omit", solo, state.CompStatusSetup)
			w := cvPut(t, r, stored.ID, engiBodyWithout(t, cvSettingsBody(t, stored, "", map[string]any{"name": "Renamed"})))
			require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
			got, err := store.LoadCompetition(stored.ID)
			require.NoError(t, err)
			assert.True(t, got.Engi)
			assert.Equal(t, "Renamed", got.Name)
			assert.Equal(t, solo, got.EngiSolo, "an omitted engiSolo keeps the stored shape")
		}
	})
	t.Run("turning engi on without the key is refused", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "solo-new", Name: "New", Kind: "individual", Format: state.CompFormatKnockout,
			PoolSize: 3, Courts: []string{"A"},
		}))
		stored, err := store.LoadCompetition("solo-new")
		require.NoError(t, err)
		w := cvPut(t, r, stored.ID, engiBodyWithout(t, cvSettingsBody(t, stored, "", map[string]any{"engi": true})))
		require.Equalf(t, http.StatusBadRequest, w.Code, "%s", w.Body.String())
		assert.Contains(t, w.Body.String(), engiShapeRequiredSentence(t))
		got, err := store.LoadCompetition(stored.ID)
		require.NoError(t, err)
		assert.False(t, got.Engi, "a refused save stores nothing")

		w = cvPut(t, r, stored.ID, cvSettingsBody(t, stored, "", map[string]any{"engi": true, "engiSolo": false}))
		require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
		got, err = store.LoadCompetition(stored.ID)
		require.NoError(t, err)
		assert.True(t, got.Engi)
		assert.False(t, got.EngiSolo, "pairs, chosen, is stored")
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
	assert.Contains(t, w.Body.String(), "competitors per side can only be changed before the draw is made")
	got, err := store.LoadCompetition(stored.ID)
	require.NoError(t, err)
	assert.False(t, got.EngiSolo, "a refused save stores nothing")

	w = cvPut(t, r, stored.ID, cvSettingsBody(t, stored, "", map[string]any{"name": "Iaido Renamed"}))
	require.Equalf(t, http.StatusOK, w.Code, "an echo of the stored setting is not a change: %s", w.Body.String())

	running := engiSoloSeed(t, store, "solo-running", true, state.CompStatusKnockout)
	w = cvPut(t, r, running.ID, cvSettingsBody(t, running, "", map[string]any{"engiSolo": false}))
	require.Equalf(t, http.StatusBadRequest, w.Code, "%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "cannot be changed once the competition has started",
		"a started competition cannot discard its draw, so it is not told to")
	assert.NotContains(t, w.Body.String(), "discard the draw")
	got, err = store.LoadCompetition(running.ID)
	require.NoError(t, err)
	assert.True(t, got.EngiSolo)
}

// A config.md holding engi_solo without engi (hand-edited, or restored) is not
// a change the operator makes: a settings save on a started competition goes
// through and stores it off, instead of refusing every save.
func TestPUTCompetition_EngiSoloStoredWithoutEngiDoesNotBlockSaves(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "solo-drift", Name: "Drift", Kind: "individual", Format: state.CompFormatKnockout,
		PoolSize: 3, Courts: []string{"A"}, EngiSolo: true, Status: state.CompStatusKnockout,
	}))
	stored, err := store.LoadCompetition("solo-drift")
	require.NoError(t, err)
	require.True(t, stored.EngiSolo, "the drifted record is on disk")

	w := cvPut(t, r, stored.ID, cvSettingsBody(t, stored, "", map[string]any{"name": "Drift Renamed"}))
	require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
	got, err := store.LoadCompetition(stored.ID)
	require.NoError(t, err)
	assert.Equal(t, "Drift Renamed", got.Name)
	assert.False(t, got.EngiSolo, "the save corrects the drift")
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

	// A config.md written before the setting (engi, no engi_solo key) loads as
	// pairs, with no migration step.
	legacy := cvConfigBytes(t, dir, "pairs-disk")
	require.Contains(t, string(legacy), "engi: true")
	legacyDir := filepath.Join(dir, "competitions", "legacy-disk")
	require.NoError(t, os.MkdirAll(legacyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "config.md"), legacy, 0o644))
	loaded, err := store.LoadCompetition("legacy-disk")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.True(t, loaded.Engi)
	assert.False(t, loaded.EngiSolo, "an absent engi_solo key reads as pairs")
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
