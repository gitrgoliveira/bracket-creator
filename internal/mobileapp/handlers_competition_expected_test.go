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

// The expected entry count (expectedParticipants) is accepted by the create
// POST and the settings PUT, 0 to engine.MaxEstimateParticipants, and is not
// output-affecting: the draw never reads it, so a draw-ready competition takes
// it without the discard-the-draw 409.

const expectedParticipantsSentence = "Expected participants must be a whole number from 0 to 1024."

func sendCompetitionJSON(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(method, path, bytes.NewBuffer(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func expectedKnockoutBody(id string, extra map[string]any) map[string]any {
	body := map[string]any{
		"id":     id,
		"name":   "Expected " + id,
		"format": state.CompFormatKnockout,
		"kind":   "individual",
		"courts": []string{"A"},
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func saveExpectedKnockout(t *testing.T, store *state.Store, id string, status state.CompetitionStatus, expected int) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   id,
		Name:                 "Expected " + id,
		Format:               state.CompFormatKnockout,
		Kind:                 "individual",
		Courts:               []string{"A"},
		Status:               status,
		ExpectedParticipants: expected,
	}))
}

func loadExpectedParticipants(t *testing.T, store *state.Store, id string) int {
	t.Helper()
	comp, err := store.LoadCompetition(id)
	require.NoError(t, err)
	require.NotNil(t, comp)
	return comp.ExpectedParticipants
}

func TestPOSTCompetition_ExpectedParticipantsStored(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	w := sendCompetitionJSON(t, r, "POST", "/api/competitions",
		expectedKnockoutBody("post-expected", map[string]any{"expectedParticipants": 40}))
	require.Equalf(t, http.StatusCreated, w.Code, "response: %s", w.Body.String())
	assert.Equal(t, 40, loadExpectedParticipants(t, store, "post-expected"))
}

func TestPOSTCompetition_ExpectedParticipantsOutOfRange(t *testing.T) {
	for _, n := range []int{-1, 1025} {
		r, store, _, _, _ := setupTestRouter(t)
		w := sendCompetitionJSON(t, r, "POST", "/api/competitions",
			expectedKnockoutBody("post-out-of-range", map[string]any{"expectedParticipants": n}))
		assert.Equalf(t, http.StatusBadRequest, w.Code, "expectedParticipants %d: %s", n, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, expectedParticipantsSentence, resp["error"], "the operator reads the sentence as it stands")
		comp, err := store.LoadCompetition("post-out-of-range")
		require.NoError(t, err)
		assert.Nil(t, comp, "a refused create stores nothing")
	}
}

func TestPUTCompetition_ExpectedParticipants(t *testing.T) {
	t.Run("stores the count", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveExpectedKnockout(t, store, "put-expected", state.CompStatusSetup, 0)
		w := sendCompetitionJSON(t, r, "PUT", "/api/competitions/put-expected",
			expectedKnockoutBody("put-expected", map[string]any{"expectedParticipants": 24}))
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
		assert.Equal(t, 24, loadExpectedParticipants(t, store, "put-expected"))
	})

	t.Run("an explicit 0 clears it", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveExpectedKnockout(t, store, "put-clear", state.CompStatusSetup, 24)
		w := sendCompetitionJSON(t, r, "PUT", "/api/competitions/put-clear",
			expectedKnockoutBody("put-clear", map[string]any{"expectedParticipants": 0}))
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
		assert.Equal(t, 0, loadExpectedParticipants(t, store, "put-clear"))
	})

	t.Run("a body without the key keeps the stored count", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveExpectedKnockout(t, store, "put-omit", state.CompStatusSetup, 24)
		w := sendCompetitionJSON(t, r, "PUT", "/api/competitions/put-omit",
			expectedKnockoutBody("put-omit", nil))
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
		assert.Equal(t, 24, loadExpectedParticipants(t, store, "put-omit"))
	})

	t.Run("out of range is a 400 with the sentence", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveExpectedKnockout(t, store, "put-range", state.CompStatusSetup, 24)
		w := sendCompetitionJSON(t, r, "PUT", "/api/competitions/put-range",
			expectedKnockoutBody("put-range", map[string]any{"expectedParticipants": 2000}))
		assert.Equalf(t, http.StatusBadRequest, w.Code, "response: %s", w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, expectedParticipantsSentence, resp["error"])
		assert.Equal(t, 24, loadExpectedParticipants(t, store, "put-range"), "a refused save keeps the stored count")
	})

	t.Run("draw ready takes it without the 409", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveExpectedKnockout(t, store, "put-draw-ready", state.CompStatusDrawReady, 0)
		w := sendCompetitionJSON(t, r, "PUT", "/api/competitions/put-draw-ready",
			expectedKnockoutBody("put-draw-ready", map[string]any{"expectedParticipants": 40}))
		require.Equalf(t, http.StatusOK, w.Code,
			"expectedParticipants is not output-affecting, so a draw-ready save of it alone must not 409: %s", w.Body.String())
		assert.Equal(t, 40, loadExpectedParticipants(t, store, "put-draw-ready"))
		comp, err := store.LoadCompetition("put-draw-ready")
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, comp.Status, "the draw stands")
	})
}

func TestGETScheduleEstimate_ExpectedParticipants(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                           "estimate-expected",
		Name:                         "Estimate Expected",
		Format:                       state.CompFormatKnockout,
		Kind:                         "individual",
		Courts:                       []string{"A"},
		StartTime:                    "09:00",
		KnockoutMatchDurationSeconds: 300,
		Status:                       state.CompStatusSetup,
		ExpectedParticipants:         40,
	}))

	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/api/competitions/estimate-expected/schedule/estimate", nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.EqualValues(t, 40, resp["participants"], "the estimate names the count it used: %s", w.Body.String())
	assert.Equal(t, "expected", resp["participantsSource"])
	assert.Greater(t, resp["totalDurationMinutes"], float64(0), "an expected count with no roster still gives a real length")
}
