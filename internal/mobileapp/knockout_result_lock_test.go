package mobileapp

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Operator decision K1 (bc-evts stage 2b, 2026-10-16): once the next knockout
// match has been played, the stored result is locked. An older finish from a
// device that did not see it is HELD (200 superseded with the downstream
// reason, kept in the history), never the 409 a correction gets. An explicit
// correction keeps the 409 and its force option.
func TestScoreHandler_PlayedNextRoundLocksTheResult(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const compID = "k1-lock"
	now := time.Now().UnixMilli()
	t0, t1, t2, t3 := now-60_000, now-40_000, now-30_000, now-5_000
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "k1", Status: state.CompStatusKnockout}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{ID: "m-r1-0", SideA: "Alice", SideAID: "alice", SideB: "Bob", SideBID: "bob", Status: state.MatchStatusRunning, ModifiedAt: t0, MatchNumber: 1},
			{ID: "m-r1-1", SideA: "Charlie", SideAID: "charlie", Status: state.MatchStatusCompleted, Winner: "Charlie", WinnerID: "charlie", MatchNumber: 2},
		},
		{{ID: "m-r2-0", SideA: "Winner of r2-m0", SideB: "Charlie", SideBID: "charlie", MatchNumber: 3}},
	}}))

	score := func(winner string, a, b []string, extra map[string]any) map[string]any {
		p := map[string]any{
			"sideA": "Alice", "sideB": "Bob", "winner": winner, "ipponsA": a, "ipponsB": b,
			"status": "completed", "changed": []string{"points", "result"},
		}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	body := func(w interface{ Bytes() []byte }) map[string]any {
		var m map[string]any
		require.NoError(t, json.Unmarshal(w.Bytes(), &m))
		return m
	}

	// The first recorded result: Bob, at t2.
	w := putScore(t, r, compID, "m-r1-0", score("Bob", []string{}, []string{"M"}, map[string]any{"modifiedAt": t2}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// The next match is played.
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Equal(t, "Bob", b.Rounds[1][0].SideA)
	b.Rounds[1][0].Status, b.Rounds[1][0].Winner, b.Rounds[1][0].WinnerID = state.MatchStatusCompleted, "Charlie", "charlie"
	b.Rounds[1][0].IpponsB = []string{"M"}
	require.NoError(t, store.SaveBracket(compID, b))

	// An older finish that never saw it: held, not a 409.
	w = putScore(t, r, compID, "m-r1-0", score("Alice", []string{"M"}, []string{}, map[string]any{"modifiedAt": t1}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	resp := body(w.Body)
	assert.Equal(t, false, resp["applied"])
	assert.Equal(t, "superseded", resp["reason"])
	assert.Equal(t, "downstream_knockout_played", resp["heldReason"])
	assert.Contains(t, resp["message"], "next round was already fought")
	assert.Contains(t, resp["message"], "tap Correct.")
	assert.Contains(t, resp["heldGroups"], "result")

	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", b.Rounds[0][0].Winner, "the stored result stands")
	hist, err := store.LoadMatchHistory(compID, "m-r1-0")
	require.NoError(t, err)
	var kept bool
	for _, e := range hist {
		kept = kept || (e.Stamp == t1 && e.Outcomes[state.GroupResult] == state.HistoryOutcomeHeld)
	}
	assert.True(t, kept, "the held finish is in the match history")

	// An explicit correction (it names a reason) keeps today's 409 and force.
	w = putScore(t, r, compID, "m-r1-0", score("Alice", []string{"M"}, []string{}, map[string]any{"modifiedAt": t3, "correctionReason": "scoresheet was misread"}))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "downstream_knockout_played", body(w.Body)["error"])
	w = putScore(t, r, compID, "m-r1-0", score("Alice", []string{"M"}, []string{}, map[string]any{"modifiedAt": t3, "correctionReason": "scoresheet was misread", "forceDownstreamReopen": true}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// bc-evts stage 4 review 2 (G2): every sentence that sends the operator to the
// correction names the button as it reads, "Correct" (admin_shiaijo.jsx,
// admin_schedule_score_editor.jsx), never "Correct result", which no button
// says. The JS words (write_result.jsx) are pinned beside them.
func TestRefusalSentencesNameTheCorrectButtonAsItReads(t *testing.T) {
	for name, msg := range map[string]string{
		"MatchClosedMessage":         MatchClosedMessage,
		"ResultLockedPlayedMessage":  ResultLockedPlayedMessage,
		"ResultLockedRunningMessage": ResultLockedRunningMessage,
	} {
		assert.True(t, strings.HasSuffix(msg, "To change the result, tap Correct."), "%s: %q", name, msg)
		assert.NotContains(t, msg, "Correct result", name)
	}
}
