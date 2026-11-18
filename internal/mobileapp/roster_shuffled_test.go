package mobileapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shuffle marker (state.Competition.RosterShuffled): only a roster PUT
// that says rosterShuffled and whose roster lands records it; nothing else
// sets or clears it.

func shuffleServe(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body == nil {
		buf = &bytes.Buffer{}
	} else {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		buf = bytes.NewBuffer(b)
	}
	req, err := http.NewRequest(method, path, buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// newShuffleComp creates a competition with six competitors through POST and
// answers its stored roster.
func newShuffleComp(t *testing.T, r *gin.Engine, store *state.Store, id string, extra map[string]any) []domain.Player {
	t.Helper()
	players := make([]map[string]any, 6)
	for i := range players {
		players[i] = map[string]any{"name": fmt.Sprintf("Fighter %c", 'A'+i), "dojo": fmt.Sprintf("Dojo %d", i+1)}
	}
	body := map[string]any{
		"id": id, "name": "Shuffle " + id, "kind": "individual", "format": state.CompFormatKnockout,
		"courts": []string{"A"}, "players": players,
	}
	for k, v := range extra {
		body[k] = v
	}
	w := shuffleServe(t, r, http.MethodPost, "/api/competitions", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	stored, err := store.LoadParticipants(id, true)
	require.NoError(t, err)
	require.Len(t, stored, 6)
	return stored
}

func reversed(ps []domain.Player) []domain.Player {
	out := make([]domain.Player, len(ps))
	for i, p := range ps {
		out[len(ps)-1-i] = p
	}
	return out
}

// rosterPut sends the participants page's roster PUT: the stored competition
// with the players, and rosterShuffled when the shuffle sent it.
func rosterPut(t *testing.T, r *gin.Engine, store *state.Store, id string, players []domain.Player, shuffled bool, rosterVersion string) *httptest.ResponseRecorder {
	t.Helper()
	comp, err := store.LoadCompetition(id)
	require.NoError(t, err)
	comp.Players = players
	comp.RosterShuffled = shuffled
	comp.RosterVersion = rosterVersion
	return shuffleServe(t, r, http.MethodPut, "/api/competitions/"+id, comp)
}

func storedShuffled(t *testing.T, store *state.Store, id string) bool {
	t.Helper()
	comp, err := store.LoadCompetition(id)
	require.NoError(t, err)
	return comp.RosterShuffled
}

func TestRosterShuffled_CreateNeverSetsIt(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	newShuffleComp(t, r, store, "create-shuffled", map[string]any{"rosterShuffled": true})
	assert.False(t, storedShuffled(t, store, "create-shuffled"), "a new competition's list was never shuffled, whatever the body says")
}

func TestRosterShuffled_ShufflePutRecordsItAndAnswersIt(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	const id = "shuffle-records"
	players := newShuffleComp(t, r, store, id, nil)
	before, err := store.LoadCompetition(id)
	require.NoError(t, err)
	versionBefore := state.ConfigVersion(before)

	w := rosterPut(t, r, store, id, reversed(players), true, state.RosterVersion(players))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, storedShuffled(t, store, id), "the shuffle's roster PUT records the marker")

	var answer struct {
		RosterShuffled bool   `json:"rosterShuffled"`
		ConfigVersion  string `json:"configVersion"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
	assert.True(t, answer.RosterShuffled, "the answer carries the marker so the page shows it at once")
	assert.Equal(t, versionBefore, answer.ConfigVersion, "a shuffle is not a setting: an open Settings page is not refused config_changed")

	stored, err := store.LoadParticipants(id, true)
	require.NoError(t, err)
	assert.Equal(t, players[5].ID, stored[0].ID, "the shuffled order landed")
}

func TestRosterShuffled_RosterPutWithoutItLeavesItUnset(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	const id = "plain-roster"
	players := newShuffleComp(t, r, store, id, nil)
	w := rosterPut(t, r, store, id, reversed(players), false, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, storedShuffled(t, store, id), "a roster save that is not a shuffle records nothing")
}

func TestRosterShuffled_RefusedRosterRecordsNothing(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("roster_changed", func(t *testing.T) {
		const id = "stale-roster"
		players := newShuffleComp(t, r, store, id, nil)
		w := rosterPut(t, r, store, id, reversed(players), true, "0000000000000000")
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "roster_changed")
		assert.False(t, storedShuffled(t, store, id), "a refused roster records no shuffle")
	})

	t.Run("validation error", func(t *testing.T) {
		const id = "invalid-roster"
		players := newShuffleComp(t, r, store, id, nil)
		bad := reversed(players)
		bad[0].Dojo = ""
		w := rosterPut(t, r, store, id, bad, true, "")
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.False(t, storedShuffled(t, store, id), "a roster refused for a blank dojo records no shuffle")
	})

	t.Run("empty roster", func(t *testing.T) {
		const id = "empty-roster"
		newShuffleComp(t, r, store, id, nil)
		w := rosterPut(t, r, store, id, []domain.Player{}, true, "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.False(t, storedShuffled(t, store, id), "a PUT carrying no players records no shuffle")
	})
}

func TestRosterShuffled_SettingsSaveNeitherSetsNorClearsIt(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	const id = "settings-merge"
	players := newShuffleComp(t, r, store, id, nil)

	settings := func(shuffled bool, name string) {
		t.Helper()
		comp, err := store.LoadCompetition(id)
		require.NoError(t, err)
		comp.Players = nil
		comp.Name = name
		comp.RosterShuffled = shuffled
		w := shuffleServe(t, r, http.MethodPut, "/api/competitions/"+id, comp)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	settings(true, "Renamed once")
	assert.False(t, storedShuffled(t, store, id), "a Settings save never sets the marker")

	w := rosterPut(t, r, store, id, reversed(players), true, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, storedShuffled(t, store, id))

	settings(false, "Renamed twice")
	assert.True(t, storedShuffled(t, store, id), "a Settings save never clears the marker")
}

func TestRosterShuffled_OnceSetItStaysSet(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	const id = "stays-set"
	players := newShuffleComp(t, r, store, id, nil)
	w := rosterPut(t, r, store, id, reversed(players), true, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, storedShuffled(t, store, id))

	// Every later roster PUT echoes the page's rosterShuffled: an echo of true
	// on a marker already set changes nothing.
	w = rosterPut(t, r, store, id, players, true, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, storedShuffled(t, store, id), "an echo of true keeps the marker")

	// A later roster edit that says nothing of it does not clear it.
	w = rosterPut(t, r, store, id, players[:5], false, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, storedShuffled(t, store, id), "a later roster edit keeps the marker")

	// Neither does a draw generated and discarded.
	w = shuffleServe(t, r, http.MethodPost, "/api/competitions/"+id+"/generate-draw", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = shuffleServe(t, r, http.MethodDelete, "/api/competitions/"+id+"/draw", nil)
	require.Less(t, w.Code, 300, w.Body.String())
	comp, err := store.LoadCompetition(id)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusSetup, comp.Status, "the draw was discarded")
	assert.True(t, comp.RosterShuffled, "discarding the draw keeps the marker")
}

func TestRosterShuffled_MarkerFailureAnswers500(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	const id = "marker-fails"
	players := newShuffleComp(t, r, store, id, nil)

	orig := markRosterShuffled
	t.Cleanup(func() { markRosterShuffled = orig })
	markRosterShuffled = func(*state.Store, string) error { return errors.New("disk refused config.md") }

	w := rosterPut(t, r, store, id, reversed(players), true, "")
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Shuffle again to record it.")
	assert.NotContains(t, w.Body.String(), "disk refused", "the cause is logged, not answered")
	assert.False(t, storedShuffled(t, store, id))

	stored, err := store.LoadParticipants(id, true)
	require.NoError(t, err)
	assert.Equal(t, players[5].ID, stored[0].ID, "the roster itself landed")
}
