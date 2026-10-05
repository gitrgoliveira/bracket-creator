package mobileapp

// bc-rfsw: a knockout correction whose new winner would change a side of a
// later match being fought now is refused on every door with 409
// downstream_knockout_running, naming the match and its shiaijo, and
// forceDownstreamReopen does not get past it (operator decision 2026-09-27).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const runningFinalSentence = "Match 2 (Final) is being fought now on Shiaijo A. Finish it or send it back to the queue, then save this correction again."

// seedRunningFinal: m-r1-0 (Shiaijo B) won by Alice, and m-r2-0, the final it
// feeds, on Shiaijo A right now with one ippon struck and no verdict.
func seedRunningFinal(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "rfsw-http", Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}, MatchNumber: 1, DisplayRound: 2, Court: "B"},
			},
			{
				{ID: "m-r2-0", SideA: "Alice", SideB: "Charlie", SideAID: "alice", SideBID: "charlie",
					Status: state.MatchStatusRunning, IpponsB: []string{"K"},
					MatchNumber: 2, DisplayRound: 1, Court: "A"},
			},
		},
	}))
}

func assertRunningRefusal(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var resp struct {
		Error          string           `json:"error"`
		MatchID        string           `json:"matchId"`
		Message        string           `json:"message"`
		RunningMatches []map[string]any `json:"runningMatches"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "downstream_knockout_running", resp.Error)
	assert.Equal(t, "m-r1-0", resp.MatchID)
	assert.Equal(t, runningFinalSentence, resp.Message)
	require.Len(t, resp.RunningMatches, 1)
	assert.Equal(t, "m-r2-0", resp.RunningMatches[0]["id"])
	assert.Equal(t, "Match 2 (Final)", resp.RunningMatches[0]["label"])
	assert.Equal(t, "A", resp.RunningMatches[0]["court"])
}

func assertFinalUntouched(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", b.Rounds[0][0].Winner, "nothing saved")
	assert.Equal(t, "Alice", b.Rounds[1][0].SideA, "the running final keeps its competitor")
	assert.Equal(t, state.MatchStatusRunning, b.Rounds[1][0].Status)
	assert.Equal(t, []string{"K"}, b.Rounds[1][0].IpponsB)
}

func serveJSON(r http.Handler, method, url string, payload any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(method, url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRunningDownstream_EveryDoorRefuses(t *testing.T) {
	doors := []struct {
		name    string
		method  string
		path    string
		payload func(force bool) any
	}{
		{"score", "PUT", "/matches/m-r1-0/score", func(force bool) any {
			return map[string]any{
				"sideA": "Alice", "sideB": "Bob", "winner": "Bob", "ipponsB": []string{"M"},
				"status": "completed", "correctionReason": "scoresheet was misread",
				"forceDownstreamReopen": force,
			}
		}},
		{"quick-score", "PUT", "/matches/m-r1-0/quick-score", func(force bool) any {
			return map[string]any{
				"sideA": "Alice", "sideB": "Bob", "teamAWins": 0, "teamBWins": 1,
				"forceDownstreamReopen": force,
			}
		}},
		{"decision", "POST", "/matches/m-r1-0/decision", func(force bool) any {
			// aka (Alice) withdraws, so Bob would go on to the final.
			return map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka", "forceDownstreamReopen": force}
		}},
		{"override-winner", "PUT", "/matches/m-r1-0/override-winner", func(force bool) any {
			return map[string]any{"winnerName": "Bob", "forceDownstreamReopen": force}
		}},
	}
	for _, d := range doors {
		for _, force := range []bool{false, true} {
			t.Run(d.name, func(t *testing.T) {
				r, store, _, _, tempDir := setupTestRouter(t)
				defer os.RemoveAll(tempDir)
				compID := "rfsw-http"
				seedRunningFinal(t, store, compID)

				w := serveJSON(r, d.method, "/api/competitions/"+compID+d.path, d.payload(force))
				assertRunningRefusal(t, w)
				assertFinalUntouched(t, store, compID)
			})
		}
	}
}

func TestRunningDownstream_BulkScoreReason(t *testing.T) {
	for _, force := range []bool{false, true} {
		r, store, _, _, tempDir := setupTestRouter(t)
		compID := "rfsw-bulk"
		seedRunningFinal(t, store, compID)

		w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/bulk-score", []map[string]any{{
			"id": "m-r1-0", "sideA": "Alice", "sideB": "Bob",
			"winner": "Bob", "ipponsB": []string{"M"},
			"status": "completed", "correctionReason": "scoresheet was misread",
			"forceDownstreamReopen": force,
		}})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp struct {
			Succeeded int `json:"succeeded"`
			Errors    []struct {
				MatchID string `json:"matchId"`
				Error   string `json:"error"`
				Reason  string `json:"reason"`
			} `json:"errors"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 0, resp.Succeeded)
		require.Len(t, resp.Errors, 1)
		assert.Equal(t, "downstream_knockout_running", resp.Errors[0].Reason, "force=%v", force)
		assert.Equal(t, runningFinalSentence, resp.Errors[0].Error)
		assertFinalUntouched(t, store, compID)
		_ = os.RemoveAll(tempDir)
	}
}

// TestRunningDownstream_DecisionRefusesBeforeTheLock is the J4 repro on the
// wire: a wrong-side kiken on a semifinal, the final started, the kiken
// re-recorded on the other side. The answer is the running refusal, never
// decision_locked, so the editor shows the sentence and no "Proceed
// anyway?" confirm.
func TestRunningDownstream_DecisionRefusesBeforeTheLock(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	compID := "rfsw-decision-lock"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "rfsw", Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Status: state.MatchStatusRunning, MatchNumber: 1, DisplayRound: 2, Court: "B"},
				{ID: "m-r1-1", SideA: "Carol", SideB: "Dave", SideAID: "carol", SideBID: "dave",
					Winner: "Carol", WinnerID: "carol", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}, MatchNumber: 2, DisplayRound: 2, Court: "B"},
			},
			{
				{ID: "m-r2-0", SideA: "Winner of r0-m0", SideB: "Carol", SideBID: "carol",
					Status: state.MatchStatusScheduled, MatchNumber: 3, DisplayRound: 1, Court: "A"},
			},
		},
	}))
	url := "/api/competitions/" + compID + "/matches/m-r1-0/decision"
	w := serveJSON(r, "POST", url, map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Equal(t, "Bob", b.Rounds[1][0].SideA)
	b.Rounds[1][0].Status = state.MatchStatusRunning
	require.NoError(t, store.SaveBracket(compID, b))

	w = serveJSON(r, "POST", url, map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "downstream_knockout_running", resp["error"], "not decision_locked")
	assert.Equal(t, "Match 3 (Final) is being fought now on Shiaijo A. Finish it or send it back to the queue, then save this correction again.", resp["message"])

	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", b.Rounds[0][0].Winner, "nothing saved")
	assert.Equal(t, "Bob", b.Rounds[1][0].SideA)
}
