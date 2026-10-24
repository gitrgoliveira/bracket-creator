package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The direct doors bind the optional {writeId, basis} (bc-evts F1): a resend
// of one the match's score event log holds is answered as the first was, and
// a malformed tag is a 400.
func TestDirectDoors_BindTheWriteTag(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "dd1", Status: "setup", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("dd1", []state.MatchResult{
		{ID: "run-1", SideA: "P1", SideB: "P2", Status: state.MatchStatusRunning},
	}))
	post := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	const path = "/api/competitions/dd1/matches/run-1/revert-to-queue"
	tagged := `{"writeId":"w-1","basis":{"seen":0}}`
	require.Equal(t, http.StatusOK, post(path, tagged).Code)
	lines, err := store.LoadScoreLog("dd1", "run-1")
	require.NoError(t, err)
	require.NotEmpty(t, lines)
	assert.Equal(t, "w-1", lines[len(lines)-1].Baseline.WriteID)
	history, err := store.LoadMatchHistory("dd1", "run-1")
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, post(path, tagged).Code, "a resend is answered as the first was")
	again, err := store.LoadMatchHistory("dd1", "run-1")
	require.NoError(t, err)
	assert.Len(t, again, len(history), "and leaves no history line")

	w := post(path, `{"writeId":"`+strings.Repeat("x", 200)+`"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "writeId")
	assert.Equal(t, http.StatusBadRequest, post("/api/competitions/dd1/matches/run-1/reopen", `{"basis":{"seen":-1}}`).Code)
	assert.Equal(t, http.StatusBadRequest, post("/api/competitions/dd1/matches/run-1/requeue-blocker-and-reopen", `{"writeId":"`+strings.Repeat("x", 200)+`"}`).Code)
}
